#!/bin/bash
# Simple script to build and run the EcoTree test using the existing go.mod

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "============================================================"
echo "🔨 Building EcoTree Go Test Program"
echo "============================================================"

# Build using the existing go environment
echo "🔨 Building..."
go build -o test_ecotree_flight test_ecotree_flight.go

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    echo ""
    echo "============================================================"
    echo "🚀 Running EcoTree Test"
    echo "============================================================"
    echo ""
    
    # Run with default parameters or pass through command line args
    ./test_ecotree_flight "$@"
    
    EXIT_CODE=$?
    
    echo ""
    echo "✅ Done!"
    echo ""
    echo "Usage examples:"
    echo "  ./test_ecotree_flight                        # Default: southampton → newcastle"
    echo "  ./test_ecotree_flight -from london -to paris # Custom route"
    echo "  ./test_ecotree_flight -headless=false        # Show browser window"
    echo ""
    
    exit $EXIT_CODE
else
    echo "❌ Build failed!"
    exit 1
fi

