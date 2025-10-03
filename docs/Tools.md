# Tools

This document describes the agent tool system: the data model, lifecycle (bootstrap/discovery/registration), invocation, safety, and UI integration.

## Data model

Tools are stored in Redis and represented by this schema (see `hdn/tools.go`):

```
Tool {
  id: string
  name: string
  description: string
  input_schema: map[string]string
  output_schema: map[string]string
  permissions: string[]              // e.g., fs:read, proc:exec, docker, net:read
  safety_level: string               // low | medium | high
  created_by: string                 // system | agent
  created_at: timestamp
  exec?: {
    type: string                     // cmd | image
    cmd?: string                     // absolute path or binary name
    args?: string[]                  // supports placeholder substitution with {param}
    image?: string                   // docker image, when type=image
  }
}
```

Redis keys:
- `tools:registry`: set of tool ids
- `tool:<id>`: JSON blob of a `Tool`

## Lifecycle

1) Bootstrap on server start (`hdn/api.go` → `APIServer.BootstrapSeedTools`)
   - Loads `tools_bootstrap.json` or `config/tools_bootstrap.json` if present.
   - Else registers a default nucleus: `tool_http_get`, `tool_file_read`, `tool_ls`, etc.

2) Discovery endpoint
   - `POST /api/v1/tools/discover` registers built-in tools and environment-dependent ones (e.g., docker).

3) Programmatic registration
   - `POST /api/v1/tools` with a full `Tool` JSON.
   - If `created_by` omitted, defaults to `system`.
   - Deletion is allowed only for `created_by=agent`.

4) Auto-generated tools from Interpreter
   - `POST /api/v1/interpret/execute` may auto-register a tool when a non-trivial code artifact is produced.
   - Proposed tools are marked `created_by=agent` and currently use `exec.type=cmd` with a Python driver.

## Invocation

- `POST /api/v1/tools/{id}/invoke` with a JSON body containing parameters.
- Placeholder substitution in `exec.args` uses `{param}` tokens; `stdin` is supported via params.
- For `exec.type=cmd`, the server executes code in a sandboxed Docker environment with a minimal Python wrapper when needed.
- For `exec.type=image`, the server runs the provided Docker image with optional command/args (host Docker required).

Output handling:
- The server attempts to parse tool output as JSON; if multiple lines are printed, it extracts the last JSON-looking line. Otherwise, it returns `{ "output": "..." }`.

## Safety and permissions

- A principles pre-check gates `invoke` operations.
- A permissive sandbox policy can be restricted via `ALLOWED_TOOL_PERMS` env var.
- The intelligent executor performs static safety checks on generated code.

## UI integration (Monitor)

- `Monitor` reads from Redis directly via `/api/tools` to list tools.
- A filter shows only auto-generated tools (`created_by=agent`).
- Deletion proxies to HDN `DELETE /api/v1/tools/{id}` and is allowed only for agent-created tools.

## Operational tips

- To repopulate after Redis wipe: restart HDN (auto-bootstrap) or call `POST /api/v1/tools/discover`.
- Persist custom tools: add them to `tools_bootstrap.json` so they reload on startup.

# Tools Catalog

This document describes the base toolset, registry schema, discovery/registration flow, and invocation usage.

## Registry Schema (Redis)

- `tools:registry` — Set of tool IDs
- `tool:{id}` — JSON blob

```json
{
  "id": "tool_http_get",
  "name": "HTTP GET",
  "description": "Fetches a URL and returns response body",
  "input_schema": { "url": "string" },
  "output_schema": { "status": "int", "body": "string" },
  "permissions": ["net:read"],
  "safety_level": "low",
  "created_by": "system",
  "created_at": "2025-09-23T12:00:00Z"
}
```

- Usage history:
  - `tools:{agent_id}:usage_history` — list of recent records `{ts, type, tool, ...}`
  - `tools:global:usage_history` — aggregate

## Events (NATS)

