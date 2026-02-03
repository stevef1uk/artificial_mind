# Playwright on ARM64/Raspberry Pi

## Overview
The headless browser tool uses Playwright Go, which requires Chromium binaries. On ARM64 systems (Raspberry Pi, ARM servers), special configuration is needed.

## Docker Configuration

The `Dockerfile.hdn.secure` has been updated to support ARM64:

1. **System Chromium**: Uses Alpine's ARM64-compatible Chromium package instead of downloading x86 binaries
2. **Skip Browser Download**: Sets `PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1` to prevent Playwright from downloading incompatible binaries
3. **Browser Path**: Points Playwright to system Chromium via `PLAYWRIGHT_BROWSERS_PATH`

### Dependencies Added
```dockerfile
chromium              # ARM64-compatible Chromium browser
chromium-chromedriver # ChromeDriver for Playwright
nss freetype harfbuzz # Font rendering libraries
ttf-freefont          # Free fonts for rendering
libstdc++ libc6-compat # C++ stdlib compatibility
```

## Building for ARM64

```bash
# On x86_64 machine (cross-compile)
docker buildx build --platform linux/arm64 -t hdn:arm64 -f Dockerfile.hdn.secure .

# On Raspberry Pi (native)
docker build -t hdn:arm64 -f Dockerfile.hdn.secure .
```

## Known Limitations

### 1. Browser Version
- System Chromium may be older than Playwright's recommended version
- Some newer Playwright features may not be available
- This is acceptable for basic scraping tasks

### 2. Performance
- ARM64 systems (especially RPi) have limited resources
- Headless browser operations are memory-intensive
- Consider:
  - Increasing timeouts (default 60s may be too short)
  - Limiting concurrent browser instances
  - Using simpler selectors to reduce processing

### 3. Font Rendering
- Some websites may render differently without all fonts
- Added `ttf-freefont` for basic font support
- Complex Unicode/emoji may not display correctly

## Testing on ARM64

```bash
# Test browser is working
docker run --rm hdn:arm64 /app/bin/tools/headless_browser \
  -url "https://example.com" \
  -actions '[{"type":"extract","extract":{"title":"title"}}]' \
  -timeout 30

# Expected output: JSON with page title
```

## Troubleshooting

### Browser Not Found
If you see "Browser binary not found":
```bash
# Check if Chromium is installed
docker run --rm hdn:arm64 which chromium-browser

# Verify Playwright env vars
docker run --rm hdn:arm64 env | grep PLAYWRIGHT
```

### Timeouts
ARM64 systems may need longer timeouts:
```go
// In headless_browser/main.go
timeout := flag.Int("timeout", 120, "Timeout in seconds") // Increased from 60
```

### Memory Issues
Monitor memory usage:
```bash
docker stats hdn

# If OOM killed, increase Docker memory limit
docker run --memory=2g hdn:arm64
```

## Alternative: x86 Binary via QEMU

If system Chromium doesn't work, you can run x86 binaries via QEMU:

```dockerfile
# Install QEMU for x86 emulation
RUN apk add --no-cache qemu-x86_64

# Let Playwright download x86 binaries
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=0
```

**Note**: This is slower and not recommended for production.

## Production Recommendations

For production ARM64 deployments:

1. **Dedicated Browser Service**: Run Playwright on a separate x86_64 machine
2. **Remote Browser Protocol**: Use Chrome DevTools Protocol over network
3. **Fallback to x86**: Deploy critical browser automation on x86_64 nodes
4. **Hybrid Architecture**: Use ARM64 for main app, x86_64 for browser tasks

## Verification Checklist

- [ ] Dockerfile builds successfully for arm64
- [ ] Chromium binary exists at `/usr/bin/chromium-browser`
- [ ] Playwright env vars are set correctly
- [ ] Simple test (example.com) works
- [ ] Timeouts are appropriate for ARM64 performance
- [ ] Memory limits account for browser overhead

