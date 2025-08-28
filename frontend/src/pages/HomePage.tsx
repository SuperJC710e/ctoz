import React, { useEffect } from 'react'
import { Link } from 'react-router-dom'
import { ArrowRightLeft, Download, Activity, Clock, CheckCircle, XCircle } from 'lucide-react'
import { useStore } from '../hooks/useStore'
import { apiClient } from '../utils/api'
import { toast } from 'sonner'

const HomePage: React.FC = () => {
  const { tasks, setTasks, isLoading, setLoading, setError } = useStore()

  useEffect(() => {
    loadTasks()
  }, [])

  const loadTasks = async () => {
    try {
      setLoading(true)
      const response = await apiClient.getTasks()
      console.log('📥 获取任务列表响应:', response)
      
      if (response.success && response.data) {
        // 后端返回的数据结构是 { tasks: [], total: number, limit: number, offset: number }
        // 我们需要访问 response.data.tasks 来获取任务数组
        const responseData = response.data as any
        const tasksData = responseData.tasks || []
        console.log('📋 解析后的任务数据:', tasksData)
        setTasks(tasksData)
      } else {
        console.log('❌ 获取任务列表失败:', response.message)
        setTasks([])
      }
    } catch (error) {
      console.error('❌ 加载任务列表时发生错误:', error)
      setError('加载任务列表失败')
      toast.error('加载任务列表失败')
      setTasks([])
    } finally {
      setLoading(false)
    }
  }

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'completed':
        return <CheckCircle className="h-5 w-5 text-green-500" />
      case 'failed':
        return <XCircle className="h-5 w-5 text-red-500" />
      case 'running':
        return <Activity className="h-5 w-5 text-blue-500 animate-spin" />
      default:
        return <Clock className="h-5 w-5 text-gray-500" />
    }
  }

  const getStatusText = (status: string) => {
    switch (status) {
      case 'pending': return 'Pending'
      case 'running': return 'Running'
      case 'completed': return 'Completed'
      case 'failed': return 'Failed'
      case 'cancelled': return 'Cancelled'
      default: return status
    }
  }

  const getTypeText = (type: string) => {
    switch (type) {
      case 'online': return 'Online Migration'
      case 'export': return 'Offline Export'
      case 'import': return 'Offline Import'
      case 'offline-export': return 'Offline Export'
      case 'offline-import': return 'Offline Import'
      default: return type
    }
  }

  return (
    <div className="space-y-8">
      {/* Hero Section */}
      <div className="text-center">
        <h1 className="text-4xl font-bold text-gray-900 mb-4">
          CasaOS to ZimaOS Migration Tool
        </h1>
        <p className="text-xl text-gray-600 max-w-3xl mx-auto">
          Safely and quickly migrate your data and configurations from CasaOS to ZimaOS. Supports both online and offline migration modes.
        </p>
      </div>

      {/* Quick Actions */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Link
          to="/online-migration"
          className="card hover:shadow-md transition-shadow duration-200 group"
        >
          <div className="flex items-center mb-4">
            <div className="p-3 bg-blue-100 rounded-lg group-hover:bg-blue-200 transition-colors duration-200">
              <ArrowRightLeft className="h-6 w-6 text-blue-600" />
            </div>
            <h3 className="text-xl font-semibold text-gray-900 ml-4">Online Migration</h3>
          </div>
          <p className="text-gray-600 mb-4">
            Connect your source and target systems directly and transfer data in real-time. Ideal for environments with stable networking.
          </p>
          <div className="flex items-center text-blue-600 font-medium">
            Start Online Migration
            <ArrowRightLeft className="h-4 w-4 ml-2" />
          </div>
        </Link>

        <Link
          to="/offline-migration"
          className="card hover:shadow-md transition-shadow duration-200 group"
        >
          <div className="flex items-center mb-4">
            <div className="p-3 bg-green-100 rounded-lg group-hover:bg-green-200 transition-colors duration-200">
              <Download className="h-6 w-6 text-green-600" />
            </div>
            <h3 className="text-xl font-semibold text-gray-900 ml-4">Offline Migration</h3>
          </div>
          <p className="text-gray-600 mb-4">
            Export a package from the source system and import it into the target system. Ideal for limited networks or step-by-step operations.
          </p>
          <div className="flex items-center text-green-600 font-medium">
            Start Offline Migration
            <Download className="h-4 w-4 ml-2" />
          </div>
        </Link>
      </div>

      {/* Recent Tasks */}
      <div className="card">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-2xl font-semibold text-gray-900">Recent Tasks</h2>
          <button
            onClick={loadTasks}
            disabled={isLoading}
            className="btn-secondary"
          >
            {isLoading ? 'Refreshing...' : 'Refresh'}
          </button>
        </div>

        {isLoading ? (
          <div className="text-center py-8">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto mb-4"></div>
            <p className="text-gray-600">Loading tasks...</p>
          </div>
        ) : !Array.isArray(tasks) || tasks.length === 0 ? (
          <div className="text-center py-12">
            <Activity className="h-12 w-12 text-gray-400 mx-auto mb-4" />
            <h3 className="text-lg font-medium text-gray-900 mb-2">No tasks yet</h3>
            <p className="text-gray-600">Start your first migration task</p>
          </div>
        ) : (
          <div className="space-y-4">
            {tasks.slice(0, 5).map((task) => (
              <div
                key={task.id}
                className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors duration-200"
              >
                <div className="flex items-center space-x-4">
                  {getStatusIcon(task.status)}
                  <div>
                    <h4 className="font-medium text-gray-900">
                      {getTypeText(task.type)}
                    </h4>
                    <p className="text-sm text-gray-600">
                      {new Date(task.created_at).toLocaleString()}
                    </p>
                  </div>
                </div>
                <div className="flex items-center space-x-4">
                  <div className="text-right">
                    <p className="text-sm font-medium text-gray-900">
                      {getStatusText(task.status)}
                    </p>
                    {task.status === 'running' && (
                      <p className="text-sm text-gray-600">
                        {task.progress}%
                      </p>
                    )}
                  </div>
                  <Link
                    to={`/status/${task.id}`}
                    className="btn-secondary text-sm"
                  >
                    View Details
                  </Link>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* 功能介绍 */}
      <div className="card">
        <h2 className="text-2xl font-semibold text-gray-900 mb-6">Overview</h2>
        
        <div className="space-y-6">
          {/* 使用场景 */}
          <div>
            <h3 className="text-lg font-medium text-gray-900 mb-3">Use Case</h3>
            <p className="text-gray-600 leading-relaxed">
              If you have a device running CasaOS and prefer ZimaOS, this tool helps you migrate your applications and data from CasaOS to ZimaOS seamlessly.
            </p>
          </div>

          {/* 迁移范围 */}
          <div>
            <h3 className="text-lg font-medium text-gray-900 mb-3">Migration Scope</h3>
            <p className="text-gray-600 mb-3">
              This tool migrates everything inside the AppData directory and the application YAML (compose) files. Applications will be re-installed on ZimaOS.
            </p>
            <ul className="list-disc list-inside text-gray-600 space-y-1">
              <li>All contents under AppData</li>
              <li>Application definitions (Compose/YAML)</li>
            </ul>
          </div>

          {/* 快速安装 */}
          <div>
            <h3 className="text-lg font-medium text-gray-900 mb-3">Quick Install</h3>
            <div className="bg-gray-50 p-4 rounded-lg">
              <h4 className="font-medium text-gray-800 mb-2">Docker Compose</h4>
              <pre className="text-sm text-gray-700 bg-white p-3 rounded border overflow-x-auto">
{`# Clone repository
git clone https://github.com/your-username/ctoz.git
cd ctoz

# Start services
docker-compose up -d`}
              </pre>
            </div>
            <div className="bg-gray-50 p-4 rounded-lg mt-3">
              <h4 className="font-medium text-gray-800 mb-2">Docker CLI</h4>
              <pre className="text-sm text-gray-700 bg-white p-3 rounded border overflow-x-auto">
{`# Pull image
docker pull your-username/ctoz:latest

# Run container
docker run -d \
  --name ctoz \
  -p 8080:8080 \
  -p 3000:3000 \
  your-username/ctoz:latest`}
              </pre>
            </div>
          </div>

          {/* 注意事项 */}
          <div>
            <h3 className="text-lg font-medium text-gray-900 mb-3">Notes</h3>
            
            <div className="space-y-4">
              <div>
                <h4 className="font-medium text-gray-800 mb-2">If some apps report migration failures</h4>
                <ul className="list-disc list-inside text-gray-600 space-y-1 ml-4">
                  <li>AppData upload always succeeds. If a folder already exists on ZimaOS, a numeric suffix is appended.</li>
                  <li>For Docker installation errors, download the YAML and import it manually on ZimaOS.</li>
                </ul>
              </div>
              
              <div>
                <h4 className="font-medium text-gray-800 mb-2">Import status not showing</h4>
                <ul className="list-disc list-inside text-gray-600 space-y-1 ml-4">
                  <li>Import status aggregates all apps and may take some time. Please wait.</li>
                  <li>Query performance is optimized. Repeat queries will hit the cache for faster responses.</li>
                </ul>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export default HomePage