package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// postJSONWithTimeout posts a JSON payload with a specified timeout and returns error if non-2xx
func postJSONWithTimeout(target string, body []byte, timeout time.Duration) error {
	// Retry with simple exponential backoff for transient errors
	maxAttempts := 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, _ := http.NewRequest("POST", target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: timeout}
		resp, err := client.Do(req)
		if err == nil && resp != nil {
			io.Copy(io.Discard, resp.Body)
			code := resp.StatusCode
			resp.Body.Close()
			if code >= 200 && code < 300 {
				return nil
			}
			lastErr = fmt.Errorf("non-2xx status: %d", code)
		} else if err != nil {
			lastErr = err
		}
		// Backoff: 1s, 2s
		if attempt < maxAttempts {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("⚠️ post %s attempt %d failed: %v (retrying in %s)", target, attempt, lastErr, backoff)
			time.Sleep(backoff)
		}
	}
	return lastErr
}

// TriggerAutonomyCycle runs a minimal self-directed reasoning cycle:
// - Generates curiosity goals for the current domain
// - Selects the highest-priority goal (first)
// - Updates context and emits a curiosity_goals_generated event
func (e *FSMEngine) TriggerAutonomyCycle() {
	log.Printf("🤖 [Autonomy] Triggering autonomy cycle")
	// Pause guard: allow manual NL Execute/goal execution to temporarily suspend autonomy
	if paused, err := e.redis.Get(e.ctx, "auto_executor:paused").Result(); err == nil && strings.TrimSpace(paused) == "1" {
		log.Printf("[Autonomy] Paused by Redis flag; skipping cycle")
		return
	}
	domain := e.getCurrentDomain()
	goals, err := e.reasoning.GenerateCuriosityGoals(domain)
	if err != nil {
		log.Printf("[Autonomy] Failed to generate curiosity goals: %v", err)
		return
	}
	if len(goals) == 0 {
		// Fallback: use Anchor Goals (from Redis) to seed a goal
		log.Printf("[Autonomy] No curiosity goals from reasoning; falling back to Anchor Goals")
		anchorsData, _ := e.redis.LRange(e.ctx, "reasoning:anchors:all", 0, -1).Result()
		if len(anchorsData) > 0 {
			var a map[string]interface{}
			_ = json.Unmarshal([]byte(anchorsData[0]), &a)
			desc := "Pursue anchor curiosity"
			if d, ok := a["description"].(string); ok && d != "" {
				desc = d
			}
			goals = append(goals, CuriosityGoal{
				ID:          fmt.Sprintf("anchor_%d", time.Now().UnixNano()),
				Type:        "anchor_curiosity",
				Description: desc,
				Domain:      domain,
				Priority:    9,
				Status:      "pending",
				Targets:     []string{},
				CreatedAt:   time.Now(),
			})
		}
	}
	if len(goals) == 0 {
		log.Printf("[Autonomy] No goals available after anchor fallback")
		return
	}
	// Check if we're already processing too many goals
	if e.isProcessingCapacityFull(domain) {
		log.Printf("[Autonomy] Processing capacity full, skipping goal selection")
		return
	}

	// Check if we're already testing too many hypotheses
	if e.isHypothesisTestingCapacityFull(domain) {
		log.Printf("[Autonomy] Hypothesis testing capacity full, skipping hypothesis testing goals")
		// Filter out hypothesis testing goals
		var filteredGoals []CuriosityGoal
		for _, goal := range goals {
			if goal.Type != "hypothesis_testing" {
				filteredGoals = append(filteredGoals, goal)
			}
		}
		goals = filteredGoals
		if len(goals) == 0 {
			log.Printf("[Autonomy] No non-hypothesis goals available")
			return
		}
	}

	// Intelligent goal selection with prioritization
	selected := e.selectBestGoal(goals, domain)
	e.context["curiosity_goals"] = goals
	e.context["curiosity_goal_count"] = len(goals)
	// Persist curiosity goals for Monitor UI with deduplication
	{
		key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)

		// Get existing goals for deduplication
		existingGoalsData, err := e.redis.LRange(e.ctx, key, 0, 199).Result()
		if err != nil {
			log.Printf("Warning: Failed to get existing goals for deduplication: %v", err)
		}

		// Parse existing goals
		existingGoals := make(map[string]CuriosityGoal)
		for _, goalData := range existingGoalsData {
			var goal CuriosityGoal
			if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
				// Create a key for deduplication based on type and target
				dedupKey := e.createDedupKey(goal)
				existingGoals[dedupKey] = goal
			}
		}

		// Add only new goals (deduplicated)
		newGoalsCount := 0
		for _, g := range goals {
			dedupKey := e.createDedupKey(g)
			if _, exists := existingGoals[dedupKey]; !exists {
				b, _ := json.Marshal(g)
				_ = e.redis.LPush(e.ctx, key, b).Err()
				existingGoals[dedupKey] = g
				newGoalsCount++
			}
		}

		_ = e.redis.LTrim(e.ctx, key, 0, 199)
		log.Printf("Added %d new goals (deduplicated from %d generated)", newGoalsCount, len(goals))
	}
	e.context["current_goal"] = selected.Description
	log.Printf("[Autonomy] Selected curiosity goal: %s (type=%s, priority=%d)", selected.Description, selected.Type, selected.Priority)

	// Mark the selected goal as active
	selected.Status = "active"
	e.updateGoalStatus(selected)
	// Enforce single-active: deactivate any other active goals in this domain
	{
		key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
		goalsData, err := e.redis.LRange(e.ctx, key, 0, 199).Result()
		if err == nil {
			for i, gd := range goalsData {
				var g CuriosityGoal
				if err := json.Unmarshal([]byte(gd), &g); err == nil {
					if g.Status == "active" && g.ID != selected.ID {
						g.Status = "pending"
						if b, err := json.Marshal(g); err == nil {
							e.redis.LSet(e.ctx, key, int64(i), b)
						}
					}
				}
			}
		}
	}

	// Update live thinking indicator
	_ = e.redis.Set(e.ctx, "reasoning:now", fmt.Sprintf("Exploring goal: %s", selected.Description), 2*time.Minute).Err()
	// Emit goal_selected so FSM can advance per YAML (no payload required)
	go func() { e.handleEvent("goal_selected", nil) }()

	// Act on the selected goal immediately by invoking knowledge bootstrap (if suitable), then reasoning actions
	// Check if this is a hypothesis testing goal
	if selected.Type == "hypothesis_testing" {
		// Handle hypothesis testing goal
		if len(selected.Targets) > 0 {
			hypothesisID := selected.Targets[0]
			e.context["current_hypothesis_id"] = hypothesisID
			log.Printf("[Autonomy] Testing hypothesis: %s", hypothesisID)

			// Emit hypothesis testing event
			go func() {
				time.Sleep(100 * time.Millisecond)
				eventData := map[string]interface{}{
					"hypothesis_id": hypothesisID,
				}
				data, _ := json.Marshal(eventData)
				e.handleEvent("hypothesis_testing_requested", data)
			}()
		}
		return
	}

	// 1) Belief query: aim at the first target if present, otherwise use the goal description
	targetQuery := selected.Description
	if len(selected.Targets) > 0 && strings.TrimSpace(selected.Targets[0]) != "" {
		targetQuery = fmt.Sprintf("related to %s", selected.Targets[0])
	}

	// Attempt autonomous Wikipedia bootstrap for gap-filling / exploration
	if selected.Type == "gap_filling" || selected.Type == "concept_exploration" {
		// Env-driven overrides to make bootstrap configurable
		// FSM_BOOTSTRAP_SEEDS: CSV list; if empty, derive from selected goal
		// FSM_BOOTSTRAP_MAX_DEPTH: integer
		// FSM_BOOTSTRAP_MAX_NODES: integer
		envSeeds := strings.TrimSpace(os.Getenv("FSM_BOOTSTRAP_SEEDS"))
		seeds := []string{}
		if envSeeds != "" {
			for _, s := range strings.Split(envSeeds, ",") {
				t := strings.TrimSpace(s)
				if t != "" {
					seeds = append(seeds, t)
				}
			}
		} else {
			seed := ""
			if len(selected.Targets) > 0 && strings.TrimSpace(selected.Targets[0]) != "" {
				seed = selected.Targets[0]
			} else {
				// crude parse: expect "concept: X" in description
				lower := strings.ToLower(selected.Description)
				idx := strings.Index(lower, "concept:")
				if idx >= 0 {
					seed = strings.TrimSpace(selected.Description[idx+len("concept:"):])
				}
			}
			if strings.TrimSpace(seed) != "" {
				seeds = []string{seed}
			}
		}

		// Depth selection: env override or heuristic
		depth := 1
		if v := strings.TrimSpace(os.Getenv("FSM_BOOTSTRAP_MAX_DEPTH")); v != "" {
			if d, err := strconv.Atoi(v); err == nil && d > 0 {
				depth = d
			}
		} else {
			emptyKey := fmt.Sprintf("autonomy:empty_cycles:%s", strings.ToLower(domain))
			emptyCycles := 0
			if v, err := e.redis.Get(e.ctx, emptyKey).Int(); err == nil {
				emptyCycles = v
			}
			if emptyCycles >= 2 {
				depth = 2
			}
		}

		// Nodes selection: env override or default 100
		maxNodes := 100
		if v := strings.TrimSpace(os.Getenv("FSM_BOOTSTRAP_MAX_NODES")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				maxNodes = n
			}
		}

		// Throughput (rpm) and batching controls
		rpm := 12
		if v := strings.TrimSpace(os.Getenv("FSM_BOOTSTRAP_RPM")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				rpm = n
			}
		}

		// Optional: limit number of seeds processed per cycle
		if v := strings.TrimSpace(os.Getenv("FSM_BOOTSTRAP_SEED_BATCH")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n < len(seeds) {
				seeds = seeds[:n]
			}
		}

		// Cooldown duration (hours). If <=0, cooldown is disabled
		cooldownHours := 24
		if v := strings.TrimSpace(os.Getenv("FSM_BOOTSTRAP_COOLDOWN_HOURS")); v != "" {
			if h, err := strconv.Atoi(v); err == nil {
				cooldownHours = h
			}
		}

		// Iterate seeds and invoke tool
		for _, seed := range seeds {
			if strings.TrimSpace(seed) == "" {
				continue
			}
			seedsSetKey := fmt.Sprintf("autonomy:bootstrap:seeds:%s", strings.ToLower(domain))
			cooldownKey := fmt.Sprintf("autonomy:bootstrap:cooldown:%s", strings.ToLower(seed))
			if cooldownHours > 0 && (e.redis.SIsMember(e.ctx, seedsSetKey, seed).Val() || e.redis.Exists(e.ctx, cooldownKey).Val() == 1) {
				log.Printf("[Autonomy] Skipping bootstrap for '%s' (cooldown/seen)", seed)
				continue
			}

			payload := map[string]interface{}{
				"seeds":          seed,
				"max_depth":      depth,
				"max_nodes":      maxNodes,
				"rpm":            rpm,
				"domain":         domain,
				"jitter_ms":      250,
				"min_confidence": 0.5,
			}
			data, _ := json.Marshal(payload)
			toolURL := strings.TrimRight(e.reasoning.hdnURL, "/") + "/api/v1/tools/tool_wiki_bootstrapper/invoke"
			req, _ := http.NewRequest("POST", toolURL, bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{Timeout: 120 * time.Second}
			if resp, err := client.Do(req); err != nil {
				log.Printf("[Autonomy] Wiki bootstrapper invoke failed: %v", err)
			} else {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				log.Printf("[Autonomy] Wiki bootstrapper invoked for seed '%s' (status=%d)", seed, resp.StatusCode)
				_ = e.redis.SAdd(e.ctx, seedsSetKey, seed).Err()
				if cooldownHours > 0 {
					_ = e.redis.Set(e.ctx, cooldownKey, "1", time.Duration(cooldownHours)*time.Hour).Err()
				}

				// Mark the goal as completed since bootstrap was successful
				selected.Status = "completed"
				e.updateGoalStatus(selected)
				e.trackRecentlyProcessedGoal(selected, domain)
				log.Printf("[Autonomy] Marked goal %s as completed", selected.ID)

				sessionID := fmt.Sprintf("autonomy_%s", strings.ReplaceAll(strings.ToLower(domain), " ", "_"))
				wmEvent := map[string]interface{}{
					"type":        "bootstrap",
					"seed":        seed,
					"status":      resp.StatusCode,
					"timestamp":   time.Now().UTC().Format(time.RFC3339),
					"description": fmt.Sprintf("Bootstrapped knowledge for '%s'", seed),
				}
				wmPayload, _ := json.Marshal(wmEvent)
				wmURL := strings.TrimRight(e.reasoning.hdnURL, "/") + "/api/v1/state/session/" + url.PathEscape(sessionID) + "/working_memory/event"
				_ = postJSONWithTimeout(wmURL, wmPayload, 15*time.Second)
				_ = e.redis.Set(e.ctx, "reasoning:now", fmt.Sprintf("Bootstrapped: %s", seed), 2*time.Minute).Err()

				intel := map[string]interface{}{
					"task_name":   "analyze_bootstrap",
					"description": fmt.Sprintf("Analyze and summarize newly bootstrapped concepts around %s", seed),
					"context": map[string]string{
						"session_id":         sessionID,
						"project_id":         "Goals",
						"prefer_traditional": "true",
					},
					"force_regenerate": true,
					"max_retries":      1,
				}
				intelData, _ := json.Marshal(intel)
				intelURL := strings.TrimRight(e.reasoning.hdnURL, "/") + "/api/v1/intelligent/execute"
				_ = postJSONWithTimeout(intelURL, intelData, 90*time.Second)
			}

			// Post-stitch obvious relations to increase graph connectivity for next cycles (best-effort)
			stitchCypher := fmt.Sprintf("MATCH (a:Concept),(b:Concept) WHERE a<>b AND a.domain='%s' AND b.domain='%s' AND size([t IN split(toLower(a.definition),' ') WHERE t IN split(toLower(b.definition),' ')]) >= 3 MERGE (a)-[:RELATED_TO]->(b)", domain, domain)
			stitchPayload := map[string]interface{}{"query": stitchCypher}
			stitchData, _ := json.Marshal(stitchPayload)
			stitchURL := strings.TrimRight(e.reasoning.hdnURL, "/") + "/api/v1/knowledge/query"
			if err := postJSONWithTimeout(stitchURL, stitchData, 30*time.Second); err != nil {
				log.Printf("[Autonomy] Post-stitch relations failed: %v", err)
			} else {
				log.Printf("[Autonomy] Post-stitch relations executed")
			}
		}
	}
	// Run belief query and persist any found beliefs in Redis for the Monitor UI
	beliefs, bErr := e.reasoning.QueryBeliefs(targetQuery, domain)
	if bErr != nil {
		log.Printf("[Autonomy] Belief query failed: %v", bErr)
	} else {
		e.context["beliefs"] = beliefs
		e.context["belief_count"] = len(beliefs)
		// persist to Redis keys that the monitor reads
		followupsStarted := 0
		maxFollowups := 3 // resource guard: cap follow-up analyses per cycle
		for _, bel := range beliefs {
			if data, err := json.Marshal(bel); err == nil {
				key := fmt.Sprintf("reasoning:beliefs:%s", domain)
				_ = e.redis.LPush(e.ctx, key, data).Err()
				_ = e.redis.LTrim(e.ctx, key, 0, 199).Err()
			}
			// Watermark and trigger follow-up analysis for new, high-confidence beliefs
			if bel.Confidence >= 0.6 {
				wmKey := fmt.Sprintf("autonomy:beliefs:seen:%s", strings.ToLower(domain))
				marker := bel.Statement
				if strings.TrimSpace(marker) == "" {
					marker = bel.ID
				}
				if strings.TrimSpace(marker) != "" && e.redis.SAdd(e.ctx, wmKey, marker).Val() == 1 {
					// Fire FSM event (best-effort)
					go func() { e.handleEvent("belief_new", nil) }()
					// Start intelligent analysis for this belief (best-effort)
					sessionID := fmt.Sprintf("autonomy_%s", strings.ReplaceAll(strings.ToLower(domain), " ", "_"))
					intel := map[string]interface{}{
						"task_name":   "analyze_belief",
						"description": fmt.Sprintf("Analyze and summarize belief: %s", marker),
						"context": map[string]string{
							"session_id":         sessionID,
							"project_id":         "Goals",
							"prefer_traditional": "true",
						},
						"force_regenerate": true,
						"max_retries":      1,
					}
					intelData, _ := json.Marshal(intel)
					intelURL := strings.TrimRight(e.reasoning.hdnURL, "/") + "/api/v1/intelligent/execute"
					_ = postJSONWithTimeout(intelURL, intelData, 60*time.Second)
					followupsStarted++
					if followupsStarted >= maxFollowups {
						// resource cap reached; stop starting more follow-ups this cycle
						break
					}
				}
			}
		}
		log.Printf("[Autonomy] Persisted %d beliefs for domain %s", len(beliefs), domain)
		// Track empty cycle count to adjust bootstrap depth
		emptyKey := fmt.Sprintf("autonomy:empty_cycles:%s", strings.ToLower(domain))
		if len(beliefs) == 0 {
			_ = e.redis.Incr(e.ctx, emptyKey).Err()
		} else {
			_ = e.redis.Del(e.ctx, emptyKey).Err()
		}
		// If none found, persist a minimal placeholder so UI shows progress
		if len(beliefs) == 0 {
			conf := 0.4
			if val, ok := e.context["last_bootstrap_ok"].(bool); ok && val {
				conf = 0.6
			}
			minimal := Belief{
				ID:          fmt.Sprintf("belief_%d", time.Now().UnixNano()),
				Statement:   fmt.Sprintf("Examined %s", targetQuery),
				Confidence:  conf,
				Source:      "autonomy.scan",
				Domain:      domain,
				CreatedAt:   time.Now(),
				LastUpdated: time.Now(),
			}
			if data, err := json.Marshal(minimal); err == nil {
				key := fmt.Sprintf("reasoning:beliefs:%s", domain)
				_ = e.redis.LPush(e.ctx, key, data).Err()
				_ = e.redis.LTrim(e.ctx, key, 0, 199).Err()
			}
		}
	}

	// 2) Inference pass
	// Inference pass and persist inferred beliefs
	inferred, iErr := e.reasoning.InferNewBeliefs(domain)
	if iErr != nil {
		log.Printf("[Autonomy] Inference failed: %v", iErr)
	} else {
		for _, bel := range inferred {
			if data, err := json.Marshal(bel); err == nil {
				key := fmt.Sprintf("reasoning:beliefs:%s", domain)
				_ = e.redis.LPush(e.ctx, key, data).Err()
				_ = e.redis.LTrim(e.ctx, key, 0, 199).Err()
			}
		}
		log.Printf("[Autonomy] Persisted %d inferred beliefs for domain %s", len(inferred), domain)
	}

	// Emit event so FSM can transition if configured
	go func() {
		time.Sleep(200 * time.Millisecond)
		// best-effort: no payload
		e.handleEvent("curiosity_goals_generated", nil)
	}()
}

