import React, { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Activity,
  CheckCircle,
  XCircle,
  Clock,
  Square,
  RefreshCw,
  Download,
  Pause
} from 'lucide-react'
import { MigrationTask, MigrationLog, TaskStatus } from '../types'
import { apiClient } from '../utils/api'
import { toast } from 'sonner'
import { wsClient } from '../utils/websocket'
import TodoList from '../components/TodoList'


const StatusPage: React.FC = () => {
  const { taskId } = useParams<{ taskId: string }>()
  const navigate = useNavigate()
  
  const [task, setTask] = useState<MigrationTask | null>(null)
  const [logs, setLogs] = useState<MigrationLog[]>([])
  // 移除了isConnected状态，因为当前未使用
  const [loading, setLoading] = useState(true)
  const [autoScroll, setAutoScroll] = useState(true)
  const autoScrollRef = useRef(autoScroll)
  const [currentStep, setCurrentStep] = useState<string>('')
  const [stepStatus, setStepStatus] = useState<string>('')
  const [stepProgress, setStepProgress] = useState<number>(0)
  
  // 移除了importStatus相关状态，使用独立的TodoList组件
  
  // 将 useCallback 移到组件顶层
  const handleTaskStatus = useCallback((message: any) => {
    setTask(prev => {
      const newTask = message.data
      
      // 只有当任务真正发生变化时才更新
      if (!prev || prev.id !== newTask.id || prev.status !== newTask.status || prev.updated_at !== newTask.updated_at) {
        // 如果任务完成，触发应用状态刷新
        if (newTask.status === 'completed') {
          // 延迟一下再刷新，确保后端数据已经保存
          setTimeout(() => {
            // 通过事件通知TodoList组件刷新
            const event = new CustomEvent('refreshAppStatus', { 
              detail: { taskId: newTask.id } 
            })
            window.dispatchEvent(event)
          }, 1000)
        }
        return newTask
      }
      return prev
    })
  }, [])

  const handleTaskProgress = useCallback((message: any) => {
    setTask(prev => {
      if (!prev) return null
      const newProgress = message.data.progress
      // 只有当进度真正发生变化时才更新
      if (prev.progress !== newProgress) {
        return { ...prev, progress: newProgress }
      }
      return prev
    })
  }, [])
  
  // 同步 autoScroll 状态到 ref
  useEffect(() => {
    autoScrollRef.current = autoScroll
  }, [autoScroll])
  
  // 移除了loadImportStatus函数和定时器逻辑，使用独立的TodoList组件
  
  useEffect(() => {
    if (!taskId) {
      navigate('/')
      return
    }
    
    // 初始化 WebSocket 连接
    const client = wsClient
    
    // 定义事件处理器函数
    const handleOpen = () => {
    }

    const handleClose = () => {
    }

    const handleError = (message: any) => {
      toast.error('WebSocket connection error')
    }

    const handleTaskLog = (message: any) => {
       setLogs(prev => {
         // 修复时间戳字段映射问题：后端使用'time'字段，前端期望'timestamp'字段
         const logData = {
           ...message.data,
           timestamp: message.data.time || message.data.timestamp || new Date().toISOString()
         }
         const newLogs = [...prev, logData]
         return newLogs.slice(-1000) // 保留最近1000条日志
       })
       if (autoScrollRef.current) {
         setTimeout(() => {
           const logContainer = document.getElementById('log-container')
           if (logContainer) {
             logContainer.scrollTop = logContainer.scrollHeight
           }
         }, 100)
       }
     }

    const handleStep = (message: any) => {
      const { step, status, message: stepMessage, progress } = message.data
      setCurrentStep(step)
      setStepStatus(status)
      if (progress !== undefined) {
        setStepProgress(progress)
      }
      
      // 添加步骤日志
      const stepLog = {
        id: Date.now().toString(),
        task_id: taskId!,
        level: status === 'error' ? 'error' as const : 'info' as const,
        message: stepMessage || getStepStatusText(step, status),
        timestamp: new Date().toISOString()
      }
      
      setLogs(prev => {
        const newLogs = [...prev, stepLog]
        return newLogs.slice(-1000)
      })
      
      if (autoScrollRef.current) {
        setTimeout(() => {
          const logContainer = document.getElementById('log-container')
          if (logContainer) {
            logContainer.scrollTop = logContainer.scrollHeight
          }
        }, 100)
      }
    }
    
    // 设置事件处理器
    client.on('open', handleOpen)
    client.on('close', handleClose)
    client.on('error', handleError)
    client.on('task_status', handleTaskStatus)
    client.on('task_progress', handleTaskProgress)
    client.on('task_log', handleTaskLog)
    client.on('step', handleStep)
    
    // 连接 WebSocket
    client.connect(taskId!)
    
    // 获取初始任务状态
    loadTaskStatus()
    
    return () => {
      // 移除所有事件监听器
      client.off('open', handleOpen)
      client.off('close', handleClose)
      client.off('error', handleError)
      client.off('task_status', handleTaskStatus)
      client.off('task_progress', handleTaskProgress)
      client.off('task_log', handleTaskLog)
      client.off('step', handleStep)
      
      client.disconnect()
    }
  }, [taskId, navigate, handleTaskStatus, handleTaskProgress])
  
  const loadTaskStatus = async () => {
    if (!taskId) return
    
    try {
      setLoading(true)
      const response = await apiClient.getTaskStatus(taskId)
      
      if (response.success && response.data) {
        setTask(response.data)
      } else {
        toast.error(response.message || '获取任务状态失败')
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : '获取任务状态失败'
      toast.error(message)
    } finally {
      setLoading(false)
    }
  }
  
  const cancelTask = async () => {
    if (!taskId || !task) return
    
    try {
      const response = await apiClient.cancelTask(taskId)
      
      if (response.success) {
        toast.success('任务已取消')
        loadTaskStatus()
      } else {
        toast.error(response.message || '取消任务失败')
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : '取消任务失败'
      toast.error(message)
    }
  }
  
  const deleteTask = async () => {
    if (!taskId || !task) return
    
    if (!confirm('Are you sure you want to delete this task? This action cannot be undone.')) {
      return
    }
    
    try {
      const response = await apiClient.deleteTask(taskId)
      
      if (response.success) {
        toast.success('Task deleted')
        navigate('/')
      } else {
        toast.error(response.message || 'Failed to delete task')
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to delete task'
      toast.error(message)
    }
  }
  
  const downloadExportData = async () => {
    if (!taskId || !task || task.type !== 'export') return
    
    try {
      const response = await apiClient.downloadExportData(taskId)
      
      if (response.success && response.data) {
        // 创建下载链接
        const link = document.createElement('a')
        link.href = response.data.download_url
        link.download = `export-${taskId}-${Date.now()}.zip`
        document.body.appendChild(link)
        link.click()
        document.body.removeChild(link)
        
        toast.success('下载已开始')
      } else {
        toast.error(response.message || '获取下载链接失败')
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : '下载失败'
      toast.error(message)
    }
  }
  
  const getStatusIcon = (status: TaskStatus) => {
    switch (status) {
      case 'running':
        return <Activity className="h-6 w-6 text-blue-600 animate-pulse" />
      case 'completed':
        return <CheckCircle className="h-6 w-6 text-green-600" />
      case 'failed':
        return <XCircle className="h-6 w-6 text-red-600" />
      case 'cancelled':
        return <Square className="h-6 w-6 text-gray-600" />
      default:
        return <Clock className="h-6 w-6 text-yellow-600" />
    }
  }
  
  const getStatusText = (status: TaskStatus) => {
    switch (status) {
      case 'running':
        return 'Running'
      case 'completed':
        return 'Completed'
      case 'failed':
        return 'Failed'
      case 'cancelled':
        return 'Cancelled'
      default:
        return 'Pending'
    }
  }

  const formatDateTime = (dateString: string | Date) => {
    try {
      const date = new Date(dateString)
      if (isNaN(date.getTime())) {
        return 'No data'
      }
      return date.toLocaleString('en-US', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
      })
    } catch (error) {
      return 'No data'
    }
  }

  const formatLogTime = (timestamp: string | Date) => {
    try {
      // 处理各种可能的时间格式
      let date: Date
      
      if (timestamp instanceof Date) {
        date = timestamp
      } else if (typeof timestamp === 'string') {
        // 尝试解析ISO格式或其他常见格式
        date = new Date(timestamp)
        
        // 如果解析失败，尝试其他格式
        if (isNaN(date.getTime())) {
          // 尝试解析时间戳（毫秒）
          const numTimestamp = parseInt(timestamp)
          if (!isNaN(numTimestamp)) {
            date = new Date(numTimestamp)
          }
        }
      } else {
        return '--:--:--'
      }
      
      // 检查日期是否有效
      if (isNaN(date.getTime())) {
        return '--:--:--'
      }
      
      // 返回时间格式 HH:MM:SS
      return date.toLocaleTimeString('en-US', {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
      })
    } catch (error) {
      // Silenced verbose console warning
      return '--:--:--'
    }
  }
  
  const getStatusColor = (status: TaskStatus) => {
    switch (status) {
      case 'running':
        return 'text-blue-600'
      case 'completed':
        return 'text-green-600'
      case 'failed':
        return 'text-red-600'
      case 'cancelled':
        return 'text-gray-600'
      default:
        return 'text-yellow-600'
    }
  }
  
  const getTypeText = (type: string) => {
    switch (type) {
      case 'online_migration':
        return 'Online Migration'
      case 'export':
        return 'Data Export'
      case 'import':
        return 'Data Import'
      default:
        return type
    }
  }

  const getStepStatusText = (step: string, status: string) => {
    const stepTexts: { [key: string]: string } = {
      'connection_test': 'Connection Test',
      'data_acquisition': 'Data Acquisition',
      'file_download': 'File Download',
      'file_extraction': 'File Extraction',
      'data_migration': 'Data Migration',
      'cleanup': 'Cleanup'
    }
    
    const statusTexts: { [key: string]: string } = {
      'start': 'Started',
      'progress': 'In Progress',
      'complete': 'Completed',
      'error': 'Failed'
    }
    
    const stepText = stepTexts[step] || step
    const statusText = statusTexts[status] || status
    
    return `${stepText}: ${statusText}`
  }

  const getStepDisplayText = (step: string) => {
    switch (step) {
      case 'connection_test':
        return 'Connection Test'
      case 'data_acquisition':
        return 'Data Acquisition'
      case 'file_download':
        return 'File Download'
      case 'file_extraction':
        return 'File Extraction'
      case 'data_migration':
        return 'Data Migration'
      case 'cleanup':
        return 'Cleanup'
      default:
        return step
    }
  }
  
  // 显示loading状态 - 当task为null或正在加载时
  if (!task || loading) {
    return (
      <div className="min-h-screen bg-gray-50">
        <div className="max-w-4xl mx-auto">
          {/* Header with back button */}
          <div className="flex items-center justify-between py-6">
            <div className="flex items-center">
              <button
                onClick={() => navigate('/')}
                className="mr-4 p-2 text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded-lg transition-colors duration-200"
              >
                <ArrowLeft className="h-5 w-5" />
              </button>
              <div>
                <h1 className="text-2xl font-bold text-gray-900">Task Status</h1>
                <p className="text-gray-600">Task ID: {taskId}</p>
              </div>
            </div>
          </div>
          
          {/* Loading content */}
          <div className="card">
            <div className="text-center py-12">
              <RefreshCw className="h-12 w-12 text-blue-600 mx-auto mb-4 animate-spin" />
              <h3 className="text-lg font-semibold text-gray-900 mb-2">Loading task status...</h3>
              <p className="text-gray-600">Retrieving task details, please wait</p>
            </div>
          </div>
          
          {/* Loading placeholder for logs */}
          <div className="card mt-6">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-gray-900">Live Logs</h3>
            </div>
            <div className="bg-gray-900 text-green-400 p-4 rounded-lg h-96 overflow-y-auto font-mono text-sm">
              <p className="text-gray-500">Waiting for task data...</p>
            </div>
          </div>
        </div>
      </div>
    )
  }
  
  return (
    <div className="max-w-4xl mx-auto space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center">
          <button
            onClick={() => navigate('/')}
            className="mr-4 p-2 text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded-lg transition-colors duration-200"
          >
            <ArrowLeft className="h-5 w-5" />
          </button>
          <div>
            <h1 className="text-2xl font-bold text-gray-900">Task Status</h1>
            <p className="text-gray-600">Task ID: {taskId}</p>
          </div>
        </div>
        

      </div>
      
      {/* Task Info */}
      <div className="card">
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-center">
            {getStatusIcon(task.status)}
            <div className="ml-3">
              <h2 className="text-xl font-semibold text-gray-900">
                {getTypeText(task.type)}
              </h2>
              <p className={`text-lg font-medium ${getStatusColor(task.status)}`}>
                {getStatusText(task.status)}
              </p>
            </div>
          </div>
          
          <div className="flex space-x-2">
            {task.status === 'running' && (
              <button
                onClick={cancelTask}
                className="btn-secondary flex items-center"
              >
                <Pause className="h-4 w-4 mr-2" />
                Cancel Task
              </button>
            )}
            
            {task.status === 'completed' && task.type === 'export' && (
              <button
                onClick={downloadExportData}
                className="btn-primary flex items-center"
              >
                <Download className="h-4 w-4 mr-2" />
                Download Data
              </button>
            )}
            
            {(task.status === 'completed' || task.status === 'failed' || task.status === 'cancelled') && (
              <button
                onClick={deleteTask}
                className="btn-secondary text-red-600 hover:text-red-700 hover:bg-red-50"
              >
                Delete Task
              </button>
            )}
          </div>
        </div>
        
        {/* Progress Bar */}
        {task.status === 'running' && (
          <div className="mb-6">
            <div className="flex justify-between text-sm text-gray-600 mb-2">
              <span>Progress</span>
              <span>{task.progress}%</span>
            </div>
            <div className="w-full bg-gray-200 rounded-full h-2 mb-3">
              <div
                className="bg-blue-600 h-2 rounded-full transition-all duration-300"
                style={{ width: `${task.progress}%` }}
              ></div>
            </div>
            
            {/* Current Step Status */}
            {currentStep && (
              <div className="bg-blue-50 border border-blue-200 rounded-lg p-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center">
                    {stepStatus === 'start' && <Clock className="h-4 w-4 text-blue-600 mr-2" />}
                    {stepStatus === 'progress' && <RefreshCw className="h-4 w-4 text-blue-600 mr-2 animate-spin" />}
                    {stepStatus === 'complete' && <CheckCircle className="h-4 w-4 text-green-600 mr-2" />}
                    {stepStatus === 'error' && <XCircle className="h-4 w-4 text-red-600 mr-2" />}
                    <span className="text-sm font-medium text-gray-900">
                      Current step: {getStepDisplayText(currentStep)}
                    </span>
                  </div>
                  <div className="flex items-center">
                    <span className={`text-sm font-medium ${
                      stepStatus === 'start' ? 'text-blue-600' :
                      stepStatus === 'progress' ? 'text-blue-600' :
                      stepStatus === 'complete' ? 'text-green-600' :
                      stepStatus === 'error' ? 'text-red-600' :
                      'text-gray-600'
                    }`}>
                      {stepStatus === 'start' && 'Started'}
                      {stepStatus === 'progress' && `In progress ${stepProgress > 0 ? `(${stepProgress}%)` : ''}`}
                      {stepStatus === 'complete' && 'Completed'}
                      {stepStatus === 'error' && 'Failed'}
                    </span>
                  </div>
                </div>
                
                {/* Step Progress Bar */}
                {stepStatus === 'progress' && stepProgress > 0 && (
                  <div className="mt-2">
                    <div className="w-full bg-blue-100 rounded-full h-1">
                      <div
                        className="bg-blue-600 h-1 rounded-full transition-all duration-300"
                        style={{ width: `${stepProgress}%` }}
                      ></div>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
        
        {/* Task Details */}
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <span className="text-gray-600">Created at:</span>
            <span className="ml-2 text-gray-900">
              {formatDateTime(task.created_at)}
            </span>
          </div>
          <div>
            <span className="text-gray-600">Updated at:</span>
            <span className="ml-2 text-gray-900">
              {formatDateTime(task.updated_at)}
            </span>
          </div>
          {task.source_host && (
            <div>
              <span className="text-gray-600">Source:</span>
              <span className="ml-2 text-gray-900">{task.source_host}</span>
            </div>
          )}
          {task.target_host && (
            <div>
              <span className="text-gray-600">Target:</span>
              <span className="ml-2 text-gray-900">{task.target_host}</span>
            </div>
          )}
        </div>
        
        {task.error && (
          <div className="mt-4 p-3 bg-red-50 border border-red-200 rounded-lg">
            <p className="text-red-800 font-medium">错误信息:</p>
            <p className="text-red-700 mt-1">{task.error}</p>
          </div>
        )}
      </div>
      
      {/* 独立的TODO列表组件 */}
      <div className="mb-6">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-lg font-semibold text-gray-900">Application Import Status</h3>
          <div className="flex items-center space-x-2">
  
          </div>
        </div>
        <TodoList taskId={taskId} />
      </div>

      {/* Logs */}
      <div className="card">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-lg font-semibold text-gray-900">Live Logs</h3>
          <div className="flex items-center space-x-2">
            <label className="flex items-center text-sm text-gray-600">
              <input
                type="checkbox"
                checked={autoScroll}
                onChange={(e) => setAutoScroll(e.target.checked)}
                className="mr-2"
              />
              Auto scroll
            </label>
            <button
              onClick={loadTaskStatus}
              className="p-1 text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded"
            >
              <RefreshCw className="h-4 w-4" />
            </button>
          </div>
        </div>
        
        <div
          id="log-container"
          className="bg-gray-900 text-green-400 p-4 rounded-lg h-96 overflow-y-auto font-mono text-sm"
        >
          {logs.length === 0 ? (
            <p className="text-gray-500">No logs yet...</p>
          ) : (
            logs.map((log, index) => (
              <div key={index} className="mb-1">
                <span className="text-gray-500">
                  [{formatLogTime(log.timestamp)}]
                </span>
                <span className={`ml-2 ${
                  log.level === 'error' ? 'text-red-400' :
                  log.level === 'warning' ? 'text-yellow-400' :
                  log.level === 'info' ? 'text-blue-400' :
                  'text-green-400'
                }`}>
                  {log.message}
                </span>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}

export default StatusPage