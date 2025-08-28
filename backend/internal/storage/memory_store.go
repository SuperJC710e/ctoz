package storage

import (
	"sync"
	"time"

	"ctoz/backend/internal/models"
)

// MemoryStore 内存存储管理器
type MemoryStore struct {
	// 任务存储
	tasks map[string]*models.MigrationTask
	tasksMutex sync.RWMutex

	// 系统连接存储
	connections map[string]*models.SystemConnection
	connectionsMutex sync.RWMutex

	// 日志存储
	logs map[string][]*models.MigrationLog
	logsMutex sync.RWMutex

	// 下载指令存储
	downloadInstructions map[string]*models.DownloadInstructions
	downloadMutex sync.RWMutex
}

// NewMemoryStore 创建新的内存存储管理器
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tasks:                make(map[string]*models.MigrationTask),
		connections:          make(map[string]*models.SystemConnection),
		logs:                 make(map[string][]*models.MigrationLog),
		downloadInstructions: make(map[string]*models.DownloadInstructions),
	}
}

// Task 相关方法

// SaveTask 保存任务
func (ms *MemoryStore) SaveTask(task *models.MigrationTask) error {
	ms.tasksMutex.Lock()
	defer ms.tasksMutex.Unlock()

	ms.tasks[task.ID] = task
	return nil
}

// GetTask 获取任务
func (ms *MemoryStore) GetTask(taskID string) (*models.MigrationTask, error) {
	ms.tasksMutex.RLock()
	defer ms.tasksMutex.RUnlock()

	task, exists := ms.tasks[taskID]
	if !exists {
		return nil, models.ErrTaskNotFound
	}
	return task, nil
}