// updateGoalStatus updates the status of a goal in Redis
func (e *FSMEngine) updateGoalStatus(goal CuriosityGoal) {
	domain := e.getCurrentDomain()
	key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)

	// Get all goals
	goalsData, err := e.redis.LRange(e.ctx, key, 0, 199).Result()
	if err != nil {
		log.Printf("Failed to get goals for status update: %v", err)
		return
	}

	// Find and update the specific goal
	for i, goalData := range goalsData {
		var existingGoal CuriosityGoal
		if err := json.Unmarshal([]byte(goalData), &existingGoal); err == nil {
			if existingGoal.ID == goal.ID {
				// Update the goal status
				existingGoal.Status = goal.Status
				updatedData, err := json.Marshal(existingGoal)
				if err == nil {
					// Replace the goal in the list
					e.redis.LSet(e.ctx, key, int64(i), updatedData)
					log.Printf("Updated goal %s status to %s", goal.ID, goal.Status)
				}
				break
			}
		}
	}
}

// selectBestGoal intelligently selects the most important goal to process
func (e *FSMEngine) selectBestGoal(goals []CuriosityGoal, domain string) CuriosityGoal {
	if len(goals) == 0 {
		return CuriosityGoal{}
	}

	// Score each goal based on multiple factors
	type scoredGoal struct {
		goal  CuriosityGoal
		score float64
	}

	var scoredGoals []scoredGoal
	for _, goal := range goals {
		score := e.calculateGoalScore(goal, domain)
		scoredGoals = append(scoredGoals, scoredGoal{goal: goal, score: score})
	}

	// Sort by score (highest first)
	for i := 0; i < len(scoredGoals)-1; i++ {
		for j := i + 1; j < len(scoredGoals); j++ {
			if scoredGoals[i].score < scoredGoals[j].score {
				scoredGoals[i], scoredGoals[j] = scoredGoals[j], scoredGoals[i]
			}
		}
	}

	// Select the highest scoring goal that passes cooldown checks
	for _, sg := range scoredGoals {
		if e.isGoalEligible(sg.goal, domain) {
			log.Printf("[Autonomy] Selected goal '%s' with score %.2f", sg.goal.Description, sg.score)
			return sg.goal
		}
	}

	// Fallback to first goal if none pass eligibility checks
	log.Printf("[Autonomy] No eligible goals found, using fallback")
	return goals[0]
}

