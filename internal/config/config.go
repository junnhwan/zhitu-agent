package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Redis      RedisConfig      `mapstructure:"redis"`
	DashScope  DashScopeConfig  `mapstructure:"dashscope"`
	RAG        RAGConfig        `mapstructure:"rag"`
	Mail       MailConfig       `mapstructure:"mail"`
	ChatMemory ChatMemoryConfig `mapstructure:"chat_memory"`
	BigModel   BigModelConfig   `mapstructure:"bigmodel"`
	Rerank     RerankConfig     `mapstructure:"rerank"`
	Monitoring MonitoringConfig `mapstructure:"monitoring"`
	Understand UnderstandConfig `mapstructure:"understand"`
}

type ServerConfig struct {
	Port        int    `mapstructure:"port"`
	ContextPath string `mapstructure:"context_path"`
}

type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	TTL      int    `mapstructure:"ttl"`
}

type DashScopeConfig struct {
	APIKey              string `mapstructure:"api_key"`
	ChatModel           string `mapstructure:"chat_model"`
	EmbeddingModel      string `mapstructure:"embedding_model"`
	EmbeddingDimensions int    `mapstructure:"embedding_dimensions"`
	RerankModel         string `mapstructure:"rerank_model"`
}

type RAGConfig struct {
	DocsPath      string              `mapstructure:"docs_path"`
	RetrieveTopK  int                 `mapstructure:"retrieve_top_k"`
	BaseRetriever RAGBaseRetrieverConfig `mapstructure:"base_retriever"`
	Rerank        RAGRerankConfig     `mapstructure:"rerank"`
}

type RAGBaseRetrieverConfig struct {
	MaxResults int     `mapstructure:"max_results"`
	MinScore   float64 `mapstructure:"min_score"`
}

type RAGRerankConfig struct {
	FinalTopN int `mapstructure:"final_top_n"`
}

type MailConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type ChatMemoryConfig struct {
	MaxMessages int                       `mapstructure:"max_messages"`
	Compression ChatMemoryCompressionConfig `mapstructure:"compression"`
	Redis       ChatMemoryRedisConfig     `mapstructure:"redis"`
}

type ChatMemoryCompressionConfig struct {
	Strategy             string `mapstructure:"strategy"`
	LLMModel             string `mapstructure:"llm_model"`
	MicroCompactThreshold int    `mapstructure:"micro_compact_threshold"`
	TokenThreshold       int    `mapstructure:"token_threshold"`
	RecentRounds         int    `mapstructure:"recent_rounds"`
	RecentTokenLimit     int    `mapstructure:"recent_token_limit"`
	SummaryTokenLimit    int    `mapstructure:"summary_token_limit"`
	SummaryPrompt        string `mapstructure:"summary_prompt"`
	FallbackRecentRounds int    `mapstructure:"fallback_recent_rounds"`
}

type ChatMemoryRedisConfig struct {
	TTLSeconds int                 `mapstructure:"ttl_seconds"`
	Lock       ChatMemoryLockConfig `mapstructure:"lock"`
}

type ChatMemoryLockConfig struct {
	ExpireSeconds  int `mapstructure:"expire_seconds"`
	RetryTimes     int `mapstructure:"retry_times"`
	RetryIntervalMs int `mapstructure:"retry_interval_ms"`
}

type BigModelConfig struct {
	APIKey string `mapstructure:"api_key"`
}

type RerankConfig struct {
	Test RerankTestConfig `mapstructure:"test"`
}

type RerankTestConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type MonitoringConfig struct {
	Prometheus MonitoringPrometheusConfig `mapstructure:"prometheus"`
	Grafana    MonitoringGrafanaConfig    `mapstructure:"grafana"`
}

type MonitoringPrometheusConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type MonitoringGrafanaConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type UnderstandConfig struct {
	Enabled              bool    `mapstructure:"enabled"`
	TreePath             string  `mapstructure:"tree_path"`
	LLMModel             string  `mapstructure:"llm_model"`
	ConfidenceThreshold  float64 `mapstructure:"confidence_threshold"`
	MaxClarifyAttempts   int     `mapstructure:"max_clarify_attempts"`
}

