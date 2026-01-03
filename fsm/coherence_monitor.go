package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	
	selfmodel "agi/self"
)

// CoherenceMonitor checks for inconsistencies across FSM, HDN, and Self-Model
type CoherenceMonitor struct {
	redis      *redis.Client
	ctx        context.Context
	hdnURL     string
	reasoning  *ReasoningEngine
	agentID    string
	httpClient *http.Client
	nc         *nats.Conn // NATS connection for listening to goal events
}

// Inconsistency represents a detected inconsistency
type Inconsistency struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"` // "belief_contradiction", "policy_conflict", "goal_drift", "behavior_loop", "strategy_conflict"
	Severity    string                 `json:"severity"` // "low", "medium", "high", "critical"
	Description string                 `json:"description"`
	Details     map[string]interface{} `json:"details"`
	DetectedAt  time.Time              `json:"detected_at"`
	Resolved    bool                   `json:"resolved"`
	Resolution  string                 `json:"resolution,omitempty"`
}

// SelfReflectionTask represents a task generated to resolve inconsistencies
type SelfReflectionTask struct {
	ID            string                 `json:"id"`
	Inconsistency string                 `json:"inconsistency_id"`
	Description   string                 `json:"description"`
	Priority      int                    `json:"priority"` // 1-10, higher is more important
	Status        string                 `json:"status"`   // "pending", "active", "resolved", "failed"
	CreatedAt     time.Time              `json:"created_at"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// NewCoherenceMonitor creates a new coherence monitor
func NewCoherenceMonitor(redis *redis.Client, hdnURL string, reasoning *ReasoningEngine, agentID string, nc *nats.Conn) *CoherenceMonitor {
	cm := &CoherenceMonitor{
		redis:      redis,
		ctx:        context.Background(),
		hdnURL:     hdnURL,
		reasoning:  reasoning,
		agentID:    agentID,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		nc:         nc,
	}
	
	// Subscribe to goal completion events to mark inconsistencies as resolved
	if nc != nil {
		cm.subscribeToGoalEvents()
	}
	
	return cm
}

// CheckCoherence performs a comprehensive coherence check across all systems
func (cm *CoherenceMonitor) CheckCoherence() ([]Inconsistency, error) {
	log.Printf("🔍 [Coherence] Starting cross-system coherence check")
	
	if cm == nil {
		return nil, fmt.Errorf("coherence monitor is nil")
	}
	
	var inconsistencies []Inconsistency
	
	// 1. Check for belief contradictions
	log.Printf("🔍 [Coherence] Checking belief contradictions...")
	beliefContradictions, err := cm.checkBeliefContradictions()
	if err != nil {
		log.Printf("⚠️ [Coherence] Error checking belief contradictions: %v", err)
	} else {
		log.Printf("✅ [Coherence] Belief contradiction check complete: %d found", len(beliefContradictions))
		inconsistencies = append(inconsistencies, beliefContradictions...)
	}
	
	// 2. Check for policy conflicts
	log.Printf("🔍 [Coherence] Checking policy conflicts...")
	policyConflicts, err := cm.checkPolicyConflicts()
	if err != nil {
		log.Printf("⚠️ [Coherence] Error checking policy conflicts: %v", err)
	} else {
		log.Printf("✅ [Coherence] Policy conflict check complete: %d found", len(policyConflicts))
		inconsistencies = append(inconsistencies, policyConflicts...)
	}
	
	// 3. Check for learned strategy conflicts
	log.Printf("🔍 [Coherence] Checking strategy conflicts...")
	strategyConflicts, err := cm.checkStrategyConflicts()
	if err != nil {
		log.Printf("⚠️ [Coherence] Error checking strategy conflicts: %v", err)
	} else {
		log.Printf("✅ [Coherence] Strategy conflict check complete: %d found", len(strategyConflicts))
		inconsistencies = append(inconsistencies, strategyConflicts...)
	}
	
	// 4. Check for long-running goal drift
	log.Printf("🔍 [Coherence] Checking goal drift...")
	goalDrift, err := cm.checkGoalDrift()
	if err != nil {
		log.Printf("⚠️ [Coherence] Error checking goal drift: %v", err)
	} else {
		log.Printf("✅ [Coherence] Goal drift check complete: %d found", len(goalDrift))
		inconsistencies = append(inconsistencies, goalDrift...)
	}
	
	// 5. Check for unexplainable behavior loops
	log.Printf("🔍 [Coherence] Checking behavior loops...")
	behaviorLoops, err := cm.checkBehaviorLoops()
	if err != nil {
		log.Printf("⚠️ [Coherence] Error checking behavior loops: %v", err)
	} else {
		log.Printf("✅ [Coherence] Behavior loop check complete: %d found", len(behaviorLoops))
		inconsistencies = append(inconsistencies, behaviorLoops...)
	}
	
	// Store inconsistencies in Redis
	for _, inc := range inconsistencies {
		cm.storeInconsistency(inc)
	}
	
	// Cleanup old coherence goals periodically (every 10th check, ~50 minutes)
	// This prevents Redis from accumulating too many old goals
	if len(inconsistencies) == 0 {
		cm.cleanupOldCoherenceGoals()
	}
	
	log.Printf("✅ [Coherence] Coherence check complete: found %d inconsistencies", len(inconsistencies))
	
	return inconsistencies, nil
}

// checkBeliefContradictions checks for contradictory beliefs
func (cm *CoherenceMonitor) checkBeliefContradictions() ([]Inconsistency, error) {
	var inconsistencies []Inconsistency
	
	// Skip if reasoning engine is not available
	if cm.reasoning == nil {
		log.Printf("⚠️ [Coherence] Reasoning engine not available, skipping belief contradiction check")
		return inconsistencies, nil
	}
	
	// Get recent beliefs from reasoning traces (limit to 10 most recent to avoid slow checks)
	tracesKey := "reasoning:traces:all"
	tracesData, err := cm.redis.LRange(cm.ctx, tracesKey, 0, 9).Result() // Limit to 10 traces
	if err != nil {
		return inconsistencies, nil // No traces yet, no contradictions
	}
	
	log.Printf("🔍 [Coherence] Checking %d reasoning traces for belief contradictions", len(tracesData))
	
	// Extract beliefs from traces (limit domains to avoid too many HTTP calls)
	beliefs := make(map[string][]Belief)
	domainsChecked := make(map[string]bool)
	maxDomains := 5 // Limit to 5 unique domains to avoid hanging
	
	for _, traceData := range tracesData {
		var trace ReasoningTrace
		if err := json.Unmarshal([]byte(traceData), &trace); err != nil {
			continue
		}
		
		// Skip if we've already checked this domain or hit the limit
		if domainsChecked[trace.Domain] || len(domainsChecked) >= maxDomains {
			continue
		}
		
		domainsChecked[trace.Domain] = true
		log.Printf("🔍 [Coherence] Querying beliefs for domain: %s", trace.Domain)
		
		// Query beliefs for this domain (may be slow if HDN is busy)
		domainBeliefs, err := cm.reasoning.QueryBeliefs("all concepts", trace.Domain)
		if err != nil {
			log.Printf("⚠️ [Coherence] Failed to query beliefs for domain %s: %v", trace.Domain, err)
			continue
		}
		
		beliefs[trace.Domain] = domainBeliefs
		log.Printf("✅ [Coherence] Retrieved %d beliefs for domain %s", len(domainBeliefs), trace.Domain)
	}
	
	// Check for contradictions within each domain
	for domain, domainBeliefs := range beliefs {
		for i, b1 := range domainBeliefs {
			for j, b2 := range domainBeliefs {
				if i >= j {
					continue
				}
				
				// Simple contradiction detection: check for opposite statements
				if cm.isContradictory(b1.Statement, b2.Statement) {
					inc := Inconsistency{
						ID:          fmt.Sprintf("belief_contradiction_%d", time.Now().UnixNano()),
						Type:        "belief_contradiction",
						Severity:    cm.calculateSeverity(b1, b2),
						Description: fmt.Sprintf("Contradictory beliefs in domain '%s': '%s' vs '%s'", domain, b1.Statement, b2.Statement),
						Details: map[string]interface{}{
							"domain":      domain,
							"belief1_id":  b1.ID,
							"belief1":     b1.Statement,
							"belief1_conf": b1.Confidence,
							"belief2_id":  b2.ID,
							"belief2":     b2.Statement,
							"belief2_conf": b2.Confidence,
						},
						DetectedAt: time.Now(),
						Resolved:   false,
					}
					inconsistencies = append(inconsistencies, inc)
				}
			}
		}
	}
	
	return inconsistencies, nil
}

// checkPolicyConflicts checks for conflicts between policies in Self-Model
func (cm *CoherenceMonitor) checkPolicyConflicts() ([]Inconsistency, error) {
	var inconsistencies []Inconsistency
	
	// Get active goals from Goal Manager
	goalMgrURL := os.Getenv("GOAL_MANAGER_URL")
	if goalMgrURL == "" {
		goalMgrURL = "http://localhost:8084"
	}
	
	// Fetch active goals via HTTP (Goal Manager API)
	activeGoalsKey := fmt.Sprintf("goals:%s:active", cm.agentID)
	goalIDs, err := cm.redis.SMembers(cm.ctx, activeGoalsKey).Result()
	if err != nil {
		return inconsistencies, nil // No goals yet
	}
	
	// Load goals and check for conflicts
	goals := make([]selfmodel.PolicyGoal, 0)
	for _, goalID := range goalIDs {
		goalKey := fmt.Sprintf("goal:%s", goalID)
		goalData, err := cm.redis.Get(cm.ctx, goalKey).Result()
		if err != nil {
			continue
		}
		
		var goal selfmodel.PolicyGoal
		if err := json.Unmarshal([]byte(goalData), &goal); err != nil {
			continue
		}
		goals = append(goals, goal)
	}
	
	// Check for conflicting goals (same domain, opposite objectives)
	for i, g1 := range goals {
		for j, g2 := range goals {
			if i >= j {
				continue
			}
			
			if cm.areGoalsConflicting(g1, g2) {
				inc := Inconsistency{
					ID:          fmt.Sprintf("policy_conflict_%d", time.Now().UnixNano()),
					Type:        "policy_conflict",
					Severity:    "medium",
					Description: fmt.Sprintf("Conflicting goals: '%s' vs '%s'", g1.Description, g2.Description),
					Details: map[string]interface{}{
						"goal1_id":   g1.ID,
						"goal1":      g1.Description,
						"goal1_priority": g1.Priority,
						"goal2_id":   g2.ID,
						"goal2":      g2.Description,
						"goal2_priority": g2.Priority,
					},
					DetectedAt: time.Now(),
					Resolved:   false,
				}
				inconsistencies = append(inconsistencies, inc)
			}
		}
	}
	
	return inconsistencies, nil
}

// checkStrategyConflicts checks for conflicts in learned strategies from HDN
func (cm *CoherenceMonitor) checkStrategyConflicts() ([]Inconsistency, error) {
	var inconsistencies []Inconsistency
	
	// Get code generation strategies from Redis
	pattern := "codegen_strategy:*"
	keys, err := cm.redis.Keys(cm.ctx, pattern).Result()
	if err != nil {
		return inconsistencies, nil
	}
	
	strategies := make(map[string]map[string]interface{})
	for _, key := range keys {
		strategyData, err := cm.redis.Get(cm.ctx, key).Result()
		if err != nil {
			continue
		}
		
		var strategy map[string]interface{}
		if err := json.Unmarshal([]byte(strategyData), &strategy); err != nil {
			continue
		}
		strategies[key] = strategy
	}
	
	// Check for conflicting strategies (same task category, opposite approaches)
	for key1, s1 := range strategies {
		for key2, s2 := range strategies {
			if key1 >= key2 {
				continue
			}
			
			if cm.areStrategiesConflicting(s1, s2) {
				inc := Inconsistency{
					ID:          fmt.Sprintf("strategy_conflict_%d", time.Now().UnixNano()),
					Type:        "strategy_conflict",
					Severity:    "low",
					Description: fmt.Sprintf("Conflicting learned strategies: %s vs %s", key1, key2),
					Details: map[string]interface{}{
						"strategy1": key1,
						"strategy2": key2,
					},
					DetectedAt: time.Now(),
					Resolved:   false,
				}
				inconsistencies = append(inconsistencies, inc)
			}
		}
	}
	
	return inconsistencies, nil
}

// checkGoalDrift checks for goals that have been active too long without progress
func (cm *CoherenceMonitor) checkGoalDrift() ([]Inconsistency, error) {
	var inconsistencies []Inconsistency
	
	// Get active goals
	activeGoalsKey := fmt.Sprintf("goals:%s:active", cm.agentID)
	goalIDs, err := cm.redis.SMembers(cm.ctx, activeGoalsKey).Result()
	if err != nil {
		return inconsistencies, nil
	}
	
	driftThreshold := 24 * time.Hour // Goals older than 24 hours without progress
	
	for _, goalID := range goalIDs {
		goalKey := fmt.Sprintf("goal:%s", goalID)
		goalData, err := cm.redis.Get(cm.ctx, goalKey).Result()
		if err != nil {
			continue
		}
		
		var goal selfmodel.PolicyGoal
		if err := json.Unmarshal([]byte(goalData), &goal); err != nil {
			continue
		}
		
		// Check if goal has been active too long without updates
		timeSinceUpdate := time.Since(goal.UpdatedAt)
		if timeSinceUpdate > driftThreshold && goal.Status == "active" {
			inc := Inconsistency{
				ID:          fmt.Sprintf("goal_drift_%d", time.Now().UnixNano()),
				Type:        "goal_drift",
				Severity:    "medium",
				Description: fmt.Sprintf("Goal '%s' has been active for %v without progress", goal.Description, timeSinceUpdate),
				Details: map[string]interface{}{
					"goal_id":           goal.ID,
					"goal_description": goal.Description,
					"created_at":        goal.CreatedAt,
					"updated_at":        goal.UpdatedAt,
					"time_since_update": timeSinceUpdate.String(),
				},
				DetectedAt: time.Now(),
				Resolved:   false,
			}
			inconsistencies = append(inconsistencies, inc)
		}
	}
	
	return inconsistencies, nil
}

// checkBehaviorLoops checks for repetitive behavior patterns that suggest loops
func (cm *CoherenceMonitor) checkBehaviorLoops() ([]Inconsistency, error) {
	var inconsistencies []Inconsistency
	
	// Get recent activity log entries
	activityKey := fmt.Sprintf("fsm:%s:activity_log", cm.agentID)
	activities, err := cm.redis.LRange(cm.ctx, activityKey, 0, 99).Result()
	if err != nil {
		return inconsistencies, nil
	}
	
	// Parse activities
	activityEntries := make([]ActivityLogEntry, 0)
	for _, activityData := range activities {
		var entry ActivityLogEntry
		if err := json.Unmarshal([]byte(activityData), &entry); err != nil {
			continue
		}
		activityEntries = append(activityEntries, entry)
	}
	
	// Check for repetitive patterns (same state transitions repeating)
	stateTransitions := make(map[string]int)
	for i := 0; i < len(activityEntries)-1; i++ {
		curr := activityEntries[i]
		next := activityEntries[i+1]
		
		if curr.State != "" && next.State != "" {
			transition := fmt.Sprintf("%s->%s", curr.State, next.State)
			stateTransitions[transition]++
		}
	}
	
	// Flag transitions that occur too frequently (potential loops)
	// DEDUPLICATION: Only flag if we haven't recently detected the same loop
	for transition, count := range stateTransitions {
		if count >= 10 { // Increased threshold from 5 to 10 to reduce false positives
			// Check if we've already flagged this transition recently
			loopKey := fmt.Sprintf("coherence:flagged_loop:%s", transition)
			if lastFlaggedTime, err := cm.redis.Get(cm.ctx, loopKey).Result(); err == nil && lastFlaggedTime != "" {
				log.Printf("⏭️ [Coherence] Loop '%s' already flagged recently, skipping duplicate detection", transition)
				continue
			}
			
			// Mark this loop as detected/flagged for 24 hours to prevent re-flagging
			// Use a longer TTL since behavior loops indicate systemic issues that need deeper analysis
			cm.redis.Set(cm.ctx, loopKey, time.Now().String(), 24*time.Hour)
			
			inc := Inconsistency{
				ID:          fmt.Sprintf("behavior_loop_%d", time.Now().UnixNano()),
				Type:        "behavior_loop",
				Severity:    "high",
				Description: fmt.Sprintf("Potential behavior loop detected: transition '%s' occurred %d times", transition, count),
				Details: map[string]interface{}{
					"transition": transition,
					"count":      count,
				},
				DetectedAt: time.Now(),
				Resolved:   false,
			}
			inconsistencies = append(inconsistencies, inc)
		}
	}
	
	return inconsistencies, nil
}

// GenerateSelfReflectionTask generates a self-reflection task for the reasoning engine
func (cm *CoherenceMonitor) GenerateSelfReflectionTask(inconsistency Inconsistency) (*SelfReflectionTask, error) {
	// Create a reflection task description
	description := fmt.Sprintf("Resolve inconsistency: %s - %s", inconsistency.Type, inconsistency.Description)
	
	// Determine priority based on severity
	priority := 5 // default
	switch inconsistency.Severity {
	case "critical":
		priority = 10
	case "high":
		priority = 8
	case "medium":
		priority = 6
	case "low":
		priority = 4
	}
	
	task := &SelfReflectionTask{
		ID:            fmt.Sprintf("reflection_%d", time.Now().UnixNano()),
		Inconsistency: inconsistency.ID,
		Description:   description,
		Priority:      priority,
		Status:        "pending",
		CreatedAt:     time.Now(),
		Metadata: map[string]interface{}{
			"inconsistency_type": inconsistency.Type,
			"severity":           inconsistency.Severity,
			"details":            inconsistency.Details,
		},
	}
	
	// Store task in Redis
	taskKey := fmt.Sprintf("coherence:reflection_tasks:%s", cm.agentID)
	taskData, err := json.Marshal(task)
	if err == nil {
		cm.redis.LPush(cm.ctx, taskKey, taskData)
		cm.redis.LTrim(cm.ctx, taskKey, 0, 99) // Keep last 100 tasks
	}
	
	log.Printf("📝 [Coherence] Generated self-reflection task: %s (priority: %d)", task.ID, task.Priority)
	
	return task, nil
}

// ResolveInconsistency prompts the reasoning engine to resolve an inconsistency
// DEDUPLICATION: Only creates one resolution goal per inconsistency to prevent infinite loops
func (cm *CoherenceMonitor) ResolveInconsistency(inconsistency Inconsistency) error {
	log.Printf("🔧 [Coherence] Attempting to resolve inconsistency: %s", inconsistency.ID)
	
	// DEDUPLICATION CHECK: See if we already created a resolution goal for this inconsistency
	resolutionGoalKey := fmt.Sprintf("coherence:resolution_goal:%s", inconsistency.ID)
	if existingGoalID, err := cm.redis.Get(cm.ctx, resolutionGoalKey).Result(); err == nil && existingGoalID != "" {
		log.Printf("⏭️ [Coherence] Resolution goal already exists for inconsistency %s (ID: %s), skipping duplicate", 
			inconsistency.ID, existingGoalID)
		return nil
	}
	
	// DEDUPLICATION CHECK: Also check if this type of inconsistency was recently created (within 24 hours)
	// For behavior loops, use transition type as key instead of full details (which includes varying counts)
	// This prevents duplicate goals for the same transition even if count changes
	var recentInconsistencyKey string
	if inconsistency.Type == "behavior_loop" {
		// For behavior loops, deduplicate by transition only, ignoring count differences
		if transition, ok := inconsistency.Details["transition"].(string); ok {
			recentInconsistencyKey = fmt.Sprintf("coherence:recent_inconsistency:%s:transition:%s", 
				inconsistency.Type, transition)
		} else {
			recentInconsistencyKey = fmt.Sprintf("coherence:recent_inconsistency:%s_%s", 
				inconsistency.Type, fmt.Sprintf("%v", inconsistency.Details))
		}
	} else {
		// For other inconsistency types, use full details
		recentInconsistencyKey = fmt.Sprintf("coherence:recent_inconsistency:%s_%s", 
			inconsistency.Type, fmt.Sprintf("%v", inconsistency.Details))
	}
	
	if lastTime, err := cm.redis.Get(cm.ctx, recentInconsistencyKey).Result(); err == nil && lastTime != "" {
		log.Printf("⏭️ [Coherence] Similar inconsistency was resolved/detected recently, skipping duplicate creation")
		return nil
	}
	
	// Mark that we're creating a resolution for this inconsistency (TTL: 24 hours)
	goalID := fmt.Sprintf("coherence_resolution_%s", inconsistency.ID)
	cm.redis.Set(cm.ctx, resolutionGoalKey, goalID, 24*time.Hour)
	cm.redis.Set(cm.ctx, recentInconsistencyKey, time.Now().String(), 24*time.Hour)
	
	// Create a prompt for the reasoning engine
	prompt := fmt.Sprintf(`You have detected an inconsistency in the system:

Type: %s
Severity: %s
Description: %s
Details: %s

Please analyze this inconsistency and provide a resolution strategy. Consider:
1. What caused this inconsistency?
2. What are the conflicting elements?
3. How can they be reconciled?
4. What actions should be taken?

Provide a clear resolution plan.`, 
		inconsistency.Type,
		inconsistency.Severity,
		inconsistency.Description,
		formatDetails(inconsistency.Details))
	
	// Use reasoning engine to generate a resolution
	// This would typically call the reasoning engine's explanation or inference capabilities
	// For now, we'll store the prompt as a curiosity goal for the reasoning engine to process
	
	curiosityGoal := CuriosityGoal{
		ID:          goalID,
		Type:        "contradiction_resolution",
		Description: prompt,
		Domain:      "system_coherence",
		Priority:    cm.severityToPriority(inconsistency.Severity),
		Status:      "pending",
		CreatedAt:   time.Now(),
	}
	
	// Store as a curiosity goal for the reasoning engine
	curiosityGoalsKey := "reasoning:curiosity_goals:system_coherence"
	goalData, err := json.Marshal(curiosityGoal)
	if err == nil {
		cm.redis.LPush(cm.ctx, curiosityGoalsKey, goalData)
		cm.redis.LTrim(cm.ctx, curiosityGoalsKey, 0, 199)
	}
	
	// Store mapping: curiosity_goal_id -> inconsistency_id for later resolution tracking
	// This allows us to mark the inconsistency as resolved when the goal completes
	mappingKey := fmt.Sprintf("coherence:goal_mapping:%s", curiosityGoal.ID)
	cm.redis.Set(cm.ctx, mappingKey, inconsistency.ID, 7*24*time.Hour) // Expire after 7 days
	
	// Mark inconsistency as being resolved
	inconsistency.Resolved = false // Will be set to true when resolution is confirmed
	cm.storeInconsistency(inconsistency)
	
	log.Printf("✅ [Coherence] Generated resolution task for inconsistency: %s (goal ID: %s)", inconsistency.ID, goalID)
	
	return nil
}

// Helper methods

func (cm *CoherenceMonitor) isContradictory(stmt1, stmt2 string) bool {
	// Simple contradiction detection: check for opposite keywords
	opposites := map[string][]string{
		"true":  {"false", "not true", "incorrect"},
		"false": {"true", "correct"},
		"always": {"never", "not always"},
		"never": {"always", "sometimes"},
		"increase": {"decrease", "reduce"},
		"decrease": {"increase", "raise"},
	}
	
	stmt1Lower := strings.ToLower(stmt1)
	stmt2Lower := strings.ToLower(stmt2)
	
	for word, opposites := range opposites {
		if strings.Contains(stmt1Lower, word) {
			for _, opposite := range opposites {
				if strings.Contains(stmt2Lower, opposite) {
					return true
				}
			}
		}
	}
	
	return false
}

func (cm *CoherenceMonitor) calculateSeverity(b1, b2 Belief) string {
	// Higher confidence contradictions are more severe
	avgConf := (b1.Confidence + b2.Confidence) / 2
	if avgConf >= 0.8 {
		return "high"
	} else if avgConf >= 0.6 {
		return "medium"
	}
	return "low"
}

func (cm *CoherenceMonitor) areGoalsConflicting(g1, g2 selfmodel.PolicyGoal) bool {
	// Check if goals have opposite objectives
	desc1 := strings.ToLower(g1.Description)
	desc2 := strings.ToLower(g2.Description)
	
	// Simple conflict detection: opposite action words
	conflicts := [][]string{
		{"increase", "decrease", "reduce"},
		{"maximize", "minimize"},
		{"enable", "disable", "prevent"},
		{"allow", "forbid", "block"},
	}
	
	for _, conflictPair := range conflicts {
		has1 := false
		has2 := false
		for _, word := range conflictPair {
			if strings.Contains(desc1, word) {
				has1 = true
			}
			if strings.Contains(desc2, word) {
				has2 = true
			}
		}
		if has1 && has2 && desc1 != desc2 {
			return true
		}
	}
	
	return false
}

func (cm *CoherenceMonitor) areStrategiesConflicting(s1, s2 map[string]interface{}) bool {
	// Check if strategies have opposite approaches for the same task category
	// This is a simplified check - in practice, would need more sophisticated analysis
	return false // Placeholder - would need actual strategy comparison logic
}

func (cm *CoherenceMonitor) severityToPriority(severity string) int {
	switch severity {
	case "critical":
		return 10
	case "high":
		return 8
	case "medium":
		return 6
	case "low":
		return 4
	default:
		return 5
	}
}

func (cm *CoherenceMonitor) storeInconsistency(inc Inconsistency) {
	key := fmt.Sprintf("coherence:inconsistencies:%s", cm.agentID)
	incData, err := json.Marshal(inc)
	if err == nil {
		cm.redis.LPush(cm.ctx, key, incData)
		cm.redis.LTrim(cm.ctx, key, 0, 199) // Keep last 200 inconsistencies
		
		// Also store by type for easier querying
		typeKey := fmt.Sprintf("coherence:inconsistencies:%s:%s", cm.agentID, inc.Type)
		cm.redis.LPush(cm.ctx, typeKey, incData)
		cm.redis.LTrim(cm.ctx, typeKey, 0, 99)
	}
}

func formatDetails(details map[string]interface{}) string {
	// Format details as a readable string
	var parts []string
	for k, v := range details {
		parts = append(parts, fmt.Sprintf("%s: %v", k, v))
	}
	return strings.Join(parts, ", ")
}

// subscribeToGoalEvents subscribes to Goal Manager events to mark inconsistencies as resolved
func (cm *CoherenceMonitor) subscribeToGoalEvents() {
	if cm.nc == nil {
		return
	}
	
	// Subscribe to goal achieved events
	_, err := cm.nc.Subscribe("agi.goal.achieved", func(msg *nats.Msg) {
		var goal map[string]interface{}
		if err := json.Unmarshal(msg.Data, &goal); err != nil {
			log.Printf("⚠️ [Coherence] Failed to unmarshal goal.achieved event: %v", err)
			return
		}
		
		goalID, _ := goal["id"].(string)
		log.Printf("🔔 [Coherence] Received goal.achieved event for goal: %s", goalID)
		
		// Check if this is a coherence resolution goal
		// Method 1: Check context if available
		if context, ok := goal["context"].(map[string]interface{}); ok {
			log.Printf("🔔 [Coherence] Goal %s has context: %+v", goalID, context)
			if source, ok := context["source"].(string); ok && source == "curiosity_goal" {
				if domain, ok := context["domain"].(string); ok && domain == "system_coherence" {
					if curiosityID, ok := context["curiosity_id"].(string); ok {
						log.Printf("✅ [Coherence] Matched coherence goal via context: curiosity_id=%s, goal_id=%s", curiosityID, goalID)
						cm.handleCoherenceGoalCompleted(curiosityID, goalID, "achieved")
						return
					}
				}
			}
		}
		
		// Method 2: Check description for coherence resolution pattern
		if desc, ok := goal["description"].(string); ok {
			if strings.Contains(desc, "You have detected an inconsistency in the system") {
				log.Printf("✅ [Coherence] Matched coherence goal via description pattern: goal_id=%s", goalID)
				// This is a coherence resolution goal - find the curiosity goal ID from mapping
				// Search all mappings to find which curiosity goal this belongs to
				cm.findAndHandleCoherenceGoal(goalID, "achieved")
			}
		}
	})
	if err != nil {
		log.Printf("⚠️ [Coherence] Failed to subscribe to goal.achieved events: %v", err)
	}
	
	// Subscribe to goal failed events
	_, err = cm.nc.Subscribe("agi.goal.failed", func(msg *nats.Msg) {
		var goal map[string]interface{}
		if err := json.Unmarshal(msg.Data, &goal); err != nil {
			log.Printf("⚠️ [Coherence] Failed to unmarshal goal.failed event: %v", err)
			return
		}
		
		goalID, _ := goal["id"].(string)
		log.Printf("🔔 [Coherence] Received goal.failed event for goal: %s", goalID)
		
		// Check if this is a coherence resolution goal
		// Method 1: Check context if available
		if context, ok := goal["context"].(map[string]interface{}); ok {
			log.Printf("🔔 [Coherence] Goal %s has context: %+v", goalID, context)
			if source, ok := context["source"].(string); ok && source == "curiosity_goal" {
				if domain, ok := context["domain"].(string); ok && domain == "system_coherence" {
					if curiosityID, ok := context["curiosity_id"].(string); ok {
						log.Printf("✅ [Coherence] Matched coherence goal via context: curiosity_id=%s, goal_id=%s", curiosityID, goalID)
						cm.handleCoherenceGoalCompleted(curiosityID, goalID, "failed")
						return
					}
				}
			}
		}
		
		// Method 2: Check description for coherence resolution pattern
		if desc, ok := goal["description"].(string); ok {
			if strings.Contains(desc, "You have detected an inconsistency in the system") {
				log.Printf("✅ [Coherence] Matched coherence goal via description pattern: goal_id=%s", goalID)
				cm.findAndHandleCoherenceGoal(goalID, "failed")
			}
		}
	})
	if err != nil {
		log.Printf("⚠️ [Coherence] Failed to subscribe to goal.failed events: %v", err)
	}
}

// findAndHandleCoherenceGoal searches for a coherence goal mapping by checking all curiosity goals
func (cm *CoherenceMonitor) findAndHandleCoherenceGoal(goalManagerID, status string) {
	// Get all coherence curiosity goals and check their mappings
	key := "reasoning:curiosity_goals:system_coherence"
	goals, err := cm.redis.LRange(cm.ctx, key, 0, 199).Result()
	if err != nil {
		return
	}
	
	for _, goalData := range goals {
		var goal CuriosityGoal
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			// Match by description pattern - coherence resolution goals have this pattern
			if goal.Status == "pending" && strings.Contains(goal.Description, "You have detected an inconsistency") {
				cm.handleCoherenceGoalCompleted(goal.ID, goalManagerID, status)
				break
			}
		}
	}
}

// handleCoherenceGoalCompleted marks the inconsistency and curiosity goal as resolved
func (cm *CoherenceMonitor) handleCoherenceGoalCompleted(curiosityGoalID, goalManagerID, status string) {
	log.Printf("✅ [Coherence] Coherence resolution goal %s completed with status: %s", curiosityGoalID, status)
	
	// Look up inconsistency ID from mapping
	mappingKey := fmt.Sprintf("coherence:goal_mapping:%s", curiosityGoalID)
	inconsistencyID, err := cm.redis.Get(cm.ctx, mappingKey).Result()
	if err != nil {
		// Fallback: try to extract from goal ID format
		if strings.HasPrefix(curiosityGoalID, "coherence_resolution_") {
			inconsistencyID = strings.TrimPrefix(curiosityGoalID, "coherence_resolution_")
		} else {
			log.Printf("⚠️ [Coherence] Could not find inconsistency ID for goal %s", curiosityGoalID)
			return
		}
	}
	
	// Mark inconsistency as resolved
	cm.markInconsistencyResolved(inconsistencyID, status)
	
	// Update curiosity goal status
	cm.updateCuriosityGoalStatus(curiosityGoalID, status)
	
	// Clean up mapping
	cm.redis.Del(cm.ctx, mappingKey)
}

// markInconsistencyResolved marks an inconsistency as resolved
func (cm *CoherenceMonitor) markInconsistencyResolved(inconsistencyID, resolutionStatus string) {
	key := fmt.Sprintf("coherence:inconsistencies:%s", cm.agentID)
	inconsistencies, err := cm.redis.LRange(cm.ctx, key, 0, 199).Result()
	if err != nil {
		return
	}
	
	for i, incData := range inconsistencies {
		var inc Inconsistency
		if err := json.Unmarshal([]byte(incData), &inc); err == nil {
			if inc.ID == inconsistencyID {
				inc.Resolved = true
				inc.Resolution = fmt.Sprintf("Resolved via Goal Manager task (status: %s)", resolutionStatus)
				
				updatedData, err := json.Marshal(inc)
				if err == nil {
					cm.redis.LSet(cm.ctx, key, int64(i), updatedData)
					log.Printf("✅ [Coherence] Marked inconsistency %s as resolved", inconsistencyID)
				}
				break
			}
		}
	}
}

// updateCuriosityGoalStatus updates the status of a curiosity goal
func (cm *CoherenceMonitor) updateCuriosityGoalStatus(goalID, status string) {
	key := "reasoning:curiosity_goals:system_coherence"
	goals, err := cm.redis.LRange(cm.ctx, key, 0, 199).Result()
	if err != nil {
		return
	}
	
	for i, goalData := range goals {
		var goal CuriosityGoal
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			if goal.ID == goalID {
				goal.Status = status
				updatedData, err := json.Marshal(goal)
				if err == nil {
					cm.redis.LSet(cm.ctx, key, int64(i), updatedData)
					log.Printf("✅ [Coherence] Updated curiosity goal %s status to %s", goalID, status)
				}
				break
			}
		}
	}
}

// cleanupOldCoherenceGoals removes old/completed coherence goals (similar to ReasoningEngine.cleanupOldGoals)
func (cm *CoherenceMonitor) cleanupOldCoherenceGoals() {
	key := "reasoning:curiosity_goals:system_coherence"
	goalsData, err := cm.redis.LRange(cm.ctx, key, 0, 199).Result()
	if err != nil {
		return
	}
	
	var activeGoals []string
	cutoffTime := time.Now().Add(-7 * 24 * time.Hour) // Remove goals older than 7 days
	
	for _, goalData := range goalsData {
		var goal CuriosityGoal
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			// Keep goals that are not completed/failed and not too old
			shouldKeep := goal.Status != "completed" &&
				goal.Status != "failed" &&
				goal.Status != "resolved" &&
				goal.CreatedAt.After(cutoffTime)
			
			if shouldKeep {
				activeGoals = append(activeGoals, goalData)
			}
		}
	}
	
	// Replace the list with only active goals
	if len(activeGoals) < len(goalsData) {
		cm.redis.Del(cm.ctx, key)
		for _, goalData := range activeGoals {
			cm.redis.LPush(cm.ctx, key, goalData)
		}
		log.Printf("🧹 [Coherence] Cleaned up %d old/completed coherence goals, kept %d active",
			len(goalsData)-len(activeGoals), len(activeGoals))
	}
}

