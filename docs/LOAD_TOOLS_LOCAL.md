# Loading Tools on Local Linux Setup

## Quick Start

Tools are **automatically loaded** when the HDN server starts. However, if you need to manually load or refresh tools:

## Method 1: Automatic (Recommended)

Tools are automatically bootstrapped when HDN server starts via `BootstrapSeedTools()`. Just start the HDN server:

```bash
# Start HDN server (tools will be auto-loaded)
cd hdn
./bin/hdn-server --mode=server --port=8080

# Or using the startup script
./scripts/start_servers.sh
```

Check logs for:
```
🔧 [BOOTSTRAP] Starting BootstrapSeedTools
✅ [REGISTER-TOOL] Successfully registered tool tool_http_get in Redis
```

## Method 2: Manual Discovery (If Tools Missing)

If tools aren't showing up, trigger discovery:

```bash
# Set HDN URL (default is 8080, but check your setup)
export HDN_URL="http://localhost:8080"  # or 8081 depending on your config

# Trigger tool discovery
curl -X POST "$HDN_URL/api/v1/tools/discover"

# Verify tools are registered
curl -s "$HDN_URL/api/v1/tools" | jq '.tools | length'
```

## Method 3: Using Bootstrap Script

```bash
# Use the bootstrap script (adjusts for your HDN port)
HDN_URL="http://localhost:8080" ./scripts/bootstrap-tools-local.sh

# Or if HDN is on port 8081
HDN_URL="http://localhost:8081" ./scripts/bootstrap-tools-local.sh
```

## Method 4: Check Current Tools

```bash
# List all registered tools
curl -s http://localhost:8080/api/v1/tools | jq '.tools[] | {id, name}'

# Count tools
curl -s http://localhost:8080/api/v1/tools | jq '.tools | length'

# Check specific tool
curl -s http://localhost:8080/api/v1/tools | jq '.tools[] | select(.id == "tool_http_get")'
```

## Method 5: Using tools_bootstrap.json (Optional)

If you have a custom `tools_bootstrap.json` file:

```bash
# Place it in the HDN directory or config directory
cp tools_bootstrap.json hdn/
# or
cp tools_bootstrap.json config/

# Restart HDN server - it will load from the file
```

## Troubleshooting

### Tools Not Loading?

1. **Check HDN server is running:**
   ```bash
   curl http://localhost:8080/health
   # or
   curl http://localhost:8081/health
   ```

2. **Check Redis is accessible:**
   ```bash
   # If using Docker
   docker exec <redis-container> redis-cli PING
   
   # If using local Redis
   redis-cli PING
   ```

3. **Check HDN logs for bootstrap messages:**
   ```bash
   # Look for these log messages:
   # 🔧 [BOOTSTRAP] Starting BootstrapSeedTools
   # ✅ [REGISTER-TOOL] Successfully registered tool...
   ```

4. **Check environment variables:**
   ```bash
   # Tools depend on EXECUTION_METHOD for some registrations
   echo $EXECUTION_METHOD
   echo $ENABLE_ARM64_TOOLS
   ```

5. **Manually trigger discovery:**
   ```bash
   curl -X POST http://localhost:8080/api/v1/tools/discover
   ```

### Default Tools Registered

On startup, HDN automatically registers:
- `tool_http_get` - HTTP GET requests
- `tool_file_read` - Read files
- `tool_file_write` - Write files
- `tool_ls` - List directories
- `tool_exec` - Execute shell commands
- `tool_html_scraper` - Scrape HTML
- `tool_docker_list` - List Docker entities
- `tool_codegen` - Generate code
- `tool_docker_build` - Build Docker images
- `tool_register` - Register new tools
- `tool_json_parse` - Parse JSON
- `tool_text_search` - Search text
- `tool_wiki_bootstrapper` - Wikipedia bootstrapper
- `tool_ssh_executor` - SSH executor (if EXECUTION_METHOD=ssh or ARM64)

## Verify Tools Are Working

```bash
# Test tool invocation
curl -X POST http://localhost:8080/api/v1/tools/tool_http_get/invoke \
  -H "Content-Type: application/json" \
  -d '{"url": "https://httpbin.org/get"}'
```

## Port Configuration

**Default ports:**
- HDN Server: `8080` (or `8081` in some setups)
- Check your `hdn/config.json` or startup command for the actual port

**To find your HDN port:**
```bash
# Check config
cat hdn/config.json | jq '.server.port'

# Or check running process
ps aux | grep hdn-server | grep -oP 'port=\K[0-9]+'
```

