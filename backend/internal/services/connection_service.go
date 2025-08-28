package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ctoz/backend/internal/models"
	"ctoz/backend/internal/storage"

	"github.com/google/uuid"
)

// ConnectionService 连接服务
type ConnectionService struct {
	client *http.Client
	store  *storage.MemoryStore
}

// NewConnectionService 创建新的连接服务
func NewConnectionService() *ConnectionService {
	return &ConnectionService{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		store: storage.NewMemoryStore(),
	}
}

// TestConnection 测试系统连接
func (s *ConnectionService) TestConnection(conn *models.SystemConnection) (*models.ConnectionTestResponse, error) {
	if conn == nil {
		return &models.ConnectionTestResponse{
			Success: false,
			Message: "连接信息不能为空",
		}, nil
	}

	// 验证必填字段
	if conn.Host == "" {
		return &models.ConnectionTestResponse{
			Success: false,
			Message: "主机地址不能为空",
		}, nil
	}

	if conn.Port <= 0 || conn.Port > 65535 {
		return &models.ConnectionTestResponse{
			Success: false,
			Message: "端口号必须在1-65535之间",
		}, nil
	}

	if conn.Username == "" {
		return &models.ConnectionTestResponse{
			Success: false,
			Message: "用户名不能为空",
		}, nil
	}

	if conn.Password == "" {
		return &models.ConnectionTestResponse{
			Success: false,
			Message: "密码不能为空",
		}, nil
	}

	// 根据系统类型进行连接测试
	switch conn.Type {
	case models.SystemTypeCasaOS:
		response, err := s.testCasaOSConnection(conn)
		if err == nil && response.Success {
			// 保存连接信息
			if conn.ID == "" {
				conn.ID = uuid.New().String()
			}
			s.store.SaveConnection(conn)
		}
		return response, err
	case models.SystemTypeZimaOS:
		response, err := s.testZimaOSConnection(conn)
		if err == nil && response.Success {
			// 保存连接信息
			if conn.ID == "" {
				conn.ID = uuid.New().String()
			}
			s.store.SaveConnection(conn)
		}
		return response, err
	default:
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("不支持的系统类型: %s", conn.Type),
		}, nil
	}
}

// testCasaOSConnection 测试CasaOS连接
func (s *ConnectionService) testCasaOSConnection(conn *models.SystemConnection) (*models.ConnectionTestResponse, error) {
	// 构建登录API URL
	apiURL := fmt.Sprintf("http://%s:%d/v1/users/login", conn.Host, conn.Port)

	// 构建登录请求体
	loginData := map[string]string{
		"username": conn.Username,
		"password": conn.Password,
	}

	loginJSON, err := json.Marshal(loginData)
	if err != nil {
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to build login payload: %v", err),
		}, nil
	}

	// 调试日志：记录请求信息
	fmt.Printf("[CasaOS DEBUG] Request URL: %s\n", apiURL)
	fmt.Printf("[CasaOS DEBUG] Request body: %s\n", string(loginJSON))

	// 创建登录请求
	req, err := http.NewRequest("POST", apiURL, strings.NewReader(string(loginJSON)))
	if err != nil {
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create login request: %v", err),
		}, nil
	}

	// 设置请求头
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Language", "en_us")
	req.Header.Set("Origin", fmt.Sprintf("http://%s:%d", conn.Host, conn.Port))
	req.Header.Set("Referer", fmt.Sprintf("http://%s:%d/", conn.Host, conn.Port))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")

	// 调试日志：记录请求头
	fmt.Printf("[CasaOS DEBUG] Request headers: %+v\n", req.Header)

	// 发送登录请求
	resp, err := s.client.Do(req)
	if err != nil {
		fmt.Printf("[CasaOS DEBUG] Request failed: %v\n", err)
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("CasaOS login connection failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	// 调试日志：记录响应状态码
	fmt.Printf("[CasaOS DEBUG] Response status: %d\n", resp.StatusCode)
	fmt.Printf("[CasaOS DEBUG] Response headers: %+v\n", resp.Header)

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("[CasaOS DEBUG] Failed to read response: %v\n", err)
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to read CasaOS login response: %v", err),
		}, nil
	}

	// 调试日志：记录完整响应内容
	fmt.Printf("[CasaOS DEBUG] Response body: %s\n", string(body))

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[CasaOS DEBUG] HTTP status error: %d\n", resp.StatusCode)
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("CasaOS login failed, status code: %d, response: %s", resp.StatusCode, string(body)),
		}, nil
	}

	// 解析登录响应
	var loginResponse map[string]interface{}
	if err := json.Unmarshal(body, &loginResponse); err != nil {
		fmt.Printf("[CasaOS DEBUG] JSON parse failed: %v\n", err)
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to parse CasaOS login response: %v", err),
		}, nil
	}

	// 调试日志：记录解析后的响应结构
	fmt.Printf("[CasaOS DEBUG] Parsed response: %+v\n", loginResponse)

	// 检查登录是否成功 - 支持数字200和布尔值true
	var isSuccess bool
	if success, ok := loginResponse["success"].(bool); ok {
		isSuccess = success
	} else if successNum, ok := loginResponse["success"].(float64); ok {
		isSuccess = successNum == 200
	} else {
		fmt.Printf("[CasaOS DEBUG] Unknown success field type: %T, value: %v\n", loginResponse["success"], loginResponse["success"])
		isSuccess = false
	}

	if !isSuccess {
		// Provide clearer error description rather than vague server messages
		message := "CasaOS login failed: invalid username or password"
		if msg, ok := loginResponse["message"].(string); ok && msg != "" && msg != "ok" {
			// Only use server message when it is meaningful
			message = fmt.Sprintf("CasaOS login failed: %s", msg)
		}
		return &models.ConnectionTestResponse{
			Success: false,
			Message: message,
		}, nil
	}

	// 提取token - 从data.token.access_token路径获取
	var token string
	if data, ok := loginResponse["data"].(map[string]interface{}); ok {
		if tokenData, ok := data["token"].(map[string]interface{}); ok {
			if accessToken, ok := tokenData["access_token"].(string); ok {
				token = accessToken
				fmt.Printf("[CasaOS DEBUG] Extracted token: %s\n", token)
			} else {
				fmt.Printf("[CasaOS DEBUG] access_token missing or wrong type: %T, value: %v\n", tokenData["access_token"], tokenData["access_token"])
			}
		} else {
			fmt.Printf("[CasaOS DEBUG] token missing or wrong type: %T, value: %v\n", data["token"], data["token"])
		}
	} else {
		fmt.Printf("[CasaOS DEBUG] data missing or wrong type: %T, value: %v\n", loginResponse["data"], loginResponse["data"])
	}

	// 保存token到连接信息
	conn.Token = token

	return &models.ConnectionTestResponse{
		Success: true,
		Message: "CasaOS login successful",
		SystemInfo: map[string]interface{}{
			"type":     "CasaOS",
			"host":     conn.Host,
			"port":     conn.Port,
			"username": conn.Username,
			"token":    token,
		},
	}, nil
}

