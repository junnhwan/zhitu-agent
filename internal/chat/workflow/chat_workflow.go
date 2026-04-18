package workflow

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

const (
	nodeEnrich      = "enrich"
	nodeRetrieve    = "retrieve"
	nodeBuildPrompt = "build_prompt"
	nodeReAct       = "react_agent"
	nodeWrap        = "wrap_response"
)

type ChatWorkflow struct {
	runnable compose.Runnable[*Request, *Response]
	agent    *react.Agent
	deps     *Deps
}

func NewChatWorkflow(ctx context.Context, deps *Deps) (*ChatWorkflow, error) {
	if deps == nil || deps.ChatModel == nil {
		return nil, fmt.Errorf("workflow: Deps.ChatModel is required")
	}

	maxStep := deps.MaxReActSteps
	if maxStep <= 0 {
		maxStep = 10
	}

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: deps.ChatModel,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: deps.Tools},
		MaxStep:          maxStep,
	})
	if err != nil {
		return nil, fmt.Errorf("workflow: react agent: %w", err)
	}

	g := compose.NewGraph[*Request, *Response]()

	if err := g.AddLambdaNode(nodeEnrich, enrichNode(deps)); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(nodeRetrieve, retrieveNode(deps)); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(nodeBuildPrompt, buildPromptNode(deps)); err != nil {
		return nil, err
	}

	sub, opts := agent.ExportGraph()
	if err := g.AddGraphNode(nodeReAct, sub, opts...); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(nodeWrap, wrapResponseNode()); err != nil {
		return nil, err
	}

	edges := [][2]string{
		{compose.START, nodeEnrich},
		{nodeEnrich, nodeRetrieve},
		{nodeRetrieve, nodeBuildPrompt},
		{nodeBuildPrompt, nodeReAct},
		{nodeReAct, nodeWrap},
		{nodeWrap, compose.END},
	}
	for _, e := range edges {
		if err := g.AddEdge(e[0], e[1]); err != nil {
			return nil, fmt.Errorf("workflow: edge %s -> %s: %w", e[0], e[1], err)
		}
	}

	r, err := g.Compile(ctx, compose.WithMaxRunSteps(maxStep+10))
	if err != nil {
		return nil, fmt.Errorf("workflow: compile: %w", err)
	}
	return &ChatWorkflow{runnable: r, agent: agent, deps: deps}, nil
}

func (w *ChatWorkflow) Invoke(ctx context.Context, req *Request) (*Response, error) {
	return w.runnable.Invoke(ctx, req)
}

// Stream runs enrich/retrieve/buildPrompt manually (cheap pure Go steps),
// then delegates to ReAct's Stream to get token-by-token output.
// Falls back from the compiled graph here because wrapping the ReAct stream
// through an additional Lambda node would collect the stream and defeat the
// point of streaming.
func (w *ChatWorkflow) Stream(ctx context.Context, req *Request) (*schema.StreamReader[*schema.Message], error) {
	e, err := enrichFn(w.deps)(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("workflow.stream enrich: %w", err)
	}
	e, err = retrieveFn(w.deps)(ctx, e)
	if err != nil {
		return nil, fmt.Errorf("workflow.stream retrieve: %w", err)
	}
	msgs, err := buildPromptFn(w.deps)(ctx, e)
	if err != nil {
		return nil, fmt.Errorf("workflow.stream build_prompt: %w", err)
	}
	return w.agent.Stream(ctx, msgs)
}
