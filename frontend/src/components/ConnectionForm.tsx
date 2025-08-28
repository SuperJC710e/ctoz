import React from 'react'
import { TestTube, CheckCircle, XCircle, Loader } from 'lucide-react'
import { SystemConnection } from '../types'

interface ConnectionFormProps {
  connection: SystemConnection
  onChange: (connection: SystemConnection) => void
  onTest: () => void
  testResult: { success: boolean; message: string } | null
  isTesting: boolean
  label: string
}

const ConnectionForm: React.FC<ConnectionFormProps> = ({
  connection,
  onChange,
  onTest,
  testResult,
  isTesting,
  label
}) => {
  const handleChange = (field: keyof SystemConnection, value: string | number) => {
    onChange({
      ...connection,
      [field]: value
    })
  }

  const isFormValid = connection.host && connection.username && connection.password

  return (
    <div className="space-y-4">
      {/* Host */}
      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">
          Host
        </label>
        <input
          type="text"
          value={connection.host}
          onChange={(e) => handleChange('host', e.target.value)}
          placeholder="192.168.1.100"
          className="input-field"
        />
      </div>

      {/* System Type - removed */}

      {/* Port */}
      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">
          Port
        </label>
        <input
          type="number"
          value={connection.port}
          onChange={(e) => handleChange('port', parseInt(e.target.value) || 80)}
          placeholder="80"
          className="input-field"
        />
        <p className="text-xs text-gray-500 mt-1">
          Web access port of CasaOS/ZimaOS. Usually 80, may differ.
        </p>
      </div>

      {/* Username */}
      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">
          Username
        </label>
        <input
          type="text"
          value={connection.username}
          onChange={(e) => handleChange('username', e.target.value)}
          placeholder="root"
          className="input-field"
        />
      </div>

      {/* Password */}
      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">
          Password
        </label>
        <input
          type="password"
          value={connection.password}
          onChange={(e) => handleChange('password', e.target.value)}
          placeholder="Enter password"
          className="input-field"
        />
      </div>



      {/* Test Connection */}
      <div className="space-y-3">
        <button
          onClick={onTest}
          disabled={!isFormValid || isTesting}
          className={`w-full flex items-center justify-center px-4 py-2 rounded-lg font-medium transition-colors duration-200 ${
            isFormValid && !isTesting
              ? 'bg-gray-100 hover:bg-gray-200 text-gray-800'
              : 'bg-gray-50 text-gray-400 cursor-not-allowed'
          }`}
        >
          {isTesting ? (
            <Loader className="h-4 w-4 mr-2 animate-spin" />
          ) : (
            <TestTube className="h-4 w-4 mr-2" />
          )}
          {isTesting ? 'Testing...' : `Test ${label} connection`}
        </button>

        {/* Test Result */}
        {testResult && (
          <div className={`flex items-center p-3 rounded-lg ${
            testResult.success 
              ? 'bg-green-50 border border-green-200' 
              : 'bg-red-50 border border-red-200'
          }`}>
            {testResult.success ? (
              <CheckCircle className="h-5 w-5 text-green-600 mr-2 flex-shrink-0" />
            ) : (
              <XCircle className="h-5 w-5 text-red-600 mr-2 flex-shrink-0" />
            )}
            <div>
              <p className={`text-sm font-medium ${
                testResult.success ? 'text-green-800' : 'text-red-800'
              }`}>
                {testResult.success ? 'Connection successful' : 'Connection failed'}
              </p>
              <p className={`text-xs ${
                testResult.success ? 'text-green-600' : 'text-red-600'
              }`}>
                {testResult.message}
              </p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export default ConnectionForm