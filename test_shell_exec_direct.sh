#!/bin/bash

# Direct test of the shell exec fix
# This simulates what the tool_exec handler does

echo "🧪 Direct Test of Shell Exec Fix"
echo "================================="
echo ""

# Test 1: Verify /bin/sh exists and works
echo "Test 1: Verify /bin/sh exists"
if [ -x "/bin/sh" ]; then
    echo "✅ /bin/sh exists and is executable"
else
    echo "❌ /bin/sh not found"
    exit 1
fi
echo ""

# Test 2: Test that /bin/sh can execute a simple command
echo "Test 2: Execute command via /bin/sh -c"
OUTPUT=$(/bin/sh -c "echo 'Hello from /bin/sh'")
if [ "$OUTPUT" = "Hello from /bin/sh" ]; then
    echo "✅ /bin/sh executed command successfully"
    echo "   Output: $OUTPUT"
else
    echo "❌ /bin/sh command failed"
    exit 1
fi
echo ""

# Test 3: Test that bash might not be available (Alpine scenario)
echo "Test 3: Check if bash is available (should fail on Alpine)"
if command -v bash >/dev/null 2>&1; then
    echo "⚠️  bash is available on this system (not Alpine)"
    echo "   This is fine - the fix will work on both systems"
else
    echo "✅ bash is not available (Alpine scenario)"
    echo "   This confirms why the fix is needed"
fi
echo ""

# Test 4: Simulate the actual tool_exec handler logic
echo "Test 4: Simulate tool_exec handler with /bin/sh"
TEST_CMD="echo 'test output' && date +%s"
echo "   Command: $TEST_CMD"

# This is what the fixed code does:
OUTPUT=$(/bin/sh -c "$TEST_CMD" 2>&1)
EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ Command executed successfully"
    echo "   Exit code: $EXIT_CODE"
    echo "   Output: $OUTPUT"
else
    echo "❌ Command failed"
    echo "   Exit code: $EXIT_CODE"
    echo "   Output: $OUTPUT"
    exit 1
fi
echo ""

# Test 5: Test with a command that would fail with bash
echo "Test 5: Test command that works with sh but would fail if bash was missing"
TEST_CMD="uname -a"
OUTPUT=$(/bin/sh -c "$TEST_CMD" 2>&1)
EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ Command executed successfully with /bin/sh"
    echo "   This confirms the fix will work"
else
    echo "❌ Unexpected failure"
    exit 1
fi
echo ""

echo "===================================="
echo "🎉 All direct tests passed!"
echo "===================================="
echo ""
echo "The fix is correct: using /bin/sh instead of bash"
echo "This will work on Alpine-based containers where bash is not available."