// calculateGoalScore calculates a priority score for a goal
func (e *FSMEngine) calculateGoalScore(goal CuriosityGoal, domain string) float64 {
	score := float64(goal.Priority) // Base priority (1-10)

	// News analysis goals get bonus for recency and impact
	if goal.Type == "news_analysis" {
		score += 2.0 // News is time-sensitive

		// High impact news gets extra priority
		if strings.Contains(strings.ToLower(goal.Description), "high") {
			score += 3.0
		} else if strings.Contains(strings.ToLower(goal.Description), "medium") {
			score += 1.5
		}

		// Recent news gets higher priority
		age := time.Since(goal.CreatedAt)
		if age < 1*time.Hour {
			score += 2.0
		} else if age < 6*time.Hour {
			score += 1.0
		}
	}

	// Gap filling goals get bonus for important concepts
	if goal.Type == "gap_filling" && len(goal.Targets) > 0 {
		concept := strings.ToLower(goal.Targets[0])

		// Important technical concepts get higher priority
		importantConcepts := []string{"ai", "machine learning", "neural", "algorithm", "data", "security", "cryptography", "blockchain", "quantum"}
		for _, important := range importantConcepts {
			if strings.Contains(concept, important) {
				score += 2.0
				break
			}
		}

		// Avoid very generic concepts
		genericConcepts := []string{"thing", "stuff", "item", "object", "concept"}
		for _, generic := range genericConcepts {
			if strings.Contains(concept, generic) {
				score -= 1.0
				break
			}
		}
	}

	// Contradiction resolution is always important
	if goal.Type == "contradiction_resolution" {
		score += 1.5
	}

	// Penalize very old goals (aging)
	age := time.Since(goal.CreatedAt)
	if age > 24*time.Hour {
		score -= 2.0
	} else if age > 12*time.Hour {
		score -= 1.0
	}

	// Penalize goals that have been tried recently
	if e.hasGoalBeenTriedRecently(goal, domain) {
		score -= 1.5
	}

	return score
}

