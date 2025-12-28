package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// MemoryConsolidator handles periodic consolidation and compression of memory
type MemoryConsolidator struct {
	redis            *redis.Client
	vectorDB         VectorDBAdapter
	domainKB         DomainKnowledgeClient
	consolidateEvery time.Duration
	ctx              context.Context
}

// ConsolidationConfig configures consolidation behavior
type ConsolidationConfig struct {
	// Interval between consolidation runs
	Interval time.Duration
	// Episode compression thresholds
	MinSimilarEpisodes int     // Minimum episodes to consider for compression
	SimilarityThreshold float64 // Similarity threshold for grouping (0-1)
	// Semantic promotion thresholds
	MinStabilityScore float64 // Minimum stability to promote to semantic memory
	MinOccurrences    int     // Minimum occurrences to consider stable
	// Archive thresholds
	TraceMaxAge        time.Duration // Maximum age for traces before archiving
	TraceMinUtility    float64       // Minimum utility score to keep trace
	// Skill extraction
	MinWorkflowRepetitions int // Minimum repetitions to extract as skill
}

// DefaultConsolidationConfig returns sensible defaults
func DefaultConsolidationConfig() *ConsolidationConfig {
	return &ConsolidationConfig{
		Interval:              1 * time.Hour,
		MinSimilarEpisodes:    5,
		SimilarityThreshold:   0.75,
		MinStabilityScore:     0.7,
		MinOccurrences:        3,
		TraceMaxAge:           7 * 24 * time.Hour, // 7 days
		TraceMinUtility:       0.3,
		MinWorkflowRepetitions: 3,
	}
}

// NewMemoryConsolidator creates a new memory consolidator
func NewMemoryConsolidator(
	redis *redis.Client,
	vectorDB VectorDBAdapter,
	domainKB DomainKnowledgeClient,
	config *ConsolidationConfig,
) *MemoryConsolidator {
	if config == nil {
		config = DefaultConsolidationConfig()
	}
	return &MemoryConsolidator{
		redis:            redis,
		vectorDB:        vectorDB,
		domainKB:        domainKB,
		consolidateEvery: config.Interval,
		ctx:             context.Background(),
	}
}

// Start begins the periodic consolidation scheduler
func (mc *MemoryConsolidator) Start() {
	log.Printf("🧠 [CONSOLIDATION] Starting memory consolidation scheduler (interval: %v)", mc.consolidateEvery)
	
	// Run immediately on start
	go func() {
		time.Sleep(30 * time.Second) // Wait 30s after startup
		mc.RunConsolidation()
	}()

	// Then run periodically
	go func() {
		ticker := time.NewTicker(mc.consolidateEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mc.RunConsolidation()
			case <-mc.ctx.Done():
				return
			}
		}
	}()
}

// RunConsolidation executes the full consolidation pipeline
func (mc *MemoryConsolidator) RunConsolidation() {
	log.Printf("🔄 [CONSOLIDATION] Starting consolidation cycle")
	startTime := time.Now()

	// 1. Compress redundant episodes into generalized schemas
	compressed, err := mc.CompressEpisodes()
	if err != nil {
		log.Printf("❌ [CONSOLIDATION] Episode compression failed: %v", err)
	} else {
		log.Printf("✅ [CONSOLIDATION] Compressed %d episode groups", compressed)
	}

	// 2. Promote stable structures to semantic memory
	promoted, err := mc.PromoteToSemantic()
	if err != nil {
		log.Printf("❌ [CONSOLIDATION] Semantic promotion failed: %v", err)
	} else {
		log.Printf("✅ [CONSOLIDATION] Promoted %d structures to semantic memory", promoted)
	}

	// 3. Archive stale or low-utility traces
	archived, err := mc.ArchiveTraces()
	if err != nil {
		log.Printf("❌ [CONSOLIDATION] Trace archiving failed: %v", err)
	} else {
		log.Printf("✅ [CONSOLIDATION] Archived %d traces", archived)
	}

	// 4. Derive skill abstractions from repeated workflows
	skills, err := mc.ExtractSkills()
	if err != nil {
		log.Printf("❌ [CONSOLIDATION] Skill extraction failed: %v", err)
	} else {
		log.Printf("✅ [CONSOLIDATION] Extracted %d skill abstractions", skills)
	}

	duration := time.Since(startTime)
	log.Printf("✅ [CONSOLIDATION] Consolidation cycle completed in %v", duration)
}

