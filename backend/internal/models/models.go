package models

import (
	"errors"
	"time"
)

// 错误定义
var (
	ErrTaskNotFound                 = errors.New("task not found")
	ErrConnectionNotFound           = errors.New("connection not found")
	ErrDownloadInstructionsNotFound = errors.New("download instructions not found")
	ErrInvalidTaskStatus            = errors.New("invalid task status")
	ErrInvalidSystemType            = errors.New("invalid system type")
	ErrConnectionFailed             = errors.New("connection failed")
	ErrMigrationFailed              = errors.New("migration failed")
	ErrExportFailed                 = errors.New("export failed")
	ErrImportFailed                 = errors.New("import failed")
)

// MigrationTask 迁移任务结构
type MigrationTask struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`   // online/offline-export/offline-import
	Status    string                 `json:"status"` // pending/running/completed/failed
	Progress  int                    `json:"progress"`
	Source    *SystemConnection      `json:"source,omitempty"`
	Target    *SystemConnection      `json:"target,omitempty"`
	Options   map[string]interface{} `json:"options"`
	Logs      []MigrationLog         `json:"logs"`
	Result    map[string]interface{} `json:"result,omitempty"`
	CreatedAt time.Time              `json:"created_at" time_format:"2006-01-02T15:04:05Z07:00"`
	UpdatedAt time.Time              `json:"updated_at" time_format:"2006-01-02T15:04:05Z07:00"`
}

// SystemConnection 系统连接信息
type SystemConnection struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"` // 不返回给前端
	Token    string `json:"token,omitempty"`
	Type     string `json:"type"` // casaos/zimaos
	Verified bool   `json:"verified"`
}

// MigrationLog 迁移日志
type MigrationLog struct {
	Level     string    `json:"level"` // info/warning/error
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp" time_format:"2006-01-02T15:04:05Z07:00"`
}

// WSMessage WebSocket消息结构
type WSMessage struct {
	Type      string                 `json:"type"` // step_start/step_progress/step_complete/step_error/console_output
	Step      string                 `json:"step,omitempty"`
	Progress  int                    `json:"progress,omitempty"`
	Message   string                 `json:"message,omitempty"`
	Result    string                 `json:"result,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Timestamp time.Time              `json:"timestamp" time_format:"2006-01-02T15:04:05Z07:00"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// DownloadInstructions 下载指引信息
type DownloadInstructions struct {
	CasaOSHost    string   `json:"casaos_host"`
	DownloadPaths []string `json:"download_paths"`
	Instructions  []string `json:"instructions"`
	PackageName   string   `json:"package_name"`
	Message       string   `json:"message"`
	FilePath      string   `json:"file_path"`
	DownloadURL   string   `json:"download_url"`
}

// APIResponse 通用API响应结构
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// TestConnectionRequest 连接测试请求
type TestConnectionRequest struct {
	Host       string `json:"host" binding:"required"`
	Username   string `json:"username" binding:"required"`
	Password   string `json:"password" binding:"required"`
	SystemType string `json:"system_type" binding:"required,oneof=casaos zimaos"`
}

// ConnectionTestRequest 连接测试请求（包装结构）
type ConnectionTestRequest struct {
	Connection SystemConnection `json:"connection" binding:"required"`
}

// OnlineMigrationRequest 在线迁移请求
type OnlineMigrationRequest struct {
	Source           SystemConnection       `json:"source" binding:"required"`
	Target           SystemConnection       `json:"target" binding:"required"`
	MigrationOptions map[string]interface{} `json:"migrationOptions"`
}

// DataExportRequest 数据导出请求
type DataExportRequest struct {
	Source        SystemConnection       `json:"source" binding:"required"`
	ExportOptions map[string]interface{} `json:"export_options" binding:"required"`
}

// DataImportRequest 数据导入请求
type DataImportRequest struct {
	Target        SystemConnection       `json:"target" binding:"required"`
	ImportOptions map[string]interface{} `json:"import_options"`
	// PackageFile 通过multipart/form-data上传
}

// ConnectionTestResponse 连接测试响应
type ConnectionTestResponse struct {
	Success    bool                   `json:"success"`
	Message    string                 `json:"message"`
	SystemInfo map[string]interface{} `json:"system_info,omitempty"`
}

// TaskResponse 任务响应
type TaskResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// ExportDataResponse 数据导出响应
type ExportDataResponse struct {
	TaskID               string                `json:"task_id"`
	DownloadInstructions *DownloadInstructions `json:"download_instructions"`
}

// TaskStatus 任务状态类型
type TaskStatus string

// 任务状态常量
const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// 任务类型常量
const (
	TaskTypeOnline        = "online"
	TaskTypeOfflineExport = "offline-export"
	TaskTypeOfflineImport = "offline-import"
	TaskTypeExport        = "export"
	TaskTypeImport        = "import"
	TaskTypeTest          = "test"
)

// 系统类型常量
const (
	SystemTypeCasaOS = "casaos"
	SystemTypeZimaOS = "zimaos"
)

// WebSocket消息类型常量
const (
	WSMsgTypeStepStart     = "step_start"
	WSMsgTypeStepProgress  = "step_progress"
	WSMsgTypeStepComplete  = "step_complete"
	WSMsgTypeStepError     = "step_error"
	WSMsgTypeConsoleOutput = "console_output"
)

// 日志级别常量
const (
	LogLevelInfo    = "info"
	LogLevelWarning = "warning"
	LogLevelError   = "error"
)

// AppImportStatus 应用导入状态
type AppImportStatus struct {
	AppName       string `json:"app_name"`
	HasAppData    bool   `json:"has_app_data"`
	AppDataStatus string `json:"app_data_status"` // success/failed/skipped
	ComposeStatus string `json:"compose_status"`  // success/failed
	OverallStatus string `json:"overall_status"`  // success/failed
	ErrorMessage  string `json:"error_message,omitempty"`
	DownloadURL   string `json:"download_url,omitempty"`
}

// ImportStatusResponse 导入状态响应
type ImportStatusResponse struct {
	TaskID   string            `json:"task_id"`
	Status   string            `json:"status"`
	Progress int               `json:"progress"`
	Apps     []AppImportStatus `json:"apps"`
	Summary  ImportSummary     `json:"summary"`
}

// ImportSummary 导入摘要
type ImportSummary struct {
	TotalApps   int `json:"total_apps"`
	SuccessApps int `json:"success_apps"`
	FailedApps  int `json:"failed_apps"`
}

// 应用状态常量
const (
	AppStatusSuccess = "success"
	AppStatusFailed  = "failed"
	AppStatusSkipped = "skipped"
)
