package services

import (
	"fmt"
	"time"

	"ctoz/backend/internal/models"
	"ctoz/backend/internal/storage"
	"ctoz/backend/internal/websocket"

	"github.com/google/uuid"
)

// TaskService 任务服务
type TaskService struct {
	store     *storage.MemoryStore
	wsManager *websocket.Manager
}

// NewTaskService 创建新的任务服务
func NewTaskService(wsManager *websocket.Manager) *TaskService {
	return &TaskService{
		store:     storage.NewMemoryStore(),
		wsManager: wsManager,
	}
}

// CreateTask 创建新任务
func (s *TaskService) CreateTask(taskType string, source, target *models.SystemConnection, options map[string]interface{}) *models.MigrationTask {
	task := &models.MigrationTask{
		ID:        uuid.New().String(),
		Type:      taskType,
		Status:    string(models.TaskStatusPending),
		Progress:  0,
		Source:    source,
		Target:    target,
		Options:   options,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	s.store.SaveTask(task)
	return task
}

// GetTask 获取任务
func (s *TaskService) GetTask(taskID string) (*models.MigrationTask, error) {
	return s.store.GetTask(taskID)
}

// UpdateTaskStatus 更新任务状态
func (s *TaskService) UpdateTaskStatus(taskID string, status string) error {
	err := s.store.UpdateTaskStatus(taskID, status)
	if err != nil {
		return err
	}

	// 发送WebSocket消息
	if s.wsManager != nil {
		switch status {
		case string(models.TaskStatusRunning):
			s.wsManager.SendTaskStatus(taskID, models.TaskStatusRunning, "Task started")
		case string(models.TaskStatusCompleted):
			s.wsManager.SendTaskStatus(taskID, models.TaskStatusCompleted, "Task completed")
		case string(models.TaskStatusFailed):
			s.wsManager.SendTaskStatus(taskID, models.TaskStatusFailed, "Task failed")
		}
	}

	return nil
}

// UpdateTaskProgress 更新任务进度
func (s *TaskService) UpdateTaskProgress(taskID string, progress int) error {
	err := s.store.UpdateTaskProgress(taskID, progress)
	if err != nil {
		return err
	}

	// 发送WebSocket消息
	if s.wsManager != nil {
		s.wsManager.SendProgress(taskID, progress, "progress", "")
	}

	return nil
}

// AddTaskLog 添加任务日志
func (s *TaskService) AddTaskLog(taskID string, level string, message string) error {
	log := &models.MigrationLog{
		Level:     level,
		Message:   message,
		Timestamp: time.Now(),
	}

	err := s.store.AddLog(taskID, log)
	if err != nil {
		return err
	}

	// 发送WebSocket消息
	if s.wsManager != nil {
		s.wsManager.SendLog(taskID, level, message)
	}

	return nil
}

// SetTaskResult 设置任务结果
func (s *TaskService) SetTaskResult(taskID string, result interface{}) error {
	return s.store.SetTaskResult(taskID, result)
}

// ListTasks 列出任务
func (s *TaskService) ListTasks() []*models.MigrationTask {
	allTasks, err := s.store.GetAllTasks()
	if err != nil {
		return []*models.MigrationTask{}
	}
	return allTasks
}

// DeleteTask 删除任务
func (s *TaskService) DeleteTask(taskID string) error {
	return s.store.DeleteTask(taskID)
}

// GetTaskLogs 获取任务日志
func (s *TaskService) GetTaskLogs(taskID string) ([]*models.MigrationLog, error) {
	return s.store.GetLogs(taskID)
}

// CleanupExpiredTasks 清理过期任务
func (s *TaskService) CleanupExpiredTasks(expireDuration time.Duration) error {
	return s.store.CleanupExpiredTasks(expireDuration)
}

// GetStats 获取任务统计信息
func (s *TaskService) GetStats() map[string]interface{} {
	return s.store.GetStats()
}

// ExecuteStep 执行步骤并发送WebSocket消息
func (s *TaskService) ExecuteStep(taskID, step string, fn func() error) error {
	// Send step start message
	s.wsManager.SendStepStart(taskID, step, "Step started")
	s.AddTaskLog(taskID, models.LogLevelInfo, fmt.Sprintf("Step started: %s", step))

	// 执行步骤
	err := fn()
	if err != nil {
		// Send step error message
		s.wsManager.SendStepError(taskID, step, "Step failed", err.Error())
		s.AddTaskLog(taskID, models.LogLevelError, fmt.Sprintf("Step failed: %s - %v", step, err))
		return err
	}

	// Send step completion message
	s.wsManager.SendStepComplete(taskID, step, "Step completed")
	s.AddTaskLog(taskID, models.LogLevelInfo, fmt.Sprintf("Step completed: %s", step))
	return nil
}

// ExecuteStepWithProgress 执行带进度的步骤
func (s *TaskService) ExecuteStepWithProgress(taskID, step string, fn func(progressCallback func(int, string)) error) error {
	// Send step start message
	s.wsManager.SendStepStart(taskID, step, "Step started")
	s.AddTaskLog(taskID, models.LogLevelInfo, fmt.Sprintf("Step started: %s", step))

	// 进度回调函数
	progressCallback := func(progress int, message string) {
		s.wsManager.SendProgress(taskID, progress, step, message)
		s.UpdateTaskProgress(taskID, progress)
		if message != "" {
			s.AddTaskLog(taskID, models.LogLevelInfo, message)
		}
	}

	// 执行步骤
	err := fn(progressCallback)
	if err != nil {
		// Send step error message
		s.wsManager.SendStepError(taskID, step, "Step failed", err.Error())
		s.AddTaskLog(taskID, models.LogLevelError, fmt.Sprintf("Step failed: %s - %v", step, err))
		return err
	}

	// Send step completion message
	s.wsManager.SendStepComplete(taskID, step, "Step completed")
	s.AddTaskLog(taskID, models.LogLevelInfo, fmt.Sprintf("Step completed: %s", step))
	return nil
}