// EpisodeGroup represents a group of similar episodes
type EpisodeGroup struct {
	Episodes    []EpisodicRecord
	Schema      *GeneralizedSchema
	Similarity  float64
	FirstSeen   time.Time
	LastSeen    time.Time
	Count       int
}

// GeneralizedSchema represents a compressed schema from multiple episodes
type GeneralizedSchema struct {
	Pattern      string                 `json:"pattern"`
	CommonTags   []string               `json:"common_tags"`
	CommonOutcome string                `json:"common_outcome"`
	AvgReward    float64                `json:"avg_reward"`
	Metadata     map[string]interface{} `json:"metadata"`
	EpisodeIDs   []string               `json:"episode_ids"`
	CreatedAt    time.Time              `json:"created_at"`
}

// CompressEpisodes identifies and compresses redundant episodes
func (mc *MemoryConsolidator) CompressEpisodes() (int, error) {
	config := DefaultConsolidationConfig()
	
	// Search for recent episodes (last 30 days)
	// Note: Weaviate filtering is complex, so we'll get all episodes and filter in memory
	queryVec := make([]float32, 8) // Placeholder vector - in production, use actual embedding
	
	// Get a sample of episodes (we'll filter by timestamp in memory)
	episodes, err := mc.vectorDB.SearchEpisodes(queryVec, 1000, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to search episodes: %w", err)
	}

	// Filter by timestamp in memory (last 30 days)
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	recentEpisodes := []EpisodicRecord{}
	for _, ep := range episodes {
		if ep.Timestamp.After(cutoff) {
			recentEpisodes = append(recentEpisodes, ep)
		}
	}
	episodes = recentEpisodes

	if len(episodes) < config.MinSimilarEpisodes {
		log.Printf("ℹ️ [CONSOLIDATION] Not enough episodes for compression (%d < %d)", len(episodes), config.MinSimilarEpisodes)
		return 0, nil
	}

	// Group similar episodes by text similarity and tags
	groups := mc.groupSimilarEpisodes(episodes, config.SimilarityThreshold)
	
	compressedCount := 0
	for _, group := range groups {
		if len(group.Episodes) >= config.MinSimilarEpisodes {
			schema := mc.createGeneralizedSchema(group)
			
			// Store schema in Redis for quick lookup
			schemaKey := fmt.Sprintf("consolidation:schema:%s", schema.Pattern[:min(50, len(schema.Pattern))])
			schemaData, _ := json.Marshal(schema)
			mc.redis.Set(mc.ctx, schemaKey, schemaData, 90*24*time.Hour) // 90 days
			
			// Mark original episodes as compressed (store IDs)
			compressedKey := fmt.Sprintf("consolidation:compressed:%s", schemaKey)
			episodeIDs := make([]string, len(group.Episodes))
			for i, ep := range group.Episodes {
				episodeIDs[i] = ep.ID
			}
			idsData, _ := json.Marshal(episodeIDs)
			mc.redis.Set(mc.ctx, compressedKey, idsData, 90*24*time.Hour)
			
			compressedCount++
			log.Printf("📦 [CONSOLIDATION] Compressed %d episodes into schema: %s", len(group.Episodes), schema.Pattern[:min(60, len(schema.Pattern))])
		}
	}

	return compressedCount, nil
}

