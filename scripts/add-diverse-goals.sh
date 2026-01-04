#!/bin/bash

# Add diverse exploration goals to kickstart the system
# Usage: ./add-diverse-goals.sh [GOAL_MANAGER_URL]

GOAL_MGR_URL="${1:-http://localhost:8090}"

echo "🎯 Adding diverse exploration goals to Goal Manager..."
echo "Goal Manager URL: $GOAL_MGR_URL"
echo ""

# Knowledge exploration goals
curl -X POST "$GOAL_MGR_URL/goal" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "[ACTIVE-LEARNING] query_knowledge_base: Explore concepts related to artificial intelligence",
    "priority": "medium",
    "context": {"source": "bootstrap", "domain": "Artificial Intelligence", "routing_hint": "knowledge_query"}
  }'
echo "✅ Added AI exploration goal"

curl -X POST "$GOAL_MGR_URL/goal" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "[ACTIVE-LEARNING] query_knowledge_base: Explore concepts related to machine learning algorithms",
    "priority": "medium",
    "context": {"source": "bootstrap", "domain": "Machine Learning", "routing_hint": "knowledge_query"}
  }'
echo "✅ Added ML exploration goal"

curl -X POST "$GOAL_MGR_URL/goal" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "[ACTIVE-LEARNING] query_knowledge_base: Explore concepts related to neural networks",
    "priority": "medium",
    "context": {"source": "bootstrap", "domain": "Deep Learning", "routing_hint": "knowledge_query"}
  }'
echo "✅ Added neural networks exploration goal"

# Tool-based goals
curl -X POST "$GOAL_MGR_URL/goal" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "Use tool_http_get to fetch latest AI research papers from ArXiv RSS feed",
    "priority": "medium",
    "context": {"source": "bootstrap", "domain": "Research", "routing_hint": "tool_call"}
  }'
echo "✅ Added ArXiv fetching goal"

curl -X POST "$GOAL_MGR_URL/goal" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "Use tool_html_scraper to extract information from Wikipedia page about cognitive architectures",
    "priority": "medium",
    "context": {"source": "bootstrap", "domain": "Cognitive Science", "routing_hint": "tool_call"}
  }'
echo "✅ Added Wikipedia scraping goal"

# Reasoning/analysis goals
curl -X POST "$GOAL_MGR_URL/goal" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "Analyze patterns in recent system behavior and identify optimization opportunities",
    "priority": "medium",
    "context": {"source": "bootstrap", "domain": "System Optimization", "routing_hint": "reasoning"}
  }'
echo "✅ Added system analysis goal"

curl -X POST "$GOAL_MGR_URL/goal" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent_1",
    "description": "Explore the relationship between learning rate and knowledge consolidation in the knowledge base",
    "priority": "medium",
    "context": {"source": "bootstrap", "domain": "Meta-Learning", "routing_hint": "reasoning"}
  }'
echo "✅ Added meta-learning goal"

echo ""
echo "🎉 Added 7 diverse goals!"
echo ""
echo "📊 Current active goals:"
curl -s "$GOAL_MGR_URL/goals/agent_1/active" | jq -r '.[] | "\(.id): \(.description[:80])"'
