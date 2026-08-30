package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	h := NewHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)

	expected := map[string]string{
		"Cache-Control":           "no-store",
		"Pragma":                  "no-cache",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Permissions-Policy":      "camera=(), geolocation=(), microphone=()",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	}
	for header, value := range expected {
		if got := resp.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
}
