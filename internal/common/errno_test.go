package common

import "testing"

func TestErrorCodeValues(t *testing.T) {
	tests := []struct {
		name     string
		errCode  ErrorCode
		wantCode int
		wantMsg  string
	}{
		{"ParamsError", ParamsError, 40000, "请求参数错误"},
		{"SensitiveWordError", SensitiveWordError, 40003, "包含敏感词，请求被拒绝"},
		{"SystemError", SystemError, 50000, "系统内部异常"},
		{"AIModelError", AIModelError, 80000, "AI模型调用失败"},
		{"RAGEmbeddingError", RAGEmbeddingError, 80020, "文档向量化失败"},
		{"ToolExecutionError", ToolExecutionError, 80030, "工具执行失败"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.errCode.Code != tt.wantCode {
				t.Errorf("ErrorCode.Code = %d, want %d", tt.errCode.Code, tt.wantCode)
			}
			if tt.errCode.Message != tt.wantMsg {
				t.Errorf("ErrorCode.Message = %q, want %q", tt.errCode.Message, tt.wantMsg)
			}
		})
	}
}

func TestNewBusinessError(t *testing.T) {
	err := NewBusinessError(AIModelError, "")
	if err.Code != AIModelError.Code {
		t.Errorf("Code = %d, want %d", err.Code, AIModelError.Code)
	}
	if err.Message != AIModelError.Message {
		t.Errorf("Message = %q, want %q", err.Message, AIModelError.Message)
	}
	if err.Error() != AIModelError.Message {
		t.Errorf("Error() = %q, want %q", err.Error(), AIModelError.Message)
	}

	err2 := NewBusinessError(SystemError, "custom message")
	if err2.Message != "custom message" {
		t.Errorf("Message = %q, want %q", err2.Message, "custom message")
	}
}

func TestSuccess(t *testing.T) {
	resp := Success("hello")
	if resp.Code != 0 {
		t.Errorf("Code = %d, want 0", resp.Code)
	}
	if resp.Data != "hello" {
		t.Errorf("Data = %v, want hello", resp.Data)
	}
	if resp.Message != "ok" {
		t.Errorf("Message = %q, want ok", resp.Message)
	}
}

func TestError(t *testing.T) {
	resp := Error(ParamsError, "")
	if resp.Code != 40000 {
		t.Errorf("Code = %d, want 40000", resp.Code)
	}
	if resp.Data != nil {
		t.Errorf("Data = %v, want nil", resp.Data)
	}
	if resp.Message != "请求参数错误" {
		t.Errorf("Message = %q, want 请求参数错误", resp.Message)
	}

	resp2 := Error(ParamsError, "custom")
	if resp2.Message != "custom" {
		t.Errorf("Message = %q, want custom", resp2.Message)
	}
}

func TestHTTPStatus(t *testing.T) {
	tests := []struct {
		name     string
		errCode  ErrorCode
		wantCode int
	}{
		{"40000 range", ParamsError, 400},
		{"50000 range", SystemError, 500},
		{"80000 range", AIModelError, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTTPStatus(tt.errCode); got != tt.wantCode {
				t.Errorf("HTTPStatus() = %d, want %d", got, tt.wantCode)
			}
		})
	}
}
