//go:build spike

// Task 14 spike: verify Eino 0.8.9 API shape for compose.Graph + react.Agent.
// Run: DASHSCOPE_API_KEY=xxx go test -tags=spike ./internal/chat/workflow/ -v -run TestSpike
//
// Goals:
//  1. Build a minimal Graph[string, string]: START -> lambda(uppercase) -> END
//  2. Build a ReAct Agent with one tool, call Generate, verify shape
//  3. Embed the ReAct Agent into an outer graph via ExportGraph + AddGraphNode
//
// No business logic. If the test passes, we know the real API and can write
// nodes.go / chat_workflow.go with confidence.

package workflow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

func TestSpikeGraphLambdaChain(t *testing.T) {
	ctx := context.Background()
	g := compose.NewGraph[string, string]()

	upper := compose.InvokableLambda(func(ctx context.Context, in string) (string, error) {
		return strings.ToUpper(in), nil
	})
	if err := g.AddLambdaNode("upper", upper); err != nil {
		t.Fatal(err)
	}

	suffix := compose.InvokableLambda(func(ctx context.Context, in string) (string, error) {
		return in + "!", nil
	})
	if err := g.AddLambdaNode("suffix", suffix); err != nil {
		t.Fatal(err)
	}

	if err := g.AddEdge(compose.START, "upper"); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge("upper", "suffix"); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge("suffix", compose.END); err != nil {
		t.Fatal(err)
	}

	r, err := g.Compile(ctx, compose.WithMaxRunSteps(10))
	if err != nil {
		t.Fatal(err)
	}

	out, err := r.Invoke(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if out != "HELLO!" {
		t.Errorf("expected HELLO!, got %q", out)
	}
	t.Logf("[spike] Graph[string,string] Invoke ok: %q", out)
}

type echoArgs struct {
	Text string `json:"text" jsonschema:"description=text to echo"`
}

func echoHandler(ctx context.Context, args *echoArgs) (string, error) {
	return "echo: " + args.Text, nil
}

func TestSpikeReActAgent(t *testing.T) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chatModel, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey:  apiKey,
		Model:   "qwen-turbo",
	})
	if err != nil {
		t.Fatal(err)
	}

	echoTool, err := utils.InferTool("echo", "echo back the given text", echoHandler)
	if err != nil {
		t.Fatal(err)
	}

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: []tool.BaseTool{echoTool}},
		MaxStep:          6,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := agent.Generate(ctx, []*schema.Message{
		schema.UserMessage("Call the echo tool with text=hi, then tell me what it returned."),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Content == "" {
		t.Fatalf("empty response: %+v", resp)
	}
	t.Logf("[spike] ReAct Generate ok: %s", resp.Content)
}

func TestSpikeReActEmbeddedInGraph(t *testing.T) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("DASHSCOPE_API_KEY not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chatModel, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey:  apiKey,
		Model:   "qwen-turbo",
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: nil},
		MaxStep:          4,
	})
	if err != nil {
		t.Fatal(err)
	}

	g := compose.NewGraph[string, *schema.Message]()

	toMsgs := compose.InvokableLambda(func(ctx context.Context, in string) ([]*schema.Message, error) {
		return []*schema.Message{schema.UserMessage(in)}, nil
	})
	if err := g.AddLambdaNode("to_msgs", toMsgs); err != nil {
		t.Fatal(err)
	}

	sub, opts := agent.ExportGraph()
	if err := g.AddGraphNode("react", sub, opts...); err != nil {
		t.Fatal(err)
	}

	if err := g.AddEdge(compose.START, "to_msgs"); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge("to_msgs", "react"); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge("react", compose.END); err != nil {
		t.Fatal(err)
	}

	r, err := g.Compile(ctx, compose.WithMaxRunSteps(10))
	if err != nil {
		t.Fatal(err)
	}

	out, err := r.Invoke(ctx, "Say hi in one short sentence.")
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Content == "" {
		t.Fatalf("empty: %+v", out)
	}
	t.Logf("[spike] Graph[string,*Message] with embedded ReAct ok: %s", out.Content)
}
