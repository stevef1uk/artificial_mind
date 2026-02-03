#!/bin/bash
# Playwright Scraper Service - Entrypoint
# Copyright (c) 2026 Steven Fisher
# Licensed for non-commercial use only. See LICENSE file.

set -euo pipefail

echo "🚀 Starting Playwright Scraper Service..."

# Set timezone
export TZ="${TZ:-UTC}"
echo "⏰ Timezone: $TZ"

# Verify Chromium is available
if command -v chromium &> /dev/null; then
    CHROMIUM_VERSION=$(chromium --version 2>/dev/null || echo "unknown")
    echo "✅ Chromium found: $CHROMIUM_VERSION"
else
    echo "⚠️  Warning: Chromium not found in PATH"
fi

# Display configuration
echo "📊 Configuration:"
echo "   - Worker Count: 3 (hardcoded in main.go)"
echo "   - Job Queue Size: 100"
echo "   - Job Retention: 30 minutes"
echo "   - Page Timeout: 20 seconds"
echo "   - Port: 8080"
echo ""

# Check if this is a health check
if [ "${1:-}" = "-health-check" ]; then
    # Simple health check: try to connect to port 8080
    if command -v wget &> /dev/null; then
        wget -q -O- http://localhost:8080/health > /dev/null 2>&1
        exit $?
    elif command -v curl &> /dev/null; then
        curl -sf http://localhost:8080/health > /dev/null 2>&1
        exit $?
    else
        # No health check tool available, assume healthy
        exit 0
    fi
fi

# Start the scraper service
echo "🎬 Starting scraper service..."
exec /app/scraper

