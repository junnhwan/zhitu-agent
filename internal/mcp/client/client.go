package client

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpclient "github.com/mark3labs/mcp-go/client"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// Client 聚合多个 MCP server 的工具，暴露给 chat service。
// 未启用时 NewClient 返回 no-op 实例（Tools/ToolInfos/ToolMap 全返空）。
type Client struct {
	cfg     config.MCPClientConfig
	servers map[string]*mcpclient.Client
	tools   []tool.InvokableTool
	mu      sync.RWMutex
}

type invokable interface {
	tool.BaseTool
	InvokableRun(ctx context.Context, argsJSON string, opts ...tool.Option) (string, error)
}

// NewClient 按 config 初始化所有 enabled server，工具名冲突时第二次出现自动加 "{serverName}__" 前缀。
// 任何单 server 初始化失败只 log warn，不阻断；返回的 *Client 永远非 nil。
func NewClient(ctx context.Context, cfg config.MCPClientConfig) *Client {
	c := &Client{cfg: cfg, servers: map[string]*mcpclient.Client{}}
	if !cfg.Enabled {
		return c
	}
	seen := map[string]struct{}{}
	for _, sc := range cfg.Servers {
		if !sc.Enabled {
			continue
		}
		cli, err := buildClient(ctx, sc)
		if err != nil {
			log.Printf("[mcp.client] init %s failed (skipped): %v", sc.Name, err)
			continue
		}
		baseTools, err := mcp.GetTools(ctx, &mcp.Config{Cli: cli})
		if err != nil {
			log.Printf("[mcp.client] list tools from %s failed (skipped): %v", sc.Name, err)
			_ = cli.Close()
			continue
		}
		c.servers[sc.Name] = cli
		added := 0
		for _, bt := range baseTools {
			it, ok := bt.(invokable)
			if !ok {
				continue
			}
			info, err := it.Info(ctx)
			if err != nil || info == nil || info.Name == "" {
				continue
			}
			name := info.Name
			if _, clash := seen[name]; clash {
				name = sc.Name + "__" + name
				it = &renamed{invokable: it, name: name, desc: info.Desc, params: info.ParamsOneOf}
			}
			seen[name] = struct{}{}
			c.tools = append(c.tools, it)
			added++
		}
		log.Printf("[mcp.client] registered %d tools from server %q", added, sc.Name)
	}
	return c
}

func (c *Client) Tools() []tool.InvokableTool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]tool.InvokableTool, len(c.tools))
	copy(out, c.tools)
	return out
}

func (c *Client) ToolInfos(ctx context.Context) ([]*schema.ToolInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*schema.ToolInfo, 0, len(c.tools))
	for _, t := range c.tools {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

func (c *Client) ToolMap(ctx context.Context) map[string]tool.InvokableTool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]tool.InvokableTool, len(c.tools))
	for _, t := range c.tools {
		info, err := t.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		out[info.Name] = t
	}
	return out
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var errs []string
	for name, cli := range c.servers {
		if err := cli.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	c.servers = map[string]*mcpclient.Client{}
	c.tools = nil
	if len(errs) > 0 {
		return fmt.Errorf("mcp.client close errors: %v", errs)
	}
	return nil
}

// renamed 在工具名冲突时为 MCP 工具包一层，对外暴露加前缀的名字，底层 InvokableRun 仍走原实例。
type renamed struct {
	invokable
	name   string
	desc   string
	params *schema.ParamsOneOf
}

func (r *renamed) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: r.name, Desc: r.desc, ParamsOneOf: r.params}, nil
}
