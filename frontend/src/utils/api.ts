import {
  APIResponse,
  ConnectionTestRequest,
  ConnectionTestResponse,
  OnlineMigrationRequest,
  DataExportRequest,
  DataImportRequest,
  TaskResponse,
  ExportDataResponse,
  MigrationTask,
  SystemInfo,
  ImportStatusResponse
} from '../types'

const API_BASE_URL = 'http://localhost:8080/api'

class ApiClient {
  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<APIResponse<T>> {
    const url = `${API_BASE_URL}${endpoint}`
    
    const config: RequestInit = {
      headers: {
        'Content-Type': 'application/json',
        ...options.headers,
      },
      ...options,
    }

    try {
      const response = await fetch(url, config)
      const data = await response.json()
      
      if (!response.ok) {
        throw new Error(data.error || `HTTP error! status: ${response.status}`)
      }
      
      return data
    } catch (error) {
      console.error('API request failed:', error)
      throw error
    }
  }

  // 连接测试
  async testConnection(request: ConnectionTestRequest): Promise<APIResponse<ConnectionTestResponse>> {
    return this.request<ConnectionTestResponse>('/test-connection', {
      method: 'POST',
      body: JSON.stringify(request),
    })
  }

  // 开始在线迁移
  async startOnlineMigration(request: OnlineMigrationRequest): Promise<APIResponse<TaskResponse>> {
    return this.request<TaskResponse>('/online-migration', {
      method: 'POST',
      body: JSON.stringify(request),
    })
  }

  // 开始数据导出
  async startDataExport(request: DataExportRequest): Promise<APIResponse<TaskResponse>> {
    return this.request<TaskResponse>('/data-export', {
      method: 'POST',
      body: JSON.stringify(request),
    })
  }

  // 开始数据导入
  async startDataImport(request: DataImportRequest): Promise<APIResponse<TaskResponse>> {
    return this.request<TaskResponse>('/data-import', {
      method: 'POST',
      body: JSON.stringify(request),
    })
  }

  // 获取任务状态
  async getTaskStatus(taskId: string): Promise<APIResponse<MigrationTask>> {
    return this.request<MigrationTask>(`/tasks/${taskId}`)
  }

  // 获取任务列表
  async getTasks(): Promise<APIResponse<MigrationTask[]>> {
    return this.request<MigrationTask[]>('/tasks')
  }

  // 取消任务
  async cancelTask(taskId: string): Promise<APIResponse<void>> {
    return this.request<void>(`/tasks/${taskId}/cancel`, {
      method: 'POST',
    })
  }

  // 删除任务
  async deleteTask(taskId: string): Promise<APIResponse<void>> {
    return this.request<void>(`/tasks/${taskId}`, {
      method: 'DELETE',
    })
  }

  // 下载导出数据
  async downloadExportData(taskId: string): Promise<APIResponse<ExportDataResponse>> {
    return this.request<ExportDataResponse>(`/tasks/${taskId}/download`)
  }

  // 获取系统信息
  async getSystemInfo(): Promise<APIResponse<SystemInfo>> {
    return this.request<SystemInfo>('/info')
  }

  // 健康检查
  async healthCheck(): Promise<APIResponse<{ status: string }>> {
    return this.request<{ status: string }>('/health')
  }

  // 获取导入状态
  async getImportStatus(taskId: string): Promise<APIResponse<ImportStatusResponse>> {
    return this.request<ImportStatusResponse>(`/tasks/${taskId}/import-status`)
  }

  // 生成应用下载链接
  getAppDownloadUrl(taskId: string, appName: string): string {
    return `${API_BASE_URL}/tasks/${taskId}/download/${appName}`
  }
}

export const apiClient = new ApiClient()
export default apiClient