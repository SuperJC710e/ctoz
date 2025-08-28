package handlers

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ctoz/backend/internal/models"
	"ctoz/backend/internal/services"
	"ctoz/backend/internal/websocket"

	"github.com/gin-gonic/gin"
)

// Handler 处理器结构体
type Handler struct {
	connService      *services.ConnectionService
	migrationService *services.MigrationService
	taskService      *services.TaskService
	wsManager        *websocket.Manager

	// 缓存相关
	importStatusCache map[string]models.ImportStatusResponse
	cacheMutex        sync.RWMutex
	cacheExpiry       map[string]time.Time
	cacheTTL          time.Duration // 缓存过期时间
}

// NewHandler 创建新的处理器
func NewHandler(
	connService *services.ConnectionService,
	migrationService *services.MigrationService,
	taskService *services.TaskService,
	wsManager *websocket.Manager,
) *Handler {
	handler := &Handler{
		connService:       connService,
		migrationService:  migrationService,
		taskService:       taskService,
		wsManager:         wsManager,
		importStatusCache: make(map[string]models.ImportStatusResponse),
		cacheExpiry:       make(map[string]time.Time),
		cacheTTL:          time.Minute * 5, // 缓存5分钟
	}

	// 启动缓存清理goroutine
	go func() {
		ticker := time.NewTicker(time.Minute) // 每分钟检查一次
		defer ticker.Stop()

		for range ticker.C {
			handler.clearExpiredCache()
		}
	}()

	return handler
}

// 缓存相关方法

// getCachedImportStatus 获取缓存的导入状态
func (h *Handler) getCachedImportStatus(taskID string) (models.ImportStatusResponse, bool) {
	h.cacheMutex.RLock()
	defer h.cacheMutex.RUnlock()

	if cached, exists := h.importStatusCache[taskID]; exists {
		if expiry, ok := h.cacheExpiry[taskID]; ok && time.Now().Before(expiry) {
			log.Printf("[DEBUG] Cache hit, TaskID: %s", taskID)
			return cached, true
		} else {
			// 缓存已过期，删除
			log.Printf("[DEBUG] Cache expired, deleting, TaskID: %s", taskID)
			delete(h.importStatusCache, taskID)
			delete(h.cacheExpiry, taskID)
		}
	}
	return models.ImportStatusResponse{}, false
}

// cacheImportStatus 缓存导入状态
func (h *Handler) cacheImportStatus(taskID string, response models.ImportStatusResponse) {
	h.cacheMutex.Lock()
	defer h.cacheMutex.Unlock()

	h.importStatusCache[taskID] = response
	h.cacheExpiry[taskID] = time.Now().Add(h.cacheTTL)

	log.Printf("[DEBUG] Caching import status, TaskID: %s, Expiry: %s", taskID, h.cacheExpiry[taskID].Format("15:04:05"))
}

// clearExpiredCache 清理过期缓存
func (h *Handler) clearExpiredCache() {
	h.cacheMutex.Lock()
	defer h.cacheMutex.Unlock()

	now := time.Now()
	for taskID, expiry := range h.cacheExpiry {
		if now.After(expiry) {
			delete(h.importStatusCache, taskID)
			delete(h.cacheExpiry, taskID)
			log.Printf("[DEBUG] Clearing expired cache, TaskID: %s", taskID)
		}
	}
}

// TestConnection 测试系统连接
func (h *Handler) TestConnection(c *gin.Context) {
	var req models.ConnectionTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Invalid request: " + err.Error(),
		})
		return
	}

	// 调试日志：记录接收到的请求
	log.Printf("[TestConnection DEBUG] received request: %+v", req)

	// 测试连接
	resp, err := h.connService.TestConnection(&req.Connection)
	if err != nil {
		// 调试日志：记录连接服务错误
		log.Printf("[TestConnection DEBUG] connService.TestConnection error: %v", err)
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Connection test failed: " + err.Error(),
		})
		return
	}

	// 调试日志：记录连接服务返回的完整响应
	log.Printf("[TestConnection DEBUG] connService.TestConnection response: %+v", resp)

	// 构建最终响应
	finalResponse := models.APIResponse{
		Success: true,
		Message: "Connection test completed",
		Data:    resp,
	}

	// 调试日志：记录最终发送给前端的响应
	log.Printf("[TestConnection DEBUG] final APIResponse: %+v", finalResponse)

	c.JSON(http.StatusOK, finalResponse)
}

