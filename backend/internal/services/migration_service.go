package services

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ctoz/backend/internal/models"

	"gopkg.in/yaml.v2"
)

// MigrationService 迁移服务
type MigrationService struct {
	connService *ConnectionService
	taskService *TaskService
	client      *http.Client
}

// NewMigrationService 创建新的迁移服务
func NewMigrationService(connService *ConnectionService, taskService *TaskService) *MigrationService {
	return &MigrationService{
		connService: connService,
		taskService: taskService,
		client: &http.Client{
			Timeout: 300 * time.Second, // 5分钟超时
		},
	}
}

// StartOnlineMigration 开始在线迁移
func (s *MigrationService) StartOnlineMigration(req *models.OnlineMigrationRequest) (*models.MigrationTask, error) {
	// 验证连接配置
	if err := s.connService.ValidateConnectionConfig(&req.Source); err != nil {
		return nil, fmt.Errorf("Invalid source connection configuration: %v", err)
	}
	if err := s.connService.ValidateConnectionConfig(&req.Target); err != nil {
		return nil, fmt.Errorf("Invalid target connection configuration: %v", err)
	}

	// 创建迁移任务
	task := s.taskService.CreateTask(
		models.TaskTypeOnline,
		&req.Source,
		&req.Target,
		req.MigrationOptions,
	)

	// 异步执行迁移
	go s.executeOnlineMigration(task)

	return task, nil
}

// executeOnlineMigration 执行在线迁移
func (s *MigrationService) executeOnlineMigration(task *models.MigrationTask) {
	// 更新任务状态为运行中
	s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusRunning))

	// 初始化应用状态列表
	var appStatuses []models.AppImportStatus
	var hasCriticalError bool = false

	defer func() {
		if r := recover(); r != nil {
			s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusFailed))
			s.taskService.AddTaskLog(task.ID, models.LogLevelError, fmt.Sprintf("Migration panic: %v", r))
		} else if hasCriticalError {
			// 只有在发生关键错误时才标记任务失败
			s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusFailed))
			s.taskService.AddTaskLog(task.ID, models.LogLevelError, "Critical error occurred during online migration; task failed")
		} else {
			// 任务成功完成
			s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusCompleted))
			s.taskService.AddTaskLog(task.ID, models.LogLevelInfo, "Online migration completed")
		}

		// 保存应用导入状态到任务结果
		s.saveAppImportStatuses(task.ID, appStatuses)
	}()

	// 步骤1: 测试源系统连接（关键步骤，失败则终止）
	err := s.taskService.ExecuteStep(task.ID, "Test source system connection", func() error {
		testResp, err := s.connService.TestConnection(task.Source)
		if err != nil {
			return fmt.Errorf("Failed to test source connection: %v", err)
		}
		if !testResp.Success {
			return fmt.Errorf("Source connection failed: %s", testResp.Message)
		}
		return nil
	})
	if err != nil {
		// ExecuteStep已经发送了错误日志到WebSocket
		hasCriticalError = true
		return
	}

	// 步骤2: 测试目标系统连接（关键步骤，失败则终止）
	err = s.taskService.ExecuteStep(task.ID, "Test target system connection", func() error {
		testResp, err := s.connService.TestConnection(task.Target)
		if err != nil {
			return fmt.Errorf("Failed to test target connection: %v", err)
		}
		if !testResp.Success {
			return fmt.Errorf("Target connection failed: %s", testResp.Message)
		}
		return nil
	})
	if err != nil {
		// ExecuteStep已经发送了错误日志到WebSocket
		hasCriticalError = true
		return
	}

	// 步骤3: 下载和处理源系统数据（关键步骤，失败则终止）
	var sourceData map[string]interface{}
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Download and process source data", func(progressCallback func(int, string)) error {
		progressCallback(5, "Start download")

		// 下载CasaOS文件
		downloadPath, err := s.downloadCasaOSFiles(task.Source, progressCallback)
		if err != nil {
			return fmt.Errorf("Failed to download files: %v", err)
		}

		progressCallback(40, "Download succeeded")
		progressCallback(45, "Extracting")

		// 解压下载的文件
		extractedPath, err := s.extractDownloadedFiles(downloadPath, progressCallback)
		if err != nil {
			return fmt.Errorf("Failed to extract files: %v", err)
		}

		progressCallback(60, "Extraction succeeded")
		progressCallback(65, "Fetching app list")

		apps, err := s.getSystemApps(task.Source)
		if err != nil {
			return fmt.Errorf("Failed to fetch app list: %v", err)
		}

		progressCallback(75, "Fetching system settings")
		settings, err := s.getSystemSettings(task.Source)
		if err != nil {
			return fmt.Errorf("Failed to fetch system settings: %v", err)
		}

		progressCallback(85, "Fetching user data")
		userData, err := s.getUserData(task.Source)
		if err != nil {
			return fmt.Errorf("Failed to fetch user data: %v", err)
		}

		sourceData = map[string]interface{}{
			"apps":          apps,
			"settings":      settings,
			"userData":      userData,
			"downloadPath":  downloadPath,
			"extractedPath": extractedPath,
		}

		progressCallback(95, "Data acquisition completed")
		return nil
	})
	if err != nil {
		hasCriticalError = true
		return
	}

	// 步骤4: 预扫描应用并初始化状态（非关键步骤，失败时记录日志但继续执行）
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Scan app configuration", func(progressCallback func(int, string)) error {
		// 获取解压路径
		extractedPath, ok := sourceData["extractedPath"].(string)
		if !ok {
			return fmt.Errorf("Extracted path not found")
		}

		progressCallback(20, "Scanning app configuration...")

		// 扫描compose文件
		appsDir := filepath.Join(extractedPath, "var/lib/casaos/apps")
		log.Printf("[DEBUG] Ready to scan apps directory: %s", appsDir)
		composeFiles, err := s.readComposeFiles(appsDir)
		if err != nil {
			errorMsg := fmt.Sprintf("Failed to read compose files: %v", err)
			log.Printf("[ERROR] %s", errorMsg)
			s.taskService.AddTaskLog(task.ID, models.LogLevelError, errorMsg)
			return fmt.Errorf(errorMsg)
		}
		log.Printf("[INFO] Scanned %d compose files successfully", len(composeFiles))

		// 检查AppData目录
		appDataPath := filepath.Join(extractedPath, "DATA/AppData")
		hasGlobalAppData := false
		if _, err := os.Stat(appDataPath); err == nil {
			hasGlobalAppData = true
		}

		progressCallback(60, "Initializing application status...")

		// 初始化每个应用的状态
		for appName := range composeFiles {
			// 检查该应用是否有AppData
			appDataDir := filepath.Join(appDataPath, appName)
			hasAppData := false
			if hasGlobalAppData {
				if _, err := os.Stat(appDataDir); err == nil {
					hasAppData = true
				}
			}

			appStatus := models.AppImportStatus{
				AppName:       appName,
				HasAppData:    hasAppData,
				AppDataStatus: models.AppStatusSkipped,
				ComposeStatus: "pending",
				OverallStatus: "pending",
			}

			appStatuses = append(appStatuses, appStatus)
		}

		// 保存compose文件到sourceData
		sourceData["composeFiles"] = composeFiles
		sourceData["hasGlobalAppData"] = hasGlobalAppData

		progressCallback(100, fmt.Sprintf("Found %d apps", len(composeFiles)))
		return nil
	})
	if err != nil {
		// 非关键步骤失败，记录错误日志但继续执行
		s.taskService.AddTaskLog(task.ID, models.LogLevelWarning, fmt.Sprintf("Failed to scan app configuration: %v, continuing with next steps", err))
		log.Printf("[WARNING] Failed to scan app configuration: %v, continuing with next steps", err)
		// 初始化空的appStatuses，避免后续步骤出错
		appStatuses = []models.AppImportStatus{}
		sourceData["composeFiles"] = make(map[string]string)
		sourceData["hasGlobalAppData"] = false
	}

	// 步骤5: 合并AppData目录（非关键步骤，失败时记录日志但继续执行）
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Merge AppData directory", func(progressCallback func(int, string)) error {
		// 获取解压路径
		extractedPath, ok := sourceData["extractedPath"].(string)
		if !ok {
			return fmt.Errorf("Extracted path not found")
		}

		hasGlobalAppData, _ := sourceData["hasGlobalAppData"].(bool)
		if !hasGlobalAppData {
			log.Printf("[INFO] AppData directory not found, skipping merge")
			progressCallback(100, "AppData directory not found, skipping merge")
			return nil
		}

		progressCallback(10, "Start merging AppData directory...")

		appDataPath := filepath.Join(extractedPath, "DATA/AppData")

		// 逐个处理有AppData的应用
		totalAppsWithData := 0
		for i := range appStatuses {
			if appStatuses[i].HasAppData {
				totalAppsWithData++
			}
		}

		completedApps := 0
		for i := range appStatuses {
			if !appStatuses[i].HasAppData {
				continue
			}

			completedApps++
			progress := 20 + (60 * completedApps / totalAppsWithData)
			progressCallback(progress, fmt.Sprintf("Merging %s AppData (%d/%d)...", appStatuses[i].AppName, completedApps, totalAppsWithData))

			// 合并单个应用的AppData
			appDataDir := filepath.Join(appDataPath, appStatuses[i].AppName)
			err := s.uploadAppDataToZimaOS(task.Target, appStatuses[i].AppName, appDataDir, task.ID)

			if err != nil {
				log.Printf("[ERROR] App %s AppData merge failed: %v", appStatuses[i].AppName, err)
				appStatuses[i].AppDataStatus = models.AppStatusFailed
				appStatuses[i].ErrorMessage = fmt.Sprintf("AppData merge failed: %v", err)
				s.taskService.AddTaskLog(task.ID, models.LogLevelError, fmt.Sprintf("App %s AppData merge failed: %v", appStatuses[i].AppName, err))
			} else {
				log.Printf("[INFO] App %s AppData merge succeeded", appStatuses[i].AppName)
				appStatuses[i].AppDataStatus = models.AppStatusSuccess
				s.taskService.AddTaskLog(task.ID, models.LogLevelInfo, fmt.Sprintf("App %s AppData merge succeeded ✓", appStatuses[i].AppName))
			}

			// 实时保存应用状态到任务结果
			s.saveAppImportStatuses(task.ID, appStatuses)
		}

		progressCallback(100, "AppData directory merge completed")
		return nil
	})
	if err != nil {
		// 非关键步骤失败，记录错误日志但继续执行
		s.taskService.AddTaskLog(task.ID, models.LogLevelWarning, fmt.Sprintf("Failed to merge AppData directory: %v, continuing with next steps", err))
		log.Printf("[WARNING] Failed to merge AppData directory: %v, continuing with next steps", err)
	}

	// 步骤6: 导入compose文件（非关键步骤，失败时记录日志但继续执行）
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Import application configuration", func(progressCallback func(int, string)) error {
		composeFiles, ok := sourceData["composeFiles"].(map[string]string)
		if !ok {
			return fmt.Errorf("Compose file data not found")
		}

		if len(composeFiles) == 0 {
			log.Printf("[WARNING] No compose files found")
			progressCallback(100, "No application configuration files found")
			return nil
		}

		log.Printf("[INFO] Start importing compose configuration for %d apps", len(composeFiles))

		// 逐个导入compose文件
		totalCompose := len(composeFiles)
		completedCompose := 0

		for appName, composeContent := range composeFiles {
			completedCompose++
			progress := 20 + (70 * completedCompose / totalCompose)
			progressCallback(progress, fmt.Sprintf("Import %s compose configuration (%d/%d)...", appName, completedCompose, totalCompose))

			// 导入单个应用的compose
			err := s.importComposeToZimaOS(task.Target, appName, composeContent, task.ID)

			if err != nil {
				log.Printf("[ERROR] App %s compose import failed: %v", appName, err)
				// 更新应用状态
				for j := range appStatuses {
					if appStatuses[j].AppName == appName {
						appStatuses[j].ComposeStatus = models.AppStatusFailed
						if appStatuses[j].ErrorMessage == "" {
							appStatuses[j].ErrorMessage = fmt.Sprintf("Compose import failed: %v", err)
						} else {
							appStatuses[j].ErrorMessage += fmt.Sprintf("; Compose import failed: %v", err)
						}
						break
					}
				}
				s.taskService.AddTaskLog(task.ID, models.LogLevelError, fmt.Sprintf("App %s compose import failed: %v", appName, err))
			} else {
				log.Printf("[INFO] App %s compose import succeeded", appName)
				// 更新应用状态
				for j := range appStatuses {
					if appStatuses[j].AppName == appName {
						appStatuses[j].ComposeStatus = models.AppStatusSuccess
						break
					}
				}
				s.taskService.AddTaskLog(task.ID, models.LogLevelInfo, fmt.Sprintf("App %s compose import succeeded ✓", appName))
			}

			// 计算整体状态
			for j := range appStatuses {
				if appStatuses[j].AppName == appName {
					appStatuses[j].OverallStatus = s.calculateOverallStatus(appStatuses[j])
					break
				}
			}

			// 实时保存应用状态到任务结果
			s.saveAppImportStatuses(task.ID, appStatuses)
		}

		progressCallback(100, "All application compose imports completed")
		log.Printf("[INFO] All application compose imports completed")
		return nil
	})
	if err != nil {
		// 非关键步骤失败，记录错误日志但继续执行
		s.taskService.AddTaskLog(task.ID, models.LogLevelWarning, fmt.Sprintf("Failed to import application configuration: %v, continuing with next steps", err))
		log.Printf("[WARNING] Failed to import application configuration: %v, continuing with next steps", err)
	}

	// 步骤6: 清理本地临时文件
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Cleanup local temporary files", func(progressCallback func(int, string)) error {
		progressCallback(50, "Cleaning up local temporary files...")

		// 清理本地下载和解压的文件
		if downloadPath, ok := sourceData["downloadPath"].(string); ok {
			if err := os.Remove(downloadPath); err != nil {
				log.Printf("[WARNING] Failed to remove downloaded file: %v", err)
			} else {
				log.Printf("[DEBUG] Downloaded file removed: %s", downloadPath)
			}
		}

		if extractedPath, ok := sourceData["extractedPath"].(string); ok {
			if err := os.RemoveAll(extractedPath); err != nil {
				log.Printf("[WARNING] Failed to remove extracted directory: %v", err)
			} else {
				log.Printf("[DEBUG] Extracted directory removed: %s", extractedPath)
			}
		}

		progressCallback(100, "Cleanup completed")
		return nil
	})
	if err != nil {
		// 清理失败不影响迁移成功，只记录日志
		log.Printf("[WARNING] Cleanup step failed: %v", err)
		s.taskService.AddTaskLog(task.ID, models.LogLevelWarning, fmt.Sprintf("Cleanup local temporary files failed: %v", err))
	}

	// 计算导入摘要
	summary := s.calculateImportSummary(appStatuses)

	// 设置任务结果
	s.taskService.SetTaskResult(task.ID, map[string]interface{}{
		"apps":            appStatuses,
		"summary":         summary,
		"completion_time": time.Now(),
		"status":          fmt.Sprintf("Import completed: %d succeeded, %d failed, total %d apps", summary.SuccessApps, summary.FailedApps, summary.TotalApps),
	})

	// 更新任务进度为100%
	s.taskService.UpdateTaskProgress(task.ID, 100)

	// 注意：任务状态更新已经在defer函数中统一管理，这里不需要重复设置
	// 如果执行到这里，说明没有发生关键错误，任务将成功完成
}

