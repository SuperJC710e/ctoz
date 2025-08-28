package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"ctoz/backend/internal/models"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// 允许所有来源，生产环境中应该更严格
		return true
	},
}

// Client WebSocket客户端
type Client struct {
	Conn   *websocket.Conn
	Send   chan models.WSMessage
	TaskID string
}

// Manager WebSocket管理器
type Manager struct {
	Clients    map[string]map[*Client]bool // taskID -> clients
	Broadcast  chan BroadcastMessage
	Register   chan *Client
	Unregister chan *Client
	mu         sync.RWMutex
}

// BroadcastMessage 广播消息
type BroadcastMessage struct {
	TaskID  string
	Message models.WSMessage
}

// NewManager 创建新的WebSocket管理器
func NewManager() *Manager {
	return &Manager{
		Clients:    make(map[string]map[*Client]bool),
		Broadcast:  make(chan BroadcastMessage),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
	}
}

// Run 运行WebSocket管理器
func (m *Manager) Run() {
	for {
		select {
		case client := <-m.Register:
			log.Printf("[DEBUG] 注册WebSocket客户端 - TaskID: %s", client.TaskID)
			m.mu.Lock()
			if m.Clients[client.TaskID] == nil {
				m.Clients[client.TaskID] = make(map[*Client]bool)
			}
			m.Clients[client.TaskID][client] = true
			log.Printf("[DEBUG] 任务 %s 现在有 %d 个连接的客户端", client.TaskID, len(m.Clients[client.TaskID]))
			m.mu.Unlock()
			log.Printf("客户端连接到任务 %s", client.TaskID)

		case client := <-m.Unregister:
			log.Printf("[DEBUG] 注销WebSocket客户端 - TaskID: %s", client.TaskID)
			m.mu.Lock()
			if clients, ok := m.Clients[client.TaskID]; ok {
				if _, ok := clients[client]; ok {
					delete(clients, client)
					close(client.Send)
					if len(clients) == 0 {
						delete(m.Clients, client.TaskID)
						log.Printf("[DEBUG] 任务 %s 的所有客户端已断开连接", client.TaskID)
					} else {
						log.Printf("[DEBUG] 任务 %s 还有 %d 个连接的客户端", client.TaskID, len(clients))
					}
				}
			}
			m.mu.Unlock()
			log.Printf("客户端从任务 %s 断开连接", client.TaskID)

		case message := <-m.Broadcast:
			m.mu.RLock()
			clients := m.Clients[message.TaskID]
			clientCount := len(clients)
			m.mu.RUnlock()

			log.Printf("[DEBUG] 广播消息到任务 %s 的 %d 个客户端 - 消息类型: %s", message.TaskID, clientCount, message.Message.Type)

			if clientCount == 0 {
				log.Printf("[DEBUG] 任务 %s 没有连接的客户端，消息被丢弃", message.TaskID)
				continue
			}

			for client := range clients {
				select {
				case client.Send <- message.Message:
					log.Printf("[DEBUG] 消息成功发送到任务 %s 的客户端", message.TaskID)
				default:
					log.Printf("[DEBUG] 客户端发送缓冲区已满，移除客户端 - TaskID: %s", message.TaskID)
					m.mu.Lock()
					delete(clients, client)
					close(client.Send)
					if len(clients) == 0 {
						delete(m.Clients, message.TaskID)
					}
					m.mu.Unlock()
				}
			}
		}
	}
}

// HandleWebSocket 处理WebSocket连接
func (m *Manager) HandleWebSocket(c *gin.Context) {
	taskID := c.Query("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "任务ID不能为空"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket升级失败: %v", err)
		return
	}

	log.Printf("[WebSocket] 客户端成功连接到任务 %s", taskID)

	client := &Client{
		Conn:   conn,
		Send:   make(chan models.WSMessage, 256),
		TaskID: taskID,
	}

	m.Register <- client

	// 启动goroutines处理读写
	go m.writePump(client)
	go m.readPump(client)
}

