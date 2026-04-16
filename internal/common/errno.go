package common

// ErrorCode mirrors Java ErrorCode enum
type ErrorCode struct {
	Code    int
	Message string
}

var (
	ParamsError         = ErrorCode{40000, "请求参数错误"}
	InvalidParamError   = ErrorCode{40001, "请求参数不合法"}
	SensitiveWordError  = ErrorCode{40003, "包含敏感词，请求被拒绝"}
	SystemError         = ErrorCode{50000, "系统内部异常"}
	AIModelError        = ErrorCode{80000, "AI模型调用失败"}
	AIModelTimeout      = ErrorCode{80001, "AI模型响应超时"}
	MemoryCompressError = ErrorCode{80010, "记忆压缩失败"}
	MemoryStoreError    = ErrorCode{80011, "记忆存储失败"}
	RAGEmbeddingError   = ErrorCode{80020, "文档向量化失败"}
	RAGRetrievalError   = ErrorCode{80021, "知识检索失败"}
	RAGRerankError      = ErrorCode{80022, "重排序失败"}
	ToolExecutionError  = ErrorCode{80030, "工具执行失败"}
	MCPConnectionError  = ErrorCode{80040, "MCP服务连接失败"}
	GuardrailBlocked    = ErrorCode{80050, "内容安全检查未通过"}
)

// BusinessException mirrors Java BusinessException
type BusinessException struct {
	Code    int
	Message string
}

func (e *BusinessException) Error() string {
	return e.Message
}

func NewBusinessError(errCode ErrorCode, msg string) *BusinessException {
	if msg == "" {
		msg = errCode.Message
	}
	return &BusinessException{Code: errCode.Code, Message: msg}
}
