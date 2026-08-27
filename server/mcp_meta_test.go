package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestThingsIconEmbedded(t *testing.T) {
	t.Parallel()

	pngMagic := []byte{0x89, 'P', 'N', 'G'}
	if !bytes.HasPrefix(thingsIconPNG, pngMagic) {
		t.Fatal("embedded icon is not a PNG")
	}
	if len(thingsIconPNG) < 1000 || len(thingsIconPNG) > 100_000 {
		t.Fatalf("embedded icon has suspicious size: %d bytes", len(thingsIconPNG))
	}
	if !strings.HasPrefix(thingsIconDataURI, "data:image/png;base64,") {
		t.Fatalf("icon data URI has wrong prefix: %.40s", thingsIconDataURI)
	}
}

func TestInitializeIncludesMetadata(t *testing.T) {
	t.Parallel()

	handler := newMCPHandler()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0.0.0"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("initialize returned status %d: %s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	for _, want := range []string{
		`"title":"` + mcpServerTitle + `"`,
		`"version":"` + mcpServerVersion + `"`,
		`data:image/png;base64,`,
		`"instructions"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("initialize response missing %q\nresponse: %.400s", want, got)
		}
	}
}
