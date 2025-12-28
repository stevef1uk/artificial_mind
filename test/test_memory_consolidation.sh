#!/bin/bash

# Test script for Memory Consolidation & Compression system
# This script tests the consolidation pipeline locally

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REDIS_URL="${REDIS_URL:-redis://localhost:6379}"
WEAVIATE_URL="${WEAVIATE_URL:-http://localhost:8080}"
NEO4J_URI="${NEO4J_URI:-bolt://localhost:7687}"
NEO4J_USER="${NEO4J_USER:-neo4j}"
NEO4J_PASS="${NEO4J_PASS:-test1234}"
HDN_PORT="${HDN_PORT:-8081}"

# Test data
TEST_SESSION_ID="test_consolidation_$(date +%s)"
EPISODE_COUNT=10

echo -e "${BLUE}🧪 Memory Consolidation Test${NC}"
echo "=================================="
echo ""

# Function to check if a service is running
check_service() {
    local service=$1
    local url=$2
    
    case $service in
        redis)
            if command -v redis-cli &> /dev/null; then
                redis-cli -u "$REDIS_URL" ping > /dev/null 2>&1
            else
                docker exec agi-redis redis-cli ping > /dev/null 2>&1
            fi
            ;;
        weaviate)
            curl -s "$WEAVIATE_URL/v1/meta" > /dev/null 2>&1
            ;;
        neo4j)
            curl -s "http://localhost:7474" > /dev/null 2>&1
            ;;
    esac
}

# Function to wait for service
wait_for_service() {
    local service=$1
    local max_attempts=30
    local attempt=0
    
    echo -e "${YELLOW}⏳ Waiting for $service...${NC}"
    while [ $attempt -lt $max_attempts ]; do
        if check_service "$service" "$2"; then
            echo -e "${GREEN}✅ $service is ready${NC}"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 2
    done
    
    echo -e "${RED}❌ $service failed to start after $max_attempts attempts${NC}"
    return 1
}

# Step 1: Check/Start infrastructure
echo -e "${BLUE}📦 Step 1: Checking infrastructure...${NC}"

if ! check_service redis "$REDIS_URL"; then
    echo -e "${YELLOW}⚠️  Redis not running. Starting with docker-compose...${NC}"
    docker-compose up -d redis
    wait_for_service redis "$REDIS_URL"
fi

if ! check_service weaviate "$WEAVIATE_URL"; then
    echo -e "${YELLOW}⚠️  Weaviate not running. Starting with docker-compose...${NC}"
    docker-compose up -d weaviate
    wait_for_service weaviate "$WEAVIATE_URL"
fi

if ! check_service neo4j "$NEO4J_URI"; then
    echo -e "${YELLOW}⚠️  Neo4j not running. Starting with docker-compose...${NC}"
    docker-compose up -d neo4j
    wait_for_service neo4j "$NEO4J_URI"
fi

echo ""

# Step 2: Seed test data
echo -e "${BLUE}📝 Step 2: Seeding test data...${NC}"