func Load(configPath string) (*Config, error) {
	v := viper.New()

	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// Environment variable overrides
	v.SetEnvPrefix("ZHU")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Set defaults matching Java application.yml
	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Override with environment variables for sensitive values
	overrideFromEnv(&cfg)

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Server
	v.SetDefault("server.port", 10010)
	v.SetDefault("server.context_path", "/api")

	// Redis
	v.SetDefault("redis.host", "127.0.0.1")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.ttl", 3600)

	// DashScope
	v.SetDefault("dashscope.chat_model", "qwen-max")
	v.SetDefault("dashscope.embedding_model", "text-embedding-v3")
	v.SetDefault("dashscope.embedding_dimensions", 1024)
	v.SetDefault("dashscope.rerank_model", "qwen3-rerank")

	// RAG
	v.SetDefault("rag.docs_path", "./docs")
	v.SetDefault("rag.retrieve_top_k", 3)
	v.SetDefault("rag.base_retriever.max_results", 30)
	v.SetDefault("rag.base_retriever.min_score", 0.55)
	v.SetDefault("rag.rerank.final_top_n", 5)

	// Mail
	v.SetDefault("mail.host", "smtp.qq.com")
	v.SetDefault("mail.port", 587)

	// Chat Memory
	v.SetDefault("chat_memory.max_messages", 20)
	v.SetDefault("chat_memory.compression.strategy", "simple")
	v.SetDefault("chat_memory.compression.llm_model", "qwen-turbo")
	v.SetDefault("chat_memory.compression.micro_compact_threshold", 2000)
	v.SetDefault("chat_memory.compression.token_threshold", 6000)
	v.SetDefault("chat_memory.compression.recent_rounds", 5)
	v.SetDefault("chat_memory.compression.recent_token_limit", 2000)
	v.SetDefault("chat_memory.compression.summary_token_limit", 500)
	v.SetDefault("chat_memory.compression.fallback_recent_rounds", 10)
	v.SetDefault("chat_memory.redis.ttl_seconds", 3600)
	v.SetDefault("chat_memory.redis.lock.expire_seconds", 5)
	v.SetDefault("chat_memory.redis.lock.retry_times", 3)
	v.SetDefault("chat_memory.redis.lock.retry_interval_ms", 100)

	// Rerank test
	v.SetDefault("rerank.test.enabled", false)

	// Monitoring
	v.SetDefault("monitoring.prometheus.enabled", true)
	v.SetDefault("monitoring.grafana.enabled", true)
}

func overrideFromEnv(cfg *Config) {
	if v := os.Getenv("APP_PORT"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.Server.Port)
	}
	if v := os.Getenv("QWEN_API_KEY"); v != "" {
		cfg.DashScope.APIKey = v
	}
	if v := os.Getenv("QWEN_CHAT_MODEL"); v != "" {
		cfg.DashScope.ChatModel = v
	}
	if v := os.Getenv("QWEN_EMBEDDING_MODEL"); v != "" {
		cfg.DashScope.EmbeddingModel = v
	}
	if v := os.Getenv("QWEN_RERANK_MODEL"); v != "" {
		cfg.DashScope.RerankModel = v
	}
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		parts := strings.SplitN(v, ":", 2)
		cfg.Redis.Host = parts[0]
		if len(parts) == 2 {
			fmt.Sscanf(parts[1], "%d", &cfg.Redis.Port)
		}
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := os.Getenv("SMTP_HOST"); v != "" {
		cfg.Mail.Host = v
	}
	if v := os.Getenv("SMTP_PORT"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.Mail.Port)
	}
	if v := os.Getenv("SMTP_USER"); v != "" {
		cfg.Mail.Username = v
	}
	if v := os.Getenv("SMTP_PASS"); v != "" {
		cfg.Mail.Password = v
	}
	if v := os.Getenv("BIGMODEL_API_KEY"); v != "" {
		cfg.BigModel.APIKey = v
	}
	if v := os.Getenv("RAG_DOCS_PATH"); v != "" {
		cfg.RAG.DocsPath = v
	}
	if v := os.Getenv("QWEN_RERANK_VERIFY_ON_STARTUP"); v == "true" {
		cfg.Rerank.Test.Enabled = true
	}
	if v := os.Getenv("GUARDRAIL_SENSITIVE_WORDS"); v != "" {
		// Stored as comma-separated, parsed in middleware
		_ = v
	}
}
