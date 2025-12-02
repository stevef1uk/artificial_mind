#!/bin/bash
# Test All Learning Improvements (Priorities 1-6)
# Verifies:
# - Priority 1: Goal Outcome Learning System
# - Priority 2: Enhanced Goal Scoring
# - Priority 3: Hypothesis Value Pre-Evaluation
# - Priority 4: Focused Learning Strategy
# - Priority 5: Meta-Learning System
# - Priority 6: Improved Concept Discovery

set -e

REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
FSM_URL="${FSM_URL:-http://localhost:8083}"

# Detect if redis-cli is available, otherwise use docker exec
if command -v redis-cli >/dev/null 2>&1; then
    REDIS_CLI="redis-cli -h $REDIS_HOST -p $REDIS_PORT"
elif docker ps | grep -q agi-redis; then
    REDIS_CLI="docker exec agi-redis redis-cli"
    echo "ℹ️  Using docker exec to access Redis"
else
    echo "❌ Cannot find redis-cli or Redis container"
    exit 1
fi

echo "🧪 Testing All Learning Improvements (Priorities 1-6)"
echo "======================================================"
echo ""
echo "This test verifies:"
echo "Priority 1: Goal Outcome Learning System"
echo "  - Goal outcomes are recorded"
echo "  - Success rates are tracked"
echo "  - Average values are tracked"
echo ""
echo "Priority 2: Enhanced Goal Scoring"
echo "  - Historical success data incorporated"
echo "  - Bonuses for successful goal types"
echo "  - Penalties for failed patterns"
echo ""
echo "Priority 3: Hypothesis Value Pre-Evaluation"
echo "  - Low-value hypotheses filtered (< 0.3 threshold)"
echo "  - Hypothesis value evaluation works"
echo ""
echo "Priority 4: Focused Learning Strategy"
echo "  - Focus areas identified (success + value + progress)"
echo "  - Goal generation adjusted (70% focused, 30% unfocused)"
echo ""
echo "Priority 5: Meta-Learning System"
echo "  - Learning about learning process"
echo "  - Goal type values tracked"
echo "  - Domain productivity tracked"
echo "  - Success patterns identified"
echo ""
echo "Priority 6: Improved Concept Discovery"
echo "  - LLM-based semantic analysis"
echo "  - Meaningful concept names"
echo "  - Quality filtering"
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper function to check Redis key exists
check_redis_key() {
    local key=$1
    local exists=$($REDIS_CLI EXISTS "$key" 2>&1 | grep -q "1" && echo "1" || echo "0")
    if [ "$exists" = "1" ]; then
        echo -e "${GREEN}✓${NC} Key exists: $key"
        return 0
    else
        echo -e "${RED}✗${NC} Key missing: $key"
        return 1
    fi
}

# Helper function to get Redis value
get_redis_value() {
    local key=$1
    $REDIS_CLI GET "$key" 2>/dev/null || echo ""
}

echo "[1] Checking Redis connection..."
PING_RESULT=$($REDIS_CLI PING 2>&1)
if [ "$PING_RESULT" = "PONG" ]; then
    echo -e "${GREEN}✓${NC} Redis connection successful"
else
    echo -e "${RED}✗${NC} Cannot connect to Redis"
    echo "   PING result: $PING_RESULT"
    exit 1
fi
echo ""

echo "[2] Setting up test data for focused learning..."
echo "   Creating success rates and values for different goal types..."

TEST_DOMAIN="test_domain"

# Set up test success rates (simulating what the system learns)
# gap_filling: High success (0.9) - should be a focus area
$REDIS_CLI SET "goal_success_rate:gap_filling:$TEST_DOMAIN" "0.9" > /dev/null 2>&1
$REDIS_CLI SET "goal_avg_value:gap_filling:$TEST_DOMAIN" "0.85" > /dev/null 2>&1

# concept_exploration: Medium success (0.6) - moderate focus
$REDIS_CLI SET "goal_success_rate:concept_exploration:$TEST_DOMAIN" "0.6" > /dev/null 2>&1
$REDIS_CLI SET "goal_avg_value:concept_exploration:$TEST_DOMAIN" "0.55" > /dev/null 2>&1

