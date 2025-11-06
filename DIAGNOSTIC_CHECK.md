# System Diagnostic Check

## ✅ What's Working

1. **All services are running:**
   - HDN Server: ✅ Port 8081
   - Monitor UI: ✅ Port 8082  
   - FSM Server: ✅ Port 8083
   - Principles Server: ✅ Port 8084
   - Goal Manager: ✅ Port 8090

2. **Infrastructure is healthy:**
   - Redis: ✅ Running
   - Neo4j: ✅ Running
   - Weaviate: ✅ Running
   - NATS: ✅ Running

3. **Activity is happening:**
   - FSM is transitioning between states
   - Hypotheses are being generated
   - Knowledge updates are occurring

## ⚠️ Issues Found

### 1. Knowledge Query Timeout
**Problem:** FSM server is timing out when querying HDN's knowledge API:
```
"belief_query_error": "failed to execute query: Post \"http://localhost:8081/api/v1/knowledge/query\": context deadline exceeded"
```

**Possible Causes:**
- HDN knowledge endpoint is slow or hanging
- Query timeout is too short
- HDN server is overloaded

**Check:**
```bash
# Test the knowledge endpoint directly
curl -X POST http://localhost:8081/api/v1/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{"query": "MATCH (n) RETURN count(n) as total"}'
```

### 2. FSM State: "plan"
**Current State:** The FSM is in "plan" state, which means it's trying to create execution plans.

**This is normal** if:
- The system is processing a task
- It's waiting for input

**To check if it's stuck:**
```bash
# Check recent activity
curl http://localhost:8083/activity?limit=10 | jq '.activities[] | .message'

# Check current thinking
curl http://localhost:8083/thinking | jq '.thinking_focus, .current_state'
```

## 🔍 Diagnostic Commands

### Check Service Health
```bash
# All services
curl http://localhost:8082/api/status | jq '.services'

# Individual health checks
curl http://localhost:8081/health
curl http://localhost:8083/health
```

### Check FSM Activity
```bash
# Recent activity log
curl http://localhost:8083/activity?limit=20 | jq '.activities[] | "\(.timestamp) \(.message)"'

# Current state and thinking
curl http://localhost:8083/thinking | jq '.'
```

### Check HDN Capabilities
```bash
# List available tools
curl http://localhost:8081/api/v1/tools | jq '.tools | length'

# Test knowledge query
curl -X POST http://localhost:8081/api/v1/knowledge/query \
  -H "Content-Type: application/json" \
  -d '{"query": "MATCH (n) RETURN count(n) LIMIT 1"}'
```

### Check Infrastructure
```bash
# Docker services
docker-compose ps

# Check Redis
redis-cli ping

# Check Neo4j
curl http://localhost:7474

# Check Weaviate
curl http://localhost:8080/v1/.well-known/ready
```

## 🎯 What to Try Next

1. **If knowledge queries are timing out:**
   - Check Neo4j is accessible: `curl http://localhost:7474`
   - Check HDN logs for errors
   - Increase timeout in FSM config if needed

2. **If FSM seems stuck:**
   - Check activity log: `curl http://localhost:8083/activity?limit=10`
   - Send a test event to trigger FSM: Check Monitor UI at http://localhost:8082
   - Check if there are pending goals

3. **If services are slow:**
   - Check system resources: `top` or `htop`
   - Check Docker container resources: `docker stats`
   - Check service logs for errors

## 📊 Monitor UI

The easiest way to see what's happening:
- Open http://localhost:8082 in your browser
- Check the "Thinking" panel for current state
- Check the "Activity" feed (if integrated)
- Check service status in the dashboard

## 🐛 Common Issues

1. **Services running but not responding:**
   - Check if ports are actually listening: `netstat -tlnp | grep -E ':(8081|8082|8083)'`
   - Check firewall rules
   - Check service logs

2. **Timeout errors:**
   - Increase timeouts in config files
   - Check if underlying services (Neo4j, Redis) are slow
   - Check network connectivity

3. **FSM stuck in one state:**
   - Check activity log to see if transitions are happening
   - Check if events are being processed
   - Restart FSM server if needed

