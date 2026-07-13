package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type view struct {
	name string
	path string
}

var views = []view{
	{name: "Today", path: "today"},
	{name: "Inbox", path: "inbox"},
	{name: "Anytime", path: "anytime"},
	{name: "Someday", path: "someday"},
	{name: "Upcoming", path: "upcoming"},
}

type task struct {
	UUID string `json:"uuid"`
}

func main() {
	defaultURL := os.Getenv("THINGS_MCP_URL")
	if defaultURL == "" {
		defaultURL = "http://127.0.0.1:8765"
	}
	baseURL := flag.String("url", defaultURL, "Things MCP base URL")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall comparison timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	mismatch, err := compareViews(ctx, os.Stdout, strings.TrimRight(*baseURL, "/"), nativeViewIDs, mcpViewIDs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parity check failed:", err)
		os.Exit(2)
	}
	if mismatch {
		os.Exit(1)
	}
}

type idReader func(context.Context, string) ([]string, error)

func compareViews(ctx context.Context, out io.Writer, baseURL string, nativeIDs, mcpIDs idReader) (bool, error) {
	mismatch := false
	for _, item := range views {
		native, err := nativeIDs(ctx, item.name)
		if err != nil {
			return false, fmt.Errorf("read native %s: %w", item.name, err)
		}
		mcp, err := mcpIDs(ctx, baseURL+"/api/tasks/"+item.path)
		if err != nil {
			return false, fmt.Errorf("read MCP %s: %w", item.name, err)
		}

		missing, extra := diffIDs(native, mcp)
		fmt.Fprintf(out, "%s native=%d mcp=%d missing=%d extra=%d\n", item.name, len(native), len(mcp), len(missing), len(extra))
		if len(native) != len(mcp) {
			mismatch = true
		}
		if len(missing) > 0 {
			fmt.Fprintf(out, "  missing: %s\n", strings.Join(missing, ", "))
			mismatch = true
		}
		if len(extra) > 0 {
			fmt.Fprintf(out, "  extra: %s\n", strings.Join(extra, ", "))
			mismatch = true
		}
	}
	return mismatch, nil
}

func nativeViewIDs(ctx context.Context, viewName string) ([]string, error) {
	script := fmt.Sprintf(`tell application "Things3" to id of every to do of list %q`, viewName)
	output, err := exec.CommandContext(ctx, "osascript", "-e", script).Output()
	if err != nil {
		return nil, err
	}
	return parseNativeIDs(string(output)), nil
}

func parseNativeIDs(output string) []string {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}
	parts := strings.Split(output, ",")
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		if id := strings.TrimSpace(part); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func mcpViewIDs(ctx context.Context, endpoint string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if apiKey := os.Getenv("THINGS_MCP_API_KEY"); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	var tasks []task
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(tasks))
	for _, item := range tasks {
		if item.UUID == "" {
			return nil, errors.New("response contains task without UUID")
		}
		ids = append(ids, item.UUID)
	}
	return ids, nil
}

func diffIDs(want, got []string) (missing, extra []string) {
	wantSet := make(map[string]struct{}, len(want))
	gotSet := make(map[string]struct{}, len(got))
	for _, id := range want {
		wantSet[id] = struct{}{}
	}
	for _, id := range got {
		gotSet[id] = struct{}{}
	}
	for id := range wantSet {
		if _, ok := gotSet[id]; !ok {
			missing = append(missing, id)
		}
	}
	for id := range gotSet {
		if _, ok := wantSet[id]; !ok {
			extra = append(extra, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}