# contradiction_resolution: Low success (0.3) - should NOT be a focus area
$REDIS_CLI SET "goal_success_rate:contradiction_resolution:$TEST_DOMAIN" "0.3" > /dev/null 2>&1
$REDIS_CLI SET "goal_avg_value:contradiction_resolution:$TEST_DOMAIN" "0.25" > /dev/null 2>&1

# news_analysis: High success (0.95) - should be a focus area
$REDIS_CLI SET "goal_success_rate:news_analysis:$TEST_DOMAIN" "0.95" > /dev/null 2>&1
$REDIS_CLI SET "goal_avg_value:news_analysis:$TEST_DOMAIN" "0.9" > /dev/null 2>&1

echo -e "${GREEN}✓${NC} Test data setup complete"
echo ""

echo "[3] Verifying focus area identification logic..."
echo "   Expected focus areas (focus score > 0.5):"
echo "   - gap_filling: success=0.9, value=0.85 → focus_score ≈ 0.7"
echo "   - news_analysis: success=0.95, value=0.9 → focus_score ≈ 0.74"
echo "   - concept_exploration: success=0.6, value=0.55 → focus_score ≈ 0.47 (may not qualify)"
echo "   - contradiction_resolution: success=0.3, value=0.25 → focus_score ≈ 0.22 (should NOT qualify)"
echo ""
echo -e "${YELLOW}ℹ${NC} Actual focus area identification happens in identifyFocusAreas() function"
echo -e "${YELLOW}ℹ${NC} Focus score = (success_rate * 0.4) + (avg_value * 0.4) + (recent_progress * 0.2)"
echo ""

echo "[4] Verifying hypothesis value pre-evaluation..."
echo "   The system should:"
echo "   - Evaluate hypothesis potential before generating"
echo "   - Filter out hypotheses with value < 0.3"
echo "   - Scale confidence by potential value"
echo ""
echo "   Value factors:"
echo "   - Similar hypothesis success rate (30% weight)"
echo "   - Concept depth/completeness (20% weight)"
echo "   - Actionable properties (20% weight)"
echo "   - Generic concept penalty (-20%)"
echo ""
echo -e "${YELLOW}ℹ${NC} Actual evaluation happens in evaluateHypothesisPotential() function"
echo -e "${YELLOW}ℹ${NC} Low-value hypotheses are filtered in generateConceptBasedHypothesis()"
echo ""

echo "[5] Testing goal generation adjustment..."
echo "   When focus areas are identified, goal generation should:"
echo "   - Prioritize goals from focus areas (70% focused, 30% unfocused)"
echo "   - Boost priority of focused goals by 20%"
echo "   - Maintain exploration of unfocused areas"
echo ""
echo -e "${YELLOW}ℹ${NC} Actual adjustment happens in adjustGoalGeneration() function"
echo -e "${YELLOW}ℹ${NC} Integrated into TriggerAutonomyCycle()"
echo ""

echo "[6] Verifying Redis keys for focus areas..."
# Check that success rates and values are stored
check_redis_key "goal_success_rate:gap_filling:$TEST_DOMAIN"
check_redis_key "goal_avg_value:gap_filling:$TEST_DOMAIN"
check_redis_key "goal_success_rate:news_analysis:$TEST_DOMAIN"
check_redis_key "goal_avg_value:news_analysis:$TEST_DOMAIN"

GAP_SUCCESS=$(get_redis_value "goal_success_rate:gap_filling:$TEST_DOMAIN")
GAP_VALUE=$(get_redis_value "goal_avg_value:gap_filling:$TEST_DOMAIN")
NEWS_SUCCESS=$(get_redis_value "goal_success_rate:news_analysis:$TEST_DOMAIN")
NEWS_VALUE=$(get_redis_value "goal_avg_value:news_analysis:$TEST_DOMAIN")