// StartOnlineMigration 开始在线迁移
func (h *Handler) StartOnlineMigration(c *gin.Context) {
	log.Printf("[DEBUG] Received online migration request")

	// 读取原始请求体用于调试
	body, _ := c.GetRawData()
	log.Printf("[DEBUG] Raw request body: %s", string(body))

	// 重新设置请求体，因为GetRawData会消耗它
	c.Request.Body = ioutil.NopCloser(strings.NewReader(string(body)))

	var req models.OnlineMigrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[ERROR] Failed to parse request body: %v", err)
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Invalid request: " + err.Error(),
		})
		return
	}

	log.Printf("[DEBUG] Parsed request: Source=%s:%d, Target=%s:%d",
		req.Source.Host, req.Source.Port, req.Target.Host, req.Target.Port)

	// 开始迁移
	task, err := h.migrationService.StartOnlineMigration(&req)
	if err != nil {
		log.Printf("[ERROR] Failed to start online migration: %v", err)
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to start online migration: " + err.Error(),
		})
		return
	}

	log.Printf("[DEBUG] Online migration task created: %s", task.ID)

	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Online migration started",
		Data: map[string]interface{}{
			"task_id": task.ID,
			"status":  task.Status,
		},
	})
}

// StartDataExport 开始数据导出 - 直接下载
func (h *Handler) StartDataExport(c *gin.Context) {
	var req models.DataExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "请求参数无效: " + err.Error(),
		})
		return
	}

	// 直接生成并返回压缩包
	filePath, err := h.migrationService.CreateDirectExport(&req.Source)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "生成导出文件失败: " + err.Error(),
		})
		return
	}

	// 设置响应头
	c.Header("Content-Type", "application/gzip")
	c.Header("Content-Disposition", "attachment; filename=\"casaos-export.tar.gz\"")
	c.Header("Content-Transfer-Encoding", "binary")

	// 发送文件
	c.File(filePath)

	// 清理临时文件
	go func() {
		time.Sleep(5 * time.Second)
		os.Remove(filePath)
	}()
}

