#!/bin/bash
# Script to initialize inference rules in Redis
# This stores inference rules that the system will use for generating beliefs

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

NAMESPACE=${K8S_NAMESPACE:-agi}
DOMAIN=${DOMAIN:-General}

echo -e "${BLUE}=== Initialize Inference Rules ===${NC}\n"
echo "Domain: $DOMAIN"
echo "Namespace: $NAMESPACE"
echo ""

# Check if Redis is accessible
if ! kubectl exec -n $NAMESPACE deployment/redis -- redis-cli PING > /dev/null 2>&1; then
    echo -e "${RED}❌ Cannot connect to Redis${NC}"
    exit 1
fi

echo -e "${YELLOW}Storing inference rules for domain '$DOMAIN'...${NC}"

# Store inference rules as JSON in Redis list
# Key format: inference_rules:{domain}

RULES_KEY="inference_rules:$DOMAIN"

# Clear existing rules for this domain (optional - comment out if you want to keep existing)
echo "Clearing existing rules for domain '$DOMAIN'..."
kubectl exec -n $NAMESPACE deployment/redis -- redis-cli DEL "$RULES_KEY" > /dev/null 2>&1 || true

# Rule 1: Academic Field Classification
RULE1='{
  "id": "academic_field_classification",
  "name": "Academic Field Classification",
  "pattern": "MATCH (a:Concept) WHERE a.domain = '\''$domain'\'' AND (a.definition CONTAINS '\''study'\'' OR a.definition CONTAINS '\''science'\'' OR a.definition CONTAINS '\''field'\'' OR a.definition CONTAINS '\''discipline'\'') RETURN a LIMIT 1000",
  "conclusion": "ACADEMIC_FIELD",
  "confidence": 0.85,
  "domain": "'$DOMAIN'",
  "description": "Identify academic fields based on definition keywords",
  "examples": ["Concepts with '\''study'\'', '\''science'\'', '\''field'\'', or '\''discipline'\'' in definition are academic fields"]
}'

# Rule 2: Technology Classification
RULE2='{
  "id": "technology_classification",
  "name": "Technology Classification",
  "pattern": "MATCH (a:Concept) WHERE a.domain = '\''$domain'\'' AND (a.definition CONTAINS '\''technology'\'' OR a.definition CONTAINS '\''machine'\'' OR a.definition CONTAINS '\''system'\'' OR a.definition CONTAINS '\''device'\'') RETURN a LIMIT 1000",
  "conclusion": "TECHNOLOGY",
  "confidence": 0.85,
  "domain": "'$DOMAIN'",
  "description": "Identify technology-related concepts",
  "examples": ["Concepts with '\''technology'\'', '\''machine'\'', '\''system'\'', or '\''device'\'' in definition are technologies"]
}'

# Rule 3: Concept Similarity
RULE3='{
  "id": "concept_similarity",
  "name": "Concept Similarity",
  "pattern": "MATCH (a:Concept), (b:Concept) WHERE a.domain = '\''$domain'\'' AND b.domain = '\''$domain'\'' AND a.name <> b.name AND (a.name CONTAINS b.name OR b.name CONTAINS a.name OR a.name =~ b.name OR b.name =~ a.name) RETURN a, b LIMIT 1000",
  "conclusion": "SIMILAR_TO",
  "confidence": 0.7,
  "domain": "'$DOMAIN'",
  "description": "Find similar concepts based on name similarity",
  "examples": ["Computer and Computing are similar concepts"]
}'

# Rule 4: Domain Relationships
RULE4='{
  "id": "domain_relationships",
  "name": "Domain Relationships",
  "pattern": "MATCH (a:Concept), (b:Concept) WHERE a.domain = '\''$domain'\'' AND b.domain = '\''$domain'\'' AND a.name <> b.name AND (a.definition CONTAINS b.name OR b.definition CONTAINS a.name) RETURN a, b LIMIT 1000",
  "conclusion": "RELATED_TO",
  "confidence": 0.6,
  "domain": "'$DOMAIN'",
  "description": "Find concepts that reference each other in their definitions",
  "examples": ["Concepts that mention each other in their definitions are related"]
}'

# Rule 5: Practical Application
RULE5='{
  "id": "practical_application",
  "name": "Practical Application",
  "pattern": "MATCH (a:Concept) WHERE a.domain = '\''$domain'\'' AND (a.definition CONTAINS '\''practice'\'' OR a.definition CONTAINS '\''application'\'' OR a.definition CONTAINS '\''use'\'' OR a.definition CONTAINS '\''implement'\'') RETURN a LIMIT 1000",
  "conclusion": "PRACTICAL_APPLICATION",
  "confidence": 0.75,
  "domain": "'$DOMAIN'",
  "description": "Identify concepts with practical applications",
  "examples": ["Concepts with '\''practice'\'', '\''application'\'', '\''use'\'', or '\''implement'\'' in definition are practical"]
}'

# Store each rule
for i in 1 2 3 4 5; do
    RULE_VAR="RULE$i"
    RULE_CONTENT="${!RULE_VAR}"
    echo "Storing rule $i..."
    echo "$RULE_CONTENT" | kubectl exec -i -n $NAMESPACE deployment/redis -- redis-cli LPUSH "$RULES_KEY" > /dev/null
done

# Verify rules were stored
RULE_COUNT=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LLEN "$RULES_KEY" 2>/dev/null || echo "0")
echo ""
if [ "$RULE_COUNT" -gt 0 ]; then
    echo -e "${GREEN}✅ Successfully stored $RULE_COUNT inference rules for domain '$DOMAIN'${NC}"
    echo ""
    echo "Rules stored in Redis key: $RULES_KEY"
    echo ""
    echo "To verify:"
    echo "  kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LLEN \"$RULES_KEY\""
    echo ""
    echo "To view rules:"
    echo "  kubectl exec -n $NAMESPACE deployment/redis -- redis-cli LRANGE \"$RULES_KEY\" 0 -1"
else
    echo -e "${RED}❌ Failed to store rules${NC}"
    exit 1
fi

