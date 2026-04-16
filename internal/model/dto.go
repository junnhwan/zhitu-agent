package model

// ChatRequest mirrors Java ChatRequest DTO
type ChatRequest struct {
	SessionID int64  `json:"sessionId"`
	UserID    int64  `json:"userId"`
	Prompt    string `json:"prompt"`
}

// KnowledgeRequest mirrors Java KnowledgeRequest DTO
type KnowledgeRequest struct {
	Question   string `json:"question"`
	Answer     string `json:"answer"`
	SourceName string `json:"sourceName"`
}