// GetAllTasks 获取所有任务
func (ms *MemoryStore) GetAllTasks() ([]*models.MigrationTask, error) {
	ms.tasksMutex.RLock()
	defer ms.tasksMutex.RUnlock()

	tasks := make([]*models.MigrationTask, 0, len(ms.tasks))
	for _, task := range ms.tasks {
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// DeleteTask 删除任务
func (ms *MemoryStore) DeleteTask(taskID string) error {
	ms.tasksMutex.Lock()
	defer ms.tasksMutex.Unlock()

	if _, exists := ms.tasks[taskID]; !exists {
		return models.ErrTaskNotFound
	}

	delete(ms.tasks, taskID)
	// 同时删除相关日志
	ms.logsMutex.Lock()
	delete(ms.logs, taskID)
	ms.logsMutex.Unlock()

	return nil
}

// UpdateTaskStatus 更新任务状态
func (ms *MemoryStore) UpdateTaskStatus(taskID string, status string) error {
	ms.tasksMutex.Lock()
	defer ms.tasksMutex.Unlock()

	task, exists := ms.tasks[taskID]
	if !exists {
		return models.ErrTaskNotFound
	}

	task.Status = status
	task.UpdatedAt = time.Now()
	return nil
}

// UpdateTaskProgress 更新任务进度
func (ms *MemoryStore) UpdateTaskProgress(taskID string, progress int) error {
	ms.tasksMutex.Lock()
	defer ms.tasksMutex.Unlock()

	task, exists := ms.tasks[taskID]
	if !exists {
		return models.ErrTaskNotFound
	}

	task.Progress = progress
	task.UpdatedAt = time.Now()
	return nil
}

// SetTaskResult 设置任务结果
func (ms *MemoryStore) SetTaskResult(taskID string, result interface{}) error {
	ms.tasksMutex.Lock()
	defer ms.tasksMutex.Unlock()

	task, exists := ms.tasks[taskID]
	if !exists {
		return models.ErrTaskNotFound
	}

	// 类型断言为map[string]interface{}
	if resultMap, ok := result.(map[string]interface{}); ok {
		task.Result = resultMap
	} else {
		// 如果不是map类型，创建一个包装的map
		task.Result = map[string]interface{}{
			"data": result,
		}
	}
	task.UpdatedAt = time.Now()
	return nil
}

// Connection 相关方法

// SaveConnection 保存系统连接
func (ms *MemoryStore) SaveConnection(conn *models.SystemConnection) error {
	ms.connectionsMutex.Lock()
	defer ms.connectionsMutex.Unlock()

	ms.connections[conn.ID] = conn
	return nil
}

// GetConnection 获取系统连接
func (ms *MemoryStore) GetConnection(connID string) (*models.SystemConnection, error) {
	ms.connectionsMutex.RLock()
	defer ms.connectionsMutex.RUnlock()

	conn, exists := ms.connections[connID]
	if !exists {
		return nil, models.ErrConnectionNotFound
	}
	return conn, nil
}

// GetAllConnections 获取所有连接
func (ms *MemoryStore) GetAllConnections() ([]*models.SystemConnection, error) {
	ms.connectionsMutex.RLock()
	defer ms.connectionsMutex.RUnlock()

	conns := make([]*models.SystemConnection, 0, len(ms.connections))
	for _, conn := range ms.connections {
		conns = append(conns, conn)
	}
	return conns, nil
}

// DeleteConnection 删除连接
func (ms *MemoryStore) DeleteConnection(connID string) error {
	ms.connectionsMutex.Lock()
	defer ms.connectionsMutex.Unlock()

	if _, exists := ms.connections[connID]; !exists {
		return models.ErrConnectionNotFound
	}

	delete(ms.connections, connID)
	return nil
}

// Log 相关方法

// AddLog 添加日志
func (ms *MemoryStore) AddLog(taskID string, log *models.MigrationLog) error {
	ms.logsMutex.Lock()
	defer ms.logsMutex.Unlock()

	if ms.logs[taskID] == nil {
		ms.logs[taskID] = make([]*models.MigrationLog, 0)
	}

	ms.logs[taskID] = append(ms.logs[taskID], log)
	return nil
}

// GetLogs 获取任务日志
func (ms *MemoryStore) GetLogs(taskID string) ([]*models.MigrationLog, error) {
	ms.logsMutex.RLock()
	defer ms.logsMutex.RUnlock()

	logs, exists := ms.logs[taskID]
	if !exists {
		return []*models.MigrationLog{}, nil
	}
	return logs, nil
}

// ClearLogs 清除任务日志
func (ms *MemoryStore) ClearLogs(taskID string) error {
	ms.logsMutex.Lock()
	defer ms.logsMutex.Unlock()

	delete(ms.logs, taskID)
	return nil
}

// DownloadInstructions 相关方法

// SaveDownloadInstructions 保存下载指令
func (ms *MemoryStore) SaveDownloadInstructions(taskID string, instructions *models.DownloadInstructions) error {
	ms.downloadMutex.Lock()
	defer ms.downloadMutex.Unlock()

	ms.downloadInstructions[taskID] = instructions
	return nil
}

// GetDownloadInstructions 获取下载指令
func (ms *MemoryStore) GetDownloadInstructions(taskID string) (*models.DownloadInstructions, error) {
	ms.downloadMutex.RLock()
	defer ms.downloadMutex.RUnlock()

	instructions, exists := ms.downloadInstructions[taskID]
	if !exists {
		return nil, models.ErrDownloadInstructionsNotFound
	}
	return instructions, nil
}

// DeleteDownloadInstructions 删除下载指令
func (ms *MemoryStore) DeleteDownloadInstructions(taskID string) error {
	ms.downloadMutex.Lock()
	defer ms.downloadMutex.Unlock()

	delete(ms.downloadInstructions, taskID)
	return nil
}

// 清理相关方法

// CleanupExpiredTasks 清理过期任务
func (ms *MemoryStore) CleanupExpiredTasks(expireDuration time.Duration) error {
	ms.tasksMutex.Lock()
	defer ms.tasksMutex.Unlock()

	now := time.Now()
	expiredTasks := make([]string, 0)

	for taskID, task := range ms.tasks {
		if now.Sub(task.UpdatedAt) > expireDuration {
			expiredTasks = append(expiredTasks, taskID)
		}
	}

	// 删除过期任务
	for _, taskID := range expiredTasks {
		delete(ms.tasks, taskID)
		// 同时删除相关日志和下载指令
		ms.logsMutex.Lock()
		delete(ms.logs, taskID)
		ms.logsMutex.Unlock()

		ms.downloadMutex.Lock()
		delete(ms.downloadInstructions, taskID)
		ms.downloadMutex.Unlock()
	}

	return nil
}

// GetStats 获取存储统计信息
func (ms *MemoryStore) GetStats() map[string]interface{} {
	ms.tasksMutex.RLock()
	ms.connectionsMutex.RLock()
	ms.logsMutex.RLock()
	ms.downloadMutex.RLock()

	defer ms.tasksMutex.RUnlock()
	defer ms.connectionsMutex.RUnlock()
	defer ms.logsMutex.RUnlock()
	defer ms.downloadMutex.RUnlock()

	totalLogs := 0
	for _, logs := range ms.logs {
		totalLogs += len(logs)
	}

	return map[string]interface{}{
		"tasks":                len(ms.tasks),
		"connections":          len(ms.connections),
		"total_logs":           totalLogs,
		"download_instructions": len(ms.downloadInstructions),
	}
}