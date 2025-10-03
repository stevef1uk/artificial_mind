#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${1:-agi}"

echo "[tools-register] Using namespace: ${NAMESPACE}"

POD_NAME=$(kubectl -n "${NAMESPACE}" get pods -l app=hdn-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [[ -z "${POD_NAME}" ]]; then
  echo "[tools-register] ERROR: No hdn-server pod found in namespace ${NAMESPACE}" >&2
  exit 1
fi

echo "[tools-register] Target pod: ${POD_NAME}"

post_json() {
  local json="$1"
  kubectl -n "${NAMESPACE}" exec "${POD_NAME}" -- sh -lc \
    "printf '%s' '$json' | wget -qO- --header=Content-Type:application/json --post-data=- http://127.0.0.1:8080/api/v1/tools" || \
    echo "[tools-register] WARN: registration call failed"
  echo
}

echo "[tools-register] Registering built-in binaries as tools..."

# html_scraper
post_json '{"id":"tool_html_scraper","name":"HTML Scraper","description":"Parse HTML and extract title/headings/paragraphs/links","input_schema":{"url":"string"},"output_schema":{"items":"array"},"permissions":["net:read"],"safety_level":"low","created_by":"system"}'

# json_parse
post_json '{"id":"tool_json_parse","name":"JSON Parse","description":"Parse JSON","input_schema":{"text":"string"},"output_schema":{"object":"json"},"permissions":[],"safety_level":"low","created_by":"system"}'

# text_search
post_json '{"id":"tool_text_search","name":"Text Search","description":"Search text","input_schema":{"pattern":"string","text":"string"},"output_schema":{"matches":"string[]"},"permissions":[],"safety_level":"low","created_by":"system"}'

# file_read
post_json '{"id":"tool_file_read","name":"File Reader","description":"Read file","input_schema":{"path":"string"},"output_schema":{"content":"string"},"permissions":["fs:read"],"safety_level":"medium","created_by":"system"}'

# file_write
post_json '{"id":"tool_file_write","name":"File Writer","description":"Write file","input_schema":{"path":"string","content":"string"},"output_schema":{"written":"int"},"permissions":["fs:write"],"safety_level":"high","created_by":"system"}'

# exec (sandboxed)
post_json '{"id":"tool_exec","name":"Shell Exec","description":"Run shell command (sandboxed)","input_schema":{"cmd":"string"},"output_schema":{"stdout":"string","stderr":"string","exit_code":"int"},"permissions":["proc:exec"],"safety_level":"high","created_by":"system"}'

# docker_list
post_json '{"id":"tool_docker_list","name":"Docker List","description":"List docker entities","input_schema":{"type":"string"},"output_schema":{"items":"string[]"},"permissions":["docker"],"safety_level":"medium","created_by":"system"}'

# codegen
post_json '{"id":"tool_codegen","name":"Codegen","description":"Generate code via LLM","input_schema":{"spec":"string"},"output_schema":{"code":"string"},"permissions":["llm"],"safety_level":"medium","created_by":"system"}'

echo "[tools-register] Done. You can refresh the Monitor Tools page."


