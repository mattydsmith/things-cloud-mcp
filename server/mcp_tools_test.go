package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestToolsListIncludesParityTools drives the real MCP handler over JSON-RPC
// and checks that the SDK-parity tools and parameters are registered.
func TestToolsListIncludesParityTools(t *testing.T) {
	t.Parallel()

	handler := newMCPHandler()

	post := func(body, sessionID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if sessionID != "" {
			req.Header.Set("Mcp-Session-Id", sessionID)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	init := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0.0.0"}}}`, "")
	if init.Code != http.StatusOK {
		t.Fatalf("initialize returned status %d: %s", init.Code, init.Body.String())
	}
	session := init.Header().Get("Mcp-Session-Id")

	rec := post(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list returned status %d: %s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()

	for _, tool := range []string{
		`"things_cancel_task"`,
		`"things_purge_task"`,
		`"things_edit_area"`,
		`"things_edit_tag"`,
		`"things_edit_checklist_item"`,
		`"things_list_tag_tasks"`,
		// new parameters on existing tools
		`"reminder"`,
		`"heading"`,
	} {
		if !strings.Contains(got, tool) {
			t.Errorf("tools/list missing %s", tool)
		}
	}
}
