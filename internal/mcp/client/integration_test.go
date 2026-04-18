//go:build mcp

package client

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// TestStdioIntegration launches @modelcontextprotocol/server-everything via npx
// and verifies we can list + call tools.
// Run: go test -tags=mcp ./internal/mcp/client/ -v -run TestStdioIntegration
func TestStdioIntegration(t *testing.T) {
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := config.MCPClientConfig{
		Enabled: true,
		Servers: []config.MCPServerConfig{
			{
				Name:      "everything",
				Transport: "stdio",
				Command:   []string{"npx", "-y", "@modelcontextprotocol/server-everything"},
				Enabled:   true,
				TimeoutMs: 30000,
			},
		},
	}
	c := NewClient(ctx, cfg, Hooks{})
	defer c.Close()

	tools := c.Tools()
	if len(tools) == 0 {
		t.Fatal("expected tools from server-everything, got 0")
	}
	t.Logf("server-everything exposed %d tools", len(tools))

	infos, err := c.ToolInfos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var echoName string
	for _, info := range infos {
		t.Logf("  - %s: %s", info.Name, firstLine(info.Desc))
		if strings.EqualFold(info.Name, "echo") {
			echoName = info.Name
		}
	}
	if echoName == "" {
		t.Skip("no echo tool in server-everything, skipping call test")
	}
	toolMap := c.ToolMap(ctx)
	args, _ := json.Marshal(map[string]any{"message": "hello from zhitu"})
	out, err := toolMap[echoName].InvokableRun(ctx, string(args))
	if err != nil {
		t.Fatalf("echo call: %v", err)
	}
	if !strings.Contains(out, "hello from zhitu") {
		t.Errorf("echo result missing input: %s", out)
	}
	t.Logf("echo response: %s", out)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
