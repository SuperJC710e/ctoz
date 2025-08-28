import React, { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowRightLeft, Server, Play } from 'lucide-react'
import { SystemConnection, ConnectionTestRequest, OnlineMigrationRequest } from '../types'
import { apiClient } from '../utils/api'
import { useStore } from '../hooks/useStore'
import { toast } from 'sonner'
import ConnectionForm from '../components/ConnectionForm'

const OnlineMigrationPage: React.FC = () => {
  const navigate = useNavigate()
  const { setLoading } = useStore()
  const [sourceConnection, setSourceConnection] = useState<SystemConnection>({
    host: '',
    port: 80,
    username: '',
    password: '',
    type: 'casaos'
  })
  const [targetConnection, setTargetConnection] = useState<SystemConnection>({
    host: '',
    port: 80,
    username: '',
    password: '',
    type: 'zimaos'
  })
  const [sourceTestResult, setSourceTestResult] = useState<{ success: boolean; message: string } | null>(null)
  const [targetTestResult, setTargetTestResult] = useState<{ success: boolean; message: string } | null>(null)
  const [isTestingSource, setIsTestingSource] = useState(false)
  const [isTestingTarget, setIsTestingTarget] = useState(false)
  const [isStarting, setIsStarting] = useState(false)

  const testConnection = async (connection: SystemConnection, isSource: boolean) => {
    const setTesting = isSource ? setIsTestingSource : setIsTestingTarget
    const setResult = isSource ? setSourceTestResult : setTargetTestResult
    
    try {
      setTesting(true)
      const request: ConnectionTestRequest = { connection }
      const response = await apiClient.testConnection(request)
      
      if (response.success && response.data) {
        // 检查实际的连接测试结果
        if (response.data.success) {
          setResult({ success: true, message: response.data.message })
          toast.success(`${isSource ? 'Source' : 'Target'} system login successful`)
        } else {
          setResult({ success: false, message: response.data.message })
          toast.error(`${isSource ? 'Source' : 'Target'} system login failed`)
        }
      } else {
        setResult({ success: false, message: response.message || 'Connection failed' })
        toast.error(`${isSource ? 'Source' : 'Target'} system login failed`)
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Connection test failed'
      setResult({ success: false, message })
      toast.error(`${isSource ? 'Source' : 'Target'} system connection test failed: ${message}`)
    } finally {
      setTesting(false)
    }
  }

  const startMigration = async () => {
    if (!sourceTestResult?.success || !targetTestResult?.success) {
      toast.error('Please test and ensure both connections succeed first')
      return
    }

    try {
      setIsStarting(true)
      setLoading(true)
      
      const request: OnlineMigrationRequest = {
        source: sourceConnection,
        target: targetConnection,
        migrationOptions: {
          includeApps: true,
          includeSettings: true,
          includeUserData: true
        }
      }
      
      const response = await apiClient.startOnlineMigration(request)
      
      if (response.success && response.data) {
        toast.success('Online migration task started')
        navigate(`/status/${response.data.task_id}`)
      } else {
        toast.error(response.message || 'Failed to start migration task')
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to start migration task'
      toast.error(message)
    } finally {
      setIsStarting(false)
      setLoading(false)
    }
  }

  const canStartMigration = sourceTestResult?.success && targetTestResult?.success && !isStarting

  return (
    <div className="max-w-4xl mx-auto space-y-8">
      {/* Header */}
      <div className="text-center">
        <div className="flex items-center justify-center mb-4">
          <ArrowRightLeft className="h-8 w-8 text-blue-600 mr-3" />
          <h1 className="text-3xl font-bold text-gray-900">Online Migration</h1>
        </div>
        <p className="text-lg text-gray-600">
          Connect the source and target systems directly to transfer data and configurations in real time.
        </p>
      </div>

      {/* Migration Flow */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 items-center">
        {/* Source System */}
        <div className="card">
          <div className="flex items-center mb-4">
            <Server className="h-6 w-6 text-orange-600 mr-3" />
            <h2 className="text-xl font-semibold text-gray-900">Source (CasaOS)</h2>
          </div>
          
          <ConnectionForm
            connection={sourceConnection}
            onChange={setSourceConnection}
            onTest={() => testConnection(sourceConnection, true)}
            testResult={sourceTestResult}
            isTesting={isTestingSource}
            label="CasaOS"
          />
        </div>

        {/* Arrow */}
        <div className="flex justify-center">
          <div className="p-4 bg-blue-100 rounded-full">
            <ArrowRightLeft className="h-8 w-8 text-blue-600" />
          </div>
        </div>

        {/* Target System */}
        <div className="card">
          <div className="flex items-center mb-4">
            <Server className="h-6 w-6 text-blue-600 mr-3" />
            <h2 className="text-xl font-semibold text-gray-900">Target (ZimaOS)</h2>
          </div>
          
          <ConnectionForm
            connection={targetConnection}
            onChange={setTargetConnection}
            onTest={() => testConnection(targetConnection, false)}
            testResult={targetTestResult}
            isTesting={isTestingTarget}
            label="ZimaOS"
          />
        </div>
      </div>

      {/* Migration Info */}
      <div className="card bg-blue-50 border-blue-200">
        <h3 className="text-lg font-semibold text-blue-900 mb-3">What Will Be Migrated</h3>
        <ul className="space-y-2 text-blue-800">
          <li className="flex items-center">
            <div className="w-2 h-2 bg-blue-600 rounded-full mr-3"></div>
            Application configuration (AppData)
          </li>
          <li className="flex items-center">
            <div className="w-2 h-2 bg-blue-600 rounded-full mr-3"></div>
            Docker container configuration (Compose)
          </li>
        </ul>
      </div>

      {/* Start Migration */}
      <div className="text-center">
        <button
          onClick={startMigration}
          disabled={!canStartMigration}
          className={`inline-flex items-center px-6 py-3 text-lg font-medium rounded-lg transition-colors duration-200 ${
            canStartMigration
              ? 'bg-blue-600 hover:bg-blue-700 text-white'
              : 'bg-gray-300 text-gray-500 cursor-not-allowed'
          }`}
        >
          <Play className="h-5 w-5 mr-2" />
          {isStarting ? 'Starting...' : 'Start Migration'}
        </button>
        
        {(!sourceTestResult?.success || !targetTestResult?.success) && (
          <p className="text-sm text-gray-600 mt-2">
            Please test and ensure both connections succeed first
          </p>
        )}
      </div>
    </div>
  )
}

export default OnlineMigrationPage