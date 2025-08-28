// 系统连接配置
export interface SystemConnection {
  host: string
  port: number
  username: string
  password: string
  type: 'casaos' | 'zimaos'
  token?: string
}

// 迁移任务状态
export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

// 迁移任务
export interface MigrationTask {
  id: string
  type: 'online' | 'export' | 'import' | 'offline-export' | 'offline-import' | 'test'
  status: TaskStatus
  progress: number
  source?: SystemConnection
  target?: SystemConnection
  options?: Record<string, any>
  logs?: MigrationLog[] | null
  result?: {
    apps?: AppImportStatus[]
    summary?: ImportSummary
    [key: string]: any
  } | null
  created_at: string
  updated_at: string
  // 兼容旧字段
  source_connection?: SystemConnection
  target_connection?: SystemConnection
  source_host?: string
  target_host?: string
  export_path?: string
  import_path?: string
  error?: string
  error_message?: string
}

// 迁移日志
export interface MigrationLog {
  id: string
  task_id: string
  level: 'info' | 'warning' | 'error'
  message: string
  timestamp: string
}

// WebSocket消息
export interface WSMessage {
  type: 'task_status' | 'task_progress' | 'task_log' | 'step'
  data: any
  timestamp: string
}

// API响应
export interface APIResponse<T = any> {
  success: boolean
  data?: T
  message?: string
  error?: string
}

// 连接测试请求
export interface ConnectionTestRequest {
  connection: SystemConnection
}

// 连接测试响应
export interface ConnectionTestResponse {
  success: boolean
  message: string
  system_info?: {
    os: string
    version: string
    architecture: string
  }
}

// 在线迁移请求
export interface OnlineMigrationRequest {
  source: SystemConnection
  target: SystemConnection
  migrationOptions?: {
    includeApps?: boolean
    includeSettings?: boolean
    includeUserData?: boolean
  }
}

// 数据导出请求
export interface DataExportRequest {
  source_connection: SystemConnection
  export_path: string
}

// 数据导入请求
export interface DataImportRequest {
  target_connection: SystemConnection
  import_path: string
}

// 任务响应
export interface TaskResponse {
  task_id: string
  status: TaskStatus
  message: string
}

// 导出数据响应
export interface ExportDataResponse {
  download_url: string
  file_size: number
  expires_at: string
}

// 系统信息
export interface SystemInfo {
  version: string
  build_time: string
  go_version: string
  os: string
}

// 应用导入状态
export interface AppImportStatus {
  app_name: string
  has_app_data: boolean
  app_data_status: string
  compose_status: string
  overall_status: string
  error_message?: string
  download_url?: string
}

// 导入摘要
export interface ImportSummary {
  total_apps: number
  success_apps: number
  failed_apps: number
}

// 导入状态响应
export interface ImportStatusResponse {
  task_id: string
  status: string
  progress: number
  apps: AppImportStatus[]
  summary: ImportSummary
}