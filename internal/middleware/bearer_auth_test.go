package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newTestRouter(token string, onReject func()) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Any("/protected", BearerAuth(token, onReject), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

func TestBearerAuthAllowsMatching(t *testing.T) {
	r := newTestRouter("sesame", nil)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer sesame")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("code=%d, want 200", w.Code)
	}
}

func TestBearerAuthRejectsMissing(t *testing.T) {
	var rejected int
	r := newTestRouter("sesame", func() { rejected++ })
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code=%d, want 401", w.Code)
	}
	if rejected != 1 {
		t.Errorf("onReject called %d times, want 1", rejected)
	}
}

func TestBearerAuthRejectsWrong(t *testing.T) {
	r := newTestRouter("sesame", nil)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer nope")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code=%d, want 401", w.Code)
	}
}

func TestBearerAuthRejectsBareToken(t *testing.T) {
	r := newTestRouter("sesame", nil)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "sesame")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code=%d, want 401", w.Code)
	}
}
