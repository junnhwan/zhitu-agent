package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	_ "github.com/joho/godotenv/autoload"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/zhitu-agent/zhitu-agent/internal/chat"
	"github.com/zhitu-agent/zhitu-agent/internal/config"
	"github.com/zhitu-agent/zhitu-agent/internal/handler"
	"github.com/zhitu-agent/zhitu-agent/internal/middleware"
	"github.com/zhitu-agent/zhitu-agent/internal/monitor"
	"github.com/zhitu-agent/zhitu-agent/internal/rag"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Initialize RAG (Redis + embedding + indexer + retriever)
	var ragSystem *rag.RAG
	ragSystem, err = rag.NewRAG(ctx, cfg, monitor.DefaultRegistry.AiMetrics)
	if err != nil {
		log.Printf("Warning: RAG initialization failed (continuing without RAG): %v", err)
	} else {
		// Startup: load docs, verify rerank, start auto-reload
		ragSystem.Startup(ctx)
		log.Println("RAG system initialized successfully")
	}

	// 3. Initialize chat service
	chatService, err := chat.NewService(cfg, ragSystem)
	if err != nil {
		log.Fatalf("Failed to initialize chat service: %v", err)
	}

	// 3.1 Initialize orchestrator for multi-agent chat
	chatService.InitOrchestrator()

	// 4. Setup Gin router
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

	// Prometheus metrics endpoint — mirrors Java Micrometer /metrics
	if cfg.Monitoring.Prometheus.Enabled {
		r.GET("/metrics", gin.WrapH(promhttp.HandlerFor(monitor.DefaultRegistry.Prometheus, promhttp.HandlerOpts{})))
		log.Println("Prometheus metrics endpoint enabled at /metrics")
	}

	// Static files — mirrors Java static resources
	r.StaticFile("/chat", "./static/gpt.html")
	r.StaticFile("/gpt.html", "./static/gpt.html")
	r.StaticFile("/ai.png", "./static/ai.png")
	r.StaticFile("/user.png", "./static/user.png")

	// 5. Start server
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

	// Shutdown RAG
	if ragSystem != nil {
		ragSystem.Shutdown()
	}

	cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