// groupSimilarEpisodes groups episodes by similarity
func (mc *MemoryConsolidator) groupSimilarEpisodes(episodes []EpisodicRecord, threshold float64) []EpisodeGroup {
	groups := []EpisodeGroup{}
	used := make(map[int]bool)

	for i, ep1 := range episodes {
		if used[i] {
			continue
		}

		group := EpisodeGroup{
			Episodes:   []EpisodicRecord{ep1},
			FirstSeen:  ep1.Timestamp,
			LastSeen:   ep1.Timestamp,
			Count:      1,
		}
		used[i] = true

		// Find similar episodes
		for j, ep2 := range episodes {
			if used[j] || i == j {
				continue
			}

			similarity := mc.calculateSimilarity(ep1, ep2)
			if similarity >= threshold {
				group.Episodes = append(group.Episodes, ep2)
				group.Similarity = similarity
				if ep2.Timestamp.Before(group.FirstSeen) {
					group.FirstSeen = ep2.Timestamp
				}
				if ep2.Timestamp.After(group.LastSeen) {
					group.LastSeen = ep2.Timestamp
				}
				group.Count++
				used[j] = true
			}
		}

		if len(group.Episodes) > 1 {
			groups = append(groups, group)
		}
	}

	return groups
}

// calculateSimilarity calculates similarity between two episodes
func (mc *MemoryConsolidator) calculateSimilarity(ep1, ep2 EpisodicRecord) float64 {
	// Simple text similarity (Jaccard-like on words)
	words1 := strings.Fields(strings.ToLower(ep1.Text))
	words2 := strings.Fields(strings.ToLower(ep2.Text))
	
	// Create word sets
	set1 := make(map[string]bool)
	for _, w := range words1 {
		if len(w) > 2 { // Ignore very short words
			set1[w] = true
		}
	}
	set2 := make(map[string]bool)
	for _, w := range words2 {
		if len(w) > 2 {
			set2[w] = true
		}
	}

	// Calculate intersection and union
	intersection := 0
	union := len(set1)
	for w := range set2 {
		if set1[w] {
			intersection++
		} else {
			union++
		}
	}

	if union == 0 {
		return 0.0
	}

	textSim := float64(intersection) / float64(union)

	// Tag similarity
	tagSim := 0.0
	if len(ep1.Tags) > 0 && len(ep2.Tags) > 0 {
		tagSet1 := make(map[string]bool)
		for _, t := range ep1.Tags {
			tagSet1[t] = true
		}
		tagMatches := 0
		for _, t := range ep2.Tags {
			if tagSet1[t] {
				tagMatches++
			}
		}
		tagSim = float64(tagMatches) / float64(max(len(ep1.Tags), len(ep2.Tags)))
	}

	// Outcome similarity
	outcomeSim := 0.0
	if ep1.Outcome != "" && ep2.Outcome != "" {
		if ep1.Outcome == ep2.Outcome {
			outcomeSim = 1.0
		}
	}

	// Weighted combination
	return 0.6*textSim + 0.2*tagSim + 0.2*outcomeSim
}

// createGeneralizedSchema creates a generalized schema from a group of episodes
func (mc *MemoryConsolidator) createGeneralizedSchema(group EpisodeGroup) *GeneralizedSchema {
	if len(group.Episodes) == 0 {
		return nil
	}

	// Extract common pattern from text (simplified - use first episode's text as base)
	pattern := group.Episodes[0].Text
	if len(pattern) > 200 {
		pattern = pattern[:200] + "..."
	}

	// Find common tags
	tagCounts := make(map[string]int)
	for _, ep := range group.Episodes {
		for _, tag := range ep.Tags {
			tagCounts[tag]++
		}
	}
	commonTags := []string{}
	for tag, count := range tagCounts {
		if count >= len(group.Episodes)/2 { // Appears in at least half
			commonTags = append(commonTags, tag)
		}
	}

	// Most common outcome
	outcomeCounts := make(map[string]int)
	for _, ep := range group.Episodes {
		if ep.Outcome != "" {
			outcomeCounts[ep.Outcome]++
		}
	}
	commonOutcome := ""
	maxCount := 0
	for outcome, count := range outcomeCounts {
		if count > maxCount {
			maxCount = count
			commonOutcome = outcome
		}
	}

	// Average reward
	avgReward := 0.0
	rewardCount := 0
	for _, ep := range group.Episodes {
		if ep.Reward != 0 {
			avgReward += ep.Reward
			rewardCount++
		}
	}
	if rewardCount > 0 {
		avgReward /= float64(rewardCount)
	}

	// Collect episode IDs
	episodeIDs := make([]string, len(group.Episodes))
	for i, ep := range group.Episodes {
		episodeIDs[i] = ep.ID
	}

	return &GeneralizedSchema{
		Pattern:      pattern,
		CommonTags:   commonTags,
		CommonOutcome: commonOutcome,
		AvgReward:    avgReward,
		Metadata: map[string]interface{}{
			"episode_count": len(group.Episodes),
			"similarity":    group.Similarity,
			"first_seen":    group.FirstSeen.Format(time.RFC3339),
			"last_seen":     group.LastSeen.Format(time.RFC3339),
		},
		EpisodeIDs: episodeIDs,
		CreatedAt:  time.Now(),
	}
}

