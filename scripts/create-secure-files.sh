#!/bin/bash

# =============================================================================
# Create Secure Files Script
# =============================================================================
# Simple script to create encrypted binaries using secure-packager
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

# Create secure directory
mkdir -p secure/

# Components to encrypt
declare -A components=(
    ["fsm-server"]="bin/fsm-server"
    ["hdn-server"]="bin/hdn-server"
    ["principles-server"]="bin/principles-server"
    ["monitor-ui"]="bin/monitor-ui"
    ["goal-manager"]="bin/goal-manager"
)

# Encrypt each component
for component in "${!components[@]}"; do
    input_file="${components[$component]}"
    output_file="secure/agi-${component}.pem"
    
    if [ ! -f "$input_file" ]; then
        print_error "Binary not found: $input_file"
        print_status "Run 'make build' first"
        exit 1
    fi
    
    print_status "Encrypting $component..."
    
    secure-packager encrypt \
        --input "$input_file" \
        --output "$output_file" \
        --key "$SECURE_PACKAGER_KEY" \
        --algorithm "$SECURE_PACKAGER_ALGORITHM" \
        --salt "$SECURE_PACKAGER_SALT"
    
    print_status "✅ Created $output_file"
done

print_status "🎉 All secure files created successfully!"
print_warning "⚠️  Remember: Never commit the secure/ directory to git!"