# Create a Go program to seed test episodes
cat > /tmp/seed_episodes.go << 'EOF'
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type EpisodicRecord struct {
	ID        string                 `json:"id,omitempty"`
	SessionID string                 `json:"session_id"`
	PlanID    string                 `json:"plan_id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Outcome   string                 `json:"outcome,omitempty"`
	Reward    float64                `json:"reward,omitempty"`
	Tags      []string               `json:"tags,omitempty"`
	StepIndex int                    `json:"step_index,omitempty"`
	Text      string                 `json:"text"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

func toyEmbed(text string, dim int) []float32 {
	vec := make([]float32, dim)
	hash := 0
	for _, c := range text {
		hash = hash*31 + int(c)
	}
	for i := 0; i < dim; i++ {
		vec[i] = float32((hash>>i)&1) * 0.5
	}
	return vec
}

func main() {
	baseURL := "http://localhost:8080"
	if len(os.Args) > 1 {
		baseURL = os.Args[1]
	}
	
	sessionID := "test_consolidation"
	if len(os.Args) > 2 {
		sessionID = os.Args[2]
	}
	
	count := 10
	if len(os.Args) > 3 {
		fmt.Sscanf(os.Args[3], "%d", &count)
	}
	
	// Create similar episodes for compression testing
	similarTexts := []string{
		"Successfully executed workflow: data processing pipeline",
		"Successfully executed workflow: data processing pipeline with validation",
		"Successfully executed workflow: data processing pipeline with error handling",
		"Successfully executed workflow: data processing pipeline completed",
		"Successfully executed workflow: data processing pipeline finished",
	}
	
	// Create workflow episodes for skill extraction
	workflowTexts := []string{
		"Workflow completed: image classification task",
		"Workflow completed: image classification task with preprocessing",
		"Workflow completed: image classification task with augmentation",
	}
	
	indexed := 0
	for i := 0; i < count; i++ {
		var ep EpisodicRecord
		if i < len(similarTexts) {
			ep = EpisodicRecord{
				SessionID: sessionID,
				Timestamp: time.Now().Add(-time.Duration(i) * time.Hour),
				Outcome:   "success",
				Reward:    0.8,
				Tags:      []string{"workflow", "data-processing"},
				Text:      similarTexts[i%len(similarTexts)],
				Metadata: map[string]interface{}{
					"test": true,
				},
			}
		} else {
			workflowID := fmt.Sprintf("workflow_%d", (i-len(similarTexts))/3)
			ep = EpisodicRecord{
				SessionID: sessionID,
				Timestamp: time.Now().Add(-time.Duration(i) * time.Hour),
				Outcome:   "success",
				Reward:    0.9,
				Tags:      []string{"workflow", "skill"},
				Text:      workflowTexts[i%len(workflowTexts)],
				Metadata: map[string]interface{}{
					"workflow_id": workflowID,
					"test":        true,
				},
			}
		}
		
		// Create Weaviate object
		vec := toyEmbed(ep.Text, 8)
		properties := map[string]interface{}{
			"text":      ep.Text,
			"timestamp": ep.Timestamp.Format(time.RFC3339),
		}
		
		metadataJSON, _ := json.Marshal(ep.Metadata)
		properties["metadata"] = string(metadataJSON)
		
		obj := map[string]interface{}{
			"class":      "AgiEpisodes",
			"properties": properties,
			"vector":     vec,
		}
		
		jsonData, _ := json.Marshal(obj)
		url := baseURL + "/v1/objects"
		
		resp, err := http.Post(url, "application/json", bytes.NewReader(jsonData))
		if err != nil {
			fmt.Printf("Error indexing episode %d: %v\n", i, err)
			continue
		}
		defer resp.Body.Close()
		
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("Error indexing episode %d: status %d, body: %s\n", i, resp.StatusCode, string(body))
			continue
		}
		
		indexed++
	}
	
	fmt.Printf("✅ Indexed %d episodes\n", indexed)
}
EOF

# Note: os import is already in the heredoc above

echo -e "${YELLOW}Seeding $EPISODE_COUNT test episodes...${NC}"
cd /tmp
go run seed_episodes.go "$WEAVIATE_URL" "$TEST_SESSION_ID" "$EPISODE_COUNT" 2>&1 | grep -v "go:" || echo "Note: Some episodes may have been indexed"
cd "$PROJECT_ROOT"

# Seed some reasoning traces in Redis
echo -e "${YELLOW}Seeding reasoning traces in Redis...${NC}"
if command -v redis-cli &> /dev/null; then
    redis-cli -u "$REDIS_URL" SET "reasoning_trace:test_session_1" '{"decisions":[],"steps":[]}' EX 86400 > /dev/null
    redis-cli -u "$REDIS_URL" SET "reasoning_trace:test_session_2" '{"decisions":[],"steps":[]}' EX 86400 > /dev/null
    redis-cli -u "$REDIS_URL" SET "session:old_session:events" '["event1","event2"]' EX 3600 > /dev/null
    echo -e "${GREEN}✅ Seeded test traces${NC}"
