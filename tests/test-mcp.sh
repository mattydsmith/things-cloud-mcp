#!/usr/bin/env bash
set -euo pipefail

# MCP integration test — exercises all write tools with a named test cycle.
# Usage: ./test-mcp.sh [cycle_name] [base_url]
# Example: BASE_URL=https://your-app-name.fly.dev ./test-mcp.sh 001

CYCLE="${1:-001}"
BASE="${2:-${BASE_URL:-}}"
if [ -z "${BASE}" ]; then
  echo "Error: base URL is required. Pass as the second arg or set BASE_URL."
  exit 1
fi
PREFIX="[test-${CYCLE}]"
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
CREATED_TASK=""
CREATED_PROJECT=""
CREATED_AREA=""
CREATED_TAG=""

# --- helpers ---

mcp_call() {
  local tool="$1" args="$2"
  sleep 1  # avoid Things Cloud 429 rate limiting
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

# --- test cycle ---

echo ""
echo "=== MCP Write Tool Test Cycle: ${CYCLE} ==="
echo "    Endpoint: ${ENDPOINT}"
echo "    Prefix:   ${PREFIX}"
echo ""

# 1. Create tag
echo "--- Create Tag ---"
RESP=$(mcp_call "things_create_tag" "{\"title\":\"${PREFIX} Tag\",\"shorthand\":\"t${CYCLE}\"}" | extract_text)
CREATED_TAG=$(field "$RESP" uuid)
check "tag created" "$([ -n "$CREATED_TAG" ] && echo ok || echo '')" "ok"
echo "    uuid: ${CREATED_TAG}"

# 2. Create area
echo "--- Create Area ---"
RESP=$(mcp_call "things_create_area" "{\"title\":\"${PREFIX} Area\"}" | extract_text)
CREATED_AREA=$(field "$RESP" uuid)
check "area created" "$([ -n "$CREATED_AREA" ] && echo ok || echo '')" "ok"
echo "    uuid: ${CREATED_AREA}"

# 3. Create project (in area, with deadline)
echo "--- Create Project ---"
RESP=$(mcp_call "things_create_project" "{\"title\":\"${PREFIX} Project\",\"note\":\"Test project notes\",\"when\":\"anytime\",\"deadline\":\"2099-12-31\",\"area\":\"${CREATED_AREA}\"}" | extract_text)
CREATED_PROJECT=$(field "$RESP" uuid)
check "project created" "$([ -n "$CREATED_PROJECT" ] && echo ok || echo '')" "ok"
echo "    uuid: ${CREATED_PROJECT}"

# 4. Create task (in project, with tag, note, deadline, today)
echo "--- Create Task ---"
RESP=$(mcp_call "things_create_task" "{\"title\":\"${PREFIX} Task\",\"note\":\"Test task notes\",\"when\":\"today\",\"deadline\":\"2099-12-31\",\"project\":\"${CREATED_PROJECT}\",\"tags\":\"${CREATED_TAG}\"}" | extract_text)
CREATED_TASK=$(field "$RESP" uuid)
check "task created" "$([ -n "$CREATED_TASK" ] && echo ok || echo '')" "ok"
echo "    uuid: ${CREATED_TASK}"

# 5. Get task — verify fields
echo "--- Get Task ---"
TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "title matches" "$(field "$TASK" title)" "${PREFIX} Task"
check "status is open" "$(field "$TASK" status)" "open"
check "project matches" "$(field "$TASK" project_id)" "${CREATED_PROJECT}"

# 6. Get area
echo "--- Get Area ---"
AREA=$(mcp_call "things_get_area" "{\"uuid\":\"${CREATED_AREA}\"}" | extract_text)
check "area title" "$(field "$AREA" title)" "${PREFIX} Area"

# 7. Get tag
echo "--- Get Tag ---"
TAG=$(mcp_call "things_get_tag" "{\"uuid\":\"${CREATED_TAG}\"}" | extract_text)
check "tag title" "$(field "$TAG" title)" "${PREFIX} Tag"

# 8. Edit task
echo "--- Edit Task ---"
RESP=$(mcp_call "things_edit_task" "{\"uuid\":\"${CREATED_TASK}\",\"title\":\"${PREFIX} Task (edited)\",\"note\":\"Updated notes\"}" | extract_text)
check "edit ok" "$(field "$RESP" status)" "updated"

TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "title updated" "$(field "$TASK" title)" "${PREFIX} Task (edited)"

# 9. Clear notes
echo "--- Edit Task: Clear Notes ---"
RESP=$(mcp_call "things_edit_task" "{\"uuid\":\"${CREATED_TASK}\",\"note\":\"none\"}" | extract_text)
check "clear notes ok" "$(field "$RESP" status)" "updated"

TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "notes cleared" "$(field "$TASK" note)" ""

# 10. Set area on task
echo "--- Edit Task: Set Area ---"
RESP=$(mcp_call "things_edit_task" "{\"uuid\":\"${CREATED_TASK}\",\"area\":\"${CREATED_AREA}\"}" | extract_text)
check "set area ok" "$(field "$RESP" status)" "updated"

TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "area set" "$(field "$TASK" area_id)" "${CREATED_AREA}"

# 11. Clear area
echo "--- Edit Task: Clear Area ---"
RESP=$(mcp_call "things_edit_task" "{\"uuid\":\"${CREATED_TASK}\",\"area\":\"none\"}" | extract_text)
check "clear area ok" "$(field "$RESP" status)" "updated"

TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "area cleared" "$(field "$TASK" area_id)" ""

# 12. Create task with repeat
echo "--- Create Repeating Task ---"
RESP=$(mcp_call "things_create_task" "{\"title\":\"${PREFIX} Repeat Daily\",\"when\":\"today\",\"repeat\":\"daily\"}" | extract_text)
REPEAT_TASK=$(field "$RESP" uuid)
check "repeat task created" "$([ -n "$REPEAT_TASK" ] && echo ok || echo '')" "ok"

# 13. Create task with repeat after completion
echo "--- Create Repeat After Completion ---"
RESP=$(mcp_call "things_create_task" "{\"title\":\"${PREFIX} Repeat Weekly AC\",\"when\":\"today\",\"repeat\":\"weekly after completion\"}" | extract_text)
REPEAT_AC_TASK=$(field "$RESP" uuid)
check "repeat-ac task created" "$([ -n "$REPEAT_AC_TASK" ] && echo ok || echo '')" "ok"

# 14. Create task with every N interval
echo "--- Create Every 2 Weeks ---"
RESP=$(mcp_call "things_create_task" "{\"title\":\"${PREFIX} Every 2 Weeks\",\"when\":\"today\",\"repeat\":\"every 2 weeks\"}" | extract_text)
REPEAT_2W_TASK=$(field "$RESP" uuid)
check "every-2-weeks task created" "$([ -n "$REPEAT_2W_TASK" ] && echo ok || echo '')" "ok"

# 15. Edit task to add repeat
echo "--- Edit Task: Add Repeat ---"
RESP=$(mcp_call "things_edit_task" "{\"uuid\":\"${CREATED_TASK}\",\"repeat\":\"monthly\"}" | extract_text)
check "add repeat ok" "$(field "$RESP" status)" "updated"

# 16. Edit task to clear repeat
echo "--- Edit Task: Clear Repeat ---"
RESP=$(mcp_call "things_edit_task" "{\"uuid\":\"${CREATED_TASK}\",\"repeat\":\"none\"}" | extract_text)
check "clear repeat ok" "$(field "$RESP" status)" "updated"

# 17. Create subtask
echo "--- Create Subtask ---"
RESP=$(mcp_call "things_create_task" "{\"title\":\"${PREFIX} Subtask\",\"parent_task\":\"${CREATED_TASK}\"}" | extract_text)
SUBTASK=$(field "$RESP" uuid)
check "subtask created" "$([ -n "$SUBTASK" ] && echo ok || echo '')" "ok"

# 18. Verify subtask parent
echo "--- Verify Subtask ---"
STASK=$(mcp_call "things_get_task" "{\"uuid\":\"${SUBTASK}\"}" | extract_text)
check "subtask parent" "$(field "$STASK" project_id)" "${CREATED_TASK}"

# 19. Edit task to move under parent
echo "--- Edit Task: Set Parent ---"
RESP2=$(mcp_call "things_create_task" "{\"title\":\"${PREFIX} Orphan\"}" | extract_text)
ORPHAN_TASK=$(field "$RESP2" uuid)
RESP=$(mcp_call "things_edit_task" "{\"uuid\":\"${ORPHAN_TASK}\",\"parent_task\":\"${CREATED_TASK}\"}" | extract_text)
check "set parent ok" "$(field "$RESP" status)" "updated"

# 20. Create checklist item
echo "--- Create Checklist Item ---"
RESP=$(mcp_call "things_create_checklist_item" "{\"title\":\"${PREFIX} Checklist Item\",\"task_uuid\":\"${CREATED_TASK}\"}" | extract_text)
CREATED_CHECKLIST=$(field "$RESP" uuid)
check "checklist item created" "$([ -n "$CREATED_CHECKLIST" ] && echo ok || echo '')" "ok"

# 21. List checklist items
echo "--- List Checklist Items ---"
CLIST=$(mcp_call "things_list_checklist_items" "{\"task_uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "checklist item in list" "$(has_uuid "$CLIST" "$CREATED_CHECKLIST")" "true"

# 22. Complete checklist item
echo "--- Complete Checklist Item ---"
RESP=$(mcp_call "things_complete_checklist_item" "{\"uuid\":\"${CREATED_CHECKLIST}\"}" | extract_text)
check "checklist complete ok" "$(field "$RESP" status)" "completed"

# 23. Uncomplete checklist item
echo "--- Uncomplete Checklist Item ---"
RESP=$(mcp_call "things_uncomplete_checklist_item" "{\"uuid\":\"${CREATED_CHECKLIST}\"}" | extract_text)
check "checklist uncomplete ok" "$(field "$RESP" status)" "uncompleted"

# 24. Delete checklist item
echo "--- Delete Checklist Item ---"
RESP=$(mcp_call "things_delete_checklist_item" "{\"uuid\":\"${CREATED_CHECKLIST}\"}" | extract_text)
check "checklist delete ok" "$(field "$RESP" status)" "deleted"

# 25. Move to someday
echo "--- Move to Someday ---"
RESP=$(mcp_call "things_move_to_someday" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "move to someday" "$(field "$RESP" status)" "moved_to_someday"

# 26. Move to anytime
echo "--- Move to Anytime ---"
RESP=$(mcp_call "things_move_to_anytime" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "move to anytime" "$(field "$RESP" status)" "moved_to_anytime"

# 27. Move to inbox
echo "--- Move to Inbox ---"
RESP=$(mcp_call "things_move_to_inbox" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "move to inbox" "$(field "$RESP" status)" "moved_to_inbox"

# 28. Move to today
echo "--- Move to Today ---"
RESP=$(mcp_call "things_move_to_today" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "move to today" "$(field "$RESP" status)" "moved_to_today"

# 29. Complete task
echo "--- Complete Task ---"
RESP=$(mcp_call "things_complete_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "complete ok" "$(field "$RESP" status)" "completed"

TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "status is completed" "$(field "$TASK" status)" "completed"

# 30. Uncomplete task
echo "--- Uncomplete Task ---"
RESP=$(mcp_call "things_uncomplete_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "uncomplete ok" "$(field "$RESP" status)" "uncompleted"

TASK=$(mcp_call "things_get_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "status is open again" "$(field "$TASK" status)" "open"

# 31. Trash task
echo "--- Trash Task ---"
RESP=$(mcp_call "things_trash_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "trash ok" "$(field "$RESP" status)" "trashed"

# 32. Untrash task
echo "--- Untrash Task ---"
RESP=$(mcp_call "things_untrash_task" "{\"uuid\":\"${CREATED_TASK}\"}" | extract_text)
check "untrash ok" "$(field "$RESP" status)" "restored"

# 33. List today — verify task appears
echo "--- List Today ---"
TODAY=$(mcp_call "things_list_today" "{}" | extract_text)
check "task in today" "$(has_uuid "$TODAY" "$CREATED_TASK")" "true"

# 34. List project tasks
echo "--- List Project Tasks ---"
PROJ=$(mcp_call "things_list_project_tasks" "{\"project_uuid\":\"${CREATED_PROJECT}\"}" | extract_text)
check "task in project" "$(has_uuid "$PROJ" "$CREATED_TASK")" "true"

# 35. List completed tasks
echo "--- List Completed ---"
COMPLETED=$(mcp_call "things_list_completed" "{\"limit\":5}" | extract_text)
check "completed returns array" "$(echo "$COMPLETED" | python3 -c "import sys,json; items=json.loads(sys.stdin.read()); print('ok' if isinstance(items, list) else 'fail')")" "ok"
check "completed includes completed_at" "$(echo "$COMPLETED" | python3 -c "import sys,json; items=json.loads(sys.stdin.read()); print('ok' if (not items or all('completed_at' in i for i in items)) else 'fail')")" "ok"

# --- cleanup ---

echo ""
echo "--- Cleanup ---"
mcp_call "things_trash_task" "{\"uuid\":\"${CREATED_TASK}\"}" > /dev/null 2>&1 && echo "    Trashed task"
mcp_call "things_trash_task" "{\"uuid\":\"${CREATED_PROJECT}\"}" > /dev/null 2>&1 && echo "    Trashed project"
[ -n "$SUBTASK" ] && mcp_call "things_trash_task" "{\"uuid\":\"${SUBTASK}\"}" > /dev/null 2>&1 && echo "    Trashed subtask"
[ -n "$ORPHAN_TASK" ] && mcp_call "things_trash_task" "{\"uuid\":\"${ORPHAN_TASK}\"}" > /dev/null 2>&1 && echo "    Trashed orphan task"
[ -n "$REPEAT_TASK" ] && mcp_call "things_trash_task" "{\"uuid\":\"${REPEAT_TASK}\"}" > /dev/null 2>&1 && echo "    Trashed repeat daily task"
[ -n "$REPEAT_AC_TASK" ] && mcp_call "things_trash_task" "{\"uuid\":\"${REPEAT_AC_TASK}\"}" > /dev/null 2>&1 && echo "    Trashed repeat-ac task"
[ -n "$REPEAT_2W_TASK" ] && mcp_call "things_trash_task" "{\"uuid\":\"${REPEAT_2W_TASK}\"}" > /dev/null 2>&1 && echo "    Trashed every-2-weeks task"
echo "    Note: area '${CREATED_AREA}' and tag '${CREATED_TAG}' cannot be trashed via API"

# --- summary & log ---

RESULT="PASS"
if [ "$FAIL" -gt 0 ]; then
  RESULT="FAIL"
fi

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed (cycle ${CYCLE}) ==="
echo ""

# Append to log file
{
  echo "$(date -u '+%Y-%m-%d %H:%M:%S UTC')  test-mcp  cycle=${CYCLE}  ${RESULT}  ${PASS} passed, ${FAIL} failed"
  if [ -n "$FAILURES" ]; then
    printf "%b" "$FAILURES"
  fi
} >> "$LOG_FILE"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
