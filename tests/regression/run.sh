#!/bin/bash
set -e

# Change to this directory
cd "$(dirname "$0")"

echo "🚀 Starting Regression Infrastructure..."
echo "📂 Working directory: $(pwd)"

# Cleanup previous runs
# Cleanup previous runs and force image rebuild
docker-compose -f docker-compose.test.yml down -v --remove-orphans || true
docker rmi regression-hdn regression-fsm regression-test-runner regression-scraper regression-mock-llm 2>/dev/null || true

# Run tests
echo "🔨 Building and Running Tests..."
if docker-compose -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from test-runner; then
    echo "✅ Tests Passed!"
    docker-compose -f docker-compose.test.yml down -v
    exit 0
else
    echo "❌ Tests Failed!"
    echo "📜 Test Runner Logs (FAILURE REASON):"
    docker-compose -f docker-compose.test.yml logs test-runner
    echo "📜 All Service Logs:"
    # docker-compose -f docker-compose.test.yml logs
    docker-compose -f docker-compose.test.yml down -v
    exit 1
fi
