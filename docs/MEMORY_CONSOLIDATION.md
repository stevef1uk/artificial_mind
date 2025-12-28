# Memory Consolidation & Compression System

## Overview

The Memory Consolidation & Compression system provides a periodic pipeline to optimize memory usage across the three-tier memory architecture:

- **Redis** → Working/trace memory (ephemeral)
- **Qdrant** → Episodic memory (vector search)
- **Neo4j** → Semantic knowledge (graph database)

## Architecture

The consolidation system runs periodically (default: every hour) and performs four main operations:

### 1. Episode Compression

**Purpose**: Compress redundant episodes into generalized schemas

**Process**:
- Searches for recent episodes (last 30 days) from Qdrant
- Groups similar episodes by text similarity, tags, and outcomes
- Creates generalized schemas that capture common patterns
- Stores schemas in Redis for quick lookup
- Marks original episodes as compressed

**Configuration**:
- `MinSimilarEpisodes`: Minimum episodes to consider for compression (default: 5)
- `SimilarityThreshold`: Similarity threshold for grouping (default: 0.75)

**Output**: Generalized schemas stored in Redis with keys like `consolidation:schema:*`

### 2. Semantic Promotion

**Purpose**: Promote stable structures to semantic memory (Neo4j)

**Process**:
- Analyzes all compressed schemas from Redis
- Calculates stability scores based on:
  - Episode count (more episodes = more stable)
  - Time span (longer span = more stable)
  - Outcome consistency
- Promotes schemas with high stability scores to Neo4j as Concepts
- Adds properties and metadata to the concepts

**Configuration**:
- `MinStabilityScore`: Minimum stability to promote (default: 0.7)
- `MinOccurrences`: Minimum occurrences to consider stable (default: 3)

**Output**: Concepts in Neo4j domain knowledge graph

### 3. Trace Archiving

**Purpose**: Archive stale or low-utility traces from Redis

**Process**:
- Finds all reasoning trace keys in Redis
- Calculates utility scores based on:
  - Recency (longer TTL = more recent = higher utility)
  - Size (larger traces might be more useful)
- Archives traces that are:
  - Older than `TraceMaxAge` (default: 7 days)
  - Have utility below `TraceMinUtility` (default: 0.3)
- Moves archived traces to `archive:reasoning_trace:*` keys
- Cleans up old session events

**Configuration**:
- `TraceMaxAge`: Maximum age before archiving (default: 7 days)
- `TraceMinUtility`: Minimum utility score to keep (default: 0.3)

**Output**: Archived traces in Redis with extended TTL (1 year)

### 4. Skill Abstraction Extraction

**Purpose**: Derive skill abstractions from repeated workflows

**Process**:
- Searches for episodes with `workflow_id` in metadata
- Groups episodes by workflow ID
- Extracts skills from workflows with sufficient repetitions
- Calculates success rates and average durations
- Stores skills in Redis
- Promotes highly successful skills (≥80% success rate) to Neo4j

**Configuration**:
- `MinWorkflowRepetitions`: Minimum repetitions to extract as skill (default: 3)

**Output**: Skill abstractions in Redis and Neo4j

## Integration

### HDN Server

The consolidation system is automatically initialized when the HDN server starts, if:
- Vector database (Qdrant/Weaviate) is available
- Domain knowledge client (Neo4j) is available

**Initialization** (in `hdn/server.go`):
```go
if server.vectorDB != nil && server.domainKnowledge != nil {
    consolidator := mempkg.NewMemoryConsolidator(
        redisClient,
        server.vectorDB,
        server.domainKnowledge,
        mempkg.DefaultConsolidationConfig(),
    )
    server.memoryConsolidator = consolidator
    consolidator.Start()
}
```

### Configuration

Default configuration can be customized by creating a custom `ConsolidationConfig`:

```go
config := &mempkg.ConsolidationConfig{
    Interval:              2 * time.Hour,  // Run every 2 hours
    MinSimilarEpisodes:    10,            // Require 10 similar episodes
    SimilarityThreshold:   0.8,           // Higher similarity threshold
    MinStabilityScore:     0.8,            // Higher stability requirement
    MinOccurrences:        5,               // More occurrences needed
    TraceMaxAge:           14 * 24 * time.Hour, // 14 days
    TraceMinUtility:       0.4,             // Higher utility threshold
    MinWorkflowRepetitions: 5,             // More repetitions for skills
}
```

## Data Structures

### GeneralizedSchema

Represents a compressed schema from multiple episodes:

```go
type GeneralizedSchema struct {
    Pattern      string                 // Common pattern text
    CommonTags   []string               // Tags appearing in ≥50% of episodes
    CommonOutcome string                // Most common outcome
    AvgReward    float64                // Average reward
    Metadata     map[string]interface{} // Additional metadata
    EpisodeIDs   []string               // IDs of compressed episodes
    CreatedAt    time.Time              // Creation timestamp
}
```

### SkillAbstraction

Represents an extracted skill from repeated workflows:

```go
type SkillAbstraction struct {
    Name        string                 // Skill name
    Description string                 // Skill description
    Pattern     string                 // Workflow pattern
    WorkflowIDs []string               // Associated workflow IDs
    SuccessRate float64                // Success rate (0-1)
    AvgDuration time.Duration          // Average execution duration
    Tags        []string               // Common tags
    Metadata    map[string]interface{} // Additional metadata
    CreatedAt   time.Time              // Creation timestamp
}
```

## Monitoring

The consolidation system logs its activities:

- `🔄 [CONSOLIDATION] Starting consolidation cycle`
- `✅ [CONSOLIDATION] Compressed N episode groups`
- `✅ [CONSOLIDATION] Promoted N structures to semantic memory`
- `✅ [CONSOLIDATION] Archived N traces`
- `✅ [CONSOLIDATION] Extracted N skill abstractions`
- `✅ [CONSOLIDATION] Consolidation cycle completed in X`

## Benefits

1. **Reduced Memory Footprint**: Compressed episodes take less space than individual episodes
2. **Improved Retrieval**: Generalized schemas enable faster pattern matching
3. **Knowledge Growth**: Stable patterns are promoted to semantic memory for long-term use
4. **Skill Discovery**: Repeated workflows are automatically identified as reusable skills
5. **Automatic Cleanup**: Stale traces are archived, keeping working memory fresh

## Future Enhancements

Potential improvements:
- Use actual embeddings for similarity calculation (currently uses text-based similarity)
- Track access patterns to improve utility scoring
- Implement incremental consolidation (only process new episodes)
- Add metrics and monitoring dashboards
- Support custom consolidation strategies per domain
- Implement compression ratios and space savings tracking