// PromoteToSemantic promotes stable patterns to semantic memory (Neo4j)
func (mc *MemoryConsolidator) PromoteToSemantic() (int, error) {
	if mc.domainKB == nil {
		return 0, fmt.Errorf("semantic knowledge base not available")
	}

	config := DefaultConsolidationConfig()
	promotedCount := 0

	// Get all schemas from Redis
	keys, err := mc.redis.Keys(mc.ctx, "consolidation:schema:*").Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get schemas: %w", err)
	}

	for _, key := range keys {
		schemaData, err := mc.redis.Get(mc.ctx, key).Result()
		if err != nil {
			continue
		}

		var schema GeneralizedSchema
		if err := json.Unmarshal([]byte(schemaData), &schema); err != nil {
			continue
		}

		// Calculate stability score
		stability := mc.calculateStability(&schema)
		if stability < config.MinStabilityScore {
			continue
		}

		// Check if it meets minimum occurrences
		episodeCount := len(schema.EpisodeIDs)
		if episodeCount < config.MinOccurrences {
			continue
		}

		// Promote to semantic memory
		if mc.domainKB != nil {
			// Extract concept name from pattern
			conceptName := mc.extractConceptName(schema.Pattern)
			if conceptName != "" {
				concept := &Concept{
					Name:       conceptName,
					Domain:     mc.extractDomain(schema.CommonTags),
					Definition: schema.Pattern,
				}

				if err := mc.domainKB.SaveConcept(mc.ctx, concept); err != nil {
					log.Printf("⚠️ [CONSOLIDATION] Failed to save concept: %v", err)
					continue
				}

				// Add properties from metadata
				for key, value := range schema.Metadata {
					description := fmt.Sprintf("%v", value)
					mc.domainKB.AddProperty(mc.ctx, conceptName, key, description, "metadata")
				}

				// Mark as promoted
				promotedKey := fmt.Sprintf("consolidation:promoted:%s", key)
				mc.redis.Set(mc.ctx, promotedKey, "true", 0) // No expiration
				promotedCount++

				log.Printf("📈 [CONSOLIDATION] Promoted schema to semantic memory: %s (stability: %.2f)", conceptName, stability)
			}
		}
	}

	return promotedCount, nil
}

// calculateStability calculates how stable a schema is
func (mc *MemoryConsolidator) calculateStability(schema *GeneralizedSchema) float64 {
	// Stability based on:
	// 1. Number of episodes (more = more stable)
	// 2. Time span (longer = more stable)
	// 3. Consistency of outcomes

	episodeCount := float64(len(schema.EpisodeIDs))
	if episodeCount == 0 {
		return 0.0
	}

	// Episode count score (normalized to 0-1, saturates at 10)
	countScore := math.Min(1.0, float64(episodeCount)/10.0)

	// Time span score
	firstSeen, _ := time.Parse(time.RFC3339, schema.Metadata["first_seen"].(string))
	lastSeen, _ := time.Parse(time.RFC3339, schema.Metadata["last_seen"].(string))
	span := lastSeen.Sub(firstSeen)
	spanDays := span.Hours() / 24.0
	spanScore := math.Min(1.0, spanDays/7.0) // Saturates at 7 days

	// Outcome consistency (if all have same outcome, higher score)
	outcomeScore := 0.5 // Default
	if schema.CommonOutcome != "" {
		outcomeScore = 0.8 // Having a common outcome increases stability
	}

	// Weighted combination
	return 0.4*countScore + 0.3*spanScore + 0.3*outcomeScore
}