// StartDataExport 开始数据导出
func (s *MigrationService) StartDataExport(req *models.DataExportRequest) (*models.MigrationTask, error) {
	// 验证连接配置
	if err := s.connService.ValidateConnectionConfig(&req.Source); err != nil {
		return nil, fmt.Errorf("Invalid target connection configuration: %v", err)
	}

	// 创建导出任务
	task := s.taskService.CreateTask(
		models.TaskTypeExport,
		&req.Source,
		nil,
		req.ExportOptions,
	)

	// 异步执行导出
	go s.executeDataExport(task)

	return task, nil
}

// executeDataExport 执行数据导出
func (s *MigrationService) executeDataExport(task *models.MigrationTask) {
	// 更新任务状态为运行中
	s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusRunning))
	var hasCriticalError bool = false

	defer func() {
		if r := recover(); r != nil {
			s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusFailed))
			s.taskService.AddTaskLog(task.ID, models.LogLevelError, fmt.Sprintf("Panic occurred during export: %v", r))
		} else if hasCriticalError {
			// 只有在发生关键错误时才标记任务失败
			s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusFailed))
			s.taskService.AddTaskLog(task.ID, models.LogLevelError, "Critical error occurred during data export; task failed")
		} else {
			// 任务成功完成
			s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusCompleted))
			s.taskService.AddTaskLog(task.ID, models.LogLevelInfo, "Data export completed")
		}
	}()

	// 步骤1: 测试源系统连接（关键步骤，失败则终止）
	err := s.taskService.ExecuteStep(task.ID, "Test source system connection", func() error {
		testResp, err := s.connService.TestConnection(task.Source)
		if err != nil {
			return fmt.Errorf("Failed to test source connection: %v", err)
		}
		if !testResp.Success {
			return fmt.Errorf("Source connection failed: %s", testResp.Message)
		}
		return nil
	})
	if err != nil {
		// ExecuteStep已经发送了错误日志到WebSocket
		hasCriticalError = true
		return
	}

	// 步骤2: 导出数据（关键步骤，失败则终止）
	var exportData map[string]interface{}
	var exportPath string
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Export system data", func(progressCallback func(int, string)) error {
		options := task.Options
		exportData = make(map[string]interface{})

		if exportApps, ok := options["export_apps"].(bool); ok && exportApps {
			progressCallback(20, "Export application data")
			apps, err := s.getSystemApps(task.Source)
			if err != nil {
				return fmt.Errorf("Failed to export application data: %v", err)
			}
			exportData["apps"] = apps
		}

		if exportSettings, ok := options["export_settings"].(bool); ok && exportSettings {
			progressCallback(50, "Export system settings")
			settings, err := s.getSystemSettings(task.Source)
			if err != nil {
				return fmt.Errorf("Failed to export system settings: %v", err)
			}
			exportData["settings"] = settings
		}

		if exportUserData, ok := options["export_data"].(bool); ok && exportUserData {
			progressCallback(70, "Export user data")
			userData, err := s.getUserData(task.Source)
			if err != nil {
				return fmt.Errorf("Failed to export user data: %v", err)
			}
			exportData["userData"] = userData
		}

		progressCallback(90, "Generate export file")
		filePath, err := s.createExportFile(task.ID, exportData)
		if err != nil {
			return fmt.Errorf("Failed to generate export file: %v", err)
		}
		exportPath = filePath

		progressCallback(100, "Data export completed")
		return nil
	})
	if err != nil {
		hasCriticalError = true
		return
	}

	// 设置任务结果
	s.taskService.SetTaskResult(task.ID, map[string]interface{}{
		"export_file":     exportPath,
		"export_size":     s.getFileSize(exportPath),
		"completion_time": time.Now(),
		"download_instructions": &models.DownloadInstructions{
			Message:     "Data export completed. Please download the export file from CasaOS manually.",
			FilePath:    exportPath,
			DownloadURL: fmt.Sprintf("/downloads/%s", filepath.Base(exportPath)),
			Instructions: []string{
				"1. Sign in to CasaOS",
				"2. Open the File Manager",
				"3. Locate the export file: " + exportPath,
				"4. Download the file to your local machine",
				"5. Use this file to import on the target system",
			},
		},
	})

	// 更新任务进度为100%
	s.taskService.UpdateTaskProgress(task.ID, 100)

	// 注意：任务状态更新已经在defer函数中统一管理，这里不需要重复设置
	// 如果执行到这里，说明没有发生关键错误，任务将成功完成
}

// StartDataImport 开始数据导入
func (s *MigrationService) StartDataImport(req *models.DataImportRequest) (*models.MigrationTask, error) {
	// 验证连接配置
	if err := s.connService.ValidateConnectionConfig(&req.Target); err != nil {
		return nil, fmt.Errorf("Invalid target connection configuration: %v", err)
	}

	// 创建导入任务
	task := s.taskService.CreateTask(
		models.TaskTypeImport,
		nil,
		&req.Target,
		req.ImportOptions,
	)

	// 异步执行导入
	go s.executeDataImport(task)

	return task, nil
}

