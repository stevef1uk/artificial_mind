#!/bin/bash
# Build and run the standalone EcoTree Go program

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "============================================================"
echo "🔨 Building EcoTree Go Program"
echo "============================================================"

# Create a temporary directory for this standalone program
TMP_DIR=$(mktemp -d)
echo "📁 Using temporary directory: $TMP_DIR"

# Copy files
cp test_ecotree_flight.go "$TMP_DIR/"
cp go.mod.ecotree "$TMP_DIR/go.mod"

cd "$TMP_DIR"

# Initialize go module and download dependencies
echo "📦 Downloading dependencies..."
go mod tidy

# Build the program
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
    
    # Copy the binary back to the original directory
    echo ""
    echo "📦 Copying binary to $SCRIPT_DIR/test_ecotree_flight"
    cp test_ecotree_flight "$SCRIPT_DIR/"
    
    # Cleanup
    cd "$SCRIPT_DIR"
    rm -rf "$TMP_DIR"
    
    echo ""
    echo "✅ Done! You can run it directly with: ./test_ecotree_flight"
    echo ""
    
    exit $EXIT_CODE
else
    echo "❌ Build failed!"
    cd "$SCRIPT_DIR"
    rm -rf "$TMP_DIR"
    exit 1
fi

