package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPAuthMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name     string
		apiKey   string
		header   string
		url      string
		wantCode int
	}{
		{"no key configured, no auth", "", "", "/mcp", http.StatusOK},
		{"key configured, valid bearer header", "secret", "Bearer secret", "/mcp", http.StatusOK},
		{"key configured, valid query param", "secret", "", "/mcp?key=secret", http.StatusOK},
		{"key configured, no auth", "secret", "", "/mcp", http.StatusUnauthorized},
		{"key configured, wrong bearer header", "secret", "Bearer wrong", "/mcp", http.StatusUnauthorized},
		{"key configured, wrong query param", "secret", "", "/mcp?key=wrong", http.StatusUnauthorized},
		{"key configured, malformed header", "secret", "secret", "/mcp", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("API_KEY", tt.apiKey)

			req := httptest.NewRequest(http.MethodPost, tt.url, nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()

			mcpAuthMiddleware(okHandler).ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}