// executeDataImport 执行数据导入
func (s *MigrationService) executeDataImport(task *models.MigrationTask) {
	// 更新任务状态为运行中
	s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusRunning))

	// 初始化应用导入状态跟踪
	var appStatuses []models.AppImportStatus
	var hasCriticalError bool = false

	defer func() {
		if r := recover(); r != nil {
			s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusFailed))
			s.taskService.AddTaskLog(task.ID, models.LogLevelError, fmt.Sprintf("Panic occurred during import: %v", r))
		} else if hasCriticalError {
			// 只有在发生关键错误时才标记任务失败
			s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusFailed))
			s.taskService.AddTaskLog(task.ID, models.LogLevelError, "Critical error occurred during offline import; task failed")
		} else {
			// 任务成功完成
			s.taskService.UpdateTaskStatus(task.ID, string(models.TaskStatusCompleted))
			s.taskService.AddTaskLog(task.ID, models.LogLevelInfo, "Offline import completed")
		}

		// 保存应用导入状态到任务结果
		s.saveAppImportStatuses(task.ID, appStatuses)
	}()

	// 步骤1: 测试目标系统连接（关键步骤，失败则终止）
	err := s.taskService.ExecuteStep(task.ID, "Test target system connection", func() error {
		testResp, err := s.connService.TestConnection(task.Target)
		if err != nil {
			return fmt.Errorf("Failed to test target connection: %v", err)
		}
		if !testResp.Success {
			return fmt.Errorf("Target connection failed: %s", testResp.Message)
		}
		return nil
	})
	if err != nil {
		// ExecuteStep已经发送了错误日志到WebSocket
		hasCriticalError = true
		return
	}

	// 步骤2: 解析导入文件（关键步骤，失败则终止）
	var sourceData map[string]interface{}
	var extractedPath string
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Parse import file", func(progressCallback func(int, string)) error {
		// 安全获取import_file字段
		importFileValue, exists := task.Options["import_file"]
		if !exists {
			return fmt.Errorf("Missing required import_file parameter")
		}

		importFile, ok := importFileValue.(string)
		if !ok || importFile == "" {
			return fmt.Errorf("import_file must be a non-empty string, got type: %T, value: %v", importFileValue, importFileValue)
		}

		progressCallback(10, "Start parsing import file...")
		log.Printf("[INFO] Start parsing import file: %s", importFile)

		// 解压导入文件
		progressCallback(30, "Extract import file...")
		extractDir := filepath.Join("uploads", "extracted_import")

		// 清理之前的解压目录（如果存在）
		if err := os.RemoveAll(extractDir); err != nil {
			log.Printf("[WARNING] Failed to remove previous extraction directory: %v", err)
		}

		// 重新创建解压目录，确保权限正确
		if err := os.MkdirAll(extractDir, 0755); err != nil {
			return fmt.Errorf("Failed to create extraction directory: %v", err)
		}

		// 确保目录权限正确
		if err := os.Chmod(extractDir, 0755); err != nil {
			log.Printf("[WARNING] Failed to set directory permissions: %v", err)
		}

		log.Printf("[DEBUG] Extraction directory created: %s", extractDir)

		// 根据文件实际格式选择解压函数（而不是扩展名）
		actualFormat, err := s.detectFileFormat(importFile)
		if err != nil {
			return fmt.Errorf("Failed to detect file format: %v", err)
		}

		log.Printf("[INFO] Detected file format: %s", actualFormat)

		switch actualFormat {
		case "gzip":
			// 使用tar.gz解压函数
			if err := s.extractTarGz(importFile, extractDir); err != nil {
				return fmt.Errorf("Failed to extract tar.gz file: %v", err)
			}
		case "zip":
			// 使用ZIP解压函数
			if err := s.extractZipFile(importFile, extractDir); err != nil {
				return fmt.Errorf("Failed to extract ZIP file: %v", err)
			}
		default:
			return fmt.Errorf("Unsupported file format: %s, only ZIP and GZIP are supported", actualFormat)
		}
		extractedPath = extractDir

		progressCallback(60, "Parsing CasaOS structure...")
		// 解析CasaOS导出结构，而不是查找migration_data.json
		sourceData = map[string]interface{}{
			"extractedPath": extractedPath,
		}

		progressCallback(100, "Import file parsing completed")
		return nil
	})
	if err != nil {
		hasCriticalError = true
		return
	}

	// 步骤3: 扫描应用配置并初始化状态（非关键步骤，失败时记录日志但继续执行）
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Scan app configuration", func(progressCallback func(int, string)) error {
		// 获取解压路径
		extractedPath, ok := sourceData["extractedPath"].(string)
		if !ok {
			return fmt.Errorf("Extracted path not found")
		}

		progressCallback(20, "Scanning app configuration...")

		// 扫描compose文件
		appsDir := filepath.Join(extractedPath, "var/lib/casaos/apps")
		log.Printf("[DEBUG] Ready to scan apps directory: %s", appsDir)
		composeFiles, err := s.readComposeFiles(appsDir)
		if err != nil {
			errorMsg := fmt.Sprintf("Failed to read compose files: %v", err)
			log.Printf("[ERROR] %s", errorMsg)
			s.taskService.AddTaskLog(task.ID, models.LogLevelError, errorMsg)
			return fmt.Errorf(errorMsg)
		}
		log.Printf("[INFO] Scanned %d compose files successfully", len(composeFiles))

		// 检查AppData目录
		appDataPath := filepath.Join(extractedPath, "DATA/AppData")
		hasGlobalAppData := false
		if _, err := os.Stat(appDataPath); err == nil {
			hasGlobalAppData = true
		}

		progressCallback(60, "Initializing application status...")

		// 初始化每个应用的状态
		for appName := range composeFiles {
			// 检查该应用是否有AppData
			appDataDir := filepath.Join(appDataPath, appName)
			hasAppData := false
			if hasGlobalAppData {
				if _, err := os.Stat(appDataDir); err == nil {
					hasAppData = true
				}
			}

			appStatus := models.AppImportStatus{
				AppName:       appName,
				HasAppData:    hasAppData,
				AppDataStatus: models.AppStatusSkipped,
				ComposeStatus: "pending",
				OverallStatus: "pending",
			}

			appStatuses = append(appStatuses, appStatus)
		}

		// 保存compose文件到sourceData
		sourceData["composeFiles"] = composeFiles
		sourceData["hasGlobalAppData"] = hasGlobalAppData

		progressCallback(100, fmt.Sprintf("Found %d apps", len(composeFiles)))
		return nil
	})
	if err != nil {
		// 非关键步骤失败，记录错误日志但继续执行
		s.taskService.AddTaskLog(task.ID, models.LogLevelWarning, fmt.Sprintf("Failed to scan app configuration: %v, continuing with next steps", err))
		log.Printf("[WARNING] Failed to scan app configuration: %v, continuing with next steps", err)
		// 初始化空的appStatuses，避免后续步骤出错
		appStatuses = []models.AppImportStatus{}
		sourceData["composeFiles"] = make(map[string]string)
		sourceData["hasGlobalAppData"] = false
	}

	// 步骤4: 合并AppData目录（非关键步骤，失败时记录日志但继续执行）
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Merge AppData directory", func(progressCallback func(int, string)) error {
		// 获取解压路径
		extractedPath, ok := sourceData["extractedPath"].(string)
		if !ok {
			return fmt.Errorf("Extracted path not found")
		}

		hasGlobalAppData, _ := sourceData["hasGlobalAppData"].(bool)
		if !hasGlobalAppData {
			log.Printf("[INFO] AppData directory not found, skipping merge")
			progressCallback(100, "AppData directory not found, skipping merge")
			return nil
		}

		progressCallback(10, "Start merging AppData directory...")

		appDataPath := filepath.Join(extractedPath, "DATA/AppData")

		// 逐个处理有AppData的应用
		totalAppsWithData := 0
		for i := range appStatuses {
			if appStatuses[i].HasAppData {
				totalAppsWithData++
			}
		}

		completedApps := 0
		for i := range appStatuses {
			if !appStatuses[i].HasAppData {
				continue
			}

			completedApps++
			progress := 20 + (60 * completedApps / totalAppsWithData)
			progressCallback(progress, fmt.Sprintf("Merging %s AppData (%d/%d)...", appStatuses[i].AppName, completedApps, totalAppsWithData))

			// 合并单个应用的AppData
			appDataDir := filepath.Join(appDataPath, appStatuses[i].AppName)
			err := s.uploadAppDataToZimaOS(task.Target, appStatuses[i].AppName, appDataDir, task.ID)

			if err != nil {
				log.Printf("[ERROR] App %s AppData merge failed: %v", appStatuses[i].AppName, err)
				appStatuses[i].AppDataStatus = models.AppStatusFailed
				appStatuses[i].ErrorMessage = fmt.Sprintf("AppData merge failed: %v", err)
				s.taskService.AddTaskLog(task.ID, models.LogLevelError, fmt.Sprintf("App %s AppData merge failed: %v", appStatuses[i].AppName, err))
			} else {
				log.Printf("[INFO] App %s AppData merge succeeded", appStatuses[i].AppName)
				appStatuses[i].AppDataStatus = models.AppStatusSuccess
				s.taskService.AddTaskLog(task.ID, models.LogLevelInfo, fmt.Sprintf("App %s AppData merge succeeded ✓", appStatuses[i].AppName))
			}

			// 实时保存应用状态到任务结果
			s.saveAppImportStatuses(task.ID, appStatuses)
		}

		progressCallback(100, "AppData directory merge completed")
		return nil
	})
	if err != nil {
		// 非关键步骤失败，记录错误日志但继续执行
		s.taskService.AddTaskLog(task.ID, models.LogLevelWarning, fmt.Sprintf("Failed to merge AppData directory: %v, continuing with next steps", err))
		log.Printf("[WARNING] Failed to merge AppData directory: %v, continuing with next steps", err)
	}

	// 步骤5: 导入应用配置(Compose)（非关键步骤，失败时记录日志但继续执行）
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Import application configuration", func(progressCallback func(int, string)) error {
		composeFiles, ok := sourceData["composeFiles"].(map[string]string)
		if !ok {
			return fmt.Errorf("Compose file data not found")
		}

		if len(composeFiles) == 0 {
			log.Printf("[WARNING] No compose files found")
			progressCallback(100, "No application configuration files found")
			return nil
		}

		log.Printf("[INFO] Start importing compose configuration for %d apps", len(composeFiles))

		// 逐个导入compose文件
		totalCompose := len(composeFiles)
		completedCompose := 0

		for appName, composeContent := range composeFiles {
			completedCompose++
			progress := 20 + (70 * completedCompose / totalCompose)
			progressCallback(progress, fmt.Sprintf("Import %s compose configuration (%d/%d)...", appName, completedCompose, totalCompose))

			// 导入单个应用的compose
			err := s.importComposeToZimaOS(task.Target, appName, composeContent, task.ID)

			// 找到对应的appStatus并更新
			for i := range appStatuses {
				if appStatuses[i].AppName == appName {
					if err != nil {
						log.Printf("[ERROR] App %s compose import failed: %v", appName, err)
						appStatuses[i].ComposeStatus = models.AppStatusFailed
						if appStatuses[i].ErrorMessage != "" {
							appStatuses[i].ErrorMessage += "; "
						}
						appStatuses[i].ErrorMessage += fmt.Sprintf("Compose import failed: %v", err)
						s.taskService.AddTaskLog(task.ID, models.LogLevelError, fmt.Sprintf("App %s compose import failed: %v", appName, err))
					} else {
						log.Printf("[INFO] App %s compose import succeeded", appName)
						appStatuses[i].ComposeStatus = models.AppStatusSuccess
						s.taskService.AddTaskLog(task.ID, models.LogLevelInfo, fmt.Sprintf("App %s compose import succeeded ✓", appName))
					}

					// 计算整体状态
					appStatuses[i].OverallStatus = s.calculateOverallStatus(appStatuses[i])
					break
				}
			}

			// 实时保存应用状态到任务结果
			s.saveAppImportStatuses(task.ID, appStatuses)
		}

		progressCallback(100, "All application compose imports completed")
		log.Printf("[INFO] All application compose imports completed")
		return nil
	})
	if err != nil {
		// 非关键步骤失败，记录错误日志但继续执行
		s.taskService.AddTaskLog(task.ID, models.LogLevelWarning, fmt.Sprintf("Failed to import application configuration: %v, continuing with next steps", err))
		log.Printf("[WARNING] Failed to import application configuration: %v, continuing with next steps", err)
	}

	// 步骤6: 清理本地临时文件
	err = s.taskService.ExecuteStepWithProgress(task.ID, "Cleanup local temporary files", func(progressCallback func(int, string)) error {
		progressCallback(50, "Cleaning up local temporary files...")

		// 清理本地下载和解压的文件
		if downloadPath, ok := sourceData["downloadPath"].(string); ok {
			if err := os.Remove(downloadPath); err != nil {
				log.Printf("[WARNING] Failed to remove downloaded file: %v", err)
			} else {
				log.Printf("[DEBUG] Downloaded file removed: %s", downloadPath)
			}
		}

		if extractedPath, ok := sourceData["extractedPath"].(string); ok {
			if err := os.RemoveAll(extractedPath); err != nil {
				log.Printf("[WARNING] Failed to remove extracted directory: %v", err)
			} else {
				log.Printf("[DEBUG] Extracted directory removed: %s", extractedPath)
			}
		}

		progressCallback(100, "Cleanup completed")
		return nil
	})
	if err != nil {
		// 清理失败不影响迁移成功，只记录日志
		log.Printf("[WARNING] Cleanup step failed: %v", err)
		s.taskService.AddTaskLog(task.ID, models.LogLevelWarning, fmt.Sprintf("Cleanup local temporary files failed: %v", err))
	}

	// 计算导入摘要
	summary := s.calculateImportSummary(appStatuses)

	// 设置任务结果
	s.taskService.SetTaskResult(task.ID, map[string]interface{}{
		"apps":            appStatuses,
		"summary":         summary,
		"completion_time": time.Now(),
		"status":          fmt.Sprintf("Import completed: %d succeeded, %d failed, total %d apps", summary.SuccessApps, summary.FailedApps, summary.TotalApps),
	})

	// 更新任务进度为100%
	s.taskService.UpdateTaskProgress(task.ID, 100)

	// 注意：任务状态更新已经在defer函数中统一管理，这里不需要重复设置
	// 如果执行到这里，说明没有发生关键错误，任务将成功完成
}

