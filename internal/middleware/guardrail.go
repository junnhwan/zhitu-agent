package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zhitu-agent/zhitu-agent/internal/common"
)

// defaultSensitiveWords matches Java SafeInputGuardrail
var defaultSensitiveWords = []string{"死", "杀"}

// Guardrail mirrors Java SafeInputGuardrail — checks prompt for sensitive words
func Guardrail() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != "POST" {
			c.Next()
			return
		}

		// Read request body
		data, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Next()
			return
		}

		// Restore body for downstream handlers
		c.Request.Body = io.NopCloser(bytes.NewReader(data))

		// Parse body to extract prompt field
		var body map[string]interface{}
		if err := json.Unmarshal(data, &body); err != nil {
			c.Next()
			return
		}

		prompt, ok := body["prompt"].(string)
		if !ok {
			c.Next()
			return
		}

		// Check sensitive words — matches Java: "提问不能包含敏感词！！！！！"
		words := defaultSensitiveWords
		if customWords := c.GetStringSlice("guardrail_sensitive_words"); len(customWords) > 0 {
			words = customWords
		}

		for _, word := range words {
			if word != "" && strings.Contains(prompt, word) {
				c.JSON(400, common.Error(common.SensitiveWordError, "提问不能包含敏感词！！！！！"))
				c.Abort()
				return
			}
		}

		c.Next()
	}
}
