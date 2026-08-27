#!/usr/bin/env bash
set -euo pipefail

# Smoke test — core daily workflow: create, read, complete, trash.
# Designed to run regularly (e.g. daily cron) to detect Things Cloud API changes.
# Usage: ./test-smoke.sh [base_url]
# Example: BASE_URL=https://your-app-name.fly.dev ./test-smoke.sh

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

mcp_call() {
  local tool="$1" args="$2"
  sleep 1
  curl -s --max-time 60 "${ENDPOINT}" \
    -X POST \
    -H "Content-Type: application/json" \
    ${CURL_AUTH[@]+"${CURL_AUTH[@]}"} \
    --data-raw "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"${tool}\",\"arguments\":${args}}}"
}

extract_text() {
  python3 -c "import sys,json; r=json.loads(sys.stdin.read()); print(r['result']['content'][0]['text'])" 2>/dev/null || echo ""
}

field() {
  local json="$1" key="$2"
  echo "$json" | python3 -c "import sys,json; print(json.loads(sys.stdin.read()).get('$key',''))" 2>/dev/null || echo ""
}

is_array() {
  local json="$1"
  echo "$json" | python3 -c "import sys,json; print('true' if isinstance(json.loads(sys.stdin.read()), list) else 'false')" 2>/dev/null || echo "false"
}

has_uuid() {
  local json="$1" uuid="$2"
  echo "$json" | python3 -c "
import sys,json
items=json.loads(sys.stdin.read())
print('true' if any(i.get('uuid')=='$uuid' for i in items) else 'false')
"
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

# --- smoke tests ---

echo ""
echo "=== Smoke Test ==="
echo "    Endpoint: ${ENDPOINT}"
echo "    Time:     $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo ""

# 1. Health check
echo "--- Health ---"
HEALTH=$(curl -s --max-time 10 "${BASE}/")
check "service up" "$(echo "$HEALTH" | python3 -c "import sys,json; print(json.loads(sys.stdin.read()).get('status',''))" 2>/dev/null || echo "")" "ok"

# 2. Read: list today (verifies sync + read path)
echo "--- Read ---"
TODAY=$(mcp_call "things_list_today" "{}" | extract_text)
check "list today returns array" "$(is_array "$TODAY")" "true"

PROJECTS=$(mcp_call "things_list_projects" "{}" | extract_text)
check "list projects returns array" "$(is_array "$PROJECTS")" "true"

TAGS=$(mcp_call "things_list_tags" "{}" | extract_text)
check "list tags returns array" "$(is_array "$TAGS")" "true"

# 3. Write: create a task
echo "--- Create ---"
RESP=$(mcp_call "things_create_task" "{\"title\":\"[smoke] Test task\",\"when\":\"today\"}" | extract_text)
TASK_UUID=$(field "$RESP" uuid)
check "task created" "$([ -n "$TASK_UUID" ] && echo ok || echo '')" "ok"

# 4. Read back the task
echo "--- Get ---"
TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${TASK_UUID}\"}" | extract_text)
check "get task title" "$(field "$TASK" title)" "[smoke] Test task"
check "get task status" "$(field "$TASK" status)" "open"

# 5. Edit the task
echo "--- Edit ---"
RESP=$(mcp_call "things_edit_task" "{\"uuid\":\"${TASK_UUID}\",\"title\":\"[smoke] Test task (edited)\"}" | extract_text)
check "edit ok" "$(field "$RESP" status)" "updated"

# 6. Complete the task
echo "--- Complete ---"
RESP=$(mcp_call "things_complete_task" "{\"uuid\":\"${TASK_UUID}\"}" | extract_text)
check "complete ok" "$(field "$RESP" status)" "completed"

TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${TASK_UUID}\"}" | extract_text)
check "status is completed" "$(field "$TASK" status)" "completed"

# 7. Trash the task (cleanup)
echo "--- Trash ---"
RESP=$(mcp_call "things_trash_task" "{\"uuid\":\"${TASK_UUID}\"}" | extract_text)
check "trash ok" "$(field "$RESP" status)" "trashed"

# --- summary & log ---

RESULT="PASS"
if [ "$FAIL" -gt 0 ]; then
  RESULT="FAIL"
fi

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
echo ""

{
  echo "$(date -u '+%Y-%m-%d %H:%M:%S UTC')  test-smoke  ${RESULT}  ${PASS} passed, ${FAIL} failed"
  if [ -n "$FAILURES" ]; then
    printf "%b" "$FAILURES"
  fi
} >> "$LOG_FILE"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
