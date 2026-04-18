package workflow

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type stubModel struct{}

func (stubModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("stub", nil), nil
}
func (stubModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}
func (s stubModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}

func TestChatWorkflowCompiles(t *testing.T) {
	ctx := context.Background()
	w, err := NewChatWorkflow(ctx, &Deps{
		ChatModel:    stubModel{},
		SystemPrompt: "你是一个助手",
	})
	if err != nil {
		t.Fatalf("compile workflow: %v", err)
	}
	if w == nil || w.runnable == nil {
		t.Fatalf("workflow or runnable is nil")
	}
}

func TestChatWorkflowRejectsNilModel(t *testing.T) {
	if _, err := NewChatWorkflow(context.Background(), &Deps{}); err == nil {
		t.Errorf("expected error for missing ChatModel")
	}
}
