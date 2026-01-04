# News Articles Not Showing in UI - Analysis

## Problem
Very few new articles are showing in the UI despite the news ingestor running hourly.

## Root Causes

### 1. Duplicate Filtering Too Aggressive
- **24-hour TTL** on duplicate detection
- BBC articles stay on front page for **days**, not hours
- After first 24 hours, all articles are marked as duplicates
- Result: Only 1-2 new articles per hour pass duplicate check (out of 25 discovered)

**Current behavior:**
- Hour 1: 25 articles discovered, 24 new, 1 duplicate → 24 articles processed
- Hour 2-24: 25 articles discovered, all duplicates → 0 articles processed
- Hour 25+: Same articles still on BBC, all marked as duplicates → 0 articles processed

### 2. FSM Not Running ⚠️ CRITICAL
- **FSM deployment doesn't exist** - `kubectl get deployment fsm-server-rpi58` returns NotFound
- FSM server is responsible for receiving NATS news events and storing them in Weaviate
- **Without FSM, news events published to NATS are lost** - they're never stored in Weaviate
- Articles won't appear in UI even if ingestor publishes them successfully
- This is the **primary blocker** - even if duplicates are fixed, articles won't show without FSM

### 3. LLM Classification Filtering
- Articles are classified by LLM as "alert", "relation", or "skip"
- Many articles may be filtered out by LLM (low confidence, not newsworthy, etc.)
- Only articles that pass LLM classification get published to NATS

## Current State

**Weaviate:**
- ✅ WikipediaArticle schema class exists (verified - has all required properties)
- 10 articles with source="news:fsm" (but these are from tool creation events, not BBC news)
- No recent BBC news articles stored

**News Ingestor:**
- Running hourly (cron: `0 * * * *`)
- Finding 25 articles per run
- Most filtered as duplicates (37 duplicates in Redis)
- Only 1 article per hour passes duplicate check
- Publishing to NATS subjects: `agi.events.news.relations` and `agi.events.news.alerts`

**Redis:**
- 37 duplicate keys (24-hour TTL)
- 0 news relations in `reasoning:news_relations:recent`
- 0 news alerts in `reasoning:news_alerts:recent`

## Solutions

### Solution 1: Reduce Duplicate TTL (Quick Fix)
**File**: `cmd/bbc-news-ingestor/main.go:77`

Change from 24 hours to 6-12 hours:
```go
// Mark as processed with 6-hour expiration (articles rotate faster)
err = redisClient.Set(ctx, key, title, 6*time.Hour).Err()
```

**Pros**: Quick fix, allows more articles through
**Cons**: May process same article multiple times if it stays on front page > 6 hours

### Solution 2: Deploy/Start FSM Server ⚠️ CRITICAL
**FSM deployment is missing** - this must be fixed first:
```bash
# Deploy FSM server
kubectl apply -f k3s/fsm-server-rpi58.yaml

# Verify it's running
kubectl get pods -n agi | grep fsm
kubectl logs -n agi <fsm-pod> | grep -i "news\|nats\|storeNewsEvent"
```

**Without FSM running, no news articles will be stored in Weaviate, regardless of other fixes.**

### Solution 3: Improve Duplicate Detection
Instead of URL-based duplicates, use a combination:
- URL + timestamp (allow same URL if > 24 hours old)
- Or reduce TTL to 6 hours
- Or check if article is actually new (compare timestamps)

### Solution 4: Increase News Ingestor Frequency
Change cron schedule from hourly to every 30 minutes:
```yaml
schedule: "*/30 * * * *"  # Every 30 minutes
```

### Solution 5: Reduce LLM Filtering
- Lower confidence thresholds for alerts/relations
- Allow more articles through even if LLM says "skip"
- Or skip LLM classification for some articles

## Recommended Fix

**CRITICAL - Do First:**
1. **Deploy FSM server** - Without this, no articles will be stored
   ```bash
   kubectl apply -f k3s/fsm-server-rpi58.yaml
   kubectl rollout status deployment/fsm-server-rpi58 -n agi
   ```

**Then:**
2. Reduce duplicate TTL from 24h to 6h (allows more articles through)
3. Verify FSM is receiving and storing news events
4. Check FSM logs for news storage errors

**Long-term:**
1. Implement smarter duplicate detection (URL + time-based)
2. Increase ingestor frequency if needed
3. Add monitoring for news ingestion pipeline

## Verification

After fixes, verify:
```bash
# Check articles in Weaviate
kubectl exec -n agi deployment/monitor-ui -- sh -c "curl -s -X POST http://weaviate.agi.svc.cluster.local:8080/v1/graphql -H 'Content-Type: application/json' -d '{\"query\": \"{ Get { WikipediaArticle(limit: 10, where: { path: [\\\"source\\\"], operator: Equal, valueString: \\\"news:fsm\\\" }, sort: [{path: [\\\"timestamp\\\"], order: desc}]) { title timestamp } } }\"}'" | jq '.data.Get.WikipediaArticle | length'

# Check news in Redis
kubectl exec -n agi deployment/redis -- redis-cli LLEN "reasoning:news_relations:recent"
kubectl exec -n agi deployment/redis -- redis-cli LLEN "reasoning:news_alerts:recent"

# Check FSM logs
kubectl logs -n agi <fsm-pod> | grep -i "storeNewsEvent\|news.*Weaviate"
```

