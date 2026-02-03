#!/bin/bash
# Quick MCP test - same as the standalone tests we just ran

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "🧪 Quick MCP EcoTree Test (Southampton → Newcastle)"
echo ""

"$SCRIPT_DIR/test_mcp_ecotree_complete.sh" southampton newcastle

