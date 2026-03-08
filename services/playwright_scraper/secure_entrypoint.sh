#!/bin/bash
# Playwright Scraper Service - Secure Entrypoint
# Copyright (c) 2026 Steven Fisher
# Licensed for non-commercial use only. See LICENSE file.

set -euo pipefail

echo "🔒 Playwright Scraper Service (Secure Mode)"

# Set timezone
export TZ="${TZ:-UTC}"
echo "⏰ Timezone: $TZ"

# Check if this is a health check
if [ "${1:-}" = "-health-check" ]; then
    # Simple health check: try to connect to port 8085
    if command -v wget &> /dev/null; then
        wget -q -O- http://localhost:8085/health > /dev/null 2>&1
        exit $?
    elif command -v curl &> /dev/null; then
        curl -sf http://localhost:8085/health > /dev/null 2>&1
        exit $?
    else
        # No health check tool available, assume healthy if process is running
        pgrep -f scraper > /dev/null 2>&1
        exit $?
    fi
fi

# Verify customer private key exists
CUSTOMER_KEY="${SECURE_CUSTOMER_PRIVATE_PATH:-/keys/customer_private.pem}"
if [ ! -f "$CUSTOMER_KEY" ]; then
    echo "❌ Error: Customer private key not found at $CUSTOMER_KEY"
    echo "   Please mount the secure-customer-private secret to /keys/"
    exit 1
fi

echo "✅ Customer private key found"

# Verify vendor token exists
VENDOR_TOKEN="${SECURE_VENDOR_TOKEN:-}"
if [ -z "$VENDOR_TOKEN" ]; then
    echo "❌ Error: SECURE_VENDOR_TOKEN environment variable not set"
    echo "   Please set the secure-vendor token"
    exit 1
fi

echo "✅ Vendor token configured"

# Create working directory for unpacking
WORK_DIR="${UNPACK_WORK_DIR:-/tmp/unpack}"
mkdir -p "$WORK_DIR"

echo "📦 Decrypting scraper binary..."

# Write vendor token to file for unpack
TOKEN_FILE="/tmp/vendor.token"
printf "%s" "$VENDOR_TOKEN" > "$TOKEN_FILE"

# Unpack the encrypted binary
if ! /usr/local/bin/unpack \
    -zip ./scraper.enc \
    -priv "$CUSTOMER_KEY" \
    -work "$WORK_DIR" \
    -out "$WORK_DIR" \
    -license-token "$TOKEN_FILE"; then
    echo "❌ Failed to decrypt scraper binary"
    echo "   Check that the customer key and vendor token are correct"
    exit 1
fi

echo "✅ Binary decrypted successfully"

# Verify the unpacked binary exists
if [ ! -f "$WORK_DIR/scraper" ]; then
    echo "❌ Error: Decrypted scraper binary not found at $WORK_DIR/scraper"
    ls -la "$WORK_DIR/" || true
    exit 1
fi

# Make the binary executable
chmod +x "$WORK_DIR/scraper"

# Verify Chromium is available
if command -v chromium &> /dev/null; then
    CHROMIUM_VERSION=$(chromium --version 2>/dev/null || echo "unknown")
    echo "✅ Chromium found: $CHROMIUM_VERSION"
else
    echo "⚠️  Warning: Chromium not found in PATH"
fi

# Display configuration
echo ""
echo "📊 Configuration:"
echo "   - Worker Count: 3"
echo "   - Job Queue Size: 100"
echo "   - Job Retention: 30 minutes"
echo "   - Page Timeout: 20 seconds"
echo "   - Port: 8085"
echo ""

# Change to scraper user and run the binary
echo "🎬 Starting scraper service as user 'scraper'..."

# Run as scraper user (using gosu which is compatible with Debian)
if command -v gosu &> /dev/null; then
    exec gosu scraper "$WORK_DIR/scraper"
elif command -v su-exec &> /dev/null; then
    exec su-exec scraper "$WORK_DIR/scraper"
else
    # Fallback: run as root (not ideal but works)
    echo "⚠️  Warning: gosu/su-exec not found, running as root"
    exec "$WORK_DIR/scraper"
fi