// isGoalEligible checks if a goal is eligible for processing (cooldown, etc.)
func (e *FSMEngine) isGoalEligible(goal CuriosityGoal, domain string) bool {
	// Check cooldown for gap filling and exploration goals
	if goal.Type == "gap_filling" || goal.Type == "concept_exploration" {
		seed := ""
		if len(goal.Targets) > 0 && strings.TrimSpace(goal.Targets[0]) != "" {
			seed = goal.Targets[0]
		} else {
			lower := strings.ToLower(goal.Description)
			idx := strings.Index(lower, "concept:")
			if idx >= 0 {
				seed = strings.TrimSpace(goal.Description[idx+len("concept:"):])
			}
		}
		if seed != "" {
			seedsSetKey := fmt.Sprintf("autonomy:bootstrap:seeds:%s", strings.ToLower(domain))
			cooldownKey := fmt.Sprintf("autonomy:bootstrap:cooldown:%s", strings.ToLower(seed))
			if e.redis.SIsMember(e.ctx, seedsSetKey, seed).Val() || e.redis.Exists(e.ctx, cooldownKey).Val() == 1 {
				return false
			}
		}
	}

	return true
}

// hasGoalBeenTriedRecently checks if a goal has been attempted recently
func (e *FSMEngine) hasGoalBeenTriedRecently(goal CuriosityGoal, domain string) bool {
	// Check if goal was recently processed (within last 2 hours)
	recentKey := fmt.Sprintf("autonomy:recent_goals:%s", domain)
	goalHash := fmt.Sprintf("%x", sha256.Sum256([]byte(goal.Description)))
	return e.redis.SIsMember(e.ctx, recentKey, goalHash).Val()
}