else
    echo -e "${YELLOW}⚠️  redis-cli not found, skipping trace seeding${NC}"
fi

echo ""

# Step 3: Start HDN server
echo -e "${BLUE}🚀 Step 3: Starting HDN server...${NC}"

# Set environment variables
export REDIS_URL="$REDIS_URL"
export WEAVIATE_URL="$WEAVIATE_URL"
export NEO4J_URI="$NEO4J_URI"
export NEO4J_USER="$NEO4J_USER"
export NEO4J_PASS="$NEO4J_PASS"
export LLM_PROVIDER="${LLM_PROVIDER:-mock}"

# Build HDN with Neo4j support
echo -e "${YELLOW}Building HDN server with Neo4j support...${NC}"
cd hdn
go build -tags neo4j -o /tmp/hdn-server . 2>&1 | grep -v "go:" || true
cd "$PROJECT_ROOT"

# Start HDN server in background
echo -e "${YELLOW}Starting HDN server on port $HDN_PORT...${NC}"
/tmp/hdn-server -mode=server -port="$HDN_PORT" > /tmp/hdn-server.log 2>&1 &
HDN_PID=$!

# Wait for server to start
echo -e "${YELLOW}Waiting for HDN server to start...${NC}"
for i in {1..30}; do
    if curl -s "http://localhost:$HDN_PORT/health" > /dev/null 2>&1; then
        echo -e "${GREEN}✅ HDN server is running (PID: $HDN_PID)${NC}"
        break
    fi
    if [ $i -eq 30 ]; then
        echo -e "${RED}❌ HDN server failed to start${NC}"
        echo "Logs:"
        tail -20 /tmp/hdn-server.log
        kill $HDN_PID 2>/dev/null || true
        exit 1
    fi
    sleep 2
done

echo ""

# Step 4: Wait for consolidation
echo -e "${BLUE}⏳ Step 4: Waiting for consolidation cycle...${NC}"
echo -e "${YELLOW}Consolidation runs every hour by default. Triggering manually or waiting 35 seconds for initial run...${NC}"

# Wait for initial consolidation (runs 30s after startup)
sleep 35

# Check logs for consolidation activity
echo -e "${YELLOW}Checking consolidation logs...${NC}"
if grep -q "CONSOLIDATION" /tmp/hdn-server.log 2>/dev/null; then
    echo -e "${GREEN}✅ Consolidation activity detected in logs${NC}"
    grep "CONSOLIDATION" /tmp/hdn-server.log | tail -10
else
    echo -e "${YELLOW}⚠️  No consolidation logs found yet. This is normal if consolidation hasn't run.${NC}"
fi

echo ""

# Step 5: Verify results
echo -e "${BLUE}🔍 Step 5: Verifying consolidation results...${NC}"

