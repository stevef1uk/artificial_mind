#!/bin/bash

# Script to clean up duplicate behavior loop goals from the Goal Manager
# Usage: ./cleanup-duplicate-goals.sh [GOAL_MANAGER_URL] [AGENT_ID]

GOAL_MGR_URL="${1:-http://localhost:8090}"
AGENT_ID="${2:-agent_1}"

echo "🧹 Starting duplicate goal cleanup..."
echo "Goal Manager URL: $GOAL_MGR_URL"
echo "Agent ID: $AGENT_ID"

# Fetch all active goals
GOALS=$(curl -s "$GOAL_MGR_URL/goals/$AGENT_ID/active")

if [ -z "$GOALS" ] || [ "$GOALS" == "[]" ]; then
    echo "✅ No active goals found. Nothing to clean up."
    exit 0
fi

# Parse goals and deduplicate by normalized description
# Keep the most recent, delete older ones
echo "$GOALS" | jq -r '.[] | "\(.id)|\(.description)|\(.created_at)"' | sort | while read -r line; do
    IFS='|' read -r id desc created_at <<< "$line"
    
    # Normalize description (lowercase, trim whitespace)
    normalized=$(echo "$desc" | tr '[:upper:]' '[:lower:]' | xargs)
    
    # Check if we've seen this normalized description before
    # If yes, it's a duplicate - delete it
    if grep -q "^$normalized$" /tmp/seen_descriptions 2>/dev/null; then
        echo "🗑️  Deleting duplicate goal: $id (desc: $desc)"
        curl -s -X DELETE "$GOAL_MGR_URL/goal/$id" > /dev/null
    else
        # Store this normalized description to track duplicates
        echo "$normalized" >> /tmp/seen_descriptions
    fi
done

# Cleanup temp file
rm -f /tmp/seen_descriptions

echo "✅ Duplicate goal cleanup complete!"
echo ""
echo "📊 Final active goals:"
curl -s "$GOAL_MGR_URL/goals/$AGENT_ID/active" | jq -r '.[] | "\(.id): \(.description)"'
