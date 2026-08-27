#!/usr/bin/env bash
set -euo pipefail

# MCP protocol-level tests — verifies JSON-RPC handshake and error handling.
# Usage: ./test-mcp-protocol.sh [base_url]

BASE="${1:-${BASE_URL:-}}"
if [ -z "${BASE}" ]; then
  echo "Error: base URL is required. Pass as the first arg or set BASE_URL."
  exit 1
fi
ENDPOINT="${BASE}/mcp"

# Optional bearer auth — set API_KEY if the server was deployed with one.
CURL_AUTH=()
if [ -n "${API_KEY:-}" ]; then
  CURL_AUTH=(-H "Authorization: Bearer ${API_KEY}")
fi
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_FILE="${SCRIPT_DIR}/test-results.log"

PASS=0
FAIL=0
FAILURES=""

# --- helpers ---

mcp_raw() {
  local body="$1"
  sleep 1
  curl -s --max-time 30 "${ENDPOINT}" \
    -X POST \
    -H "Content-Type: application/json" \
    ${CURL_AUTH[@]+"${CURL_AUTH[@]}"} \
    --data-raw "$body"
}

check() {
  local label="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    printf "  \033[32mPASS\033[0m  %s\n" "$label"
    PASS=$((PASS + 1))
  else
    printf "  \033[31mFAIL\033[0m  %s (got: %s, want: %s)\n" "$label" "$got" "$want"
    FAIL=$((FAIL + 1))
    FAILURES="${FAILURES}    FAIL  ${label} (got: ${got}, want: ${want})\n"
  fi
}

# --- tests ---

echo ""
echo "=== MCP Protocol Tests ==="
echo "    Endpoint: ${ENDPOINT}"
echo ""

# 1. Initialize
echo "--- Initialize ---"
RESP=$(mcp_raw '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}')
SERVER_NAME=$(echo "$RESP" | python3 -c "import sys,json; r=json.loads(sys.stdin.read()); print(r.get('result',{}).get('serverInfo',{}).get('name',''))" 2>/dev/null || echo "")
SERVER_VER=$(echo "$RESP" | python3 -c "import sys,json; r=json.loads(sys.stdin.read()); print(r.get('result',{}).get('serverInfo',{}).get('version',''))" 2>/dev/null || echo "")
HAS_TOOLS=$(echo "$RESP" | python3 -c "import sys,json; r=json.loads(sys.stdin.read()); print('true' if 'tools' in r.get('result',{}).get('capabilities',{}) else 'false')" 2>/dev/null || echo "false")
check "server name" "$SERVER_NAME" "Things Cloud"
check "server version" "$SERVER_VER" "1.1.0"
check "has tools capability" "$HAS_TOOLS" "true"

# 2. List tools
echo "--- List Tools ---"
RESP=$(mcp_raw '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}')
TOOL_COUNT=$(echo "$RESP" | python3 -c "import sys,json; r=json.loads(sys.stdin.read()); print(len(r.get('result',{}).get('tools',[])))" 2>/dev/null || echo "0")
check "tool count is 36" "$TOOL_COUNT" "36"

# Verify a few key tools exist
HAS_CREATE=$(echo "$RESP" | python3 -c "
import sys,json
r=json.loads(sys.stdin.read())
tools=[t['name'] for t in r.get('result',{}).get('tools',[])]
print('true' if 'things_create_task' in tools else 'false')
" 2>/dev/null || echo "false")
check "has things_create_task" "$HAS_CREATE" "true"

HAS_LIST=$(echo "$RESP" | python3 -c "
import sys,json
r=json.loads(sys.stdin.read())
tools=[t['name'] for t in r.get('result',{}).get('tools',[])]
print('true' if 'things_list_today' in tools else 'false')
" 2>/dev/null || echo "false")
check "has things_list_today" "$HAS_LIST" "true"

HAS_UPCOMING=$(echo "$RESP" | python3 -c "
import sys,json
r=json.loads(sys.stdin.read())
tools=[t['name'] for t in r.get('result',{}).get('tools',[])]
print('true' if 'things_list_upcoming' in tools else 'false')
" 2>/dev/null || echo "false")
check "has things_list_upcoming" "$HAS_UPCOMING" "true"

HAS_HEADING=$(echo "$RESP" | python3 -c "
import sys,json
r=json.loads(sys.stdin.read())
tools=[t['name'] for t in r.get('result',{}).get('tools',[])]
print('true' if 'things_create_heading' in tools else 'false')
" 2>/dev/null || echo "false")
check "has things_create_heading" "$HAS_HEADING" "true"

# Verify tools have input schemas
HAS_SCHEMA=$(echo "$RESP" | python3 -c "
import sys,json
r=json.loads(sys.stdin.read())
tools=r.get('result',{}).get('tools',[])
print('true' if all('inputSchema' in t for t in tools) else 'false')
" 2>/dev/null || echo "false")
check "all tools have inputSchema" "$HAS_SCHEMA" "true"

# 3. Unknown tool
echo "--- Unknown Tool ---"
RESP=$(mcp_raw '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nonexistent_tool","arguments":{}}}')
IS_ERROR=$(echo "$RESP" | python3 -c "
import sys,json
r=json.loads(sys.stdin.read())
print('true' if r.get('error') or r.get('result',{}).get('isError') else 'false')
" 2>/dev/null || echo "false")
check "unknown tool returns error" "$IS_ERROR" "true"

# 4. Missing required params
echo "--- Missing Required Params ---"
RESP=$(mcp_raw '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"things_create_task","arguments":{}}}')
IS_ERROR=$(echo "$RESP" | python3 -c "
import sys,json
r=json.loads(sys.stdin.read())
print('true' if r.get('result',{}).get('isError') else 'false')
" 2>/dev/null || echo "false")
check "missing title returns error" "$IS_ERROR" "true"

RESP=$(mcp_raw '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"things_get_task","arguments":{}}}')
IS_ERROR=$(echo "$RESP" | python3 -c "
import sys,json
r=json.loads(sys.stdin.read())
print('true' if r.get('result',{}).get('isError') else 'false')
" 2>/dev/null || echo "false")
check "missing uuid returns error" "$IS_ERROR" "true"

# --- summary & log ---

RESULT="PASS"
if [ "$FAIL" -gt 0 ]; then
  RESULT="FAIL"
fi

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
echo ""

{
  echo "$(date -u '+%Y-%m-%d %H:%M:%S UTC')  test-mcp-protocol  ${RESULT}  ${PASS} passed, ${FAIL} failed"
  if [ -n "$FAILURES" ]; then
    printf "%b" "$FAILURES"
  fi
} >> "$LOG_FILE"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