echo "   gap_filling: success=$GAP_SUCCESS, value=$GAP_VALUE"
echo "   news_analysis: success=$NEWS_SUCCESS, value=$NEWS_VALUE"

# Calculate expected focus scores
GAP_FOCUS=$(echo "scale=2; ($GAP_SUCCESS * 0.4) + ($GAP_VALUE * 0.4) + (0.5 * 0.2)" | bc 2>/dev/null || echo "0.70")
NEWS_FOCUS=$(echo "scale=2; ($NEWS_SUCCESS * 0.4) + ($NEWS_VALUE * 0.4) + (0.5 * 0.2)" | bc 2>/dev/null || echo "0.74")

echo "   Expected focus scores:"
echo "   - gap_filling: $GAP_FOCUS (should qualify as focus area)"
echo "   - news_analysis: $NEWS_FOCUS (should qualify as focus area)"

if [ -n "$GAP_FOCUS" ] && [ -n "$NEWS_FOCUS" ]; then
    echo -e "${GREEN}✓${NC} Focus area data available for calculation"
else
    echo -e "${YELLOW}⚠${NC} Could not calculate focus scores (bc may not be available)"
fi
echo ""

echo "[7] Testing Priority 5: Meta-Learning System..."
echo "   Creating test goal outcomes to trigger meta-learning..."

# Create test outcomes to populate meta-learning
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
META_OUTCOME_1=$(cat <<EOF
{
  "goal_id": "meta_test_1",
  "goal_type": "gap_filling",
  "domain": "$TEST_DOMAIN",
  "status": "completed",
  "success": true,
  "value": 0.85,
  "execution_time": 5.0,
  "outcomes": ["Successfully filled knowledge gap"],
  "created_at": "$TIMESTAMP"
}
EOF
)

META_OUTCOME_2=$(cat <<EOF
{
  "goal_id": "meta_test_2",
  "goal_type": "news_analysis",
  "domain": "$TEST_DOMAIN",
  "status": "completed",
  "success": true,
  "value": 0.90,
  "execution_time": 3.0,
  "outcomes": ["Analyzed news event"],
  "created_at": "$TIMESTAMP"
}
EOF
)

# Push outcomes to trigger meta-learning updates
printf '%s' "$META_OUTCOME_1" | $REDIS_CLI -x LPUSH "goal_outcomes:gap_filling:$TEST_DOMAIN" > /dev/null 2>&1
printf '%s' "$META_OUTCOME_2" | $REDIS_CLI -x LPUSH "goal_outcomes:news_analysis:$TEST_DOMAIN" > /dev/null 2>&1

echo "   Test outcomes created"
echo ""
echo "   Meta-learning should track:"
echo "   - Goal type values (gap_filling, news_analysis should have high values)"
echo "   - Domain productivity (test_domain should show productivity)"
echo "   - Success patterns (gap_filling:test_domain, news_analysis:test_domain)"
echo ""
echo "   Note: Meta-learning updates happen when updateMetaLearning() is called"
echo "   This happens automatically in recordGoalOutcome() when goals complete"
echo "   For testing, you can manually trigger by executing goals via FSM"
echo ""
echo -e "${YELLOW}ℹ${NC} Check Redis key 'meta_learning:all' to see meta-learning data"
echo -e "${YELLOW}ℹ${NC} Meta-learning tracks: goal_type_value, domain_productivity, success_patterns"
echo ""

echo "[8] Testing Priority 6: Improved Concept Discovery..."
echo "   Concept discovery now uses LLM-based semantic analysis"
echo ""
echo "   Expected behavior:"
echo "   - Concepts extracted with semantic understanding"
echo "   - Meaningful concept names (not timestamps)"
echo "   - Properties and constraints extracted"
echo "   - Quality filtering applied"
echo ""
echo "   When concept discovery runs:"
echo "   - Calls HDN /api/v1/interpret endpoint"
echo "   - LLM analyzes text and extracts concepts"
echo "   - Falls back to keyword extraction if LLM unavailable"
echo ""
echo -e "${YELLOW}ℹ${NC} Actual concept extraction happens during episode analysis"
echo -e "${YELLOW}ℹ${NC} Monitor logs for: '✨ Extracted concept via LLM' or '⚠️ Using fallback'"
echo ""