// extractConceptName extracts a concept name from a pattern
func (mc *MemoryConsolidator) extractConceptName(pattern string) string {
	// Simple extraction: use first meaningful phrase
	words := strings.Fields(pattern)
	if len(words) == 0 {
		return ""
	}

	// Take first 3-5 words as concept name
	nameWords := words[:min(5, len(words))]
	return strings.Join(nameWords, " ")
}

// extractDomain extracts domain from tags
func (mc *MemoryConsolidator) extractDomain(tags []string) string {
	// Look for domain-like tags
	for _, tag := range tags {
		if strings.Contains(tag, "domain") || strings.Contains(tag, "category") {
			return tag
		}
	}
	if len(tags) > 0 {
		return tags[0]
	}
	return "General"
}

// ArchiveTraces archives stale or low-utility traces from Redis
func (mc *MemoryConsolidator) ArchiveTraces() (int, error) {
	config := DefaultConsolidationConfig()
	archivedCount := 0

	// Find all reasoning trace keys
	keys, err := mc.redis.Keys(mc.ctx, "reasoning_trace:*").Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get trace keys: %w", err)
	}

	cutoffTime := time.Now().Add(-config.TraceMaxAge)

	for _, key := range keys {
		// Get TTL to check age
		ttl, err := mc.redis.TTL(mc.ctx, key).Result()
		if err != nil {
			continue
		}

		// Check if trace is old
		createdTime := time.Now().Add(-ttl)
		if createdTime.Before(cutoffTime) {
			// Calculate utility
			utility := mc.calculateTraceUtility(key)
			if utility < config.TraceMinUtility {
				// Archive by moving to archive key
				traceData, err := mc.redis.Get(mc.ctx, key).Result()
				if err == nil {
					archiveKey := strings.Replace(key, "reasoning_trace:", "archive:reasoning_trace:", 1)
					mc.redis.Set(mc.ctx, archiveKey, traceData, 365*24*time.Hour) // Keep archived for 1 year
					mc.redis.Del(mc.ctx, key)
					archivedCount++
					log.Printf("📦 [CONSOLIDATION] Archived trace: %s (utility: %.2f)", key, utility)
				}
			}
		}
	}

	// Also archive old session events
	sessionKeys, _ := mc.redis.Keys(mc.ctx, "session:*:events").Result()
	for _, key := range sessionKeys {
		ttl, err := mc.redis.TTL(mc.ctx, key).Result()
		if err != nil {
			continue
		}
		// If TTL is very short (expiring soon) and hasn't been accessed, archive
		if ttl < 1*time.Hour {
			// Check last access (simplified - in production, track access times)
			mc.redis.Del(mc.ctx, key) // Just delete old session events
			archivedCount++
		}
	}

	return archivedCount, nil
}

// calculateTraceUtility calculates utility score for a trace
func (mc *MemoryConsolidator) calculateTraceUtility(key string) float64 {
	// Simple utility: based on recency and size
	// In production, could track access frequency, success rates, etc.
	
	ttl, err := mc.redis.TTL(mc.ctx, key).Result()
	if err != nil {
		return 0.0
	}

	// Longer TTL = more recent = higher utility
	// Normalize to 0-1 (assuming max TTL of 24 hours)
	maxTTL := 24 * time.Hour
	recencyScore := float64(ttl) / float64(maxTTL)
	if recencyScore > 1.0 {
		recencyScore = 1.0
	}

	// Size score (larger traces might be more useful)
	traceData, err := mc.redis.Get(mc.ctx, key).Result()
	if err != nil {
		return recencyScore
	}
	sizeScore := math.Min(1.0, float64(len(traceData))/1000.0) // Normalize to 1KB

	return 0.7*recencyScore + 0.3*sizeScore
}

