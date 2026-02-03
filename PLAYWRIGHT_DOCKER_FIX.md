# Playwright Docker Integration - Technical Summary

## Problem
The MCP scraper with embedded Playwright was failing in Docker containers with:
```
failed to start Playwright: please install the driver (v1.52.0) first
```

## Root Cause
1. **Alpine Linux incompatibility**: playwright-go's bundled `node` binary requires glibc, but Alpine uses musl libc
2. **Symbol not found**: The node binary failed with `fcntl64: symbol not found` and `unsupported relocation type 37`
3. **No pre-installed driver**: Playwright driver must be installed before first use

## Solution
### 1. Switch to Debian Base Image
Changed from `alpine:latest` to `debian:bookworm-slim` for full glibc support.

### 2. Auto-Install Driver on First Use
```go
// In mcp_knowledge_server.go
var (
    playwrightOnce    sync.Once
    playwrightInitErr error
)

func (s *MCPKnowledgeServer) executePlaywrightOperations(...) {
    // Install driver once, skip browsers (use system Chromium)
    playwrightOnce.Do(func() {
        if err := pw.Install(&pw.RunOptions{SkipInstallBrowsers: true}); err != nil {
            playwrightInitErr = fmt.Errorf("failed to install driver: %v", err)
        }
    })
    
    if playwrightInitErr != nil {
        return nil, playwrightInitErr
    }
    
    pwInstance, err := pw.Run()
    ...
}
```

### 3. Use System Chromium
```go
executablePath := "/usr/bin/chromium"
browser, err := pwInstance.Chromium.Launch(pw.BrowserTypeLaunchOptions{
    Headless:       pw.Bool(true),
    ExecutablePath: &executablePath,
})
```

## Dockerfile Changes
```dockerfile
# OLD: Alpine with manual driver download (didn't work)
FROM alpine:latest
RUN apk add chromium ...
RUN curl playwright-1.52.0-linux-arm64.zip ...  # Failed due to glibc

# NEW: Debian with auto-install
FROM debian:bookworm-slim
RUN apt-get install chromium fonts-liberation libnss3 ...
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
# Driver installs automatically on first scrape request
```

## Results
✅ **Working!** Successfully scrapes EcoTree CO2 calculator:
- Driver installs on first request (~6 seconds one-time setup)
- Extracts CO2 emissions: 292 kg
- Distance: 910 km
- Handles complex interactions (clicks, fills, waits)

## Image Size Impact
- Alpine + failed attempts: ~850 MB
- Debian + Chromium + working Playwright: ~1.1 GB
- Trade-off: +250 MB for functional browser automation

## Testing
```bash
# Build on RPI
docker build -f Dockerfile.hdn.secure \
  --build-arg CUSTOMER_PUBLIC_KEY=secure/customer_public.pem \
  --build-arg VENDOR_PUBLIC_KEY=secure/vendor_public.pem \
  -t stevef1uk/hdn-server:secure .

# Push
docker push stevef1uk/hdn-server:secure

# Restart Kubernetes deployment
kubectl rollout restart deployment/hdn-server-rpi58 -n agi

# Test
./test/test_mcp_ecotree_k8s.sh agi
```

## Key Learnings
1. **glibc is required**: playwright-go cannot run on musl libc (Alpine)
2. **Auto-install works**: `pw.Install()` handles driver setup at runtime
3. **System browsers work**: No need to download Playwright's browsers
4. **First request is slower**: ~6s for one-time driver installation

## Commit
`b1a9e07` - "fix: Playwright integration - use Debian for glibc, auto-install driver"