// 辅助方法

// getSystemApps 获取系统应用列表
func (s *MigrationService) getSystemApps(conn *models.SystemConnection) ([]interface{}, error) {
	// 模拟获取应用列表
	return []interface{}{
		map[string]interface{}{
			"name":    "Nextcloud",
			"version": "25.0.0",
			"port":    "8080",
		},
		map[string]interface{}{
			"name":    "Jellyfin",
			"version": "10.8.0",
			"port":    "8096",
		},
	}, nil
}

// getSystemSettings 获取系统设置
func (s *MigrationService) getSystemSettings(conn *models.SystemConnection) (map[string]interface{}, error) {
	// 模拟获取系统设置
	return map[string]interface{}{
		"timezone": "Asia/Shanghai",
		"language": "zh-CN",
		"theme":    "dark",
	}, nil
}

// getUserData 获取用户数据
func (s *MigrationService) getUserData(conn *models.SystemConnection) (map[string]interface{}, error) {
	// 模拟获取用户数据
	return map[string]interface{}{
		"documents": "/home/user/Documents",
		"downloads": "/home/user/Downloads",
	}, nil
}

// migrateApps 迁移应用
func (s *MigrationService) migrateApps(target *models.SystemConnection, apps interface{}) error {
	// 模拟应用迁移
	time.Sleep(2 * time.Second)
	return nil
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractZipFile 解压ZIP文件到指定目录
func (s *MigrationService) extractZipFile(src, dest string) error {
	// 打开ZIP文件
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("Failed to open ZIP file: %v", err)
	}
	defer r.Close()

	// 创建目标目录
	err = os.MkdirAll(dest, 0755)
	if err != nil {
		return fmt.Errorf("Failed to create destination directory: %v", err)
	}

	// 确保目标目录权限正确
	if err := os.Chmod(dest, 0755); err != nil {
		log.Printf("[WARNING] Failed to set destination directory permissions: %v", err)
	}

	log.Printf("[DEBUG] Starting to extract ZIP file: %s -> %s", src, dest)

	// 解压文件
	for _, f := range r.File {
		// 构建目标文件路径
		path := filepath.Join(dest, f.Name)

		// 检查路径安全性，防止目录遍历攻击
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("Insecure file path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			// 创建目录
			err = os.MkdirAll(path, 0755) // 使用统一的权限
			if err != nil {
				log.Printf("[ERROR] Failed to create directory: %s, error: %v", path, err)
				return fmt.Errorf("Failed to create directory: %s - %v", path, err)
			}
			// 设置目录权限
			if err := os.Chmod(path, 0755); err != nil {
				log.Printf("[WARNING] Failed to set directory permissions: %s - %v", path, err)
			}
			log.Printf("[DEBUG] Created directory: %s", path)
			continue
		}

		// 创建文件的父目录
		parentDir := filepath.Dir(path)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			log.Printf("[ERROR] Failed to create parent directory: %s, error: %v", parentDir, err)
			return fmt.Errorf("Failed to create parent directory: %s - %v", parentDir, err)
		}
		// 设置父目录权限
		if err := os.Chmod(parentDir, 0755); err != nil {
			log.Printf("[WARNING] Failed to set parent directory permissions: %s - %v", parentDir, err)
		}

		// 打开ZIP中的文件
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("Failed to open file inside ZIP: %v", err)
		}

		// 创建目标文件
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644) // 使用统一的文件权限
		if err != nil {
			rc.Close()
			log.Printf("[ERROR] Failed to create target file: %s, error: %v", path, err)
			return fmt.Errorf("Failed to create target file: %s - %v", path, err)
		}

		// 复制文件内容
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return fmt.Errorf("Failed to copy file content: %v", err)
		}

		log.Printf("[DEBUG] Extracted file: %s", path)
	}

	log.Printf("[DEBUG] ZIP file extraction completed: %s", src)
	return nil
}

// calculateOverallStatus 计算应用的整体状态
func (s *MigrationService) calculateOverallStatus(appStatus models.AppImportStatus) string {
	// 如果有AppData，则需要AppData和Compose都成功
	if appStatus.HasAppData {
		if appStatus.AppDataStatus == models.AppStatusSuccess && appStatus.ComposeStatus == models.AppStatusSuccess {
			return models.AppStatusSuccess
		}
		return models.AppStatusFailed
	}

	// 如果没有AppData，则只需要Compose成功
	return appStatus.ComposeStatus
}

// calculateImportSummary 计算导入摘要
func (s *MigrationService) calculateImportSummary(appStatuses []models.AppImportStatus) models.ImportSummary {
	summary := models.ImportSummary{
		TotalApps: len(appStatuses),
	}

	for _, app := range appStatuses {
		if app.OverallStatus == models.AppStatusSuccess {
			summary.SuccessApps++
		} else {
			summary.FailedApps++
		}
	}

	return summary
}

// saveAppImportStatuses 保存应用导入状态到任务结果
func (s *MigrationService) saveAppImportStatuses(taskID string, appStatuses []models.AppImportStatus) {
	// 计算摘要
	summary := s.calculateImportSummary(appStatuses)

	// 保存到任务结果
	s.taskService.SetTaskResult(taskID, map[string]interface{}{
		"apps":    appStatuses,
		"summary": summary,
	})

	log.Printf("[INFO] Saved app import status: total %d, succeeded %d, failed %d", summary.TotalApps, summary.SuccessApps, summary.FailedApps)
}

