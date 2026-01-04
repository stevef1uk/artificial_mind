#!/bin/bash
# Test script to verify Go matrix operations work with Docker and environment variables

set -e

echo "🧪 Testing Go Matrix Operations with Docker"
echo "=============================================="

# Test matrices
MATRIX1='[[1,2],[3,4]]'
MATRIX2='[[5,6],[7,8]]'

# Expected output: [6 8] on first line, [10 12] on second line

# Create test Go code
cat > /tmp/test_matrix.go << 'GOCODE'
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func addMatrices(matrix1 [][]int, matrix2 [][]int) [][]int {
	result := make([][]int, len(matrix1))
	for i := range matrix1 {
		result[i] = make([]int, len(matrix1[i]))
		for j := range matrix1[i] {
			result[i][j] = matrix1[i][j] + matrix2[i][j]
		}
	}
	return result
}

func main() {
	matrix1Str := os.Getenv("matrix1")
	if matrix1Str == "" {
		fmt.Fprintf(os.Stderr, "ERROR: matrix1 environment variable not set\n")
		os.Exit(1)
	}
	
	matrix2Str := os.Getenv("matrix2")
	if matrix2Str == "" {
		fmt.Fprintf(os.Stderr, "ERROR: matrix2 environment variable not set\n")
		os.Exit(1)
	}
	
	fmt.Fprintf(os.Stderr, "DEBUG: matrix1=%s\n", matrix1Str)
	fmt.Fprintf(os.Stderr, "DEBUG: matrix2=%s\n", matrix2Str)
	
	var matrix1 [][]int
	if err := json.Unmarshal([]byte(matrix1Str), &matrix1); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to parse matrix1: %v\n", err)
		os.Exit(1)
	}
	
	var matrix2 [][]int
	if err := json.Unmarshal([]byte(matrix2Str), &matrix2); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to parse matrix2: %v\n", err)
		os.Exit(1)
	}
	
	result := addMatrices(matrix1, matrix2)
	
	for i := 0; i < len(result); i++ {
		fmt.Println(result[i])
	}
}
GOCODE

echo ""
echo "📝 Test code created at /tmp/test_matrix.go"
echo ""

# Test 1: Run with Docker (like the SSH fallback does)
echo "🧪 Test 1: Running with Docker (SSH fallback method)"
echo "---------------------------------------------------"
WORK=$(mktemp -d /tmp/go_test_XXXXXX)
cp /tmp/test_matrix.go "$WORK/main.go"
cd "$WORK"
go mod init testmod >/dev/null 2>&1 || true

echo "Running: docker run --rm -e matrix1='$MATRIX1' -e matrix2='$MATRIX2' -v \"$WORK\":/app -w /app golang:1.21-alpine sh -c \"go mod tidy >/dev/null 2>&1 && go build -o app ./main.go && ./app\""
echo ""

OUTPUT=$(docker run --rm -e "matrix1=$MATRIX1" -e "matrix2=$MATRIX2" -v "$WORK":/app -w /app golang:1.21-alpine sh -c "go mod tidy >/dev/null 2>&1 && go build -o app ./main.go && ./app" 2>&1)

echo "Output:"
echo "$OUTPUT"
echo ""
echo "Exit code: $?"
echo ""

# Check if output matches expected pattern
if echo "$OUTPUT" | grep -qE "(\[6 8\]|6 8)" && echo "$OUTPUT" | grep -qE "(\[10 12\]|10 12)"; then
    echo "✅ Test 1 PASSED - Output matches expected pattern"
else
    echo "❌ Test 1 FAILED - Output does not match expected pattern"
    echo "Expected: [6 8] on one line, [10 12] on next line"
fi

# Cleanup
rm -rf "$WORK"
echo ""

# Test 2: Test the exact command format used in SSH fallback
echo "🧪 Test 2: Testing exact SSH fallback command format"
echo "---------------------------------------------------"
WORK2=$(mktemp -d /tmp/go_test2_XXXXXX)
cp /tmp/test_matrix.go "$WORK2/main.go"
cd "$WORK2"
go mod init tmpmod >/dev/null 2>&1 || true

# Build the exact command as it appears in the code
ENV_FLAGS="-e matrix1='$MATRIX1' -e matrix2='$MATRIX2'"
CMD="docker run --rm $ENV_FLAGS -v \"$WORK2\":/app -w /app golang:1.21-alpine sh -c \"go mod tidy >/dev/null 2>&1 && go build -o app ./main.go && ./app\" 2>&1"

echo "Command: $CMD"
echo ""

OUTPUT2=$(eval "$CMD")

echo "Output:"
echo "$OUTPUT2"
echo ""
echo "Exit code: $?"
echo ""

if echo "$OUTPUT2" | grep -qE "(\[6 8\]|6 8)" && echo "$OUTPUT2" | grep -qE "(\[10 12\]|10 12)"; then
    echo "✅ Test 2 PASSED - Output matches expected pattern"
else
    echo "❌ Test 2 FAILED - Output does not match expected pattern"
    echo "Expected: [6 8] on one line, [10 12] on next line"
fi

# Cleanup
rm -rf "$WORK2"
echo ""

echo "🧪 Test Summary"
echo "==============="
echo "If both tests passed, the Docker execution works correctly."
echo "If tests failed, check the error messages above."