// ExportDownload 直接导出并下载压缩包
func (h *Handler) ExportDownload(c *gin.Context) {
	var req struct {
		SourceConnection models.SystemConnection `json:"source_connection"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "请求参数无效: " + err.Error(),
		})
		return
	}

	// 直接生成并返回压缩包
	filePath, err := h.migrationService.CreateDirectExport(&req.SourceConnection)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "生成导出文件失败: " + err.Error(),
		})
		return
	}

	// 设置响应头
	c.Header("Content-Type", "application/gzip")
	c.Header("Content-Disposition", "attachment; filename=\"casaos-export.tar.gz\"")
	c.Header("Content-Transfer-Encoding", "binary")

	// 发送文件
	c.File(filePath)

	// 清理临时文件
	go func() {
		time.Sleep(5 * time.Second)
		os.Remove(filePath)
	}()
}

// StartDataImport 开始数据导入
func (h *Handler) StartDataImport(c *gin.Context) {
	var req models.DataImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[ERROR] StartDataImport - failed to bind request: %v", err)
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Invalid request: " + err.Error(),
		})
		return
	}

	// 调试日志：记录接收到的请求
	log.Printf("[DEBUG] StartDataImport - request received: Target={Host:%s, Port:%d, Username:%s, Type:%s}, Options=%+v",
		req.Target.Host, req.Target.Port, req.Target.Username, req.Target.Type, req.ImportOptions)

	// 修复系统类型大小写问题
	if strings.ToLower(req.Target.Type) == "casaos" {
		req.Target.Type = models.SystemTypeCasaOS
	} else if strings.ToLower(req.Target.Type) == "zimaos" {
		req.Target.Type = models.SystemTypeZimaOS
	}

	// 开始导入
	task, err := h.migrationService.StartDataImport(&req)
	if err != nil {
		log.Printf("[ERROR] StartDataImport - failed to start: %v", err)
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to start data import: " + err.Error(),
		})
		return
	}

	log.Printf("[INFO] StartDataImport - task started, TaskID: %s", task.ID)
	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Data import started",
		Data: map[string]interface{}{
			"task_id": task.ID,
			"status":  task.Status,
		},
	})
}

// GetTaskStatus 获取任务状态
func (h *Handler) GetTaskStatus(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Task ID is required",
		})
		return
	}

	// 获取任务
	task, err := h.taskService.GetTask(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.APIResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// 创建任务副本，不返回敏感信息
	taskCopy := *task
	if taskCopy.Source != nil {
		sourceCopy := *taskCopy.Source
		sourceCopy.Password = "" // 不返回密码
		sourceCopy.Token = ""    // 不返回令牌
		taskCopy.Source = &sourceCopy
	}
	if taskCopy.Target != nil {
		targetCopy := *taskCopy.Target
		targetCopy.Password = "" // 不返回密码
		targetCopy.Token = ""    // 不返回令牌
		taskCopy.Target = &targetCopy
	}

	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Task status retrieved",
		Data:    &taskCopy,
	})
}

// ListTasks 获取任务列表
func (h *Handler) ListTasks(c *gin.Context) {
	// 获取查询参数
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")
	status := c.Query("status")
	taskType := c.Query("type")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100 // 限制最大返回数量
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// 获取所有任务
	allTasks := h.taskService.ListTasks()

	// 过滤任务
	var filteredTasks []*models.MigrationTask
	for _, task := range allTasks {
		// 状态过滤
		if status != "" && task.Status != status {
			continue
		}
		// 类型过滤
		if taskType != "" && task.Type != taskType {
			continue
		}
		filteredTasks = append(filteredTasks, task)
	}

	// 分页
	total := len(filteredTasks)
	start := offset
	end := offset + limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	var pagedTasks []*models.MigrationTask
	if start < end {
		pagedTasks = filteredTasks[start:end]
	} else {
		pagedTasks = []*models.MigrationTask{}
	}

	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Task list retrieved",
		Data: map[string]interface{}{
			"tasks":  pagedTasks,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// DeleteTask 删除任务
func (h *Handler) DeleteTask(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Task ID is required",
		})
		return
	}

	// 检查任务是否存在
	task, err := h.taskService.GetTask(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.APIResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// 检查任务状态，运行中的任务不能删除
	if task.Status == string(models.TaskStatusRunning) {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Running tasks cannot be deleted",
		})
		return
	}

	// 删除任务
	if err := h.taskService.DeleteTask(taskID); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to delete task: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Task deleted successfully",
	})
}

// GetTaskLogs 获取任务日志
func (h *Handler) GetTaskLogs(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Task ID is required",
		})
		return
	}

	// 检查任务是否存在
	if _, err := h.taskService.GetTask(taskID); err != nil {
		c.JSON(http.StatusNotFound, models.APIResponse{
			Success: false,
			Message: "Task not found",
		})
		return
	}

	// 获取任务日志
	logs, err := h.taskService.GetTaskLogs(taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to get task logs: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Task logs retrieved",
		Data:    logs,
	})
}

// HandleWebSocket 处理WebSocket连接
func (h *Handler) HandleWebSocket(c *gin.Context) {
	taskID := c.Query("task_id")
	if taskID == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// 检查任务是否存在
	_, err := h.taskService.GetTask(taskID)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// 处理WebSocket连接
	h.wsManager.HandleWebSocket(c)

	// 移除不必要的测试消息发送逻辑
	// 当WebSocket连接建立时，不需要发送测试消息
	// 任务状态和日志会通过正常的业务流程发送
}

// GetSystemInfo 获取系统信息
func (h *Handler) GetSystemInfo(c *gin.Context) {
	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "System info",
		Data: map[string]interface{}{
			"name":        "CasaOS to ZimaOS Migration Tool",
			"version":     "1.0.0",
			"description": "A tool for migrating from CasaOS to ZimaOS",
			"features": []string{
				"Online migration",
				"Offline export/import",
				"Live status updates",
				"Web UI",
			},
			"supported_systems": []string{
				"CasaOS",
				"ZimaOS",
			},
		},
	})
}

// HealthCheck 健康检查
func (h *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Service is healthy",
		Data: map[string]interface{}{
			"status":    "healthy",
			"timestamp": "2024-01-01T00:00:00Z",
			"uptime":    "running",
		},
	})
}

// TestWebSocket 测试WebSocket消息发送
func (h *Handler) TestWebSocket(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Task ID is required",
		})
		return
	}

	// 发送测试日志消息
	log.Printf("[DEBUG] Sending WebSocket test message to task: %s", taskID)
	h.taskService.AddTaskLog(taskID, models.LogLevelInfo, "This is a WebSocket test message")
	h.taskService.AddTaskLog(taskID, models.LogLevelError, "This is an error level test message")
	h.taskService.AddTaskLog(taskID, models.LogLevelWarning, "This is a warning level test message")

	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "WebSocket test message sent",
		Data: map[string]interface{}{
			"task_id":       taskID,
			"sent_messages": 3,
		},
	})
}

// CreateTestTask 创建测试任务
func (h *Handler) CreateTestTask(c *gin.Context) {
	// 创建一个测试任务
	task := h.taskService.CreateTask(
		models.TaskTypeTest,
		&models.SystemConnection{Host: "test-source", Port: 22, Username: "test"},
		&models.SystemConnection{Host: "test-target", Port: 22, Username: "test"},
		map[string]interface{}{"test": true},
	)

	// 添加一些初始日志
	h.taskService.AddTaskLog(task.ID, models.LogLevelInfo, "Test task created")
	h.taskService.AddTaskLog(task.ID, models.LogLevelInfo, "Preparing for WebSocket test")

	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Test task created successfully",
		Data: map[string]interface{}{
			"task_id":   task.ID,
			"task_type": task.Type,
			"status":    task.Status,
		},
	})
}

// GetImportStatus 获取导入状态列表
func (h *Handler) GetImportStatus(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Task ID is required",
		})
		return
	}

	// 获取任务
	task, err := h.taskService.GetTask(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, models.APIResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	// 检查任务类型是否为导入相关
	if task.Type != models.TaskTypeImport && task.Type != models.TaskTypeOnline && task.Type != models.TaskTypeOfflineImport {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task type does not support import status query"})
		return
	}

	// 仅当任务不在运行中时才使用缓存
	if task.Status != string(models.TaskStatusRunning) {
		if cachedResponse, ok := h.getCachedImportStatus(taskID); ok {
			log.Printf("[DEBUG] GetImportStatus - Using cached data, TaskID: %s", taskID)
			c.JSON(http.StatusOK, models.APIResponse{
				Success: true,
				Message: "Import status retrieved (cached)",
				Data:    cachedResponse,
			})
			return
		}
	}

	// 添加详细的调试日志
	log.Printf("[DEBUG] GetImportStatus - TaskID: %s, TaskType: %s, TaskStatus: %s", taskID, task.Type, task.Status)
	log.Printf("[DEBUG] GetImportStatus - Task.Result is nil: %v", task.Result == nil)
	if task.Result != nil {
		log.Printf("[DEBUG] GetImportStatus - Task.Result keys: %v", getMapKeys(task.Result))
	}

	// 从任务结果中获取应用状态列表
	apps := make([]models.AppImportStatus, 0) // 保证序列化为 [] 而不是 null
	var summary models.ImportSummary

	if task.Result != nil {
		// 处理apps数据
		if appsData, ok := task.Result["apps"]; ok {
			// 首先尝试直接转换为[]models.AppImportStatus
			if appsSlice, ok := appsData.([]models.AppImportStatus); ok {
				if appsSlice != nil {
					apps = appsSlice
				}
			} else if appsSlice, ok := appsData.([]interface{}); ok {
				// 批量处理，减少日志输出
				for _, appData := range appsSlice {
					if appMap, ok := appData.(map[string]interface{}); ok {
						app := models.AppImportStatus{
							AppName:       getString(appMap, "app_name"),
							HasAppData:    getBool(appMap, "has_app_data"),
							AppDataStatus: getString(appMap, "app_data_status"),
							ComposeStatus: getString(appMap, "compose_status"),
							OverallStatus: getString(appMap, "overall_status"),
							ErrorMessage:  getString(appMap, "error_message"),
						}
						apps = append(apps, app)
					}
				}
			}
		}

		// 处理summary数据
		if summaryData, ok := task.Result["summary"]; ok {
			if summaryMap, ok := summaryData.(map[string]interface{}); ok {
				summary = models.ImportSummary{
					TotalApps:   getInt(summaryMap, "total_apps"),
					SuccessApps: getInt(summaryMap, "success_apps"),
					FailedApps:  getInt(summaryMap, "failed_apps"),
				}
			}
		}
	}

	// 为每个应用生成下载链接
	for i := range apps {
		apps[i].DownloadURL = "/api/tasks/" + taskID + "/download/" + apps[i].AppName
	}

	response := models.ImportStatusResponse{
		TaskID:   taskID,
		Status:   task.Status,
		Progress: task.Progress,
		Apps:     apps,
		Summary:  summary,
	}

	// 仅当任务不在运行中时才缓存结果
	if task.Status != string(models.TaskStatusRunning) {
		h.cacheImportStatus(taskID, response)
	}

	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Import status retrieved",
		Data:    response,
	})
}

// 辅助函数：安全地从map中获取字符串值
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// 辅助函数：安全地从map中获取布尔值
func getBool(m map[string]interface{}, key string) bool {
	if val, ok := m[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// 辅助函数：安全地从map中获取整数值
func getInt(m map[string]interface{}, key string) int {
	if val, ok := m[key]; ok {
		if i, ok := val.(float64); ok {
			return int(i)
		}
		if i, ok := val.(int); ok {
			return i
		}
	}
	return 0
}

// 辅助函数：获取map的所有键
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// DownloadAppPackage 下载应用压缩包
func (h *Handler) DownloadAppPackage(c *gin.Context) {
	taskID := c.Param("id")
	appName := c.Param("appName")

	if taskID == "" || appName == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Task ID and app name are required",
		})
		return
	}

	// 创建应用压缩包
	packagePath, err := h.migrationService.CreateAppPackage(taskID, appName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create app package: %v", err),
		})
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, models.APIResponse{
			Success: false,
			Message: "Package file not found",
		})
		return
	}

	// 设置响应头
	fileName := fmt.Sprintf("%s_%s.zip", appName, taskID)
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	c.Header("Content-Type", "application/zip")

	// 发送文件
	c.File(packagePath)

	// 可选：下载完成后删除临时文件
	// go func() {
	// 	time.Sleep(5 * time.Minute) // 等待5分钟后删除
	// 	os.Remove(packagePath)
	// }()
}

// DataImportUpload 处理文件上传并启动数据导入
func (h *Handler) DataImportUpload(c *gin.Context) {
	log.Printf("[DEBUG] Received file upload import request")

	// 解析multipart form
	err := c.Request.ParseMultipartForm(500 << 20) // 500MB
	if err != nil {
		log.Printf("[ERROR] Failed to parse multipart form: %v", err)
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Failed to parse upload data: " + err.Error(),
		})
		return
	}

	// 获取上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		log.Printf("[ERROR] Failed to get uploaded file: %v", err)
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Failed to get uploaded file: " + err.Error(),
		})
		return
	}
	defer file.Close()

	log.Printf("[DEBUG] Uploaded file info: Filename=%s, Size=%d", header.Filename, header.Size)

	// 验证文件类型
	fileName := strings.ToLower(header.Filename)
	if !strings.HasSuffix(fileName, ".tar.gz") && !strings.HasSuffix(fileName, ".zip") {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Unsupported file format, please upload .tar.gz or .zip files",
		})
		return
	}

	// 验证文件大小（500MB限制）
	if header.Size > 500*1024*1024 {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "File size exceeds limit (500MB)",
		})
		return
	}

	// 获取目标连接信息
	targetConnectionStr := c.Request.FormValue("target_connection")
	if targetConnectionStr == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Missing target connection information",
		})
		return
	}

	var targetConnection models.SystemConnection
	if err := json.Unmarshal([]byte(targetConnectionStr), &targetConnection); err != nil {
		log.Printf("[ERROR] Failed to parse target connection information: %v", err)
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Failed to parse target connection information: " + err.Error(),
		})
		return
	}

	log.Printf("[DEBUG] Target connection info: %s:%d", targetConnection.Host, targetConnection.Port)

	// 创建临时目录保存上传的文件
	uploadDir := "./uploads"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Printf("[ERROR] Failed to create upload directory: %v", err)
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to create upload directory: " + err.Error(),
		})
		return
	}

	// 生成唯一的文件名
	timestamp := time.Now().Format("20060102_150405")
	fileExt := filepath.Ext(header.Filename)
	savedFileName := fmt.Sprintf("import_%s%s", timestamp, fileExt)
	savedFilePath := filepath.Join(uploadDir, savedFileName)

	// 保存上传的文件
	dstFile, err := os.Create(savedFilePath)
	if err != nil {
		log.Printf("[ERROR] Failed to create target file: %v", err)
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to save uploaded file: " + err.Error(),
		})
		return
	}
	defer dstFile.Close()

	// 复制文件内容
	copiedBytes, err := io.Copy(dstFile, file)
	if err != nil {
		log.Printf("[ERROR] Failed to copy file content: %v", err)
		os.Remove(savedFilePath) // 清理失败的文件
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to save file content: " + err.Error(),
		})
		return
	}

	// 强制刷新文件缓冲区到磁盘
	if err := dstFile.Sync(); err != nil {
		log.Printf("[ERROR] Failed to flush file buffer: %v", err)
		os.Remove(savedFilePath)
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to save file: " + err.Error(),
		})
		return
	}

	// 关闭文件句柄以确保写入完成
	dstFile.Close()

	// 验证文件大小是否正确
	savedFileInfo, err := os.Stat(savedFilePath)
	if err != nil {
		log.Printf("[ERROR] Failed to get saved file info: %v", err)
		os.Remove(savedFilePath)
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to verify saved file: " + err.Error(),
		})
		return
	}

	if savedFileInfo.Size() != copiedBytes || savedFileInfo.Size() != header.Size {
		log.Printf("[ERROR] File size mismatch: Original=%d, Copied=%d, Saved=%d", header.Size, copiedBytes, savedFileInfo.Size())
		os.Remove(savedFilePath)
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "File save incomplete, please re-upload",
		})
		return
	}

	log.Printf("[DEBUG] File saved successfully: %s, Size verified: %d bytes", savedFilePath, savedFileInfo.Size())

	// 验证上传的文件格式（根据文件内容而非扩展名）
	actualFormat, err := detectFileFormat(savedFilePath)
	if err != nil {
		log.Printf("[ERROR] Failed to detect file format: %v", err)
		os.Remove(savedFilePath) // 清理无效文件
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: "Failed to detect file format: " + err.Error(),
		})
		return
	}

	log.Printf("[DEBUG] Detected file format: %s", actualFormat)

	// 验证文件格式是否支持
	if actualFormat != "gzip" && actualFormat != "zip" {
		log.Printf("[ERROR] Unsupported file format: %s", actualFormat)
		os.Remove(savedFilePath) // 清理无效文件
		c.JSON(http.StatusBadRequest, models.APIResponse{
			Success: false,
			Message: fmt.Sprintf("Unsupported file format: %s, please upload gzip or zip format files", actualFormat),
		})
		return
	}

	log.Printf("[DEBUG] File format verified: %s", actualFormat)

	// 如果是gzip文件，进行额外的完整性验证
	if actualFormat == "gzip" {
		if err := validateGzipFile(savedFilePath); err != nil {
			log.Printf("[ERROR] gzip file validation failed: %v", err)
			os.Remove(savedFilePath)
			c.JSON(http.StatusBadRequest, models.APIResponse{
				Success: false,
				Message: "Uploaded gzip file is corrupted or incomplete: " + err.Error(),
			})
			return
		}
		log.Printf("[DEBUG] gzip file integrity verified")
	}

	// 创建数据导入请求
	importRequest := &models.DataImportRequest{
		Target: targetConnection,
		ImportOptions: map[string]interface{}{
			"import_file": savedFilePath,
		},
	}

	// 启动数据导入任务
	task, err := h.migrationService.StartDataImport(importRequest)
	if err != nil {
		log.Printf("[ERROR] Failed to start data import task: %v", err)
		os.Remove(savedFilePath) // 清理上传的文件
		c.JSON(http.StatusInternalServerError, models.APIResponse{
			Success: false,
			Message: "Failed to start data import task: " + err.Error(),
		})
		return
	}

	log.Printf("[DEBUG] Data import task created: %s", task.ID)

	// 返回成功响应
	c.JSON(http.StatusOK, models.APIResponse{
		Success: true,
		Message: "File uploaded successfully, data import task started",
		Data: map[string]interface{}{
			"task_id": task.ID,
			"status":  task.Status,
		},
	})

	// 异步清理上传的文件（任务完成后）
	go func() {
		// 等待任务完成或失败后清理文件
		for {
			time.Sleep(30 * time.Second)
			currentTask, err := h.taskService.GetTask(task.ID)
			if err != nil {
				break
			}
			if currentTask.Status == string(models.TaskStatusCompleted) ||
				currentTask.Status == string(models.TaskStatusFailed) {
				os.Remove(savedFilePath)
				log.Printf("[DEBUG] Cleaning up uploaded file: %s", savedFilePath)
				break
			}
		}
	}()
}

// detectFileFormat 根据文件魔数检测文件格式
func detectFileFormat(filePath string) (string, error) {
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

	log.Printf("[DEBUG] File first %d bytes: %v", n, buf[:n])

	// 检测ZIP格式 (PK signature: 0x504B)
	if n >= 2 && buf[0] == 0x50 && buf[1] == 0x4B {
		return "zip", nil
	}

	// 检测GZIP格式 (magic number: 0x1F8B)
	if n >= 2 && buf[0] == 0x1F && buf[1] == 0x8B {
		return "gzip", nil
	}

	// 如果都不匹配，返回未知格式
	return "unknown", nil
}

// validateGzipFile 验证文件是否为有效的gzip格式
func validateGzipFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("Failed to open file: %v", err)
	}
	defer file.Close()

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("Failed to get file info: %v", err)
	}

	log.Printf("[DEBUG] Validating gzip file: %s, Size: %d bytes", filePath, fileInfo.Size())

	// 检查文件大小
	if fileInfo.Size() < 10 {
		return fmt.Errorf("File too small, not a valid gzip file")
	}

	// 读取并验证gzip文件头
	buf := make([]byte, 10)
	n, err := file.Read(buf)
	if err != nil {
		return fmt.Errorf("Failed to read file header: %v", err)
	}

	log.Printf("[DEBUG] gzip file header first %d bytes: %v", n, buf[:n])

	// 验证gzip魔数
	if n < 2 || buf[0] != 0x1F || buf[1] != 0x8B {
		return fmt.Errorf("Incorrect magic number: %02X %02X, expected: 1F 8B", buf[0], buf[1])
	}

	// 重置文件指针
	_, err = file.Seek(0, 0)
	if err != nil {
		return fmt.Errorf("Failed to reset file pointer: %v", err)
	}

	// 尝试创建gzip reader
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		log.Printf("[ERROR] Failed to create gzip reader: %v", err)
		return fmt.Errorf("Failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	// 尝试读取一些数据以确保文件完整
	testBuf := make([]byte, 1024)
	bytesRead, err := gzReader.Read(testBuf)
	if err != nil && err != io.EOF {
		log.Printf("[ERROR] Failed to read gzip content: %v, bytes read: %d", err, bytesRead)
		return fmt.Errorf("Failed to read gzip content: %v", err)
	}

	log.Printf("[DEBUG] gzip file validation successful, read %d bytes of data", bytesRead)
	return nil
}
