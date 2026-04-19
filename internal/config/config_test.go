package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Create a minimal config file
	content := `
server:
  port: 10010
  context_path: /api
redis:
  host: 127.0.0.1
  port: 6379
dashscope:
  api_key: "test-key"
`
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Port != 10010 {
		t.Errorf("Server.Port = %d, want 10010", cfg.Server.Port)
	}
	if cfg.Server.ContextPath != "/api" {
		t.Errorf("Server.ContextPath = %q, want /api", cfg.Server.ContextPath)
	}
	if cfg.DashScope.APIKey != "test-key" {
		t.Errorf("DashScope.APIKey = %q, want test-key", cfg.DashScope.APIKey)
	}
	if cfg.DashScope.ChatModel != "qwen-max" {
		t.Errorf("DashScope.ChatModel = %q, want qwen-max (default)", cfg.DashScope.ChatModel)
	}
	if cfg.Redis.Host != "127.0.0.1" {
		t.Errorf("Redis.Host = %q, want 127.0.0.1", cfg.Redis.Host)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	content := `
server:
  port: 10010
redis:
  host: 127.0.0.1
  port: 6379
dashscope:
  api_key: "original"
`
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Set env overrides
	os.Setenv("QWEN_API_KEY", "env-key")
	os.Setenv("REDIS_ADDR", "10.0.0.1")
	defer os.Unsetenv("QWEN_API_KEY")
	defer os.Unsetenv("REDIS_ADDR")

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.DashScope.APIKey != "env-key" {
		t.Errorf("DashScope.APIKey = %q, want env-key (env override)", cfg.DashScope.APIKey)
	}
	if cfg.Redis.Host != "10.0.0.1" {
		t.Errorf("Redis.Host = %q, want 10.0.0.1 (env override)", cfg.Redis.Host)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("nonexistent.yaml")
	if err == nil {
		t.Error("Load() should return error for missing file")
	}
}

func TestLoadMCPClientURLEnvExpansion(t *testing.T) {
	content := `
server:
  port: 10010
redis:
  host: 127.0.0.1
  port: 6379
mcp:
  client:
    enabled: true
    servers:
      - name: bigmodel-search
        transport: sse
        url: "https://open.bigmodel.cn/api/mcp/web_search/sse?Authorization=${BIGMODEL_API_KEY_TEST}"
        enabled: true
      - name: stdio-with-env
        transport: stdio
        command: ["sh"]
        env:
          FOO: "prefix-${MCP_TEST_FOO}-suffix"
`
	tmpFile, err := os.CreateTemp("", "config-mcp-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	os.Setenv("BIGMODEL_API_KEY_TEST", "secret-xyz")
	os.Setenv("MCP_TEST_FOO", "bar")
	defer os.Unsetenv("BIGMODEL_API_KEY_TEST")
	defer os.Unsetenv("MCP_TEST_FOO")

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.MCP.Client.Servers) != 2 {
		t.Fatalf("servers len=%d, want 2", len(cfg.MCP.Client.Servers))
	}
	wantURL := "https://open.bigmodel.cn/api/mcp/web_search/sse?Authorization=secret-xyz"
	if cfg.MCP.Client.Servers[0].URL != wantURL {
		t.Errorf("url=%q, want %q", cfg.MCP.Client.Servers[0].URL, wantURL)
	}
	if cfg.MCP.Client.Servers[1].Env["foo"] != "prefix-bar-suffix" {
		// viper lowercases map keys; downstream MCP client must use the
		// lowercased key when constructing stdio env (see types.MCPServerConfig.Env).
		t.Errorf("env foo=%q, want prefix-bar-suffix", cfg.MCP.Client.Servers[1].Env["foo"])
	}
}
