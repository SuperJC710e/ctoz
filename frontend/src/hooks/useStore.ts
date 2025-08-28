import { create } from 'zustand'
import { MigrationTask, MigrationLog, SystemConnection } from '../types'

interface AppState {
  // 任务相关状态
  tasks: MigrationTask[]
  currentTask: MigrationTask | null
  logs: MigrationLog[]
  
  // 连接配置
  sourceConnection: SystemConnection | null
  targetConnection: SystemConnection | null
  
  // UI状态
  isLoading: boolean
  error: string | null
  
  // Actions
  setTasks: (tasks: MigrationTask[]) => void
  addTask: (task: MigrationTask) => void
  updateTask: (taskId: string, updates: Partial<MigrationTask>) => void
  removeTask: (taskId: string) => void
  setCurrentTask: (task: MigrationTask | null) => void
  
  addLog: (log: MigrationLog) => void
  clearLogs: () => void
  
  setSourceConnection: (connection: SystemConnection | null) => void
  setTargetConnection: (connection: SystemConnection | null) => void
  
  setLoading: (loading: boolean) => void
  setError: (error: string | null) => void
  clearError: () => void
}

export const useStore = create<AppState>((set) => ({
  // Initial state
  tasks: [],
  currentTask: null,
  logs: [],
  sourceConnection: null,
  targetConnection: null,
  isLoading: false,
  error: null,
  
  // Task actions
  setTasks: (tasks) => set({ tasks }),
  
  addTask: (task) => set((state) => ({
    tasks: [...state.tasks, task]
  })),
  
  updateTask: (taskId, updates) => set((state) => ({
    tasks: state.tasks.map(task => 
      task.id === taskId ? { ...task, ...updates } : task
    ),
    currentTask: state.currentTask?.id === taskId 
      ? { ...state.currentTask, ...updates }
      : state.currentTask
  })),
  
  removeTask: (taskId) => set((state) => ({
    tasks: state.tasks.filter(task => task.id !== taskId),
    currentTask: state.currentTask?.id === taskId ? null : state.currentTask
  })),
  
  setCurrentTask: (task) => set({ currentTask: task }),
  
  // Log actions
  addLog: (log) => set((state) => ({
    logs: [...state.logs, log]
  })),
  
  clearLogs: () => set({ logs: [] }),
  
  // Connection actions
  setSourceConnection: (connection) => set({ sourceConnection: connection }),
  setTargetConnection: (connection) => set({ targetConnection: connection }),
  
  // UI actions
  setLoading: (loading) => set({ isLoading: loading }),
  setError: (error) => set({ error }),
  clearError: () => set({ error: null })
}))