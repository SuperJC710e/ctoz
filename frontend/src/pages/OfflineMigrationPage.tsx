import React, { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Download, Upload, Server, FileDown, FileUp } from 'lucide-react'
import { SystemConnection } from '../types'
import { apiClient } from '../utils/api'
import { useStore } from '../hooks/useStore'
import { toast } from 'sonner'
import ConnectionForm from '../components/ConnectionForm'

type MigrationStep = 'export' | 'import'

const OfflineMigrationPage: React.FC = () => {
  const navigate = useNavigate()
  const { setLoading } = useStore()
  const [currentStep, setCurrentStep] = useState<MigrationStep>('export')
  
  // Export state
  const [sourceConnection, setSourceConnection] = useState<SystemConnection>({
    host: '',
    port: 80,
    username: '',
    password: '',
    type: 'casaos'
  })
  // 移除了exportPath相关状态，因为不再需要导出配置
  const [sourceTestResult, setSourceTestResult] = useState<{ success: boolean; message: string } | null>(null)
  const [isTestingSource, setIsTestingSource] = useState(false)
  const [isExporting, setIsExporting] = useState(false)
  const [downloadProgress, setDownloadProgress] = useState<string>('')
  const [downloadStatus, setDownloadStatus] = useState<'idle' | 'downloading' | 'success' | 'error'>('idle')
  
  // Import state
  const [targetConnection, setTargetConnection] = useState<SystemConnection>({
    host: '',
    port: 80,
    username: '',
    password: '',
    type: 'zimaos'
  })
  const [targetTestResult, setTargetTestResult] = useState<{ success: boolean; message: string } | null>(null)
  const [isTestingTarget, setIsTestingTarget] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  
  // File upload state
  const [uploadedFile, setUploadedFile] = useState<File | null>(null)
  const [uploadProgress, setUploadProgress] = useState<number>(0)
  const [uploadStatus, setUploadStatus] = useState<'idle' | 'uploading' | 'success' | 'error'>('idle')
  const [isDragOver, setIsDragOver] = useState(false)

  const testConnection = async (connection: SystemConnection, isSource: boolean) => {
    const setTesting = isSource ? setIsTestingSource : setIsTestingTarget
    const setResult = isSource ? setSourceTestResult : setTargetTestResult
    
    try {
      setTesting(true)
      const response = await apiClient.testConnection({ connection })
      
      if (response.success && response.data) {
        // 检查实际的连接测试结果
        if (response.data.success) {
          setResult({ success: true, message: response.data.message })
          toast.success(`${isSource ? 'Source' : 'Target'} system connection successful`)
        } else {
          setResult({ success: false, message: response.data.message })
          toast.error(`${isSource ? 'Source' : 'Target'} system connection failed`)
        }
      } else {
        setResult({ success: false, message: response.message || 'Connection failed' })
        toast.error(`${isSource ? 'Source' : 'Target'} system connection failed`)
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Connection test failed'
      setResult({ success: false, message })
      toast.error(`${isSource ? 'Source' : 'Target'} system connection test failed: ${message}`)
    } finally {
      setTesting(false)
    }
  }

  const startExport = async () => {
    if (!sourceTestResult?.success) {
      toast.error('Please test and ensure the source system connection is successful')
      return
    }

    try {
      setIsExporting(true)
      setLoading(true)
      setDownloadStatus('downloading')
      setDownloadProgress('Connecting to server...')
      
      // 直接下载压缩包
      const response = await fetch('/api/export-download', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          source_connection: sourceConnection
        })
      })
      
      if (response.ok) {
        setDownloadProgress('Generating compressed package...')
        
        // 获取文件名
        const contentDisposition = response.headers.get('Content-Disposition')
        let filename = 'casaos-export.tar.gz'
        if (contentDisposition) {
          const filenameMatch = contentDisposition.match(/filename="(.+)"/)
          if (filenameMatch) {
            filename = filenameMatch[1]
          }
        }
        
        setDownloadProgress('Downloading file...')
        
        // 创建下载链接
        const blob = await response.blob()
        const url = window.URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = filename
        document.body.appendChild(a)
        a.click()
        window.URL.revokeObjectURL(url)
        document.body.removeChild(a)
        
        setDownloadStatus('success')
        setDownloadProgress('Download complete! File saved locally')
        toast.success('Data export complete, file downloaded locally')
      } else {
        const errorData = await response.json()
        setDownloadStatus('error')
        setDownloadProgress('Download failed: ' + (errorData.message || 'Unknown error'))
        toast.error(errorData.message || 'Export failed')
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Export failed'
      setDownloadStatus('error')
      setDownloadProgress('Download failed: ' + message)
      toast.error(message)
    } finally {
      setIsExporting(false)
      setLoading(false)
      // 3秒后重置状态
      setTimeout(() => {
        setDownloadStatus('idle')
        setDownloadProgress('')
      }, 3000)
    }
  }

  const startImport = async () => {
    if (!targetTestResult?.success) {
      toast.error('Please test and ensure the target system connection is successful')
      return
    }

    if (!uploadedFile) {
      toast.error('Please select the file to import')
      return
    }

    try {
      setIsImporting(true)
      setLoading(true)
      setUploadStatus('uploading')
      setUploadProgress(0)
      
      // 创建FormData用于文件上传
      const formData = new FormData()
      formData.append('file', uploadedFile)
      formData.append('target_connection', JSON.stringify(targetConnection))
      
      // 使用fetch进行文件上传，支持进度监控
      const response = await fetch('/api/data-import-upload', {
        method: 'POST',
        body: formData,
      })
      
      if (response.ok) {
        const result = await response.json()
        if (result.success && result.data) {
          setUploadStatus('success')
          setUploadProgress(100)
          toast.success('File uploaded successfully, data import task started')
          navigate(`/status/${result.data.task_id}`)
        } else {
          setUploadStatus('error')
          toast.error(result.message || 'Failed to start import task')
        }
      } else {
        const errorData = await response.json()
        setUploadStatus('error')
        toast.error(errorData.message || 'File upload failed')
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to start import task'
      setUploadStatus('error')
      toast.error(message)
    } finally {
      setIsImporting(false)
      setLoading(false)
      // 3秒后重置上传状态
      setTimeout(() => {
        if (uploadStatus !== 'success') {
          setUploadStatus('idle')
          setUploadProgress(0)
        }
      }, 3000)
    }
  }

  // 文件处理函数
  const handleFileSelect = (file: File) => {
    // 验证文件类型 - 支持 .tar.gz 和 .zip 格式
    const allowedTypes = ['.tar.gz', '.zip']
    const fileName = file.name.toLowerCase()
    const isValidType = allowedTypes.some(type => fileName.endsWith(type))
    
    if (!isValidType) {
      toast.error('Please select a .tar.gz or .zip format file')
      return
    }
    
    // 验证文件大小（限制为500MB）
    const maxSize = 500 * 1024 * 1024 // 500MB
    if (file.size > maxSize) {
      toast.error('File size cannot exceed 500MB')
      return
    }
    
    setUploadedFile(file)
    setUploadStatus('idle')
    setUploadProgress(0)
    toast.success(`File selected: ${file.name}`)
  }

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (file) {
      handleFileSelect(file)
    }
  }

  const handleDragOver = (event: React.DragEvent) => {
    event.preventDefault()
    setIsDragOver(true)
  }

  const handleDragLeave = (event: React.DragEvent) => {
    event.preventDefault()
    setIsDragOver(false)
  }

  const handleDrop = (event: React.DragEvent) => {
    event.preventDefault()
    setIsDragOver(false)
    
    const files = event.dataTransfer.files
    if (files.length > 0) {
      handleFileSelect(files[0])
    }
  }

  return (
    <div className="max-w-4xl mx-auto space-y-8">
      {/* Header */}
      <div className="text-center">
        <div className="flex items-center justify-center mb-4">
          <Download className="h-8 w-8 text-green-600 mr-3" />
          <h1 className="text-3xl font-bold text-gray-900">Offline Migration</h1>
        </div>
        <p className="text-lg text-gray-600">
          Export data first, then import into the target system. Suitable for network-restricted or step-by-step operations.
        </p>
      </div>

      {/* Step Selector */}
      <div className="flex justify-center">
        <div className="flex bg-gray-100 rounded-lg p-1">
          <button
            onClick={() => setCurrentStep('export')}
            className={`flex items-center px-4 py-2 rounded-md font-medium transition-colors duration-200 ${
              currentStep === 'export'
                ? 'bg-white text-green-700 shadow-sm'
                : 'text-gray-600 hover:text-gray-900'
            }`}
          >
            <FileDown className="h-4 w-4 mr-2" />
            Step 1: Export Data
          </button>
          <button
            onClick={() => setCurrentStep('import')}
            className={`flex items-center px-4 py-2 rounded-md font-medium transition-colors duration-200 ${
              currentStep === 'import'
                ? 'bg-white text-blue-700 shadow-sm'
                : 'text-gray-600 hover:text-gray-900'
            }`}
          >
            <FileUp className="h-4 w-4 mr-2" />
            Step 2: Import Data
          </button>
        </div>
      </div>

      {/* Export Step */}
      {currentStep === 'export' && (
        <div className="space-y-6">
          <div className="card">
            <div className="flex items-center mb-6">
              <Server className="h-6 w-6 text-orange-600 mr-3" />
              <h2 className="text-xl font-semibold text-gray-900">Source Configuration (CasaOS)</h2>
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



          <div className="card bg-green-50 border-green-200">
            <h3 className="text-lg font-semibold text-green-900 mb-3">Export Contents</h3>
            <ul className="space-y-2 text-green-800">
              <li className="flex items-center">
                <div className="w-2 h-2 bg-green-600 rounded-full mr-3"></div>
                App configuration and data
              </li>
              <li className="flex items-center">
                <div className="w-2 h-2 bg-green-600 rounded-full mr-3"></div>
                Docker container configuration
              </li>
              <li className="flex items-center">
                <div className="w-2 h-2 bg-green-600 rounded-full mr-3"></div>
                User data and files
              </li>
              <li className="flex items-center">
                <div className="w-2 h-2 bg-green-600 rounded-full mr-3"></div>
                System configuration files
              </li>
            </ul>
          </div>

          <div className="text-center">
            <button
              onClick={startExport}
              disabled={!sourceTestResult?.success || isExporting}
              className={`inline-flex items-center px-6 py-3 text-lg font-medium rounded-lg transition-colors duration-200 ${
                sourceTestResult?.success && !isExporting
                  ? 'bg-green-600 hover:bg-green-700 text-white'
                  : 'bg-gray-300 text-gray-500 cursor-not-allowed'
              }`}
            >
              <Download className="h-5 w-5 mr-2" />
              {isExporting ? 'Exporting...' : 'Start Export'}
            </button>
            
            {/* 下载进度显示 */}
            {downloadStatus !== 'idle' && (
              <div className="mt-4 p-4 rounded-lg border max-w-md mx-auto">
                <div className={`flex items-center justify-center space-x-2 ${
                  downloadStatus === 'downloading' ? 'text-blue-600' :
                  downloadStatus === 'success' ? 'text-green-600' :
                  'text-red-600'
                }`}>
                  {downloadStatus === 'downloading' && (
                    <div className="animate-spin rounded-full h-4 w-4 border-2 border-blue-600 border-t-transparent"></div>
                  )}
                  {downloadStatus === 'success' && (
                    <div className="w-4 h-4 bg-green-600 rounded-full flex items-center justify-center">
                      <div className="w-2 h-2 bg-white rounded-full"></div>
                    </div>
                  )}
                  {downloadStatus === 'error' && (
                    <div className="w-4 h-4 bg-red-600 rounded-full flex items-center justify-center">
                      <div className="w-1 h-3 bg-white rounded-full"></div>
                    </div>
                  )}
                  <span className="text-sm font-medium">{downloadProgress}</span>
                </div>
              </div>
            )}
            
            {!sourceTestResult?.success && (
              <p className="text-sm text-gray-600 mt-2">
                Please test and ensure the source system connection is successful
              </p>
            )}
          </div>
        </div>
      )}

      {/* Import Step */}
      {currentStep === 'import' && (
        <div className="space-y-6">
          <div className="card">
            <div className="flex items-center mb-6">
              <Server className="h-6 w-6 text-blue-600 mr-3" />
              <h2 className="text-xl font-semibold text-gray-900">Target System Configuration (ZimaOS)</h2>
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

          {/* 文件上传 */}
          <div className="card">
            <h3 className="text-lg font-semibold text-gray-900 mb-4">Select Import File</h3>
            <div className="space-y-4">
              {/* 文件拖拽上传区域 */}
              <div
                className={`border-2 border-dashed rounded-lg p-8 text-center transition-colors ${
                  isDragOver
                    ? 'border-blue-400 bg-blue-50'
                    : uploadedFile
                    ? 'border-green-400 bg-green-50'
                    : 'border-gray-300 hover:border-gray-400'
                }`}
                onDragOver={handleDragOver}
                onDragLeave={handleDragLeave}
                onDrop={handleDrop}
              >
                <input
                  type="file"
                  id="file-upload"
                  className="hidden"
                  accept=".tar.gz"
                  onChange={handleFileChange}
                />
                
                {uploadedFile ? (
                  <div className="space-y-2">
                    <div className="text-green-600">
                      <svg className="mx-auto h-12 w-12" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                      </svg>
                    </div>
                    <p className="text-sm font-medium text-gray-900">{uploadedFile.name}</p>
                    <p className="text-xs text-gray-500">
                      Size: {(uploadedFile.size / (1024 * 1024)).toFixed(2)} MB
                    </p>
                    <button
                      type="button"
                      onClick={() => {
                        setUploadedFile(null)
                        setUploadStatus('idle')
                        setUploadProgress(0)
                      }}
                      className="text-sm text-red-600 hover:text-red-800"
                    >
                      Remove File
                    </button>
                  </div>
                ) : (
                  <div className="space-y-2">
                    <div className="text-gray-400">
                      <svg className="mx-auto h-12 w-12" stroke="currentColor" fill="none" viewBox="0 0 48 48">
                        <path d="M28 8H12a4 4 0 00-4 4v20m32-12v8m0 0v8a4 4 0 01-4 4H12a4 4 0 01-4-4v-4m32-4l-3.172-3.172a4 4 0 00-5.656 0L28 28M8 32l9.172-9.172a4 4 0 015.656 0L28 28m0 0l4 4m4-24h8m-4-4v8m-12 4h.02" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
                      </svg>
                    </div>
                    <div>
                      <p className="text-sm text-gray-600">
                        Drag and drop files here, or
                        <label htmlFor="file-upload" className="cursor-pointer text-blue-600 hover:text-blue-800 font-medium">
                          click to select a file
                        </label>
                      </p>
                      <p className="text-xs text-gray-500 mt-1">
                        Supports .tar.gz format, max 500MB
                      </p>
                    </div>
                  </div>
                )}
              </div>
              
              {/* 上传进度 */}
              {uploadStatus === 'uploading' && (
                <div className="space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-gray-600">Upload Progress</span>
                    <span className="text-gray-600">{uploadProgress}%</span>
                  </div>
                  <div className="w-full bg-gray-200 rounded-full h-2">
                    <div
                      className="bg-blue-600 h-2 rounded-full transition-all duration-300"
                      style={{ width: `${uploadProgress}%` }}
                    ></div>
                  </div>
                </div>
              )}
              
              {/* 状态提示 */}
              {uploadStatus === 'success' && (
                <div className="flex items-center space-x-2 text-green-600">
                  <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  <span className="text-sm font-medium">File uploaded successfully</span>
                </div>
              )}
              
              {uploadStatus === 'error' && (
                <div className="flex items-center space-x-2 text-red-600">
                  <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  <span className="text-sm font-medium">File upload failed</span>
                </div>
              )}
            </div>
          </div>

          <div className="card bg-blue-50 border-blue-200">
            <h3 className="text-lg font-semibold text-blue-900 mb-3">Import Instructions</h3>
            <div className="space-y-3 text-blue-800">
              <p>Before starting the import, please ensure:</p>
              <ul className="space-y-2 ml-4">
                <li className="flex items-center">
                  <div className="w-2 h-2 bg-blue-600 rounded-full mr-3"></div>
                  Completed the data export step
                </li>
                <li className="flex items-center">
                  <div className="w-2 h-2 bg-blue-600 rounded-full mr-3"></div>
                  Target system has sufficient storage space
                </li>
                <li className="flex items-center">
                  <div className="w-2 h-2 bg-blue-600 rounded-full mr-3"></div>
                  Target system is installed with ZimaOS
                </li>
              </ul>
            </div>
          </div>

          <div className="text-center">
            <button
              onClick={startImport}
              disabled={!targetTestResult?.success || !uploadedFile || isImporting || uploadStatus === 'uploading'}
              className={`inline-flex items-center px-6 py-3 text-lg font-medium rounded-lg transition-colors duration-200 ${
                targetTestResult?.success && uploadedFile && !isImporting && uploadStatus !== 'uploading'
                  ? 'bg-blue-600 hover:bg-blue-700 text-white'
                  : 'bg-gray-300 text-gray-500 cursor-not-allowed'
              }`}
            >
              <Upload className="h-5 w-5 mr-2" />
              {isImporting ? 'Importing...' : uploadStatus === 'uploading' ? 'Uploading file...' : 'Start Import'}
            </button>
            
            {!targetTestResult?.success && (
              <p className="text-sm text-gray-600 mt-2">
                Please test and ensure the target system connection is successful
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export default OfflineMigrationPage