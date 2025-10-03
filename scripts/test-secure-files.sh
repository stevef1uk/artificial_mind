#!/bin/bash

# =============================================================================
# Test Secure Files Script
# =============================================================================
# Simple script to test that encrypted binaries can be decrypted
# =============================================================================

set -e

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Check prerequisites
if [ -z "$SECURE_PACKAGER_KEY" ]; then
    print_error "SECURE_PACKAGER_KEY not set. Run: source .env.secure"
    exit 1
fi

if [ -z "$SECURE_PACKAGER_SALT" ]; then
    print_error "SECURE_PACKAGER_SALT not set. Run: source .env.secure"
    exit 1
fi

# Set defaults
export SECURE_PACKAGER_ALGORITHM="${SECURE_PACKAGER_ALGORITHM:-AES-256-GCM}"

# Components to test
components=(
    "fsm-server"
    "hdn-server"
    "principles-server"
    "monitor-ui"
    "goal-manager"
)

# Test each component
for component in "${components[@]}"; do
    encrypted_file="secure/agi-${component}.pem"
    test_output="/tmp/test-${component}"
    
    if [ ! -f "$encrypted_file" ]; then
        print_error "❌ Missing: $encrypted_file"
        continue
    fi
    
    print_status "Testing $component..."
    
    if secure-packager decrypt \
        --input "$encrypted_file" \
        --output "$test_output" \
        --key "$SECURE_PACKAGER_KEY" \
        --algorithm "$SECURE_PACKAGER_ALGORITHM" \
        --salt "$SECURE_PACKAGER_SALT" 2>/dev/null; then
        
        if [ -f "$test_output" ] && [ -x "$test_output" ]; then
            print_status "✅ $component decryption successful"
            rm -f "$test_output"
        else
            print_error "❌ $component decryption failed"
        fi
    else
        print_error "❌ $component decryption failed"
    fi
done

print_status "🎉 Secure files test completed!"