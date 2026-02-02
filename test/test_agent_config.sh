#!/bin/bash
# Test agent configuration loading

set -e

echo "🧪 Testing Agent Configuration Loading..."

# Test 1: Check if config file exists
echo ""
echo "Test 1: Check config file exists"
if [ -f "config/agents.yaml" ]; then
    echo "✅ config/agents.yaml exists"
    echo "   File size: $(wc -l < config/agents.yaml) lines"
else
    echo "❌ config/agents.yaml not found"
    exit 1
fi

# Test 2: Validate YAML syntax
echo ""
echo "Test 2: Validate YAML syntax"
python3 -c "import yaml; yaml.safe_load(open('config/agents.yaml'))" && echo "✅ YAML syntax is valid" || echo "❌ YAML syntax error"

# Test 3: Check agent count
echo ""
echo "Test 3: Count agents in config"
AGENT_COUNT=$(python3 -c "import yaml; data=yaml.safe_load(open('config/agents.yaml')); print(len(data.get('agents', [])))")
echo "   Found $AGENT_COUNT agent(s) in config"

# Test 4: List agent IDs
echo ""
echo "Test 4: Agent IDs in config"
python3 -c "import yaml; data=yaml.safe_load(open('config/agents.yaml')); [print(f'  - {a[\"id\"]}') for a in data.get('agents', [])]"

echo ""
echo "✅ Configuration tests completed"

