package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/zhitu-agent/zhitu-agent/internal/chat"
	"github.com/zhitu-agent/zhitu-agent/internal/config"
	"github.com/zhitu-agent/zhitu-agent/internal/handler"
	"github.com/zhitu-agent/zhitu-agent/internal/middleware"
)

func main() {
	// 1. Load configuration
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate essential config
	if cfg.DashScope.APIKey == "" {
		log.Fatal("DASHSCOPE_API_KEY is required. Set it via config.yaml (dashscope.api_key) or DASHSCOPE_API_KEY env var")
	}

	// 2. Initialize chat service
	chatService, err := chat.NewService(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize chat service: %v", err)
	}

	// 3. Setup Gin router
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// Middleware chain — mirrors Java interceptor order
	r.Use(middleware.CORS())
	r.Use(middleware.Observability())
	r.Use(middleware.Guardrail())
	r.Use(middleware.ErrorHandler())

	// API routes — mirrors Java context-path: /api
	api := r.Group(cfg.Server.ContextPath)
	chatHandler := handler.NewChatHandler(chatService)
	handler.RegisterRoutes(api, chatHandler)

	// Health check
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Static files — mirrors Java static resources
	r.StaticFile("/chat", "./static/gpt.html")

	// 4. Start server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Starting ZhituAgent server on %s", addr)

	// Graceful shutdown
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")
}