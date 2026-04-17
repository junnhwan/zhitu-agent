package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestGuardrailAllowsNormalPrompt(t *testing.T) {
	r := gin.New()
	r.Use(Guardrail())
	r.POST("/chat", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	body := `{"prompt":"你好，请帮我写代码"}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestGuardrailBlocksSensitiveWord(t *testing.T) {
	r := gin.New()
	r.Use(Guardrail())
	r.POST("/chat", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	body := `{"prompt":"我想死"}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"].(float64) != 40003 {
		t.Errorf("code = %v, want 40003", resp["code"])
	}
}

func TestGuardrailBlocksSecondSensitiveWord(t *testing.T) {
	r := gin.New()
	r.Use(Guardrail())
	r.POST("/chat", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	body := `{"prompt":"杀毒软件"}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGuardrailSkipsNonPost(t *testing.T) {
	r := gin.New()
	r.Use(Guardrail())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCORSHeaders(t *testing.T) {
	r := gin.New()
	r.Use(CORS())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("CORS Origin = %q, want *", origin)
	}
	if methods := w.Header().Get("Access-Control-Allow-Methods"); methods == "" {
		t.Error("CORS Methods header missing")
	}
}

func TestCORSPreflight(t *testing.T) {
	r := gin.New()
	r.Use(CORS())
	r.POST("/chat", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/chat", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want %d", w.Code, http.StatusNoContent)
	}
}
