package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestParseNativeIDs(t *testing.T) {
	t.Parallel()

	got := parseNativeIDs("id-1, id-2, id-3\n")
	want := []string{"id-1", "id-2", "id-3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseNativeIDs() = %v, want %v", got, want)
	}
}

func TestDiffIDs(t *testing.T) {
	t.Parallel()

	missing, extra := diffIDs([]string{"b", "a"}, []string{"c", "b"})
	if !reflect.DeepEqual(missing, []string{"a"}) || !reflect.DeepEqual(extra, []string{"c"}) {
		t.Fatalf("diffIDs() missing=%v extra=%v", missing, extra)
	}
}

func TestMCPViewIDs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"UUID":"id-1"},{"uuid":"id-2"}]`)
	}))
	defer server.Close()

	got, err := mcpViewIDs(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("mcpViewIDs failed: %v", err)
	}
	if want := []string{"id-1", "id-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mcpViewIDs() = %v, want %v", got, want)
	}
}

func TestCompareViewsReportsMismatch(t *testing.T) {
	t.Parallel()

	native := func(_ context.Context, _ string) ([]string, error) {
		return []string{"native-only", "shared"}, nil
	}
	mcp := func(_ context.Context, endpoint string) ([]string, error) {
		if !strings.HasPrefix(endpoint, "http://127.0.0.1:8765/api/tasks/") {
			return nil, fmt.Errorf("unexpected endpoint %s", endpoint)
		}
		return []string{"shared", "mcp-only"}, nil
	}

	var output strings.Builder
	mismatch, err := compareViews(context.Background(), &output, "http://127.0.0.1:8765", native, mcp)
	if err != nil {
		t.Fatalf("compareViews failed: %v", err)
	}
	if !mismatch {
		t.Fatal("expected mismatch")
	}
	if !strings.Contains(output.String(), "Today native=2 mcp=2 missing=1 extra=1") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestCompareViewsRejectsDuplicateIDs(t *testing.T) {
	t.Parallel()

	native := func(_ context.Context, _ string) ([]string, error) {
		return []string{"shared"}, nil
	}
	mcp := func(_ context.Context, _ string) ([]string, error) {
		return []string{"shared", "shared"}, nil
	}

	mismatch, err := compareViews(context.Background(), io.Discard, "http://127.0.0.1:8765", native, mcp)
	if err != nil {
		t.Fatalf("compareViews failed: %v", err)
	}
	if !mismatch {
		t.Fatal("expected duplicate IDs to fail parity")
	}
}
