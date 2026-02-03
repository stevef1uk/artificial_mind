# Playwright MCP Testing Guide

## Quick Start

After restarting your HDN server, run:

```bash
./quick_mcp_test.sh
```

This tests the exact same Southampton → Newcastle route through your MCP server.

## Available Test Scripts

### 1. Quick MCP Test
```bash
./quick_mcp_test.sh
```
- Fast single test
- Tests Southampton → Newcastle via MCP
- Expected result: **292 kg CO2**

### 2. Complete MCP Test (with options)
```bash
./test_mcp_ecotree_complete.sh [from_city] [to_city]
```
- Detailed output with structured data
- Customizable cities
- Shows full MCP response

Examples:
```bash
./test_mcp_ecotree_complete.sh southampton newcastle  # Default
./test_mcp_ecotree_complete.sh london paris           # Custom route
./test_mcp_ecotree_complete.sh amsterdam berlin       # Another route
```

### 3. Comparison Test (All Three Methods)
```bash
./compare_all_tests.sh [from_city] [to_city]
```
- Runs Python, Go, and MCP tests side-by-side
- Compares results for validation
- Shows execution times
- Verifies all three produce identical results

Example output:
```
Test Method          | CO2 (kg)        | Time (s)
-----------------------------------------------------
Python Playwright    | 292             | 12
Go Playwright        | 292             | 8
MCP Server           | 292             | 10

✅ All three tests produced identical results!
```

## Standalone Tests (for reference)

### Python Playwright
```bash
# Activate virtual environment first
source playwright_venv/bin/activate

# Run test
python test_ecotree_flight.py southampton newcastle

# Deactivate when done
deactivate
```

### Go Playwright
```bash
cd tools/ecotree_test
./ecotree_test -from southampton -to newcastle
```

## Expected Results

For **Southampton → Newcastle** route:
- ✅ CO2 Emissions: **292 kg**
- ✅ Distance: **910 km**
- ✅ Fun fact: Could power Eiffel Tower lights for 4 days

## Troubleshooting

### MCP Server Not Running
```bash
# Check if server is running
curl http://localhost:8081/health

# If not, start it
./restart_hdn.sh
```

### Check Server Logs
```bash
# Watch live logs
tail -f /tmp/hdn_server.log

# Search for errors
grep ERROR /tmp/hdn_server.log

# Search for Playwright logs
grep "MCP-SCRAPE" /tmp/hdn_server.log
```

### Build Issues
```bash
# Rebuild HDN server
make build-hdn

# Rebuild Go standalone test
cd tools/ecotree_test
go build -o ecotree_test main.go
```

### Python Environment Issues
```bash
# Recreate Python venv
rm -rf playwright_venv
./setup_playwright_venv.sh
```

## Test Sequence

Recommended testing sequence after making changes:

1. **Build:**
   ```bash
   make build-hdn
   ```

2. **Start Server:**
   ```bash
   ./restart_hdn.sh
   ```

3. **Run Quick Test:**
   ```bash
   ./quick_mcp_test.sh
   ```

4. **If issues, check logs:**
   ```bash
   tail -100 /tmp/hdn_server.log | grep "MCP-SCRAPE"
   ```

5. **For validation, run comparison:**
   ```bash
   ./compare_all_tests.sh
   ```

## What Each Test Validates

### Python Test (`test_ecotree_flight.py`)
- ✅ Playwright Python works on your system
- ✅ Browser automation is functional
- ✅ Website is accessible
- ✅ Baseline for comparison

### Go Standalone Test (`tools/ecotree_test/`)
- ✅ Playwright Go works on your system  
- ✅ Same operations in Go produce same results
- ✅ Direct Playwright API usage

### MCP Server Test (`test_mcp_ecotree_complete.sh`)
- ✅ TypeScript config parsing works
- ✅ Operation conversion is correct
- ✅ MCP server executes Playwright correctly
- ✅ Results extraction works
- ✅ MCP response formatting is correct
- ✅ **End-to-end integration works!**

## Custom Routes

Test other flight routes:

```bash
# European routes
./quick_mcp_test.sh london paris
./quick_mcp_test.sh amsterdam berlin
./quick_mcp_test.sh madrid rome

# UK routes
./quick_mcp_test.sh manchester edinburgh
./quick_mcp_test.sh bristol glasgow

# Custom
./test_mcp_ecotree_complete.sh <your_city> <destination_city>
```

**Note:** City names should be lowercase, single words work best. For multi-word cities, try the main name (e.g., "san francisco" → "san").

## Integration with Your Workflow

Once validated, you can use this in your MCP clients:

### MCP Client Example
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "scrape_url",
    "arguments": {
      "url": "https://ecotree.green/en/calculate-flight-co2",
      "typescript_config": "<your TypeScript code here>"
    }
  }
}
```

### Claude Desktop / Other MCP Clients

Your MCP server now exposes the `scrape_url` tool with TypeScript config support. Any MCP client can use it to:

1. Record Playwright interactions as TypeScript
2. Send TypeScript to your MCP server
3. Get scraped results back

## Success Criteria

✅ **All tests pass** = Playwright MCP integration working perfectly!

- Python test extracts 292 kg CO2
- Go test extracts 292 kg CO2  
- MCP test extracts 292 kg CO2
- All three results match
- Server logs show successful Playwright execution

## Next Steps

After confirming tests pass:

1. ✅ Use `scrape_url` tool in your MCP clients
2. ✅ Record other websites with Playwright codegen
3. ✅ Convert TypeScript tests to MCP scraping workflows
4. ✅ Integrate with your AI agents for dynamic web scraping

Happy testing! 🎉

