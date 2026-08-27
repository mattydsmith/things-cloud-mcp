#!/usr/bin/env bash
set -euo pipefail

# MCP read tool tests — verifies all read-only tools return valid responses.
# Usage: ./test-mcp-read.sh [base_url]

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

first_uuid() {
  local json="$1"
  echo "$json" | python3 -c "
import sys,json
items=json.loads(sys.stdin.read())
print(items[0]['uuid'] if items else '')
" 2>/dev/null || echo ""
}

has_field() {
  local json="$1" key="$2"
  echo "$json" | python3 -c "
import sys,json
items=json.loads(sys.stdin.read())
print('true' if items and '$key' in items[0] else 'false')
" 2>/dev/null || echo "false"
}

is_error() {
  local raw="$1"
  echo "$raw" | python3 -c "
import sys,json
r=json.loads(sys.stdin.read())
c=r.get('result',{})
print('true' if c.get('isError') else 'false')
" 2>/dev/null || echo "false"
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
echo "=== MCP Read Tool Tests ==="
echo "    Endpoint: ${ENDPOINT}"
echo ""

# 1. List today
echo "--- things_list_today ---"
TODAY=$(mcp_call "things_list_today" "{}" | extract_text)
check "returns array" "$(is_array "$TODAY")" "true"
check "items have uuid" "$(has_field "$TODAY" uuid)" "true"
check "items have title" "$(has_field "$TODAY" title)" "true"
check "items have status" "$(has_field "$TODAY" status)" "true"

# 2. List inbox
echo "--- things_list_inbox ---"
INBOX=$(mcp_call "things_list_inbox" "{}" | extract_text)
check "returns array" "$(is_array "$INBOX")" "true"

# 3. List anytime
echo "--- things_list_anytime ---"
ANYTIME=$(mcp_call "things_list_anytime" "{}" | extract_text)
check "returns array" "$(is_array "$ANYTIME")" "true"

# 4. List someday
echo "--- things_list_someday ---"
SOMEDAY=$(mcp_call "things_list_someday" "{}" | extract_text)
check "returns array" "$(is_array "$SOMEDAY")" "true"

# 5. List upcoming
echo "--- things_list_upcoming ---"
UPCOMING=$(mcp_call "things_list_upcoming" "{}" | extract_text)
check "returns array" "$(is_array "$UPCOMING")" "true"

# 6. List all tasks
echo "--- things_list_all_tasks ---"
ALL=$(mcp_call "things_list_all_tasks" "{}" | extract_text)
check "returns array" "$(is_array "$ALL")" "true"
FIRST_TASK_UUID=$(first_uuid "$ALL")
check "has tasks" "$([ -n "$FIRST_TASK_UUID" ] && echo ok || echo '')" "ok"

# 7. List projects
echo "--- things_list_projects ---"
PROJECTS=$(mcp_call "things_list_projects" "{}" | extract_text)
check "returns array" "$(is_array "$PROJECTS")" "true"
check "items have uuid" "$(has_field "$PROJECTS" uuid)" "true"
check "items have title" "$(has_field "$PROJECTS" title)" "true"
FIRST_PROJECT_UUID=$(first_uuid "$PROJECTS")

# 8. List areas
echo "--- things_list_areas ---"
AREAS=$(mcp_call "things_list_areas" "{}" | extract_text)
check "returns array" "$(is_array "$AREAS")" "true"
check "items have uuid" "$(has_field "$AREAS" uuid)" "true"
check "items have title" "$(has_field "$AREAS" title)" "true"
FIRST_AREA_UUID=$(first_uuid "$AREAS")

# 9. List tags
echo "--- things_list_tags ---"
TAGS=$(mcp_call "things_list_tags" "{}" | extract_text)
check "returns array" "$(is_array "$TAGS")" "true"
check "items have uuid" "$(has_field "$TAGS" uuid)" "true"
check "items have title" "$(has_field "$TAGS" title)" "true"
FIRST_TAG_UUID=$(first_uuid "$TAGS")

# 7. List project tasks
echo "--- things_list_project_tasks ---"
if [ -n "$FIRST_PROJECT_UUID" ]; then
  PTASKS=$(mcp_call "things_list_project_tasks" "{\"project_uuid\":\"${FIRST_PROJECT_UUID}\"}" | extract_text)
  check "returns array" "$(is_array "$PTASKS")" "true"
else
  printf "  \033[33mSKIP\033[0m  no projects to test with\n"
fi

# 8. List area tasks
echo "--- things_list_area_tasks ---"
if [ -n "$FIRST_AREA_UUID" ]; then
  ATASKS=$(mcp_call "things_list_area_tasks" "{\"area_uuid\":\"${FIRST_AREA_UUID}\"}" | extract_text)
  check "returns array" "$(is_array "$ATASKS")" "true"
else
  printf "  \033[33mSKIP\033[0m  no areas to test with\n"
fi

# 9. List checklist items
echo "--- things_list_checklist_items ---"
if [ -n "$FIRST_TASK_UUID" ]; then
  CLIST=$(mcp_call "things_list_checklist_items" "{\"task_uuid\":\"${FIRST_TASK_UUID}\"}" | extract_text)
  check "returns array" "$(is_array "$CLIST")" "true"
else
  printf "  \033[33mSKIP\033[0m  no tasks to test with\n"
fi

# 10. Get task
echo "--- things_get_task ---"
if [ -n "$FIRST_TASK_UUID" ]; then
  TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${FIRST_TASK_UUID}\"}" | extract_text)
  check "has uuid" "$(field "$TASK" uuid)" "$FIRST_TASK_UUID"
  check "has title" "$([ -n "$(field "$TASK" title)" ] && echo ok || echo '')" "ok"
  check "has status" "$([ -n "$(field "$TASK" status)" ] && echo ok || echo '')" "ok"
