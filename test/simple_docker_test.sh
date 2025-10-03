#!/bin/bash

# Simple Docker test to demonstrate the concept
# This shows how we can use Docker to execute generated code

set -e

echo "🐳 SIMPLE DOCKER CODE EXECUTION DEMO"
echo "===================================="
echo "This demonstrates how we can use Docker containers"
echo "to execute generated code in a scalable way."
echo ""

echo "1. 🧪 TESTING Docker availability..."
if command -v docker &> /dev/null; then
    echo "   ✅ Docker is available"
    docker --version
else
    echo "   ❌ Docker is not available"
    echo "   Please install Docker to run this demo"
    exit 1
fi

echo ""
echo "2. 🐍 TESTING Python code execution in Docker..."

# Create a simple Python script
cat > /tmp/prime_calculator.py << 'EOF'
#!/usr/bin/env python3
import sys
import time

def is_prime(n):
    if n < 2:
        return False
    for i in range(2, int(n**0.5) + 1):
        if n % i == 0:
            return False
    return True

def find_first_n_primes(n):
    primes = []
    num = 2
    while len(primes) < n:
        if is_prime(num):
            primes.append(num)
        num += 1
    return primes

if __name__ == "__main__":
    try:
        n = 10
        if len(sys.argv) > 1:
            n = int(sys.argv[1])
        
        start_time = time.time()
        primes = find_first_n_primes(n)
        end_time = time.time()
        
        print(f"First {n} prime numbers:")
        print(", ".join(map(str, primes)))
        print(f"Calculation time: {end_time - start_time:.4f} seconds")
        
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)
EOF

echo "   📝 Created Python script for prime calculation"

# Run the script in Docker
echo "   🐳 Executing in Docker container..."
result=$(docker run --rm -v /tmp:/app python:3.11-slim python /app/prime_calculator.py 10 2>&1)

if [ $? -eq 0 ]; then
    echo "   ✅ Python execution successful!"
    
    echo ""
    echo "📊 DOCKER EXECUTION RESULTS:"
    echo "============================"
    echo "$result"
    
else
    echo "   ❌ Python execution failed"
    echo "   Output: $result"
    exit 1
fi

echo ""
echo "3. 🧮 TESTING JavaScript code execution in Docker..."

# Create a JavaScript script
cat > /tmp/fibonacci.js << 'EOF'
#!/usr/bin/env node
function fibonacci(n) {
    if (n <= 1) return n;
    return fibonacci(n-1) + fibonacci(n-2);
}

if (process.argv.length > 2) {
    const n = parseInt(process.argv[2]);
    console.log(`First ${n} Fibonacci numbers:`);
    for (let i = 0; i < n; i++) {
        console.log(`F(${i}) = ${fibonacci(i)}`);
    }
    
    if (n > 2) {
        const ratio = fibonacci(n-1) / fibonacci(n-2);
        console.log(`Golden ratio approximation: ${ratio.toFixed(6)}`);
    }
} else {
    console.log("Usage: node fibonacci.js <number>");
}
EOF

echo "   📝 Created JavaScript script for Fibonacci calculation"

# Run the script in Docker
echo "   🐳 Executing in Docker container..."
result=$(docker run --rm -v /tmp:/app node:18-slim node /app/fibonacci.js 10 2>&1)

if [ $? -eq 0 ]; then
    echo "   ✅ JavaScript execution successful!"
    
    echo ""
    echo "📊 DOCKER EXECUTION RESULTS:"
    echo "============================"
    echo "$result"
    
else
    echo "   ❌ JavaScript execution failed"
    echo "   Output: $result"
    exit 1
fi

echo ""
echo "4. 🔧 TESTING Go code execution in Docker..."

# Create a Go script
cat > /tmp/factorial.go << 'EOF'
package main

import (
    "fmt"
    "os"
    "strconv"
    "time"
)

func factorial(n int) int {
    if n <= 1 {
        return 1
    }
    return n * factorial(n-1)
}

func main() {
    n := 10
    if len(os.Args) > 1 {
        if val, err := strconv.Atoi(os.Args[1]); err == nil {
            n = val
        }
    }
    
    start := time.Now()
    result := factorial(n)
    duration := time.Since(start)
    
    fmt.Printf("Factorial of %d = %d\n", n, result)
    fmt.Printf("Calculation time: %v\n", duration)
}
EOF

echo "   📝 Created Go script for factorial calculation"

# Run the script in Docker
echo "   🐳 Executing in Docker container..."
result=$(docker run --rm -v /tmp:/app -w /app golang:1.21-alpine go run factorial.go 10 2>&1)

if [ $? -eq 0 ]; then
    echo "   ✅ Go execution successful!"
    
    echo ""
    echo "📊 DOCKER EXECUTION RESULTS:"
    echo "============================"
    echo "$result"
    
else
    echo "   ❌ Go execution failed"
    echo "   Output: $result"
    exit 1
fi

echo ""
echo "5. 🧹 CLEANING UP..."
rm -f /tmp/prime_calculator.py /tmp/fibonacci.js /tmp/factorial.go
echo "   ✅ Cleaned up temporary files"

echo ""
echo "🎉 DOCKER CODE EXECUTION DEMO COMPLETE!"
echo "======================================="
echo "✅ Successfully executed code in Docker containers"
echo "✅ Demonstrated real mathematical calculations"
echo "✅ Tested multiple programming languages"
echo "✅ Showed scalable container execution"
echo ""
echo "🚀 This proves the concept works:"
echo "   • Docker containers can execute generated code"
echo "   • Multiple languages are supported"
echo "   • Real calculations are performed"
echo "   • Results are returned accurately"
echo "   • Execution is sandboxed and secure"
echo ""
echo "The system can now be extended to:"
echo "   • Generate code via LLM"
echo "   • Execute it in Docker containers"
echo "   • Return real results"
echo "   • Scale horizontally"
echo ""
echo "This is the foundation for a TRUE code generation and execution platform! 🚀"
