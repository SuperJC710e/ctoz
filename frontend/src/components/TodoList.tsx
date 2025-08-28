import { useState, useEffect, useCallback } from 'react'
import { CheckCircle, XCircle, AlertCircle } from 'lucide-react'
import { AppImportStatus, ImportSummary } from '../types'
import apiClient from '../utils/api'

interface TodoListProps {
  className?: string
  taskId?: string
}

export default function TodoList({ taskId }: TodoListProps) {
  const [apps, setApps] = useState<AppImportStatus[]>([])
  const [summary, setSummary] = useState<ImportSummary | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [refreshCount, setRefreshCount] = useState(0)
  const [maxRefreshAttempts] = useState(20)
  const [lastDataHash, setLastDataHash] = useState<string>('')
  const [, setStableCount] = useState<number>(0)

  const generateDataHash = useCallback((apps: AppImportStatus[], summary: ImportSummary | null) => {
    const dataString = JSON.stringify({
      apps: apps.map(app => ({
        app_name: app.app_name,
        overall_status: app.overall_status,
        app_data_status: app.app_data_status,
        compose_status: app.compose_status
      })),
      summary: summary
    })
    return btoa(dataString).slice(0, 16)
  }, [])

  const startSmartRefresh = useCallback(async () => {
    let attempts = 0
    let lastHashLocal = lastDataHash
    let stableLocal = 0

    const performRefresh = async () => {
      if (attempts >= maxRefreshAttempts) {
        setRefreshing(false)
        return
      }
      attempts++
      setRefreshCount(attempts)

      try {
        const response = await apiClient.getImportStatus(taskId!)
        if (response.success && response.data) {
          const newApps: AppImportStatus[] = response.data.apps || []
          const calculatedSummary: ImportSummary = {
            total_apps: newApps.length,
            success_apps: newApps.filter(app => app.overall_status === 'success').length,
            failed_apps: newApps.filter(app => app.overall_status === 'failed').length
          }
          const newHash = generateDataHash(newApps, calculatedSummary)

          // 仅当数据发生变化时才更新状态，避免闪烁
          if (newHash !== lastHashLocal) {
            setApps(newApps)
            setSummary(calculatedSummary)
            lastHashLocal = newHash
            setLastDataHash(newHash)
            stableLocal = 0
            setStableCount(0)
          } else {
            stableLocal += 1
            setStableCount(stableLocal)
          }

          // 停止条件：
          // 1) 已有内容且出现两次相同数据；或 2) 任务不再运行且数据未变化
          const hasContent = newApps.length > 0
          const taskRunning = response.data.status === 'running'
          if ((hasContent && stableLocal >= 1) || (!taskRunning && stableLocal >= 1)) {
            setRefreshing(false)
            return
          }

          // 继续尝试
          setTimeout(performRefresh, 2000)
        } else {
          // 失败时也继续尝试，直到达到最大次数
          setTimeout(performRefresh, 2000)
        }
      } catch (_err) {
        // 出错时也继续尝试，直到达到最大次数
        setTimeout(performRefresh, 2000)
      }
    }

    await performRefresh()
  }, [taskId, maxRefreshAttempts, lastDataHash, generateDataHash])

  const loadAppStatus = useCallback(async () => {
    setLoading(true)
    setError(null)

    try {
      const response = await apiClient.getImportStatus(taskId!)

      if (response.success && response.data) {
        const newApps: AppImportStatus[] = response.data.apps || []
        const calculatedSummary: ImportSummary = {
          total_apps: newApps.length,
          success_apps: newApps.filter(app => app.overall_status === 'success').length,
          failed_apps: newApps.filter(app => app.overall_status === 'failed').length
        }

        const newHash = generateDataHash(newApps, calculatedSummary)
        setLastDataHash(newHash)

        setApps(newApps)
        setSummary(calculatedSummary)

        // 若任务运行中且当前无内容，自动进入智能刷新；否则不自动刷新
        if (response.data.status === 'running' && newApps.length === 0 && !refreshing) {
          setRefreshing(true)
          setRefreshCount(0)
          setStableCount(0)
          setTimeout(() => startSmartRefresh(), 1500)
        }

        if (refreshing) {
          setRefreshCount(prev => prev + 1)
          setRefreshing(false)
        }
      } else {
        setError(response.message || 'Failed to get app status')
        if (refreshing) {
          setRefreshCount(prev => prev + 1)
          setRefreshing(false)
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
      if (refreshing) {
        setRefreshCount(prev => prev + 1)
        setRefreshing(false)
      }
    } finally {
      setLoading(false)
    }
  }, [taskId, refreshing, generateDataHash, startSmartRefresh])

  useEffect(() => {
    loadAppStatus()

    const handleRefreshAppStatus = (event: CustomEvent) => {
      if (event.detail.taskId === taskId) {
        setRefreshing(true)
        setRefreshCount(0)
        setStableCount(0)
        setTimeout(() => {
          startSmartRefresh()
        }, 3000)
      }
    }

    window.addEventListener('refreshAppStatus', handleRefreshAppStatus as EventListener)
    return () => {
      window.removeEventListener('refreshAppStatus', handleRefreshAppStatus as EventListener)
    }
  }, [taskId, loadAppStatus, startSmartRefresh])

  const handleDownload = (app: AppImportStatus) => {
    if (!taskId) return
    const downloadUrl = apiClient.getAppDownloadUrl(taskId, app.app_name)
    const link = document.createElement('a')
    link.href = downloadUrl
    link.download = `${app.app_name}.zip`
    link.target = '_blank'
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'success':
        return <CheckCircle className="w-5 h-5 text-green-600" />
      case 'failed':
        return <XCircle className="w-5 h-5 text-red-600" />
      default:
        return <AlertCircle className="w-5 h-5 text-yellow-600" />
    }
  }

  const getStatusText = (status: string) => {
    switch (status) {
      case 'success':
        return 'Success'
      case 'failed':
        return 'Failed'
      default:
        return 'Unknown'
    }
  }

  const getStatusColorClass = (status: string) => {
    switch (status) {
      case 'success':
        return 'text-green-600 bg-green-50'
      case 'failed':
        return 'text-red-600 bg-red-50'
      default:
        return 'text-yellow-600 bg-yellow-50'
    }
  }

  if (loading || refreshing) {
    return (
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
        <div className="flex items-center justify-center py-8">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      </div>
    )
  }

  return (
    <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center space-x-2">
          <h3 className="text-lg font-medium">Application Import Status</h3>
          {summary && (
            <span className="text-sm text-gray-500">Total {summary.total_apps} · Success {summary.success_apps} · Failed {summary.failed_apps}</span>
          )}
        </div>
        <div className="flex items-center space-x-3">
          {refreshing && (
            <span className="text-sm text-gray-500">Refreshing... {refreshCount}/{maxRefreshAttempts}</span>
          )}
        </div>
      </div>

      {error ? (
        <div className="bg-red-50 text-red-700 p-4 rounded-md">{error}</div>
      ) : apps.length === 0 ? (
        <div className="text-gray-500 text-center py-6">No applications found.</div>
      ) : (
        <div className="space-y-3">
          {apps.map((app) => (
            <div key={app.app_name} className="flex items-center justify-between p-3 bg-white rounded border">
              <div className="flex items-center space-x-3">
                {getStatusIcon(app.overall_status)}
                <div>
                  <div className="font-medium">{app.app_name}</div>
                  <div className={`text-sm ${getStatusColorClass(app.overall_status)}`}>{getStatusText(app.overall_status)}</div>
                </div>
              </div>
              {app.download_url && (
                <button
                  onClick={() => handleDownload(app)}
                  className="px-3 py-1.5 text-sm bg-blue-600 text-white rounded hover:bg-blue-700"
                >
                  Download
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}