- `agi.tool.discovered` — Registered from discovery
- `agi.tool.created` — Synthesized/registered by agent
- `agi.tool.invoked` — Invocation started (emitted by FSM or HDN)
- `agi.tool.result` — Invocation result
- `agi.tool.failed` — Invocation error

## Permissions & Safety

- `permissions`: coarse-grained capabilities (e.g., `fs:read`, `fs:write`, `net:read`, `docker`, `proc:exec`).
- `safety_level`: `low | medium | high`.
- Pre-exec Principles check includes context: tool id, permissions, safety level, agent id, project id.
- Sandbox Policy: default-permissive (tools run in Docker). To restrict, set `ALLOWED_TOOL_PERMS` env (comma-separated whitelist).

## Endpoints

- `GET /api/v1/tools` — list registered tools
- `POST /api/v1/tools` — register tool JSON
- `POST /api/v1/tools/discover` — seed discovery
- `POST /api/v1/tools/{id}/invoke` — invoke a tool

Headers (optional):
- `X-Agent-ID`, `X-Project-ID`, `X-Caller-Scopes` (e.g., `read_registry,invoke_tool`).

## Seed Tools

### Perception / Data

- `tool_http_get`
  - Inputs: `{ url: string }`
  - Outputs: `{ status: int, body: string }`
  - Perms: `net:read`
  - Binary: `tools/http_get`

- `tool_html_scraper`
  - Inputs: `{ url: string, selectors?: string[] (future) }`
  - Outputs: `{ items: [{ tag, text, attributes }] }`
  - Binary: `tools/html_scraper`

- `tool_json_parse`
  - Inputs: JSON (stdin or `file` flag)
  - Outputs: pretty-printed JSON
  - Binary: `tools/json_parse`

- `tool_text_search`
  - Inputs: regex pattern, text via stdin
  - Outputs: `{ matches: string[] }`
  - Binary: `tools/text_search`

### Action / System

- `tool_file_read`
  - Inputs: `{ path: string }`
  - Outputs: `{ content: string }`
  - Perms: `fs:read`
  - Binary: `tools/file_read`

- `tool_file_write`
  - Inputs: `{ path: string, content: string }`
  - Outputs: `{ written: int }`
  - Perms: `fs:write`
  - Binary: `tools/file_write`

- `tool_exec`
  - Inputs: `{ cmd: string }`
  - Outputs: `{ stdout, stderr, exit_code }`
  - Perms: `proc:exec`
  - Binary: `tools/exec`

- `tool_docker_list`
  - Inputs: `{ type: "containers"|"images" }`
  - Outputs: `{ items: string[] }`
  - Perms: `docker`
  - Binary: `tools/docker_list`

### Self-Extension

- `tool_codegen`
  - Inputs: `{ spec: string }`
  - Outputs: `{ code: string }`
  - Perms: `llm`
  - Binary: `tools/codegen` (stub; HDN LLM integration preferred)

## Examples

### Register a tool

```bash
curl -s -X POST http://localhost:8081/api/v1/tools \
  -H 'Content-Type: application/json' \
  -d '{
    "id":"tool_http_get",
    "name":"HTTP GET",
    "description":"Fetch URL",
    "input_schema":{"url":"string"},
    "output_schema":{"status":"int","body":"string"},
    "permissions":["net:read"],
    "safety_level":"low",
    "created_by":"system"
  }'
```

### Invoke a tool

```bash
curl -s -X POST http://localhost:8081/api/v1/tools/tool_http_get/invoke \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com"}'
```

## Bootstrap

- On startup, HDN loads `tools_bootstrap.json` if present; otherwise registers a default nucleus set.
- Failsafe ensures minimum tools exist even after Redis clears.

## Notes

- Monitor exposes `GET /api/tools` and `GET /api/tools/usage` to list tools and recent invocations.
- FSM logs usage to `tools:{agent_id}:usage_history` and emits user-visible effects via the Monitor.
