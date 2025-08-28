package main

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"ctoz/backend/internal/handlers"
	"ctoz/backend/internal/middleware"
	"ctoz/backend/internal/services"
	"ctoz/backend/internal/websocket"
)

func main() {
	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	// 创建Gin引擎
	r := gin.New()

	// 添加中间件
	r.Use(middleware.Logger())
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.RequestID())
	r.Use(middleware.Security())
	r.Use(middleware.ErrorHandler())
	r.Use(middleware.Timeout(30 * time.Second))

	// 创建WebSocket管理器
	wsManager := websocket.NewManager()
	go wsManager.Run()

	// 创建服务
	connService := services.NewConnectionService()
	taskService := services.NewTaskService(wsManager)
	migrationService := services.NewMigrationService(connService, taskService)

	// 创建处理器
	handler := handlers.NewHandler(connService, migrationService, taskService, wsManager)

	// 健康检查
	r.GET("/health", handler.HealthCheck)
	r.GET("/info", handler.GetSystemInfo)

	// API路由组
	api := r.Group("/api")
	{
		// 连接测试
		api.POST("/test-connection", handler.TestConnection)

		// 在线迁移
		api.POST("/online-migration", handler.StartOnlineMigration)

		// 数据导出
		api.POST("/data-export", handler.StartDataExport)
		
		// 直接导出下载
		api.POST("/export-download", handler.ExportDownload)

		// 数据导入
		api.POST("/data-import", handler.StartDataImport)
		
		// 文件上传导入
		api.POST("/data-import-upload", handler.DataImportUpload)
		
		// WebSocket测试端点
		api.POST("/test-websocket/:taskId", handler.TestWebSocket)
		
		// 创建测试任务
		api.POST("/create-test-task", handler.CreateTestTask)

		// 任务管理
		tasks := api.Group("/tasks")
		{
			tasks.GET("", handler.ListTasks)
			tasks.GET("/:id", handler.GetTaskStatus)
		tasks.DELETE("/:id", handler.DeleteTask)
		// 获取任务日志
		tasks.GET("/:id/logs", handler.GetTaskLogs)
		// 获取导入状态
		tasks.GET("/:id/import-status", handler.GetImportStatus)
			// 下载应用压缩包
			tasks.GET("/:id/download/:appName", handler.DownloadAppPackage)
		}
	}

	// WebSocket路由
	r.GET("/ws", handler.HandleWebSocket)

	// 静态文件服务（前端）
	r.Static("/static", "./dist")
	r.StaticFile("/", "./dist/index.html")
	r.NoRoute(func(c *gin.Context) {
		c.File("./dist/index.html")
	})

	// 启动服务器
	log.Println("CasaOS to ZimaOS Migration Tool 服务器启动在端口 :8080")
	log.Println("访问 http://localhost:8080 查看Web界面")
	log.Println("API文档: http://localhost:8080/info")
	log.Fatal(r.Run(":8080"))
}