else
  printf "  \033[33mSKIP\033[0m  no tasks to test with\n"
fi

# 11. Get area
echo "--- things_get_area ---"
if [ -n "$FIRST_AREA_UUID" ]; then
  AREA=$(mcp_call "things_get_area" "{\"uuid\":\"${FIRST_AREA_UUID}\"}" | extract_text)
  check "has uuid" "$(field "$AREA" uuid)" "$FIRST_AREA_UUID"
  check "has title" "$([ -n "$(field "$AREA" title)" ] && echo ok || echo '')" "ok"
else
  printf "  \033[33mSKIP\033[0m  no areas to test with\n"
fi

# 12. Get tag
echo "--- things_get_tag ---"
if [ -n "$FIRST_TAG_UUID" ]; then
  TAG=$(mcp_call "things_get_tag" "{\"uuid\":\"${FIRST_TAG_UUID}\"}" | extract_text)
  check "has uuid" "$(field "$TAG" uuid)" "$FIRST_TAG_UUID"
  check "has title" "$([ -n "$(field "$TAG" title)" ] && echo ok || echo '')" "ok"
else
  printf "  \033[33mSKIP\033[0m  no tags to test with\n"
fi

# 13-15. Error handling — nonexistent UUIDs
echo "--- Error Handling ---"
RAW=$(mcp_call "things_get_task" "{\"uuid\":\"nonexistent-uuid-12345\"}")
check "get_task bad uuid returns error" "$(is_error "$RAW")" "true"

RAW=$(mcp_call "things_get_area" "{\"uuid\":\"nonexistent-uuid-12345\"}")
check "get_area bad uuid returns error" "$(is_error "$RAW")" "true"

RAW=$(mcp_call "things_get_tag" "{\"uuid\":\"nonexistent-uuid-12345\"}")
check "get_tag bad uuid returns error" "$(is_error "$RAW")" "true"

# --- summary & log ---

RESULT="PASS"
if [ "$FAIL" -gt 0 ]; then
  RESULT="FAIL"
fi

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
echo ""

{
  echo "$(date -u '+%Y-%m-%d %H:%M:%S UTC')  test-mcp-read  ${RESULT}  ${PASS} passed, ${FAIL} failed"
  if [ -n "$FAILURES" ]; then
    printf "%b" "$FAILURES"
  fi
} >> "$LOG_FILE"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
