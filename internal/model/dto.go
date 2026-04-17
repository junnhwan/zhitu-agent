package model

// ChatRequest mirrors Java ChatRequest DTO
type ChatRequest struct {
	SessionID int64  `json:"sessionId" binding:"required"`
	UserID    int64  `json:"userId" binding:"required"`
	Prompt    string `json:"prompt" binding:"required"`
}

// KnowledgeRequest mirrors Java KnowledgeRequest DTO
type KnowledgeRequest struct {
	Question   string `json:"question" binding:"required"`
	Answer     string `json:"answer" binding:"required"`
	SourceName string `json:"sourceName" binding:"required"`
}