// CreateAppPackage 为指定应用创建包含AppData和Compose文件的压缩包
func (s *MigrationService) CreateAppPackage(taskID, appName string) (string, error) {
	// 获取任务信息
	task, err := s.taskService.GetTask(taskID)
	if err != nil {
		return "", fmt.Errorf("Failed to get task: %v", err)
	}

	// 检查任务类型
	if task.Type != string(models.TaskTypeImport) && task.Type != string(models.TaskTypeOnline) && task.Type != string(models.TaskTypeOfflineImport) {
		return "", fmt.Errorf("Incorrect task type")
	}

	// 查找解压后的目录
	var extractedPath string

	// 扫描download目录，查找解压后的文件夹
	downloadDir := "./download"
	entries, err := os.ReadDir(downloadDir)
	if err != nil {
		return "", fmt.Errorf("Failed to read download directory: %v", err)
	}

	// 查找最新的解压目录（不是zip文件）
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasSuffix(entry.Name(), ".zip") {
			testPath := filepath.Join(downloadDir, entry.Name())
			// 检查是否包含DATA和var目录
			dataPath := filepath.Join(testPath, "DATA")
			varPath := filepath.Join(testPath, "var")
			if _, err := os.Stat(dataPath); err == nil {
				if _, err := os.Stat(varPath); err == nil {
					extractedPath = testPath
					break
				}
			}
		}
	}

	if extractedPath == "" {
		return "", fmt.Errorf("Extracted backup directory not found")
	}

	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "app_package_*")
	if err != nil {
		return "", fmt.Errorf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建应用包目录
	appPackageDir := filepath.Join(tempDir, "app_package")
	err = os.MkdirAll(appPackageDir, 0755)
	if err != nil {
		return "", fmt.Errorf("Failed to create application package directory: %v", err)
	}

	// 查找匹配的应用文件夹（大小写不敏感）
	var matchedAppName string

	// 在apps目录中查找匹配的文件夹
	appsDir := filepath.Join(extractedPath, "var/lib/casaos/apps")
	if entries, err := os.ReadDir(appsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && strings.EqualFold(entry.Name(), appName) {
				matchedAppName = entry.Name()
				break
			}
		}
	}

	// 如果在apps目录中没找到，在AppData目录中查找
	if matchedAppName == "" {
		appdataDir := filepath.Join(extractedPath, "DATA/AppData")
		if entries, err := os.ReadDir(appdataDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() && strings.EqualFold(entry.Name(), appName) {
					matchedAppName = entry.Name()
					break
				}
			}
		}
	}

	if matchedAppName == "" {
		return "", fmt.Errorf("未找到应用 %s 的相关文件", appName)
	}

	// 复制Compose文件（如果存在）
	composeSourcePath := filepath.Join(extractedPath, "var/lib/casaos/apps", matchedAppName, "docker-compose.yml")
	composeTargetPath := filepath.Join(appPackageDir, "docker-compose.yml")
	if _, err := os.Stat(composeSourcePath); err == nil {
		err = s.copyFile(composeSourcePath, composeTargetPath)
		if err != nil {
			return "", fmt.Errorf("Failed to copy Compose file: %v", err)
		}
		log.Printf("[INFO] Copied Compose file for app %s", matchedAppName)
	} else {
		log.Printf("[WARNING] Compose file not found for app %s: %s", matchedAppName, composeSourcePath)
	}

	// 复制AppData目录（如果存在）
	appdataSourcePath := filepath.Join(extractedPath, "DATA/AppData", matchedAppName)
	appdataTargetPath := filepath.Join(appPackageDir, "AppData")
	if _, err := os.Stat(appdataSourcePath); err == nil {
		err = s.copyDir(appdataSourcePath, appdataTargetPath)
		if err != nil {
			return "", fmt.Errorf("Failed to copy AppData directory: %v", err)
		}
		log.Printf("[INFO] Copied AppData directory for app %s", matchedAppName)
	} else {
		log.Printf("[INFO] App %s has no AppData directory", matchedAppName)
	}

	// 创建应用包压缩文件
	packagesDir := "./packages"
	err = os.MkdirAll(packagesDir, 0755)
	if err != nil {
		return "", fmt.Errorf("Failed to create packages directory: %v", err)
	}

	packageFileName := fmt.Sprintf("%s_%s.zip", appName, taskID)
	packagePath := filepath.Join(packagesDir, packageFileName)

	// 创建ZIP文件
	err = s.createZipFile(appPackageDir, packagePath)
	if err != nil {
		return "", fmt.Errorf("Failed to create archive: %v", err)
	}

	log.Printf("[INFO] Created archive for app %s: %s", appName, packagePath)
	return packagePath, nil
}

// copyFile 复制文件
func (s *MigrationService) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// 创建目标目录
	err = os.MkdirAll(filepath.Dir(dst), 0755)
	if err != nil {
		return err
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// copyDir 复制目录
func (s *MigrationService) copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 计算相对路径
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// 目标路径
		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			// 创建目录
			return os.MkdirAll(targetPath, info.Mode())
		} else {
			// 复制文件
			return s.copyFile(path, targetPath)
		}
	})
}

// createZipFile 创建ZIP文件
func (s *MigrationService) createZipFile(sourceDir, targetZip string) error {
	zipFile, err := os.Create(targetZip)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录本身
		if info.IsDir() {
			return nil
		}

		// 计算相对路径
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// 创建ZIP文件条目
		writer, err := archive.Create(relPath)
		if err != nil {
			return err
		}

		// 打开源文件
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// 复制文件内容到ZIP
		_, err = io.Copy(writer, file)
		return err
	})
}

// readComposeFiles 读取本地apps目录下的所有compose文件
func (s *MigrationService) readComposeFiles(appsDir string) (map[string]string, error) {
	composeFiles := make(map[string]string)

	log.Printf("[DEBUG] Start scanning apps directory: %s", appsDir)

	// 检查apps目录是否存在
	if _, err := os.Stat(appsDir); os.IsNotExist(err) {
		log.Printf("[ERROR] apps directory does not exist: %s", appsDir)
		return nil, fmt.Errorf("apps directory does not exist: %s. Please verify the import file is a valid CasaOS export.", appsDir)
	}

	// 遍历apps目录
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		log.Printf("[ERROR] Failed to read apps directory: %v", err)
		return nil, fmt.Errorf("Failed to read apps directory: %v", err)
	}

	log.Printf("[DEBUG] Found %d entries in apps directory", len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appName := entry.Name()
		composeFilePath := filepath.Join(appsDir, appName, "docker-compose.yml")

		// 检查compose文件是否存在
		if _, err := os.Stat(composeFilePath); os.IsNotExist(err) {
			log.Printf("[WARNING] Compose file not found for app %s: %s", appName, composeFilePath)
			continue
		}

		// 读取compose文件内容
		content, err := os.ReadFile(composeFilePath)
		if err != nil {
			log.Printf("[ERROR] Failed to read compose file for app %s: %v", appName, err)
			continue
		}

		composeFiles[appName] = string(content)
		log.Printf("[DEBUG] Read compose file for app %s, size: %d bytes", appName, len(content))
	}

	log.Printf("[INFO] Total %d compose files read", len(composeFiles))
	return composeFiles, nil
}

// importComposeToZimaOS 导入compose文件到ZimaOS
func (s *MigrationService) importComposeToZimaOS(target *models.SystemConnection, appName, composeContent, taskID string) error {
	// 记录开始导入
	s.taskService.AddTaskLog(taskID, models.LogLevelInfo, fmt.Sprintf("Start importing app: %s", appName))

	// 构建API URL
	apiURL := fmt.Sprintf("http://%s:%d/v2/app_management/compose?dry_run=false&check_port_conflict=true", target.Host, target.Port)

	// 创建HTTP请求
	req, err := http.NewRequest("POST", apiURL, strings.NewReader(composeContent))
	if err != nil {
		errorMsg := fmt.Sprintf("App %s: Failed to create request: %v", appName, err)
		s.taskService.AddTaskLog(taskID, models.LogLevelError, errorMsg)
		return fmt.Errorf(errorMsg)
	}

	// 设置请求头
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Authorization", target.Token)
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "application/yaml")
	req.Header.Set("Language", "en_US")
	req.Header.Set("Origin", fmt.Sprintf("http://%s:%d", target.Host, target.Port))
	req.Header.Set("Referer", fmt.Sprintf("http://%s:%d/modules/icewhale_app/?_t=%d", target.Host, target.Port, time.Now().Unix()))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")

	// 发送请求
	s.taskService.AddTaskLog(taskID, models.LogLevelInfo, fmt.Sprintf("App %s: Sending import request...", appName))
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		errorMsg := fmt.Sprintf("App %s: Request failed: %v", appName, err)
		s.taskService.AddTaskLog(taskID, models.LogLevelError, errorMsg)
		return fmt.Errorf(errorMsg)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		errorMsg := fmt.Sprintf("App %s: Failed to read response: %v", appName, err)
		s.taskService.AddTaskLog(taskID, models.LogLevelError, errorMsg)
		return fmt.Errorf(errorMsg)
	}

	// 检查响应状态
	if resp.StatusCode == 200 {
		s.taskService.AddTaskLog(taskID, models.LogLevelInfo, fmt.Sprintf("App %s: Import succeeded ✓", appName))
		return nil
	} else {
		errorMsg := fmt.Sprintf("App %s: Import failed (status code: %d): %s", appName, resp.StatusCode, string(body))
		s.taskService.AddTaskLog(taskID, models.LogLevelError, errorMsg)
		return fmt.Errorf(errorMsg)
	}
}

// migrateSettings 迁移设置
func (s *MigrationService) migrateSettings(target *models.SystemConnection, settings interface{}) error {
	// 模拟设置迁移
	time.Sleep(1 * time.Second)
	return nil
}

// migrateUserData 迁移用户数据
func (s *MigrationService) migrateUserData(target *models.SystemConnection, userData interface{}) error {
	// 模拟用户数据迁移
	time.Sleep(3 * time.Second)
	return nil
}

// createExportFile 创建导出文件
func (s *MigrationService) createExportFile(taskID string, data map[string]interface{}) (string, error) {
	// 创建导出目录
	exportDir := "./exports"
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", fmt.Errorf("Failed to create export directory: %v", err)
	}

	// 生成文件名
	filename := fmt.Sprintf("casaos_export_%s_%s.zip", taskID, time.Now().Format("20060102_150405"))
	filePath := filepath.Join(exportDir, filename)

	// 创建ZIP文件
	zipFile, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("Failed to create ZIP file: %v", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// 将数据写入ZIP文件
	dataJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("Failed to serialize data: %v", err)
	}

	writer, err := zipWriter.Create("migration_data.json")
	if err != nil {
		return "", fmt.Errorf("Failed to create ZIP entry: %v", err)
	}

	_, err = writer.Write(dataJSON)
	if err != nil {
		return "", fmt.Errorf("Failed to write data: %v", err)
	}

	return filePath, nil
}