// testZimaOSConnection 测试ZimaOS连接
func (s *ConnectionService) testZimaOSConnection(conn *models.SystemConnection) (*models.ConnectionTestResponse, error) {
	// 构建登录API URL
	apiURL := fmt.Sprintf("http://%s:%d/v1/users/login", conn.Host, conn.Port)

	// 构建登录请求体
	loginData := map[string]string{
		"username": conn.Username,
		"password": conn.Password,
	}

	loginJSON, err := json.Marshal(loginData)
	if err != nil {
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to build login payload: %v", err),
		}, nil
	}

	// 调试日志：记录请求信息
	fmt.Printf("[ZimaOS DEBUG] Request URL: %s\n", apiURL)
	fmt.Printf("[ZimaOS DEBUG] Request body: %s\n", string(loginJSON))

	// 创建登录请求
	req, err := http.NewRequest("POST", apiURL, strings.NewReader(string(loginJSON)))
	if err != nil {
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create login request: %v", err),
		}, nil
	}

	// 设置请求头
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", fmt.Sprintf("http://%s:%d", conn.Host, conn.Port))
	req.Header.Set("Referer", fmt.Sprintf("http://%s:%d/", conn.Host, conn.Port))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")

	// 调试日志：记录请求头
	fmt.Printf("[ZimaOS DEBUG] Request headers: %+v\n", req.Header)

	// 发送登录请求
	resp, err := s.client.Do(req)
	if err != nil {
		fmt.Printf("[ZimaOS DEBUG] Request failed: %v\n", err)
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("ZimaOS login connection failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	// 调试日志：记录响应状态码
	fmt.Printf("[ZimaOS DEBUG] Response status: %d\n", resp.StatusCode)
	fmt.Printf("[ZimaOS DEBUG] Response headers: %+v\n", resp.Header)

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("[ZimaOS DEBUG] Failed to read response: %v\n", err)
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to read ZimaOS login response: %v", err),
		}, nil
	}

	// 调试日志：记录完整响应内容
	fmt.Printf("[ZimaOS DEBUG] Response body: %s\n", string(body))

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[ZimaOS DEBUG] HTTP status error: %d\n", resp.StatusCode)
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("ZimaOS login failed, status code: %d, response: %s", resp.StatusCode, string(body)),
		}, nil
	}

	// 解析登录响应
	var loginResponse map[string]interface{}
	if err := json.Unmarshal(body, &loginResponse); err != nil {
		fmt.Printf("[ZimaOS DEBUG] JSON parse failed: %v\n", err)
		return &models.ConnectionTestResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to parse ZimaOS login response: %v", err),
		}, nil
	}

	// 调试日志：记录解析后的响应结构
	fmt.Printf("[ZimaOS DEBUG] Parsed response: %+v\n", loginResponse)

	// 检查登录是否成功 - 支持数字200和布尔值true
	var isSuccess bool
	if success, ok := loginResponse["success"].(bool); ok {
		isSuccess = success
	} else if successNum, ok := loginResponse["success"].(float64); ok {
		isSuccess = successNum == 200
	} else {
		fmt.Printf("[ZimaOS DEBUG] Unknown success field type: %T, value: %v\n", loginResponse["success"], loginResponse["success"])
		isSuccess = false
	}

	if !isSuccess {
		// Provide clearer error description rather than vague server messages
		message := "ZimaOS login failed: invalid username or password"
		if msg, ok := loginResponse["message"].(string); ok && msg != "" && msg != "ok" {
			// Only use server message when it is meaningful
			message = fmt.Sprintf("ZimaOS login failed: %s", msg)
		}
		return &models.ConnectionTestResponse{
			Success: false,
			Message: message,
		}, nil
	}

	// 提取token - 从data.token.access_token路径获取
	var token string
	if data, ok := loginResponse["data"].(map[string]interface{}); ok {
		if tokenData, ok := data["token"].(map[string]interface{}); ok {
			if accessToken, ok := tokenData["access_token"].(string); ok {
				token = accessToken
				fmt.Printf("[ZimaOS DEBUG] Extracted token: %s\n", token)
			} else {
				fmt.Printf("[ZimaOS DEBUG] access_token missing or wrong type: %T, value: %v\n", tokenData["access_token"], tokenData["access_token"])
			}
		} else {
			fmt.Printf("[ZimaOS DEBUG] token missing or wrong type: %T, value: %v\n", data["token"], data["token"])
		}
	} else {
		fmt.Printf("[ZimaOS DEBUG] data missing or wrong type: %T, value: %v\n", loginResponse["data"], loginResponse["data"])
	}

	// 保存token到连接信息
	conn.Token = token

	return &models.ConnectionTestResponse{
		Success: true,
		Message: "ZimaOS login successful",
		SystemInfo: map[string]interface{}{
			"type":     "ZimaOS",
			"host":     conn.Host,
			"port":     conn.Port,
			"username": conn.Username,
			"token":    token,
		},
	}, nil
}

