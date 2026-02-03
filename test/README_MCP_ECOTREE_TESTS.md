# MCP EcoTree Playwright Tests

## Quick Start

After restarting your HDN server:

```bash
cd test
./quick_mcp_test.sh
```

## Available Tests

### 1. Quick Test
```bash
cd test
./quick_mcp_test.sh
```
Fast test of Southampton → Newcastle route.  
**Expected: 292 kg CO2**

### 2. Complete Test (with custom cities)
```bash
cd test
./test_mcp_ecotree_complete.sh [from_city] [to_city]
```
Full test with detailed output.

Examples:
```bash
./test_mcp_ecotree_complete.sh southampton newcastle
./test_mcp_ecotree_complete.sh london paris
```

### 3. Comparison Test
```bash
cd test  
./compare_all_tests.sh [from_city] [to_city]
```
Runs Python, Go standalone, and MCP tests side-by-side.

## Before Testing

1. **Restart HDN server:**
   ```bash
   cd ..
   ./restart_hdn.sh
   ```

2. **Wait for server to be ready** (about 5 seconds)

3. **Run test:**
   ```bash
   cd test
   ./quick_mcp_test.sh
   ```

## What You Should See

Successful test output:
```
============================================================
📊 Results
============================================================

Scraped data from https://ecotree.green/en/calculate-flight-co2#result

Title: Flying somewhere? How can I contribute to the environment with EcoTree?

✈️  CO2 Emissions: 292 kg
📏 Distance: 910 km

============================================================
✅ Success!
============================================================

🎯 Key Results:
   • CO2 Emissions: 292 kg
   • Distance: 910 km

✅ Result matches standalone tests! (292 kg CO2)
```

## Troubleshooting

### Server Not Running
```bash
cd ..
./restart_hdn.sh
```

### Check Logs
```bash
tail -f /tmp/hdn_server.log | grep "MCP-SCRAPE"
```

### Server Crashed
If you see panic errors in logs, the server needs to be restarted:
```bash
cd ..
make build-hdn
./restart_hdn.sh
```

## Test Files Location

All test scripts are now in the `/test` directory:
- `quick_mcp_test.sh` - Fast single test
- `test_mcp_ecotree_complete.sh` - Detailed test  
- `test_mcp_ecotree.sh` - Simple test (shows raw JSON)
- `compare_all_tests.sh` - Compare all methods

## Related Documentation

- `../PLAYWRIGHT_MCP_README.md` - Main setup guide
- `../TESTING_GUIDE.md` - Comprehensive testing guide  
- `../PLAYWRIGHT_INTEGRATION_SUMMARY.md` - Technical details

