#!/bin/bash

# Reset Kubernetes Databases to Clean State
# This script clears all data from Redis, Neo4j, and Weaviate

set -e

NAMESPACE="agi"

echo "🗑️  Resetting Kubernetes databases to clean state..."
echo

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 1. Clear Redis
echo -e "${YELLOW}1. Clearing Redis...${NC}"
REDIS_POD=$(kubectl get pods -n $NAMESPACE -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$REDIS_POD" ]; then
    kubectl exec -n $NAMESPACE "$REDIS_POD" -- redis-cli FLUSHALL > /dev/null 2>&1
    echo -e "${GREEN}✅ Redis cleared${NC}"
else
    echo -e "${RED}❌ Redis pod not found${NC}"
fi
echo

# 2. Clear Neo4j
echo -e "${YELLOW}2. Clearing Neo4j...${NC}"
NEO4J_POD=$(kubectl get pods -n $NAMESPACE -l app=neo4j -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$NEO4J_POD" ]; then
    # Delete all nodes and relationships
    kubectl exec -n $NAMESPACE "$NEO4J_POD" -- cypher-shell -u neo4j -p test1234 "MATCH (n) DETACH DELETE n" > /dev/null 2>&1 || \
    kubectl exec -n $NAMESPACE "$NEO4J_POD" -- sh -c 'echo "MATCH (n) DETACH DELETE n;" | cypher-shell -u neo4j -p test1234' > /dev/null 2>&1
    echo -e "${GREEN}✅ Neo4j cleared${NC}"
else
    echo -e "${RED}❌ Neo4j pod not found${NC}"
fi
echo

# 3. Clear Weaviate
echo -e "${YELLOW}3. Clearing Weaviate...${NC}"
WEAVIATE_POD=$(kubectl get pods -n $NAMESPACE -l app=weaviate -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$WEAVIATE_POD" ]; then
    # Use port-forward for reliable API access
    echo "  Setting up port-forward to Weaviate..."
    kubectl port-forward -n $NAMESPACE "$WEAVIATE_POD" 18080:8080 > /dev/null 2>&1 &
    PORT_FORWARD_PID=$!
    sleep 2
    
    # Cleanup function to kill port-forward
    cleanup_portforward() {
        kill $PORT_FORWARD_PID 2>/dev/null || true
        wait $PORT_FORWARD_PID 2>/dev/null || true
    }
    trap cleanup_portforward EXIT
    
    WEAVIATE_URL="http://localhost:18080"
    
    # Check if Weaviate is reachable
    if ! curl -s -f "$WEAVIATE_URL/v1/meta" >/dev/null 2>&1; then
        echo -e "${RED}❌ Cannot connect to Weaviate${NC}"
        cleanup_portforward
        trap - EXIT
    else
        # Get all schema classes
        SCHEMA_RESPONSE=$(curl -s "$WEAVIATE_URL/v1/schema" 2>/dev/null || echo '{"classes":[]}')
        
        # Extract class names
        if command -v jq >/dev/null 2>&1; then
            CLASSES=$(echo "$SCHEMA_RESPONSE" | jq -r '.classes[].class' 2>/dev/null || true)
        else
            # Fallback: use grep/sed if jq not available
            CLASSES=$(echo "$SCHEMA_RESPONSE" | grep -o '"class":"[^"]*"' | sed 's/"class":"\([^"]*\)"/\1/' || true)
        fi
        
        if [ -n "$CLASSES" ] && [ "$CLASSES" != "" ]; then
            DELETED_COUNT=0
            for class in $CLASSES; do
                echo "  Deleting class: $class..."
                
                # Method 1: Delete all objects first using objects endpoint with where filter
                # This deletes all objects in the class
                echo "    Deleting all objects..."
                OBJECTS_DELETE=$(curl -s -w "\n%{http_code}" -X DELETE "$WEAVIATE_URL/v1/objects?class=$class" 2>/dev/null || echo "")
                OBJECTS_HTTP=$(echo "$OBJECTS_DELETE" | tail -1)
                
                # Wait for deletion to complete
                sleep 2
                
                # Method 2: Delete the schema class (should work now that objects are gone)
                echo "    Deleting schema..."
                DELETE_RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE "$WEAVIATE_URL/v1/schema/$class" 2>/dev/null || echo "")
                HTTP_CODE=$(echo "$DELETE_RESPONSE" | tail -1)
                
                # Verify deletion
                sleep 1
                CHECK_SCHEMA=$(curl -s "$WEAVIATE_URL/v1/schema" 2>/dev/null || echo '{}')
                if echo "$CHECK_SCHEMA" | grep -q "\"class\":\"$class\""; then
                    echo "    ⚠️  Schema still exists (HTTP $HTTP_CODE), trying batch delete..."
                    # Method 3: Try batch delete as fallback
                    BATCH_DELETE=$(curl -s -w "\n%{http_code}" -X POST "$WEAVIATE_URL/v1/batch/objects" \
                        -H "Content-Type: application/json" \
                        -d "{\"match\":{\"class\":\"$class\"},\"output\":\"minimal\"}" 2>/dev/null || echo "")
                    BATCH_HTTP=$(echo "$BATCH_DELETE" | tail -1)
                    sleep 2
                    # Try schema delete again
                    curl -s -X DELETE "$WEAVIATE_URL/v1/schema/$class" >/dev/null 2>&1 || true
                fi
                
                # Final check
                sleep 1
                FINAL_CHECK=$(curl -s "$WEAVIATE_URL/v1/schema" 2>/dev/null || echo '{}')
                if ! echo "$FINAL_CHECK" | grep -q "\"class\":\"$class\""; then
                    echo "    ✅ Class deleted: $class"
                    DELETED_COUNT=$((DELETED_COUNT + 1))
                else
                    echo "    ⚠️  Class may still exist: $class"
                fi
            done
            
            # Final verification - check if any classes remain
            sleep 2
            FINAL_SCHEMA=$(curl -s "$WEAVIATE_URL/v1/schema" 2>/dev/null || echo '{"classes":[]}')
            if command -v jq >/dev/null 2>&1; then
                REMAINING=$(echo "$FINAL_SCHEMA" | jq -r '.classes | length' 2>/dev/null || echo "0")
            else
                REMAINING=$(echo "$FINAL_SCHEMA" | grep -o '"class":"[^"]*"' | wc -l || echo "0")
            fi
            
            if [ "$REMAINING" = "0" ] || [ -z "$REMAINING" ]; then
                echo -e "${GREEN}✅ Weaviate cleared: $DELETED_COUNT classes deleted${NC}"
            else
                echo -e "${YELLOW}⚠️  Weaviate partially cleared: $DELETED_COUNT classes deleted, $REMAINING remaining${NC}"
                echo "  You may need to delete the PVC for a complete reset"
            fi
        else
            echo -e "${GREEN}✅ Weaviate already empty${NC}"
        fi
        
        cleanup_portforward
        trap - EXIT
    fi
else
    echo -e "${RED}❌ Weaviate pod not found${NC}"
fi
echo

echo -e "${GREEN}✅ All databases reset to clean state!${NC}"
echo
echo "Note: If you want a complete reset (including deleting PVCs), run:"
echo "  kubectl scale deployment redis neo4j weaviate -n agi --replicas=0"
echo "  kubectl delete pvc redis-data neo4j-data weaviate-data -n agi"
echo "  kubectl apply -f k3s/pvc-redis.yaml k3s/pvc-neo4j.yaml k3s/pvc-weaviate.yaml"
echo "  kubectl scale deployment redis neo4j weaviate -n agi --replicas=1"