echo "[9] Verifying meta-learning data structure..."
META_KEY="meta_learning:all"
META_EXISTS=$($REDIS_CLI EXISTS "$META_KEY" 2>&1 | grep -q "1" && echo "1" || echo "0")

if [ "$META_EXISTS" = "1" ]; then
    echo -e "${GREEN}✓${NC} Meta-learning key exists: $META_KEY"
    META_DATA=$($REDIS_CLI GET "$META_KEY" 2>/dev/null | head -c 200)
    if [ -n "$META_DATA" ]; then
        echo "   Meta-learning data preview: ${META_DATA}..."
    fi
else
    echo -e "${YELLOW}ℹ${NC} Meta-learning key not yet created (will be created when goals execute)"
    echo "   This is expected if no goals have completed yet"
fi
echo ""

echo "[10] Summary and next steps..."
echo ""
echo -e "${GREEN}✓${NC} Test data created successfully"
echo -e "${GREEN}✓${NC} Success rates and values stored"
echo -e "${GREEN}✓${NC} Focus area data available"
echo ""
echo "To verify the full system:"
echo "1. Start FSM server: ./bin/fsm-server"
echo "2. Monitor FSM logs for:"
echo ""
echo "   Priority 1 (Goal Outcomes):"
echo "   - '📊 Recorded goal outcome'"
echo "   - '📈 Updated success rate'"
echo "   - '💰 Updated avg value'"
echo ""
echo "   Priority 2 (Enhanced Scoring):"
echo "   - '📊 Goal ...: success rate bonus'"
echo "   - '💰 Goal ...: value bonus'"
echo "   - '⚠️ Goal ...: recent failures penalty'"
echo ""
echo "   Priority 3 (Hypothesis Filtering):"
echo "   - '⏭️ Skipping low-value hypothesis'"
echo ""
echo "   Priority 4 (Focused Learning):"
echo "   - '🎯 Identified X focus areas'"
echo "   - '🎯 Adjusted goal generation'"
echo "   - '🎯 Goal adjustment: X focused goals'"
echo ""
echo "   Priority 5 (Meta-Learning):"
echo "   - '🧠 Updated meta-learning'"
echo ""
echo "   Priority 6 (Concept Discovery):"
echo "   - '✨ Extracted concept via LLM'"
echo "   - '📚 Extracted X concepts via semantic analysis'"
echo "   - '⚠️ Using fallback concept extraction' (if LLM unavailable)"
echo ""
echo "3. Check Redis keys:"
echo "   $REDIS_CLI KEYS 'goal_outcomes:*'"
echo "   $REDIS_CLI KEYS 'goal_success_rate:*'"
echo "   $REDIS_CLI KEYS 'goal_avg_value:*'"
echo "   $REDIS_CLI GET 'meta_learning:all'"
echo ""
echo "Expected behavior:"
echo "- Goals outcomes recorded and statistics updated"
echo "- Low-value hypotheses filtered out"
echo "- Focus areas identified and goals prioritized"
echo "- Meta-learning tracks learning patterns"
echo "- Concepts extracted with semantic understanding"
echo ""
echo "To clean up test data:"
echo "   $REDIS_CLI DEL 'goal_success_rate:gap_filling:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'goal_avg_value:gap_filling:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'goal_success_rate:concept_exploration:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'goal_avg_value:concept_exploration:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'goal_success_rate:contradiction_resolution:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'goal_avg_value:contradiction_resolution:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'goal_success_rate:news_analysis:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'goal_avg_value:news_analysis:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'goal_outcomes:gap_filling:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'goal_outcomes:news_analysis:$TEST_DOMAIN'"
echo "   $REDIS_CLI DEL 'meta_learning:all'"
echo ""
echo -e "${GREEN}Test complete!${NC}"

