import { WSMessage } from '../types'

type WSEventHandler = (message: WSMessage) => void

class WebSocketClient {
  private ws: WebSocket | null = null
  private handlers: Map<string, WSEventHandler[]> = new Map()
  private reconnectAttempts = 0
  private maxReconnectAttempts = 5
  private reconnectDelay = 1000
  private taskId: string | null = null

  connect(taskId: string): void {
    this.taskId = taskId
    this.connectWebSocket()
  }

  private connectWebSocket(): void {
    if (!this.taskId) return

    // 使用相对路径，让Vite代理处理WebSocket连接
    const wsUrl = `ws://${window.location.host}/ws?task_id=${this.taskId}`
    console.log(`[WebSocket] 尝试连接到: ${wsUrl}`)
    
    try {
      this.ws = new WebSocket(wsUrl)
      
      this.ws.onopen = () => {
        console.log('[WebSocket] 连接成功')
        this.reconnectAttempts = 0
      }
      
      this.ws.onmessage = (event) => {
        console.log('[WebSocket] 收到消息:', event.data)
        try {
          const message: WSMessage = JSON.parse(event.data)
          console.log('[WebSocket] 解析后的消息:', message)
          this.handleMessage(message)
        } catch (error) {
          console.error('[WebSocket] 解析消息失败:', error, '原始数据:', event.data)
        }
      }
      
      this.ws.onclose = (event) => {
        console.log(`[WebSocket] 连接关闭 - Code: ${event.code}, Reason: ${event.reason}, WasClean: ${event.wasClean}`)
        
        // 分析关闭原因
        if (event.code === 1005) {
          console.log('[WebSocket] Close code 1005: 没有状态码的异常断开，可能是网络问题或服务器主动关闭')
        } else if (event.code === 1006) {
          console.log('[WebSocket] Close code 1006: 连接异常断开，没有发送关闭帧')
        } else if (event.code === 1000) {
          console.log('[WebSocket] Close code 1000: 正常关闭')
          // 正常关闭时不需要重连
          return
        }
        
        this.attemptReconnect()
      }
      
      this.ws.onerror = (error) => {
        console.error('[WebSocket] connection error:', error)
        console.log('[WebSocket] Error details - ReadyState:', this.ws?.readyState, 'URL:', wsUrl)
        
        // trigger custom error event
        const errorHandlers = this.handlers.get('error') || []
        errorHandlers.forEach(handler => {
          try {
            handler({ type: 'task_log', timestamp: new Date().toISOString(), data: { level: 'error', message: 'WebSocket connection error', task_id: '', time: new Date().toISOString() } } as WSMessage)
          } catch (handlerError) {
            console.error('Error in WebSocket error handler:', handlerError)
          }
        })
      }
    } catch (error) {
      console.error('[WebSocket] failed to create connection:', error)
      this.attemptReconnect()
    }
  }

  private attemptReconnect(): void {
    if (this.reconnectAttempts < this.maxReconnectAttempts) {
      this.reconnectAttempts++
      console.log(`Attempting to reconnect... (${this.reconnectAttempts}/${this.maxReconnectAttempts})`)
      
      setTimeout(() => {
        this.connectWebSocket()
      }, this.reconnectDelay * this.reconnectAttempts)
    } else {
      console.error('Max reconnection attempts reached')
    }
  }

  private handleMessage(message: WSMessage): void {
    const handlers = this.handlers.get(message.type) || []
    handlers.forEach(handler => {
      try {
        handler(message)
      } catch (error) {
        console.error('Error in WebSocket message handler:', error)
      }
    })
  }

  on(eventType: string, handler: WSEventHandler): void {
    if (!this.handlers.has(eventType)) {
      this.handlers.set(eventType, [])
    }
    this.handlers.get(eventType)!.push(handler)
  }

  off(eventType: string, handler: WSEventHandler): void {
    const handlers = this.handlers.get(eventType)
    if (handlers) {
      const index = handlers.indexOf(handler)
      if (index > -1) {
        handlers.splice(index, 1)
      }
    }
  }

  disconnect(): void {
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    this.handlers.clear()
    this.taskId = null
    this.reconnectAttempts = 0
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }
}

export const wsClient = new WebSocketClient()
export default wsClient