// SkillAbstraction represents an extracted skill from repeated workflows
type SkillAbstraction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Pattern     string                 `json:"pattern"`
	WorkflowIDs []string               `json:"workflow_ids"`
	SuccessRate float64                `json:"success_rate"`
	AvgDuration time.Duration          `json:"avg_duration"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
}

// ExtractSkills derives skill abstractions from repeated workflows
func (mc *MemoryConsolidator) ExtractSkills() (int, error) {
	config := DefaultConsolidationConfig()
	extractedCount := 0

	// Search for episodes with workflow_id in metadata
	queryVec := make([]float32, 8)
	episodes, err := mc.vectorDB.SearchEpisodes(queryVec, 1000, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to search episodes: %w", err)
	}

	// Group by workflow_id
	workflowGroups := make(map[string][]EpisodicRecord)
	for _, ep := range episodes {
		if workflowID, ok := ep.Metadata["workflow_id"].(string); ok && workflowID != "" {
			workflowGroups[workflowID] = append(workflowGroups[workflowID], ep)
		}
	}

	// Extract skills from repeated workflows
	for workflowID, episodes := range workflowGroups {
		if len(episodes) < config.MinWorkflowRepetitions {
			continue
		}

		skill := mc.createSkillAbstraction(workflowID, episodes)
		if skill != nil {
			// Store skill in Redis
			skillKey := fmt.Sprintf("consolidation:skill:%s", skill.Name)
			skillData, _ := json.Marshal(skill)
			mc.redis.Set(mc.ctx, skillKey, skillData, 0) // No expiration

			// Also promote to semantic memory if highly successful
			if skill.SuccessRate >= 0.8 && mc.domainKB != nil {
				concept := &Concept{
					Name:       skill.Name,
					Domain:     "Skills",
					Definition: skill.Description,
				}
				mc.domainKB.SaveConcept(mc.ctx, concept)

				// Add skill properties
				description := fmt.Sprintf("Success rate: %.2f%%", skill.SuccessRate*100)
				mc.domainKB.AddProperty(mc.ctx, skill.Name, "success_rate", description, "metric")
			}

			extractedCount++
			log.Printf("🎯 [CONSOLIDATION] Extracted skill: %s (success: %.2f%%, repetitions: %d)", 
				skill.Name, skill.SuccessRate*100, len(episodes))
		}
	}

	return extractedCount, nil
}

// createSkillAbstraction creates a skill abstraction from workflow episodes
func (mc *MemoryConsolidator) createSkillAbstraction(workflowID string, episodes []EpisodicRecord) *SkillAbstraction {
	if len(episodes) == 0 {
		return nil
	}

	// Calculate success rate
	successCount := 0
	totalDuration := time.Duration(0)
	durationCount := 0
	allTags := make(map[string]int)

	for _, ep := range episodes {
		if ep.Outcome == "success" || ep.Reward > 0 {
			successCount++
		}
		if duration, ok := ep.Metadata["duration"].(float64); ok {
			totalDuration += time.Duration(duration) * time.Second
			durationCount++
		}
		for _, tag := range ep.Tags {
			allTags[tag]++
		}
	}

	successRate := float64(successCount) / float64(len(episodes))
	avgDuration := time.Duration(0)
	if durationCount > 0 {
		avgDuration = totalDuration / time.Duration(durationCount)
	}

	// Extract common tags
	commonTags := []string{}
	for tag, count := range allTags {
		if count >= len(episodes)/2 {
			commonTags = append(commonTags, tag)
		}
	}
	sort.Strings(commonTags)

	// Create skill name and description
	name := fmt.Sprintf("Workflow_%s", workflowID[:min(20, len(workflowID))])
	description := ""
	if len(episodes) > 0 {
		description = episodes[0].Text
		if len(description) > 200 {
			description = description[:200] + "..."
		}
	}

	workflowIDs := []string{workflowID}

	return &SkillAbstraction{
		Name:        name,
		Description: description,
		Pattern:     workflowID,
		WorkflowIDs: workflowIDs,
		SuccessRate: successRate,
		AvgDuration: avgDuration,
		Tags:        commonTags,
		Metadata: map[string]interface{}{
			"repetitions": len(episodes),
			"first_seen":  episodes[0].Timestamp.Format(time.RFC3339),
			"last_seen":   episodes[len(episodes)-1].Timestamp.Format(time.RFC3339),
		},
		CreatedAt: time.Now(),
	}
}

// Helper function for string length min (used in consolidation)
func minStringLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}

