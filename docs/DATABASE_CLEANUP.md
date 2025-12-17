# Database Cleanup Guide

## Overview

The Artificial Mind system uses multiple databases:
- **Redis** - Caching, state management, workflows, goals
- **Neo4j** - Graph database for knowledge/concepts
- **Weaviate** - Vector database for episodic memory

## Quick Cleanup

**Thorough cleanup (recommended):**
```bash
./scripts/clean_databases.sh --confirm
```

This script:
1. ✅ Stops all services first (prevents key recreation)
2. ✅ Clears Redis (all keys)
3. ✅ Clears Neo4j (all nodes and relationships)
4. ✅ Clears Weaviate (all collections)
5. ✅ Cleans persistent data directories
6. ✅ Restarts containers

**Using Make:**
```bash
make clean-databases
```

## Other Cleanup Options

### Option 1: Script-based cleanup
```bash
# Complete cleanup (stops services, clears everything)
./scripts/clean_databases.sh --confirm

# Alternative script (doesn't stop services)
./scripts/clean_all.sh --confirm
```

### Option 2: Make targets
```bash
# Clear Redis only
make clear-redis

# Clear all databases (requires confirmation)
make clear-redis CONFIRM=YES

# Full reset (stop → clean → restart)
make reset-all
```

### Option 3: Manual cleanup
```bash
# Stop services
./scripts/stop_servers.sh

# Clear Redis
docker exec agi-redis redis-cli FLUSHALL

# Clear Neo4j
docker exec -i agi-neo4j sh -c "cypher-shell -a bolt://localhost:7687 -u neo4j -p test1234 'MATCH (n) DETACH DELETE n;'"

# Clear Weaviate (delete all schema classes)
curl -X DELETE http://localhost:8080/v1/schema/AgiEpisodes
curl -X DELETE http://localhost:8080/v1/schema/AgiWiki
curl -X DELETE http://localhost:8080/v1/schema/WikipediaArticle

# Restart services
./scripts/start_servers.sh
```

## Why Stop Services First?

**Important:** The `clean_databases.sh` script stops services first because:
- Running services continuously recreate Redis keys
- Goals, workflows, and metrics are recreated in real-time
- Stopping services ensures a clean slate

## Verification

After cleanup, verify databases are empty:

```bash
# Check Redis key count
docker exec agi-redis redis-cli DBSIZE

# Check Neo4j node count
docker exec -i agi-neo4j sh -c "cypher-shell -a bolt://localhost:7687 -u neo4j -p test1234 'MATCH (n) RETURN count(n);'"

# Check Weaviate schema
curl http://localhost:8080/v1/schema
```

## Troubleshooting

**Problem:** Redis still has keys after cleanup
- **Solution:** Make sure services are stopped first
- **Check:** `ps aux | grep -E "(hdn-server|fsm-server|principles-server)"`

**Problem:** Neo4j won't clear
- **Solution:** Check Neo4j container is running and credentials are correct
- **Check:** `docker exec agi-neo4j cypher-shell -a bolt://localhost:7687 -u neo4j -p test1234 'MATCH (n) RETURN count(n);'`

**Problem:** Weaviate classes reappear
- **Solution:** This is normal - classes are recreated when new data is added
- **Note:** The objects are deleted, but schema classes may persist

## Complete Fresh Start

For a completely fresh start:

```bash
# 1. Stop everything
./scripts/stop_servers.sh
docker-compose down

# 2. Clean databases
./scripts/clean_databases.sh --confirm

# 3. Clean data directories (optional, more destructive)
./scripts/clean_data_dirs.sh

# 4. Restart everything
docker-compose up -d
./scripts/start_servers.sh
```

## Make Targets Summary

| Target | What it does |
|--------|-------------|
| `make clean-databases` | Stops services, clears all databases |
| `make clear-redis` | Clears Redis only |
| `make clear-redis CONFIRM=YES` | Clears Redis, Weaviate, Neo4j |
| `make reset-all` | Full reset: stop → clean → restart |