// trackRecentlyProcessedGoal marks a goal as recently processed to avoid immediate re-processing
func (e *FSMEngine) trackRecentlyProcessedGoal(goal CuriosityGoal, domain string) {
	recentKey := fmt.Sprintf("autonomy:recent_goals:%s", domain)
	goalHash := fmt.Sprintf("%x", sha256.Sum256([]byte(goal.Description)))

	// Add to recent goals set with 2-hour expiration
	e.redis.SAdd(e.ctx, recentKey, goalHash)
	e.redis.Expire(e.ctx, recentKey, 2*time.Hour)
}

// isProcessingCapacityFull checks if we're already processing too many goals
func (e *FSMEngine) isProcessingCapacityFull(domain string) bool {
	// Check active goals count
	activeKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	goalsData, err := e.redis.LRange(e.ctx, activeKey, 0, 199).Result()
	if err != nil {
		return false
	}

	activeCount := 0
	for _, goalData := range goalsData {
		var goal CuriosityGoal
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			if goal.Status == "active" {
				activeCount++
			}
		}
	}

	// Limit concurrent active goals per domain (default 1, override via FSM_MAX_ACTIVE_GOALS)
	maxConcurrent := 1
	if v := strings.TrimSpace(os.Getenv("FSM_MAX_ACTIVE_GOALS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConcurrent = n
		}
	}
	if activeCount >= maxConcurrent {
		log.Printf("[Autonomy] %d active goals (limit: %d), capacity full", activeCount, maxConcurrent)
		return true
	}

	return false
}