// createMockDownloadFile 创建模拟的下载文件用于演示
func (s *MigrationService) createMockDownloadFile() (string, error) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "mock_casaos_*")
	if err != nil {
		return "", fmt.Errorf("Failed to create temporary directory: %v", err)
	}

	// 创建模拟的apps目录结构
	appsDir := filepath.Join(tempDir, "apps")
	err = os.MkdirAll(filepath.Join(appsDir, "nextcloud"), 0755)
	if err != nil {
		return "", fmt.Errorf("Failed to create apps directory: %v", err)
	}

	// 创建模拟的docker-compose.yml文件
	composeContent := `version: '3.8'
services:
  nextcloud:
    image: nextcloud:latest
    ports:
      - "8080:80"
    volumes:
      - nextcloud_data:/var/www/html
volumes:
  nextcloud_data:`
	err = os.WriteFile(filepath.Join(appsDir, "nextcloud", "docker-compose.yml"), []byte(composeContent), 0644)
	if err != nil {
		return "", fmt.Errorf("Failed to create compose file: %v", err)
	}

	// 创建模拟的appdata目录结构
	appdataDir := filepath.Join(tempDir, "appdata")
	err = os.MkdirAll(filepath.Join(appdataDir, "nextcloud", "config"), 0755)
	if err != nil {
		return "", fmt.Errorf("Failed to create appdata directory: %v", err)
	}

	// 创建模拟的配置文件
	configContent := `<?php
$CONFIG = array (
  'instanceid' => 'mock_instance',
  'passwordsalt' => 'mock_salt',
  'secret' => 'mock_secret',
  'trusted_domains' => array (
    0 => 'localhost:8080',
  ),
);`
	err = os.WriteFile(filepath.Join(appdataDir, "nextcloud", "config", "config.php"), []byte(configContent), 0644)
	if err != nil {
		return "", fmt.Errorf("Failed to create config file: %v", err)
	}

	// 创建ZIP文件
	zipPath := filepath.Join(os.TempDir(), fmt.Sprintf("mock_casaos_%d.zip", time.Now().Unix()))
	err = s.createZipFile(tempDir, zipPath)
	if err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("Failed to create ZIP file: %v", err)
	}

	// 清理临时目录
	os.RemoveAll(tempDir)

	log.Printf("[DirectExport] Created mock download file: %s", zipPath)
	return zipPath, nil
}

// createDirectExportFile 创建包含实际文件的导出压缩包
func (s *MigrationService) createDirectExportFile(taskID string, data map[string]interface{}, downloadedFilePath string) (string, error) {
	// 创建导出目录
	exportDir := "./exports"
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", fmt.Errorf("Failed to create export directory: %v", err)
	}

	// 生成文件名
	filename := fmt.Sprintf("casaos_export_%s.zip", time.Now().Format("20060102_150405"))
	filePath := filepath.Join(exportDir, filename)

	// 创建ZIP文件
	zipFile, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("Failed to create ZIP file: %v", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// 1. 添加metadata JSON文件
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("Failed to serialize data: %v", err)
	}

	writer, err := zipWriter.Create("migration_data.json")
	if err != nil {
		return "", fmt.Errorf("Failed to create ZIP entry: %v", err)
	}

	_, err = writer.Write(jsonData)
	if err != nil {
		return "", fmt.Errorf("Failed to write data: %v", err)
	}

	// 2. 添加下载的CasaOS文件（包含apps和appdata目录）
	if downloadedFilePath != "" {
		// 打开下载的ZIP文件
		downloadedZip, err := zip.OpenReader(downloadedFilePath)
		if err != nil {
			return "", fmt.Errorf("Failed to open downloaded ZIP file: %v", err)
		}
		defer downloadedZip.Close()

		// 将下载的ZIP文件内容复制到新的ZIP文件中
		for _, file := range downloadedZip.File {
			// 打开源文件
			src, err := file.Open()
			if err != nil {
				return "", fmt.Errorf("Failed to open source file: %v", err)
			}

			// 在新ZIP中创建文件
			dst, err := zipWriter.Create(file.Name)
			if err != nil {
				src.Close()
				return "", fmt.Errorf("Failed to create destination file: %v", err)
			}

			// 复制文件内容
			_, err = io.Copy(dst, src)
			src.Close()
			if err != nil {
				return "", fmt.Errorf("Failed to copy file content: %v", err)
			}
		}
	}

	return filePath, nil
}

// detectFileFormat 根据文件魔数检测文件格式
func (s *MigrationService) detectFileFormat(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("Failed to open file: %v", err)
	}
	defer file.Close()

	// 读取文件前10个字节用于格式检测
	buf := make([]byte, 10)
	n, err := file.Read(buf)
	if err != nil {
		return "", fmt.Errorf("Failed to read file header: %v", err)
	}

	log.Printf("[DEBUG] First %d bytes of file: %v", n, buf[:n])

	// 检测ZIP格式 (PK signature: 0x504B)
	if n >= 2 && buf[0] == 0x50 && buf[1] == 0x4B {
		log.Printf("[DEBUG] Detected ZIP format, magic: %02X %02X", buf[0], buf[1])
		return "zip", nil
	}

	// 检测GZIP格式 (magic number: 0x1F8B)
	if n >= 2 && buf[0] == 0x1F && buf[1] == 0x8B {
		log.Printf("[DEBUG] Detected GZIP format, magic: %02X %02X", buf[0], buf[1])
		return "gzip", nil
	}

	// 如果都不匹配，返回详细的错误信息
	magicStr := ""
	if n >= 2 {
		magicStr = fmt.Sprintf("%02X %02X", buf[0], buf[1])
	} else if n >= 1 {
		magicStr = fmt.Sprintf("%02X", buf[0])
	}

	log.Printf("[ERROR] Unrecognized file format, magic: %s", magicStr)
	return "unknown", fmt.Errorf("Unsupported file format. Detected magic bytes: %s (Supported: ZIP magic 50 4B, GZIP magic 1F 8B)", magicStr)
}

// parseImportFile 解析导入文件
func (s *MigrationService) parseImportFile(filePath string) (map[string]interface{}, error) {
	// 根据文件内容检测实际格式
	actualFormat, err := s.detectFileFormat(filePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to detect file format: %v", err)
	}

	log.Printf("[DEBUG] Detected file format: %s", actualFormat)

	// 根据实际格式选择解析方法
	switch actualFormat {
	case "gzip":
		return s.parseTarGzFile(filePath)
	case "zip":
		return s.parseZipFile(filePath)
	default:
		return nil, fmt.Errorf("Unsupported file format: %s", actualFormat)
	}
}

// parseZipFile 解析ZIP格式文件
func (s *MigrationService) parseZipFile(filePath string) (map[string]interface{}, error) {
	// 打开ZIP文件
	zipReader, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to open ZIP file: %v", err)
	}
	defer zipReader.Close()

	// 查找数据文件
	for _, file := range zipReader.File {
		if strings.HasSuffix(file.Name, "migration_data.json") {
			reader, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("Failed to open data file: %v", err)
			}
			defer reader.Close()

			data, err := io.ReadAll(reader)
			if err != nil {
				return nil, fmt.Errorf("Failed to read data file: %v", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				return nil, fmt.Errorf("Failed to parse data file: %v", err)
			}

			return result, nil
		}
	}

	return nil, fmt.Errorf("Valid data file not found")
}

// parseTarGzFile 解析tar.gz格式文件
func (s *MigrationService) parseTarGzFile(filePath string) (map[string]interface{}, error) {
	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to open file: %v", err)
	}
	defer file.Close()

	// 创建gzip reader
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("Failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	// 创建tar reader
	tarReader := tar.NewReader(gzReader)

	// 遍历tar文件中的条目
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Failed to read tar entry: %v", err)
		}

		// 查找可能的数据文件
		if header.Typeflag == tar.TypeReg {
			// 尝试多种可能的文件名
			if strings.HasSuffix(header.Name, "migration_data.json") ||
				strings.HasSuffix(header.Name, "casaos-export.json") ||
				strings.HasSuffix(header.Name, "export.json") ||
				strings.Contains(header.Name, "apps.json") {

				data, err := io.ReadAll(tarReader)
				if err != nil {
					return nil, fmt.Errorf("Failed to read data file: %v", err)
				}

				var result map[string]interface{}
				if err := json.Unmarshal(data, &result); err != nil {
					return nil, fmt.Errorf("Failed to parse data file: %v", err)
				}

				return result, nil
			}
		}
	}

	// 如果没有找到JSON文件，尝试解析目录结构
	return s.parseCasaOSStructure(filePath)
}

// parseCasaOSStructure 解析CasaOS目录结构
func (s *MigrationService) parseCasaOSStructure(filePath string) (map[string]interface{}, error) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "casaos-import-*")
	if err != nil {
		return nil, fmt.Errorf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 解压tar.gz文件到临时目录
	if err := s.extractTarGz(filePath, tempDir); err != nil {
		return nil, fmt.Errorf("Failed to extract file: %v", err)
	}

	// 扫描解压后的目录结构
	apps := make([]map[string]interface{}, 0)

	// 查找apps目录或类似的应用目录
	err = filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 忽略错误，继续扫描
		}

		// 查找docker-compose.yml文件
		if info.Name() == "docker-compose.yml" || info.Name() == "docker-compose.yaml" {
			// 解析compose文件
			if appInfo := s.parseComposeFile(path); appInfo != nil {
				apps = append(apps, appInfo)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("Failed to scan directory structure: %v", err)
	}

	// 构建返回结果
	result := map[string]interface{}{
		"apps": apps,
		"summary": map[string]interface{}{
			"total":   len(apps),
			"success": 0,
			"failed":  0,
		},
	}

	return result, nil
}

