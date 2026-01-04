#!/bin/bash

# Quick test script to simulate SSH output filtering
# This tests the actual output format that would come from SSH execution

echo "🧪 Testing SSH Output Filtering"
echo "================================"
echo

# Test case 1: SSH warning only (the problematic case)
echo "Test 1: SSH warning only"
test_output="Warning: Permanently added '192.168.1.63' (ED25519) to the list of known hosts."
echo "Input: $test_output"
filtered=$(echo "$test_output" | grep -v "Warning: Permanently added" | grep -v "known hosts" | grep -v "^$")
if [ -z "$filtered" ]; then
    echo "✅ PASS: SSH message filtered out (output is empty as expected)"
else
    echo "❌ FAIL: SSH message not filtered: $filtered"
fi
echo

# Test case 2: SSH warning + actual output
echo "Test 2: SSH warning with prime numbers"
test_output="Warning: Permanently added '192.168.1.63' (ED25519) to the list of known hosts.
[2, 3, 5, 7, 11, 13, 17, 19, 23, 29]"
echo "Input:"
echo "$test_output"
filtered=$(echo "$test_output" | grep -v "Warning: Permanently added" | grep -v "known hosts")
expected="[2, 3, 5, 7, 11, 13, 17, 19, 23, 29]"
if [ "$filtered" = "$expected" ]; then
    echo "✅ PASS: SSH message filtered, output preserved"
else
    echo "❌ FAIL: Expected: $expected"
    echo "        Got:      $filtered"
fi
echo

# Test case 3: Go matrix output
echo "Test 3: SSH warning with Go matrix output"
test_output="Warning: Permanently added '192.168.1.63' (ED25519) to the list of known hosts.
[6 8]
[10 12]"
echo "Input:"
echo "$test_output"
filtered=$(echo "$test_output" | grep -v "Warning: Permanently added" | grep -v "known hosts")
expected="[6 8]
[10 12]"
if [ "$filtered" = "$expected" ]; then
    echo "✅ PASS: SSH message filtered, Go matrix output preserved"
else
    echo "❌ FAIL: Expected:"
    echo "$expected"
    echo "        Got:"
    echo "$filtered"
fi
echo

echo "✅ Quick test complete!"
echo
echo "Note: This is a simplified test. The actual Go code has more sophisticated"
echo "filtering that handles edge cases better."