// isHypothesisTestingCapacityFull checks if we're already testing too many hypotheses
func (e *FSMEngine) isHypothesisTestingCapacityFull(domain string) bool {
	// Check how many hypotheses are currently being tested
	key := fmt.Sprintf("fsm:%s:hypotheses", e.agentID)
	hypotheses, err := e.redis.HGetAll(e.ctx, key).Result()
	if err != nil {
		log.Printf("Warning: Failed to check hypothesis testing capacity: %v", err)
		return false
	}

	testingCount := 0
	for _, hypothesisData := range hypotheses {
		var hypothesis map[string]interface{}
		if err := json.Unmarshal([]byte(hypothesisData), &hypothesis); err == nil {
			if status, ok := hypothesis["status"].(string); ok && status == "testing" {
				testingCount++
			}
		}
	}

	// Limit concurrent hypothesis tests (default 1, override via FSM_MAX_CONCURRENT_HYP_TESTS)
	maxConcurrentTests := 1
	if v := strings.TrimSpace(os.Getenv("FSM_MAX_CONCURRENT_HYP_TESTS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConcurrentTests = n
		}
	}
	return testingCount >= maxConcurrentTests
}

// createDedupKey creates a unique key for goal deduplication
func (e *FSMEngine) createDedupKey(goal CuriosityGoal) string {
	// For gap filling goals, use type + first target
	if goal.Type == "gap_filling" && len(goal.Targets) > 0 {
		return fmt.Sprintf("%s:%s", goal.Type, goal.Targets[0])
	}

	// For hypothesis testing, use type + hypothesis ID (first target)
	if goal.Type == "hypothesis_testing" && len(goal.Targets) > 0 {
		return fmt.Sprintf("%s:%s", goal.Type, goal.Targets[0])
	}

	// For news goals, use type + description (which contains the specific news item)
	if goal.Type == "news_analysis" {
		return fmt.Sprintf("%s:%s", goal.Type, goal.Description)
	}

	// For other goals, use type + description
	return fmt.Sprintf("%s:%s", goal.Type, goal.Description)
}