// GetSystemInfo 获取系统信息
func (s *ConnectionService) GetSystemInfo(conn *models.SystemConnection) (map[string]interface{}, error) {
	if conn == nil {
		return nil, fmt.Errorf("connection info must not be empty")
	}

	var apiURL string
	switch conn.Type {
	case models.SystemTypeCasaOS:
		apiURL = fmt.Sprintf("http://%s:%d/v1/sys/info", conn.Host, conn.Port)
	case models.SystemTypeZimaOS:
		apiURL = fmt.Sprintf("http://%s:%d/v2/sys/info", conn.Host, conn.Port)
	default:
		return nil, fmt.Errorf("unsupported system type: %s", conn.Type)
	}

	// 创建请求
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 添加认证头
	if conn.Token != "" {
		req.Header.Set("Authorization", "Bearer "+conn.Token)
	}

	// 发送请求
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	return result, nil
}

// ValidateConnectionConfig 验证连接配置
func (s *ConnectionService) ValidateConnectionConfig(conn *models.SystemConnection) error {
	if conn == nil {
		return fmt.Errorf("连接信息不能为空")
	}

	if strings.TrimSpace(conn.Host) == "" {
		return fmt.Errorf("主机地址不能为空")
	}

	if conn.Port <= 0 || conn.Port > 65535 {
		return fmt.Errorf("端口号必须在1-65535之间")
	}

	if strings.TrimSpace(conn.Username) == "" {
		return fmt.Errorf("用户名不能为空")
	}

	if strings.TrimSpace(conn.Password) == "" {
		return fmt.Errorf("密码不能为空")
	}

	// 修复系统类型大小写问题
	lowerType := strings.ToLower(conn.Type)
	if lowerType == "casaos" {
		conn.Type = models.SystemTypeCasaOS
	} else if lowerType == "zimaos" {
		conn.Type = models.SystemTypeZimaOS
	} else {
		return fmt.Errorf("不支持的系统类型: %s", conn.Type)
	}

	return nil
}