// extractTarGz 解压tar.gz文件或ZIP文件
func (s *MigrationService) extractTarGz(src, dest string) error {
	// 检查源文件是否存在
	fileInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("Source file does not exist or is inaccessible: %v", err)
	}
	log.Printf("[DEBUG] Preparing to extract file: %s, size: %d bytes", src, fileInfo.Size())

	// 验证文件大小
	if fileInfo.Size() == 0 {
		return fmt.Errorf("File size is 0; file may be corrupted")
	}

	// 检测文件格式
	actualFormat, err := s.detectFileFormat(src)
	if err != nil {
		return fmt.Errorf("Failed to detect file format: %v", err)
	}

	log.Printf("[DEBUG] Detected file format: %s", actualFormat)

	// 根据实际格式选择解压方法
	switch actualFormat {
	case "gzip":
		log.Printf("[INFO] Using GZIP extraction method")
		return s.extractGzipFile(src, dest)
	case "zip":
		log.Printf("[INFO] Detected ZIP file; using ZIP extraction method")
		return s.extractZipFile(src, dest)
	default:
		// 如果是unknown格式，错误信息已经在detectFileFormat中生成
		if actualFormat == "unknown" {
			return err // 返回detectFileFormat的详细错误信息
		}
		return fmt.Errorf("Unsupported file format: %s. Please use ZIP or GZIP archives", actualFormat)
	}
}

// extractGzipFile 解压gzip格式的tar文件
func (s *MigrationService) extractGzipFile(src, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("Failed to open source file: %v", err)
	}
	defer file.Close()

	// 创建gzip reader
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("Failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	log.Printf("[DEBUG] gzip reader created")

	// 确保目标目录存在且权限正确
	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("Failed to create destination directory: %v", err)
	}
	if err := os.Chmod(dest, 0755); err != nil {
		log.Printf("[WARNING] Failed to set destination directory permissions: %v", err)
	}

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("Failed to read tar entry: %v", err)
		}

		target := filepath.Join(dest, header.Name)

		// 检查路径安全性，防止目录遍历攻击
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("Insecure file path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				log.Printf("[ERROR] Failed to create directory: %s, error: %v", target, err)
				return fmt.Errorf("Failed to create directory: %s - %v", target, err)
			}
			// 设置目录权限
			if err := os.Chmod(target, 0755); err != nil {
				log.Printf("[WARNING] Failed to set directory permissions: %s - %v", target, err)
			}
			log.Printf("[DEBUG] Created directory: %s", target)
		case tar.TypeReg:
			parentDir := filepath.Dir(target)
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				log.Printf("[ERROR] Failed to create parent directory: %s, error: %v", parentDir, err)
				return fmt.Errorf("Failed to create parent directory: %s - %v", parentDir, err)
			}
			// 设置父目录权限
			if err := os.Chmod(parentDir, 0755); err != nil {
				log.Printf("[WARNING] Failed to set parent directory permissions: %s - %v", parentDir, err)
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, 0644) // 使用统一的文件权限
			if err != nil {
				log.Printf("[ERROR] Failed to create file: %s, error: %v", target, err)
				return fmt.Errorf("Failed to create file: %s - %v", target, err)
			}
			if _, err := io.Copy(f, tarReader); err != nil {
				f.Close()
				return fmt.Errorf("Failed to copy file content: %v", err)
			}
			f.Close()
			log.Printf("[DEBUG] Extracted file: %s", target)
		}
	}

	log.Printf("[DEBUG] GZIP file extraction completed: %s", src)
	return nil
}

// parseComposeFile 解析docker-compose文件
func (s *MigrationService) parseComposeFile(composePath string) map[string]interface{} {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil
	}

	// 简单解析compose文件，提取应用信息
	var compose map[string]interface{}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil
	}

	// 提取服务信息
	services, ok := compose["services"].(map[string]interface{})
	if !ok {
		return nil
	}

	// 获取第一个服务作为应用信息
	for serviceName, serviceConfig := range services {
		service, ok := serviceConfig.(map[string]interface{})
		if !ok {
			continue
		}

		// 构建应用信息
		appInfo := map[string]interface{}{
			"name":         serviceName,
			"compose_path": composePath,
			"status":       "pending",
		}

		// 提取镜像信息
		if image, ok := service["image"].(string); ok {
			appInfo["image"] = image
		}

		// 提取端口信息
		if ports, ok := service["ports"]; ok {
			appInfo["ports"] = ports
		}

		return appInfo
	}

	return nil
}

// getFileSize 获取文件大小
func (s *MigrationService) getFileSize(filePath string) int64 {
	if info, err := os.Stat(filePath); err == nil {
		return info.Size()
	}
	return 0
}

// downloadCasaOSFiles 下载CasaOS文件
func (s *MigrationService) downloadCasaOSFiles(conn *models.SystemConnection, progressCallback func(int, string)) (string, error) {
	// 构建下载URL
	downloadURL := fmt.Sprintf("http://%s/v1/batch?token=%s&files=/var/lib/casaos/apps,/DATA/AppData", conn.Host, conn.Token)

	progressCallback(10, "Start downloading")

	// 创建HTTP请求
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("Failed to create download request: %v", err)
	}

	// 发送请求
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Failed to send download request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Download failed, status code: %d", resp.StatusCode)
	}

	progressCallback(20, "Downloading file")

	// 创建下载目录
	downloadDir := "./download"
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return "", fmt.Errorf("Failed to create download directory: %v", err)
	}

	// 生成文件名
	filename := fmt.Sprintf("casaos_backup_%s.zip", time.Now().Format("20060102_150405"))
	filePath := filepath.Join(downloadDir, filename)

	// 创建本地文件
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("Failed to create local file: %v", err)
	}
	defer file.Close()

	// 复制数据并显示进度
	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return "", fmt.Errorf("Failed to download file: %v", err)
	}

	progressCallback(35, fmt.Sprintf("Download completed, file size: %d bytes", written))

	return filePath, nil
}

// extractDownloadedFiles 解压下载的文件
func (s *MigrationService) extractDownloadedFiles(zipPath string, progressCallback func(int, string)) (string, error) {
	progressCallback(45, "Starting to extract file")

	// 创建解压目录
	extractDir := filepath.Join(filepath.Dir(zipPath), "extracted")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", fmt.Errorf("Failed to create extraction directory: %v", err)
	}

	// 打开ZIP文件
	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("Failed to open ZIP file: %v", err)
	}
	defer zipReader.Close()

	progressCallback(50, "Extracting file")

	// 解压文件
	for i, file := range zipReader.File {
		// 计算进度
		progress := 50 + (i*10)/len(zipReader.File)
		progressCallback(progress, fmt.Sprintf("Extracting: %s", file.Name))

		// 构建目标路径
		targetPath := filepath.Join(extractDir, file.Name)

		// 确保目录存在
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, file.FileInfo().Mode()); err != nil {
				return "", fmt.Errorf("Failed to create directory: %v", err)
			}
			continue
		}

		// 确保父目录存在
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return "", fmt.Errorf("Failed to create parent directory: %v", err)
		}

		// 打开ZIP中的文件
		src, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("Failed to open ZIP file entry: %v", err)
		}

		// 创建目标文件
		dst, err := os.Create(targetPath)
		if err != nil {
			src.Close()
			return "", fmt.Errorf("Failed to create target file: %v", err)
		}

		// 复制文件内容
		_, err = io.Copy(dst, src)
		src.Close()
		dst.Close()

		if err != nil {
			return "", fmt.Errorf("Failed to copy file content: %v", err)
		}
	}

	progressCallback(60, "Extraction completed")

	return extractDir, nil
}

// mergeAppDataToZimaOS 合并AppData目录到ZimaOS
func (s *MigrationService) mergeAppDataToZimaOS(target *models.SystemConnection, appDataPath string, taskID string, progressCallback func(int, string)) error {
	log.Printf("[INFO] Start merging AppData directory: %s", appDataPath)

	// 读取AppData目录下的所有应用目录
	entries, err := os.ReadDir(appDataPath)
	if err != nil {
		return fmt.Errorf("Failed to read AppData directory: %v", err)
	}

	if len(entries) == 0 {
		log.Printf("[INFO] AppData directory is empty, skipping merge")
		progressCallback(100, "AppData directory is empty, skipping merge")
		return nil
	}

	log.Printf("[INFO] Found %d application data directories", len(entries))
	s.taskService.AddTaskLog(taskID, models.LogLevelInfo, fmt.Sprintf("Found %d application data directories, starting merge", len(entries)))

	totalDirs := len(entries)
	completedDirs := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appName := entry.Name()
		completedDirs++
		progress := 30 + (60 * completedDirs / totalDirs) // 从30%到90%
		progressCallback(progress, fmt.Sprintf("Processing app data: %s (%d/%d)", appName, completedDirs, totalDirs))

		// 检查ZimaOS中是否已存在该应用目录
		exists, err := s.checkAppDataExists(target, appName)
		if err != nil {
			log.Printf("[WARNING] Failed to check app %s data directory: %v", appName, err)
			s.taskService.AddTaskLog(taskID, models.LogLevelWarning, fmt.Sprintf("Failed to check app %s data directory: %v", appName, err))
			continue
		}

		if exists {
			log.Printf("[WARNING] Data directory for app %s already exists, skipping merge", appName)
			s.taskService.AddTaskLog(taskID, models.LogLevelWarning, fmt.Sprintf("Data directory for app %s already exists, skipping merge ⚠️", appName))
			continue
		}

		// 上传应用数据目录到ZimaOS
		sourcePath := filepath.Join(appDataPath, appName)
		err = s.uploadAppDataToZimaOS(target, appName, sourcePath, taskID)
		if err != nil {
			log.Printf("[ERROR] Failed to upload data for app %s: %v", appName, err)
			s.taskService.AddTaskLog(taskID, models.LogLevelError, fmt.Sprintf("App %s data upload failed: %v", appName, err))
			continue
		}

		log.Printf("[INFO] App %s data merge succeeded", appName)
		s.taskService.AddTaskLog(taskID, models.LogLevelInfo, fmt.Sprintf("App %s data merge succeeded ✓ (%d/%d)", appName, completedDirs, totalDirs))
	}

	log.Printf("[INFO] AppData directory merge completed")
	return nil
}

// checkAppDataExists 检查ZimaOS中是否已存在应用数据目录
func (s *MigrationService) checkAppDataExists(target *models.SystemConnection, appName string) (bool, error) {
	// 构建检查URL
	checkURL := fmt.Sprintf("http://%s:%d/v1/file/info?path=/media/ZimaOS-HD/AppData/%s", target.Host, target.Port, appName)

	// 创建HTTP请求
	req, err := http.NewRequest("GET", checkURL, nil)
	if err != nil {
		return false, fmt.Errorf("Failed to create check request: %v", err)
	}

	// 设置认证头
	req.Header.Set("Authorization", target.Token)

	// 发送请求
	resp, err := s.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("Failed to send check request: %v", err)
	}
	defer resp.Body.Close()

	// 如果返回200，说明目录存在；如果返回404，说明目录不存在
	if resp.StatusCode == http.StatusOK {
		return true, nil
	} else if resp.StatusCode == http.StatusNotFound {
		return false, nil
	} else {
		return false, fmt.Errorf("Unexpected directory status code: %d", resp.StatusCode)
	}
}