// readPump 处理从WebSocket读取消息
func (m *Manager) readPump(client *Client) {
	defer func() {
		log.Printf("[WebSocket] 客户端从任务 %s 断开连接", client.TaskID)
		m.Unregister <- client
		client.Conn.Close()
	}()

	client.Conn.SetReadLimit(512)
	client.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	client.Conn.SetPongHandler(func(string) error {
		client.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WebSocket] 任务 %s 连接异常关闭: %v", client.TaskID, err)
			} else {
				log.Printf("[WebSocket] 任务 %s 连接正常关闭: %v", client.TaskID, err)
			}
			break
		}
	}
}

// writePump 处理向WebSocket写入消息
func (m *Manager) writePump(client *Client) {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		client.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.Send:
			client.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			data, err := json.Marshal(message)
			if err != nil {
				log.Printf("序列化消息失败: %v", err)
				continue
			}

			if err := client.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("写入WebSocket消息失败: %v", err)
				return
			}

		case <-ticker.C:
			client.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// SendMessage 发送消息到指定任务的所有客户端
func (m *Manager) SendMessage(taskID string, message models.WSMessage) {
	message.Timestamp = time.Now()
	log.Printf("[DEBUG] SendMessage - TaskID: %s, Type: %s", taskID, message.Type)
	m.Broadcast <- BroadcastMessage{
		TaskID:  taskID,
		Message: message,
	}
}

// SendTaskStatus 发送任务状态更新
func (m *Manager) SendTaskStatus(taskID string, status models.TaskStatus, message string) {
	wsMessage := models.WSMessage{
		Type: "task_status",
		Data: map[string]interface{}{
			"task_id": taskID,
			"status":  status,
			"message": message,
		},
		Timestamp: time.Now(),
	}
	m.SendMessage(taskID, wsMessage)
}

// SendProgress 发送任务进度更新
func (m *Manager) SendProgress(taskID string, progress int, step, message string) {
	wsMessage := models.WSMessage{
		Type: "task_progress",
		Data: map[string]interface{}{
			"task_id":  taskID,
			"progress": progress,
			"step":     step,
			"message":  message,
		},
		Timestamp: time.Now(),
	}
	m.SendMessage(taskID, wsMessage)
}

// SendLog 发送任务日志
func (m *Manager) SendLog(taskID, level, message string) {
	log.Printf("[DEBUG] SendLog - TaskID: %s, Level: %s, Message: %s", taskID, level, message)
	wsMessage := models.WSMessage{
		Type: "task_log",
		Data: map[string]interface{}{
			"task_id": taskID,
			"level":   level,
			"message": message,
			"time":    time.Now(),
		},
		Timestamp: time.Now(),
	}
	m.SendMessage(taskID, wsMessage)
}

// SendStepStart 发送步骤开始消息
func (m *Manager) SendStepStart(taskID, step, message string) {
	wsMessage := models.WSMessage{
		Type: "step",
		Data: map[string]interface{}{
			"task_id": taskID,
			"step":    step,
			"status":  "start",
			"message": message,
		},
		Timestamp: time.Now(),
	}
	m.SendMessage(taskID, wsMessage)
}

// SendStepProgress 发送步骤进度消息
func (m *Manager) SendStepProgress(taskID, step, message string, progress int) {
	wsMessage := models.WSMessage{
		Type: "step",
		Data: map[string]interface{}{
			"task_id":  taskID,
			"step":     step,
			"status":   "progress",
			"message":  message,
			"progress": progress,
		},
		Timestamp: time.Now(),
	}
	m.SendMessage(taskID, wsMessage)
}

// SendStepComplete 发送步骤完成消息
func (m *Manager) SendStepComplete(taskID, step, message string) {
	wsMessage := models.WSMessage{
		Type: "step",
		Data: map[string]interface{}{
			"task_id": taskID,
			"step":    step,
			"status":  "complete",
			"message": message,
		},
		Timestamp: time.Now(),
	}
	m.SendMessage(taskID, wsMessage)
}

// SendStepError 发送步骤错误消息
func (m *Manager) SendStepError(taskID, step, message, errorMsg string) {
	wsMessage := models.WSMessage{
		Type: "step",
		Data: map[string]interface{}{
			"task_id": taskID,
			"step":    step,
			"status":  "error",
			"message": message,
			"error":   errorMsg,
		},
		Timestamp: time.Now(),
	}
	m.SendMessage(taskID, wsMessage)
}