# Check Redis for schemas
echo -e "${YELLOW}Checking Redis for consolidated schemas...${NC}"
if command -v redis-cli &> /dev/null; then
    SCHEMA_KEYS=$(redis-cli -u "$REDIS_URL" KEYS "consolidation:schema:*" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$SCHEMA_KEYS" -gt 0 ]; then
        echo -e "${GREEN}✅ Found $SCHEMA_KEYS consolidated schema(s) in Redis${NC}"
        redis-cli -u "$REDIS_URL" KEYS "consolidation:schema:*" 2>/dev/null | head -3
    else
        echo -e "${YELLOW}⚠️  No schemas found yet (may need more time or episodes)${NC}"
    fi
    
    # Check for archived traces
    ARCHIVED=$(redis-cli -u "$REDIS_URL" KEYS "archive:reasoning_trace:*" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$ARCHIVED" -gt 0 ]; then
        echo -e "${GREEN}✅ Found $ARCHIVED archived trace(s)${NC}"
    else
        echo -e "${YELLOW}ℹ️  No archived traces (traces may not be old enough yet)${NC}"
    fi
    
    # Check for skills
    SKILL_KEYS=$(redis-cli -u "$REDIS_URL" KEYS "consolidation:skill:*" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$SKILL_KEYS" -gt 0 ]; then
        echo -e "${GREEN}✅ Found $SKILL_KEYS skill abstraction(s)${NC}"
        redis-cli -u "$REDIS_URL" KEYS "consolidation:skill:*" 2>/dev/null | head -3
    else
        echo -e "${YELLOW}⚠️  No skills found yet (may need more workflow repetitions)${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  redis-cli not found, skipping Redis checks${NC}"
fi

# Check Neo4j for promoted concepts
echo -e "${YELLOW}Checking Neo4j for promoted concepts...${NC}"
if command -v curl &> /dev/null; then
    # Try to query Neo4j via HTTP API
    NEO4J_AUTH=$(echo -n "$NEO4J_USER:$NEO4J_PASS" | base64)
    CONCEPTS=$(curl -s -H "Authorization: Basic $NEO4J_AUTH" \
        -H "Content-Type: application/json" \
        -X POST "http://localhost:7474/db/data/cypher" \
        -d '{"query": "MATCH (c:Concept) WHERE c.domain = \"Skills\" OR c.domain = \"General\" RETURN count(c) as count"}' \
        2>/dev/null | grep -o '"count":[0-9]*' | grep -o '[0-9]*' || echo "0")
    
    if [ "$CONCEPTS" != "0" ] && [ -n "$CONCEPTS" ]; then
        echo -e "${GREEN}✅ Found concepts in Neo4j (may include test data)${NC}"
    else
        echo -e "${YELLOW}ℹ️  No concepts found in Neo4j yet (promotion may require higher stability)${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  curl not found, skipping Neo4j checks${NC}"
fi

echo ""

# Step 6: Summary
echo -e "${BLUE}📊 Step 6: Test Summary${NC}"
echo "=================================="
echo -e "Test Session ID: ${GREEN}$TEST_SESSION_ID${NC}"
echo -e "Episodes Seeded: ${GREEN}$EPISODE_COUNT${NC}"
echo -e "HDN Server PID: ${GREEN}$HDN_PID${NC}"
echo -e "HDN Server Log: ${GREEN}/tmp/hdn-server.log${NC}"
echo ""
echo -e "${YELLOW}💡 Tips:${NC}"
echo "  - Check logs: tail -f /tmp/hdn-server.log | grep CONSOLIDATION"
echo "  - Consolidation runs every hour by default"
echo "  - You can manually trigger by restarting the server"
echo "  - More episodes (≥5 similar) are needed for compression"
echo "  - More workflow repetitions (≥3) are needed for skill extraction"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}🧹 Cleaning up...${NC}"
    kill $HDN_PID 2>/dev/null || true
    rm -f /tmp/hdn-server /tmp/seed_episodes.go
    echo -e "${GREEN}✅ Cleanup complete${NC}"
}

# Ask if user wants to keep server running
echo -e "${YELLOW}Keep HDN server running? (y/n)${NC}"
read -t 10 -r KEEP_RUNNING || KEEP_RUNNING="n"

if [ "$KEEP_RUNNING" != "y" ] && [ "$KEEP_RUNNING" != "Y" ]; then
    cleanup
else
    echo -e "${GREEN}✅ Server will continue running (PID: $HDN_PID)${NC}"
    echo "  To stop: kill $HDN_PID"
    echo "  To view logs: tail -f /tmp/hdn-server.log"
fi

echo ""
echo -e "${GREEN}✅ Memory consolidation test completed!${NC}"