// uploadAppDataToZimaOS 上传应用数据目录到ZimaOS
func (s *MigrationService) uploadAppDataToZimaOS(target *models.SystemConnection, appName, sourcePath, taskID string) error {
	log.Printf("[INFO] Start uploading data directory for app %s: %s", appName, sourcePath)

	// 创建临时压缩文件
	tempDir := "./compress"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("Failed to create temporary directory: %v", err)
	}

	// 创建临时压缩文件，使用时间戳命名
	tempZipPath := filepath.Join(tempDir, fmt.Sprintf("%s_appdata_%s.zip", appName, time.Now().Format("20060102_150405")))

	// 压缩应用数据目录
	err := s.compressDirectory(sourcePath, tempZipPath)
	if err != nil {
		return fmt.Errorf("Failed to compress app data: %v", err)
	}

	defer func() {
		// 清理临时压缩文件
		if err := os.Remove(tempZipPath); err != nil {
			log.Printf("[WARNING] Failed to remove temporary archive: %v", err)
		}
	}()

	// 上传压缩文件到ZimaOS，目标路径为/media/ZimaOS-HD/AppData，文件名为{appName}.zip
	uploadURL := fmt.Sprintf("http://%s:%d/v2_1/files/file/uploadV2", target.Host, target.Port)
	err = s.uploadFileToZimaOS(uploadURL, tempZipPath, "/media/ZimaOS-HD/AppData", fmt.Sprintf("%s.zip", appName), target.Token)
	if err != nil {
		return fmt.Errorf("Failed to upload archive: %v", err)
	}

	// 在ZimaOS上解压文件
	unzipURL := fmt.Sprintf("http://%s:%d/v2_1/files/task/decompress", target.Host, target.Port)
	err = s.extractFileOnZimaOS(unzipURL, fmt.Sprintf("/media/ZimaOS-HD/AppData/%s.zip", appName), "/media/ZimaOS-HD/AppData", target.Token)
	if err != nil {
		return fmt.Errorf("Failed to decompress file on ZimaOS: %v", err)
	}

	// 删除ZimaOS上的临时压缩文件
	deleteURL := fmt.Sprintf("http://%s:%d/v2_1/files/file", target.Host, target.Port)
	err = s.deleteFileOnZimaOS(deleteURL, fmt.Sprintf("/media/ZimaOS-HD/AppData/%s.zip", appName), target.Token)
	if err != nil {
		log.Printf("[WARNING] Failed to delete temporary archive on ZimaOS: %v", err)
	}

	log.Printf("[INFO] App %s data upload completed", appName)
	return nil
}

// compressDirectory 压缩目录
func (s *MigrationService) compressDirectory(sourceDir, zipPath string) error {
	// 创建ZIP文件
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("Failed to create ZIP file: %v", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// 遍历源目录
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 计算相对路径
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// 跳过根目录
		if relPath == "." {
			return nil
		}

		// 创建ZIP条目
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		header.Name = filepath.ToSlash(relPath)

		if info.IsDir() {
			header.Name += "/"
		}

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		// 如果是文件，复制内容
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(writer, file)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

// uploadFileToZimaOS 上传文件到ZimaOS
func (s *MigrationService) uploadFileToZimaOS(uploadURL, filePath, targetPath, filename, token string) error {
	// 获取文件信息
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("Failed to get file info: %v", err)
	}

	log.Printf("[DEBUG] ========== File Upload Debug ==========")
	log.Printf("[DEBUG] Local file path: %s", filePath)
	log.Printf("[DEBUG] File size: %d bytes", fileInfo.Size())
	log.Printf("[DEBUG] File exists: %t", !os.IsNotExist(err))
	log.Printf("[DEBUG] Target path: %s", targetPath)
	log.Printf("[DEBUG] Upload URL: %s", uploadURL)

	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("Failed to open file: %v", err)
	}
	defer file.Close()

	// 创建multipart请求 - 使用bytes.Buffer而不是strings.Builder
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加path字段
	writer.WriteField("path", targetPath)

	// 添加rename字段（空字符串）
	writer.WriteField("rename", "")

	// 手动创建文件字段以确保正确的Content-Disposition和Content-Type
	// 使用传入的filename参数而不是原始文件名
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", "application/zip")

	part, err := writer.CreatePart(h)
	if err != nil {
		return fmt.Errorf("Failed to create file field: %v", err)
	}

	_, err = io.Copy(part, file)
	if err != nil {
		return fmt.Errorf("Failed to copy file content: %v", err)
	}

	writer.Close()

	// 打印multipart表单信息
	log.Printf("[DEBUG] Multipart Content-Type: %s", writer.FormDataContentType())
	log.Printf("[DEBUG] Request body size: %d bytes", body.Len())
	log.Printf("[DEBUG] Form fields: path=%s, rename=\"\", file=%s", targetPath, filename)
	log.Printf("[DEBUG] File field Content-Disposition: form-data; name=\"file\"; filename=\"%s\"", filename)
	log.Printf("[DEBUG] File field Content-Type: application/zip")

	// 创建HTTP请求 - 使用bytes.NewReader
	req, err := http.NewRequest("POST", uploadURL, bytes.NewReader(body.Bytes()))
	if err != nil {
		return fmt.Errorf("Failed to create upload request: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", token)

	// 打印请求头信息
	log.Printf("[DEBUG] ========== Request Headers ==========")
	for key, values := range req.Header {
		for _, value := range values {
			log.Printf("[DEBUG] %s: %s", key, value)
		}
	}

	// 发送请求
	log.Printf("[DEBUG] Sending HTTP request...")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to send upload request: %v", err)
	}
	defer resp.Body.Close()

	// 打印响应头信息
	log.Printf("[DEBUG] ========== Response Info ==========")
	log.Printf("[DEBUG] Status Code: %d", resp.StatusCode)
	log.Printf("[DEBUG] Status: %s", resp.Status)
	log.Printf("[DEBUG] ========== Response Headers ==========")
	for key, values := range resp.Header {
		for _, value := range values {
			log.Printf("[DEBUG] %s: %s", key, value)
		}
	}

	// 读取响应体以获取详细错误信息
	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[DEBUG] ========== Response Body ==========")
	log.Printf("[DEBUG] Body: %s", string(respBody))
	log.Printf("[DEBUG] Body Length: %d bytes", len(respBody))
	log.Printf("[DEBUG] ========================================")

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Upload failed, status code: %d, response: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// extractFileOnZimaOS 在ZimaOS上解压文件
func (s *MigrationService) extractFileOnZimaOS(extractURL, zipPath, targetDir, token string) error {
	// 构建请求体 - 使用新的API格式
	requestBody := map[string]interface{}{
		"src":             []string{zipPath},
		"dst":             targetDir,
		"user_select":     "overwrite",
		"retain_src_file": true,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("Failed to serialize request data: %v", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequest("POST", extractURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("Failed to create decompression request: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)

	// 发送请求
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to send decompression request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Decompression failed, status code: %d", resp.StatusCode)
	}

	return nil
}

// deleteFileOnZimaOS 删除ZimaOS上的文件
func (s *MigrationService) deleteFileOnZimaOS(deleteURL, filePath, token string) error {
	// 构建请求体 - 使用新的API格式，支持批量删除
	requestBody := []string{filePath}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("Failed to serialize request data: %v", err)
	}

	log.Printf("[DEBUG] Delete file on ZimaOS:")
	log.Printf("[DEBUG] URL: %s", deleteURL)
	log.Printf("[DEBUG] Body: %s", string(jsonData))

	// 创建HTTP请求
	req, err := http.NewRequest("DELETE", deleteURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("Failed to create delete request: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)

	// 发送请求
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to send delete request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[DEBUG] Delete response status: %d", resp.StatusCode)
	log.Printf("[DEBUG] Delete response body: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Delete failed, status code: %d, response: %s", resp.StatusCode, string(body))
	}

	return nil
}

// CreateDirectExport 直接创建导出压缩包文件
func (s *MigrationService) CreateDirectExport(sourceConn *models.SystemConnection) (string, error) {
	// 测试源系统连接
	testResp, err := s.connService.TestConnection(sourceConn)
	var downloadedFilePath string

	if err != nil || !testResp.Success {
		// 连接失败时使用模拟数据进行演示
		log.Printf("[DirectExport] Connection failed; using mock data: %v", err)
		// 创建一个模拟的下载文件
		downloadedFilePath, err = s.createMockDownloadFile()
		if err != nil {
			return "", fmt.Errorf("Failed to create mock download file: %v", err)
		}
	} else {
		// 连接成功时下载真实文件
		progressCallback := func(progress int, message string) {
			log.Printf("[DirectExport] %d%% - %s", progress, message)
		}

		downloadedFilePath, err = s.downloadCasaOSFiles(sourceConn, progressCallback)
		if err != nil {
			return "", fmt.Errorf("Failed to download CasaOS files: %v", err)
		}
	}

	// 导出应用数据（用于metadata）
	apps, err := s.getSystemApps(sourceConn)
	if err != nil {
		return "", fmt.Errorf("Failed to export application data: %v", err)
	}

	// 导出系统设置
	settings, err := s.getSystemSettings(sourceConn)
	if err != nil {
		return "", fmt.Errorf("Failed to export system settings: %v", err)
	}

	// 导出用户数据
	userData, err := s.getUserData(sourceConn)
	if err != nil {
		return "", fmt.Errorf("Failed to export user data: %v", err)
	}

	// 组装导出数据
	exportData := map[string]interface{}{
		"apps":      apps,
		"settings":  settings,
		"userData":  userData,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// 创建包含实际文件的导出压缩包
	taskID := fmt.Sprintf("direct_%d", time.Now().Unix())
	filePath, err := s.createDirectExportFile(taskID, exportData, downloadedFilePath)
	if err != nil {
		return "", fmt.Errorf("Failed to create export file: %v", err)
	}

	// 清理临时下载文件
	os.Remove(downloadedFilePath)

	return filePath, nil
}
