package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MetaLearning tracks learning about the learning process itself
type MetaLearning struct {
	// Which goal types are most valuable?
	GoalTypeValue map[string]float64 `json:"goal_type_value"`

	// Which domains are most productive?
	DomainProductivity map[string]float64 `json:"domain_productivity"`

	// What strategies work best?
	StrategySuccess map[string]float64 `json:"strategy_success"`

	// What patterns lead to success?
	SuccessPatterns []SuccessPattern `json:"success_patterns"`

	UpdatedAt time.Time `json:"updated_at"`
}

// SuccessPattern represents a pattern that leads to successful learning
type SuccessPattern struct {
	Pattern     string    `json:"pattern"`      // Description of the pattern
	GoalType    string    `json:"goal_type"`    // Associated goal type
	Domain      string    `json:"domain"`       // Associated domain
	SuccessRate float64   `json:"success_rate"` // Success rate for this pattern
	Value       float64   `json:"value"`        // Average value
	Count       int       `json:"count"`        // Number of times observed
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

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

	// Identify focus areas (promising domains/goal types)
	focusAreas := e.identifyFocusAreas(domain)
	if len(focusAreas) > 0 {
		log.Printf("🎯 Identified %d focus areas for domain %s", len(focusAreas), domain)
		for _, area := range focusAreas {
			log.Printf("   - %s/%s: success=%.2f, value=%.2f, focus_score=%.2f",
				area.Domain, area.GoalType, area.SuccessRate, area.AvgValue, area.FocusScore)
		}
	}

	goals, err := e.reasoning.GenerateCuriosityGoals(domain)
	if err != nil {
		log.Printf("[Autonomy] Failed to generate curiosity goals: %v", err)
		return
	}

	// 5. Generate hypothesis testing goals for existing untested hypotheses
	// This ensures hypotheses get tested even if they were created outside the current cycle
	hypTestingGoals, err := e.generateHypothesisTestingGoalsForExisting(domain)
	if err != nil {
		log.Printf("Warning: Failed to generate hypothesis testing goals for existing hypotheses: %v", err)
	} else if len(hypTestingGoals) > 0 {
		goals = append(goals, hypTestingGoals...)
		log.Printf("🎯 Added %d hypothesis testing goals for existing hypotheses", len(hypTestingGoals))
	}

	// 6. Load existing pending goals from Redis to include in selection
	// This ensures we consider goals created in previous cycles
	existingGoalsKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	existingGoalsData, err := e.redis.LRange(e.ctx, existingGoalsKey, 0, 199).Result()
	existingCount := 0
	if err == nil {
		for _, goalData := range existingGoalsData {
			var existingGoal CuriosityGoal
			if err := json.Unmarshal([]byte(goalData), &existingGoal); err == nil {
				// Only include pending goals (not active, completed, or failed)
				if existingGoal.Status == "pending" {
					// Check if we already have this goal in our array (deduplicate by ID)
					found := false
					for _, g := range goals {
						if g.ID == existingGoal.ID {
							found = true
							break
						}
					}
					if !found {
						goals = append(goals, existingGoal)
						existingCount++
					}
				}
			}
		}
		log.Printf("📋 Loaded existing pending goals from Redis key '%s', found: %d pending goals, total goals for selection: %d", existingGoalsKey, existingCount, len(goals))
	} else {
		log.Printf("⚠️ Failed to load existing goals from Redis key '%s': %v", existingGoalsKey, err)
	}

	// Adjust goal generation based on focus areas
	if len(focusAreas) > 0 {
		goals = e.adjustGoalGeneration(goals, focusAreas, domain)
		log.Printf("🎯 Adjusted goal generation: %d goals after focusing", len(goals))
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

	// Screen goals with LLM for usefulness (reduce GPU load by filtering early)
	// Only screen if enabled and we have goals to screen
	if len(goals) > 0 {
		screenedGoals := e.screenCuriosityGoalsWithLLM(goals, domain)
		if len(screenedGoals) < len(goals) {
			log.Printf("🎯 [GOAL-SCREEN] LLM screening filtered %d goals (kept %d of %d)", 
				len(goals)-len(screenedGoals), len(screenedGoals), len(goals))
		}
		goals = screenedGoals
		if len(goals) == 0 {
			log.Printf("[Autonomy] No goals passed LLM screening")
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

				if e.goalManager != nil {
					_ = e.goalManager.PostCuriosityGoal(g, "autonomy_generated")
				}
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
	// For knowledge_building goals, extract a meaningful query from the description
	targetQuery := selected.Description
	if len(selected.Targets) > 0 && strings.TrimSpace(selected.Targets[0]) != "" {
		targetQuery = fmt.Sprintf("related to %s", selected.Targets[0])
	} else if selected.Type == "knowledge_building" || selected.Type == "gap_filling" || selected.Type == "concept_exploration" {
		// For these goal types, use a simple query that won't cause Cypher syntax errors
		// Extract domain or use a generic query
		if strings.ToLower(strings.TrimSpace(domain)) == "general" {
			targetQuery = "all concepts"
		} else {
			targetQuery = fmt.Sprintf("concepts in %s", domain)
		}
	}

	// Attempt autonomous Wikipedia bootstrap for gap-filling / exploration / knowledge building
	if selected.Type == "gap_filling" || selected.Type == "concept_exploration" || selected.Type == "exploration" || selected.Type == "knowledge_building" {
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
				// For exploration/knowledge_building goals, extract domain or use a generic seed
				if selected.Type == "exploration" || selected.Type == "knowledge_building" {
					// If domain is too generic, use a common exploration seed
					if strings.ToLower(strings.TrimSpace(domain)) == "general" {
						seed = "artificial intelligence" // Default seed for General domain exploration
					} else {
						// Use domain name as seed for specific domains
						seed = domain
					}
				} else {
					// crude parse: expect "concept: X" in description
					lower := strings.ToLower(selected.Description)
					idx := strings.Index(lower, "concept:")
					if idx >= 0 {
						seed = strings.TrimSpace(selected.Description[idx+len("concept:"):])
					}
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
				"min_confidence": 0.7, // Increased from 0.5 to reduce low-quality bootstrapping
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

				// Check if analyze_bootstrap workflow already exists for this seed (deduplication)
				// Check both FSM workflows and HDN intelligent workflows
				shouldCreate := true
				seedLower := strings.ToLower(seed)
				
				// Check FSM workflows
				workflowKey := fmt.Sprintf("fsm:%s:workflows", e.agentID)
				existingWorkflows, err := e.redis.LRange(e.ctx, workflowKey, 0, 199).Result()
				if err == nil {
					for _, wfData := range existingWorkflows {
						var wf map[string]interface{}
						if err := json.Unmarshal([]byte(wfData), &wf); err == nil {
							if name, ok := wf["name"].(string); ok && strings.EqualFold(name, "analyze_bootstrap") {
								if desc, ok := wf["description"].(string); ok && strings.Contains(strings.ToLower(desc), seedLower) {
									// Check if workflow is recent (within last hour) or still running
									if status, ok := wf["status"].(string); ok && (status == "running" || status == "pending") {
										log.Printf("🚫 [Autonomy] Skipping duplicate analyze_bootstrap workflow for '%s' (FSM workflow exists with status: %s)", seed, status)
										shouldCreate = false
										break
									}
									// Check creation time - if created within last hour, skip
									if createdAt, ok := wf["created_at"].(string); ok {
										if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
											if time.Since(t) < 1*time.Hour {
												log.Printf("🚫 [Autonomy] Skipping duplicate analyze_bootstrap workflow for '%s' (FSM workflow created %v ago)", seed, time.Since(t))
												shouldCreate = false
												break
											}
										}
									}
								}
							}
						}
					}
				}
				
				// Also check HDN intelligent workflows (stored as workflow:intelligent_*)
				if shouldCreate {
					pattern := "workflow:intelligent_*"
					keys, err := e.redis.Keys(e.ctx, pattern).Result()
					if err == nil {
						for _, key := range keys {
							wfData, err := e.redis.Get(e.ctx, key).Result()
							if err == nil {
								var wf map[string]interface{}
								if err := json.Unmarshal([]byte(wfData), &wf); err == nil {
									if taskName, ok := wf["task_name"].(string); ok && strings.EqualFold(taskName, "analyze_bootstrap") {
										if desc, ok := wf["description"].(string); ok && strings.Contains(strings.ToLower(desc), seedLower) {
											// Check if workflow is recent or still running
											if status, ok := wf["status"].(string); ok && (status == "running" || status == "pending") {
												log.Printf("🚫 [Autonomy] Skipping duplicate analyze_bootstrap workflow for '%s' (HDN workflow exists with status: %s)", seed, status)
												shouldCreate = false
												break
											}
											// Check creation time
											if startedAt, ok := wf["started_at"].(string); ok {
												if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
													if time.Since(t) < 1*time.Hour {
														log.Printf("🚫 [Autonomy] Skipping duplicate analyze_bootstrap workflow for '%s' (HDN workflow started %v ago)", seed, time.Since(t))
														shouldCreate = false
														break
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}

				if shouldCreate {
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
					log.Printf("✅ [Autonomy] Created analyze_bootstrap workflow for '%s'", seed)
				} else {
					log.Printf("ℹ️ [Autonomy] Skipped creating analyze_bootstrap workflow for '%s' (duplicate)", seed)
				}
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
			// Increased threshold from 0.6 to 0.75 to reduce noise
			if bel.Confidence >= 0.75 {
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
			// Increased baseline from 0.4 to 0.5, successful bootstrap from 0.6 to 0.7
			conf := 0.5
			if val, ok := e.context["last_bootstrap_ok"].(bool); ok && val {
				conf = 0.7
			}
			// Create uncertainty model for minimal belief
			epistemicUncertainty := EstimateEpistemicUncertainty(0, false, false)
			aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "")
			uncertainty := NewUncertaintyModel(conf, epistemicUncertainty, aleatoricUncertainty)
			
			minimal := Belief{
				ID:          fmt.Sprintf("belief_%d", time.Now().UnixNano()),
				Statement:   fmt.Sprintf("Examined %s", targetQuery),
				Confidence:  uncertainty.CalibratedConfidence,
				Source:      "autonomy.scan",
				Domain:      domain,
				CreatedAt:   time.Now(),
				LastUpdated: time.Now(),
				Uncertainty: uncertainty,
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

	// Track previous status to detect status changes
	var previousStatus string

	// Find and update the specific goal
	for i, goalData := range goalsData {
		var existingGoal CuriosityGoal
		if err := json.Unmarshal([]byte(goalData), &existingGoal); err == nil {
			if existingGoal.ID == goal.ID {
				previousStatus = existingGoal.Status
				// Update the goal status
				existingGoal.Status = goal.Status
				updatedData, err := json.Marshal(existingGoal)
				if err == nil {
					// Replace the goal in the list
					e.redis.LSet(e.ctx, key, int64(i), updatedData)
					log.Printf("Updated goal %s status from %s to %s", goal.ID, previousStatus, goal.Status)
				}
				break
			}
		}
	}

	// Record outcome if goal was completed or failed
	if goal.Status == "completed" || goal.Status == "failed" {
		// Only record if status actually changed (avoid duplicate recordings)
		if previousStatus != goal.Status {
			success := goal.Status == "completed"
			// Extract value from execution results if available
			value := e.extractGoalValue(goal, success)
			outcomes := e.extractGoalOutcomes(goal)
			e.recordGoalOutcome(goal, success, value, outcomes)
		}
	}
}

// scoredCuriosityGoal keeps track of heuristic scoring for a goal
type scoredCuriosityGoal struct {
	Goal  CuriosityGoal
	Score float64
}

// updateMetaLearning updates meta-learning statistics based on goal outcome
func (e *FSMEngine) updateMetaLearning(outcome GoalOutcome) {
	// Load current meta-learning data
	metaKey := "meta_learning:all"
	metaData, err := e.redis.Get(e.ctx, metaKey).Result()

	var meta MetaLearning
	if err == nil && metaData != "" {
		if err := json.Unmarshal([]byte(metaData), &meta); err != nil {
			meta = MetaLearning{
				GoalTypeValue:      make(map[string]float64),
				DomainProductivity: make(map[string]float64),
				StrategySuccess:    make(map[string]float64),
				SuccessPatterns:    []SuccessPattern{},
			}
		}
	} else {
		meta = MetaLearning{
			GoalTypeValue:      make(map[string]float64),
			DomainProductivity: make(map[string]float64),
			StrategySuccess:    make(map[string]float64),
			SuccessPatterns:    []SuccessPattern{},
		}
	}

	// Update goal type value
	if outcome.Success {
		currentValue := meta.GoalTypeValue[outcome.GoalType]
		// Exponential moving average
		newValue := (currentValue * 0.7) + (outcome.Value * 0.3)
		meta.GoalTypeValue[outcome.GoalType] = newValue
	}

	// Update domain productivity
	if outcome.Success {
		currentProd := meta.DomainProductivity[outcome.Domain]
		// Productivity = success rate * average value
		successRate := e.getSuccessRate(outcome.GoalType, outcome.Domain)
		avgValue := e.getAverageValue(outcome.GoalType, outcome.Domain)
		productivity := successRate * avgValue
		newProd := (currentProd * 0.7) + (productivity * 0.3)
		meta.DomainProductivity[outcome.Domain] = newProd
	}

	// Update success patterns
	e.updateSuccessPatterns(&meta, outcome)

	// Update timestamp
	meta.UpdatedAt = time.Now()

	// Save meta-learning data
	metaJSON, err := json.Marshal(meta)
	if err == nil {
		e.redis.Set(e.ctx, metaKey, metaJSON, 0)
		log.Printf("🧠 Updated meta-learning: goal_type_value=%v, domain_productivity=%v",
			meta.GoalTypeValue, meta.DomainProductivity)
	}
}

// updateSuccessPatterns updates success patterns based on outcome
func (e *FSMEngine) updateSuccessPatterns(meta *MetaLearning, outcome GoalOutcome) {
	// Create pattern key from goal type and domain
	patternKey := fmt.Sprintf("%s:%s", outcome.GoalType, outcome.Domain)

	// Find existing pattern or create new one
	var pattern *SuccessPattern
	for i := range meta.SuccessPatterns {
		if meta.SuccessPatterns[i].Pattern == patternKey {
			pattern = &meta.SuccessPatterns[i]
			break
		}
	}

	if pattern == nil {
		// Create new pattern
		pattern = &SuccessPattern{
			Pattern:   patternKey,
			GoalType:  outcome.GoalType,
			Domain:    outcome.Domain,
			FirstSeen: time.Now(),
		}
		meta.SuccessPatterns = append(meta.SuccessPatterns, *pattern)
		pattern = &meta.SuccessPatterns[len(meta.SuccessPatterns)-1]
	}

	// Update pattern statistics
	pattern.Count++
	pattern.LastSeen = time.Now()

	// Update success rate (exponential moving average)
	if outcome.Success {
		pattern.SuccessRate = (pattern.SuccessRate * 0.7) + (1.0 * 0.3)
	} else {
		pattern.SuccessRate = (pattern.SuccessRate * 0.7) + (0.0 * 0.3)
	}

	// Update value (exponential moving average)
	pattern.Value = (pattern.Value * 0.7) + (outcome.Value * 0.3)

	// Keep only top 20 patterns
	if len(meta.SuccessPatterns) > 20 {
		// Sort by success rate * value
		sort.Slice(meta.SuccessPatterns, func(i, j int) bool {
			scoreI := meta.SuccessPatterns[i].SuccessRate * meta.SuccessPatterns[i].Value
			scoreJ := meta.SuccessPatterns[j].SuccessRate * meta.SuccessPatterns[j].Value
			return scoreI > scoreJ
		})
		meta.SuccessPatterns = meta.SuccessPatterns[:20]
	}
}

// getMetaLearning retrieves current meta-learning data
func (e *FSMEngine) getMetaLearning() *MetaLearning {
	metaKey := "meta_learning:all"
	metaData, err := e.redis.Get(e.ctx, metaKey).Result()

	if err != nil {
		return &MetaLearning{
			GoalTypeValue:      make(map[string]float64),
			DomainProductivity: make(map[string]float64),
			StrategySuccess:    make(map[string]float64),
			SuccessPatterns:    []SuccessPattern{},
		}
	}

	var meta MetaLearning
	if err := json.Unmarshal([]byte(metaData), &meta); err != nil {
		return &MetaLearning{
			GoalTypeValue:      make(map[string]float64),
			DomainProductivity: make(map[string]float64),
			StrategySuccess:    make(map[string]float64),
			SuccessPatterns:    []SuccessPattern{},
		}
	}

	return &meta
}

// getBestGoalType returns the goal type with highest value according to meta-learning
func (e *FSMEngine) getBestGoalType() string {
	meta := e.getMetaLearning()

	bestType := ""
	bestValue := 0.0

	for goalType, value := range meta.GoalTypeValue {
		if value > bestValue {
			bestValue = value
			bestType = goalType
		}
	}

	return bestType
}

// getMostProductiveDomain returns the domain with highest productivity
func (e *FSMEngine) getMostProductiveDomain() string {
	meta := e.getMetaLearning()

	bestDomain := ""
	bestProd := 0.0

	for domain, prod := range meta.DomainProductivity {
		if prod > bestProd {
			bestProd = prod
			bestDomain = domain
		}
	}

	return bestDomain
}

// recordGoalOutcome records the outcome of a goal execution for learning
func (e *FSMEngine) recordGoalOutcome(goal CuriosityGoal, success bool, value float64, outcomes []string) {
	domain := e.getCurrentDomain()
	outcome := GoalOutcome{
		GoalID:        goal.ID,
		GoalType:      goal.Type,
		Domain:        domain,
		Status:        goal.Status,
		Success:       success,
		Value:         value,
		ExecutionTime: 0, // Could be tracked if execution time is available
		Outcomes:      outcomes,
		CreatedAt:     time.Now(),
	}

	// Store outcome in Redis
	outcomeData, err := json.Marshal(outcome)
	if err != nil {
		log.Printf("⚠️ Failed to marshal goal outcome: %v", err)
		return
	}

	// Store by type and domain for easy querying
	outcomeKey := fmt.Sprintf("goal_outcomes:%s:%s", goal.Type, domain)
	e.redis.LPush(e.ctx, outcomeKey, outcomeData)
	e.redis.LTrim(e.ctx, outcomeKey, 0, 199) // Keep last 200 outcomes

	// Also store in general outcomes list
	generalKey := fmt.Sprintf("goal_outcomes:all")
	e.redis.LPush(e.ctx, generalKey, outcomeData)
	e.redis.LTrim(e.ctx, generalKey, 0, 999) // Keep last 1000 outcomes

	log.Printf("📊 Recorded goal outcome: %s (type=%s, success=%v, value=%.2f)", goal.ID, goal.Type, success, value)

	// Update success rate statistics
	e.updateSuccessRate(goal.Type, domain, success)

	// Update average value statistics
	e.updateAverageValue(goal.Type, domain, value)

	// Update meta-learning (reuse existing outcome variable)
	e.updateMetaLearning(outcome)
}

// extractGoalValue extracts a value score (0-1) from goal execution results
func (e *FSMEngine) extractGoalValue(goal CuriosityGoal, success bool) float64 {
	// Base value on success
	if !success {
		return 0.1 // Low value for failures
	}

	// Check if we have execution results in context
	if lastExec, ok := e.context["last_execution"].(map[string]interface{}); ok {
		// Try to extract value indicators from execution results
		if success, ok := lastExec["success"].(bool); ok && success {
			// High value if execution was successful
			return 0.8
		}
		// Check for other value indicators
		if result, ok := lastExec["result"]; ok && result != nil {
			return 0.7 // Medium-high value if we got results
		}
	}

	// Default value based on goal type
	switch goal.Type {
	case "news_analysis":
		return 0.6 // News analysis has moderate value
	case "gap_filling":
		return 0.7 // Gap filling has good value
	case "contradiction_resolution":
		return 0.8 // Contradiction resolution has high value
	case "concept_exploration":
		return 0.5 // Exploration has moderate value
	default:
		return 0.5 // Default moderate value
	}
}

// extractGoalOutcomes extracts what was learned/achieved from goal execution
func (e *FSMEngine) extractGoalOutcomes(goal CuriosityGoal) []string {
	var outcomes []string

	// Check execution results
	if lastExec, ok := e.context["last_execution"].(map[string]interface{}); ok {
		if _, ok := lastExec["result"]; ok {
			outcomes = append(outcomes, fmt.Sprintf("Execution completed with result"))
		}
		if workflowID, ok := lastExec["workflow_id"].(string); ok && workflowID != "" {
			outcomes = append(outcomes, fmt.Sprintf("Workflow %s executed", workflowID))
		}
	}

	// Add goal-specific outcomes
	if goal.Status == "completed" {
		outcomes = append(outcomes, fmt.Sprintf("Goal '%s' completed successfully", goal.Description))
	} else if goal.Status == "failed" {
		outcomes = append(outcomes, fmt.Sprintf("Goal '%s' failed", goal.Description))
	}

	// Add domain-specific outcomes
	if len(goal.Targets) > 0 {
		outcomes = append(outcomes, fmt.Sprintf("Explored targets: %v", goal.Targets))
	}

	return outcomes
}

// updateSuccessRate updates the success rate for a goal type/domain combination
func (e *FSMEngine) updateSuccessRate(goalType, domain string, success bool) {
	key := fmt.Sprintf("goal_success_rate:%s:%s", goalType, domain)

	// Get current statistics
	statsKey := fmt.Sprintf("goal_stats:%s:%s", goalType, domain)
	statsData, err := e.redis.Get(e.ctx, statsKey).Result()

	var successes, total int
	var stats map[string]interface{}
	if err == nil && statsData != "" {
		if err := json.Unmarshal([]byte(statsData), &stats); err == nil {
			if s, ok := stats["successes"].(float64); ok {
				successes = int(s)
			}
			if t, ok := stats["total"].(float64); ok {
				total = int(t)
			}
		}
	}

	// Update statistics
	total++
	if success {
		successes++
	}

	// Calculate success rate
	successRate := float64(successes) / float64(total)

	// Store updated statistics
	stats = map[string]interface{}{
		"successes":    successes,
		"total":        total,
		"success_rate": successRate,
		"updated_at":   time.Now().Unix(),
	}
	statsJSON, _ := json.Marshal(stats)
	e.redis.Set(e.ctx, statsKey, statsJSON, 0)

	// Also store success rate separately for quick access
	e.redis.Set(e.ctx, key, successRate, 0)

	log.Printf("📈 Updated success rate for %s:%s: %.2f%% (%d/%d)", goalType, domain, successRate*100, successes, total)
}

// updateAverageValue updates the average value for a goal type/domain combination
func (e *FSMEngine) updateAverageValue(goalType, domain string, value float64) {
	key := fmt.Sprintf("goal_avg_value:%s:%s", goalType, domain)

	// Get current statistics
	statsKey := fmt.Sprintf("goal_value_stats:%s:%s", goalType, domain)
	statsData, err := e.redis.Get(e.ctx, statsKey).Result()

	var totalValue float64
	var count int
	var stats map[string]interface{}
	if err == nil && statsData != "" {
		if err := json.Unmarshal([]byte(statsData), &stats); err == nil {
			if tv, ok := stats["total_value"].(float64); ok {
				totalValue = tv
			}
			if c, ok := stats["count"].(float64); ok {
				count = int(c)
			}
		}
	}

	// Update statistics
	totalValue += value
	count++
	avgValue := totalValue / float64(count)

	// Store updated statistics
	stats = map[string]interface{}{
		"total_value": totalValue,
		"count":       count,
		"avg_value":   avgValue,
		"updated_at":  time.Now().Unix(),
	}
	statsJSON, _ := json.Marshal(stats)
	e.redis.Set(e.ctx, statsKey, statsJSON, 0)

	// Also store average value separately for quick access
	e.redis.Set(e.ctx, key, avgValue, 0)

	log.Printf("💰 Updated avg value for %s:%s: %.2f (from %d goals)", goalType, domain, avgValue, count)
}

// getSuccessRate retrieves the success rate for a goal type/domain combination
func (e *FSMEngine) getSuccessRate(goalType, domain string) float64 {
	key := fmt.Sprintf("goal_success_rate:%s:%s", goalType, domain)
	rate, err := e.redis.Get(e.ctx, key).Float64()
	if err != nil {
		return 0.5 // Default neutral success rate if no data
	}
	return rate
}

// getAverageValue retrieves the average value for a goal type/domain combination
func (e *FSMEngine) getAverageValue(goalType, domain string) float64 {
	key := fmt.Sprintf("goal_avg_value:%s:%s", goalType, domain)
	value, err := e.redis.Get(e.ctx, key).Float64()
	if err != nil {
		return 0.5 // Default neutral value if no data
	}
	return value
}

// getOutcomeCount returns the number of outcomes recorded for a goal type/domain
func (e *FSMEngine) getOutcomeCount(goalType, domain string) int {
	outcomeKey := fmt.Sprintf("goal_outcomes:%s:%s", goalType, domain)
	count, err := e.redis.LLen(e.ctx, outcomeKey).Result()
	if err != nil {
		return 0
	}
	return int(count)
}

// hasRecentFailures checks if similar goals have failed recently
func (e *FSMEngine) hasRecentFailures(goal CuriosityGoal, domain string) bool {
	// Check outcomes from last 24 hours
	cutoff := time.Now().Add(-24 * time.Hour)
	outcomeKey := fmt.Sprintf("goal_outcomes:%s:%s", goal.Type, domain)

	outcomesData, err := e.redis.LRange(e.ctx, outcomeKey, 0, 49).Result() // Check last 50 outcomes
	if err != nil {
		return false
	}

	failureCount := 0
	for _, outcomeData := range outcomesData {
		var outcome GoalOutcome
		if err := json.Unmarshal([]byte(outcomeData), &outcome); err == nil {
			// Only count recent failures
			if outcome.CreatedAt.After(cutoff) && !outcome.Success {
				failureCount++
			}
		}
	}

	// If we have 3+ recent failures of this type, consider it a pattern
	return failureCount >= 3
}

// LearningProgress tracks learning progress by domain/type
type LearningProgress struct {
	Domain         string  `json:"domain"`
	GoalType       string  `json:"goal_type"`
	SuccessRate    float64 `json:"success_rate"`
	AvgValue       float64 `json:"avg_value"`
	RecentProgress float64 `json:"recent_progress"` // Progress in last N goals
	FocusScore     float64 `json:"focus_score"`     // Should we focus here?
}

// identifyFocusAreas identifies promising areas to focus learning on
func (e *FSMEngine) identifyFocusAreas(domain string) []LearningProgress {
	var focusAreas []LearningProgress

	// Get all goal types we've tracked
	goalTypes := []string{"gap_filling", "concept_exploration", "contradiction_resolution", "news_analysis"}

	for _, goalType := range goalTypes {
		successRate := e.getSuccessRate(goalType, domain)
		avgValue := e.getAverageValue(goalType, domain)

		// Calculate recent progress (success rate improvement in last 10 goals)
		recentProgress := e.calculateRecentProgress(goalType, domain)

		// Calculate focus score: combination of success rate, value, and recent progress
		// Higher scores = more promising areas
		focusScore := (successRate * 0.4) + (avgValue * 0.4) + (recentProgress * 0.2)

		// Add exploration bonus for goal types with few outcomes (encourage trying new types)
		outcomeCount := e.getOutcomeCount(goalType, domain)
		if outcomeCount < 5 && outcomeCount > 0 {
			focusScore += 0.1 // Small exploration bonus for types we've tried but don't have much data
		}

		log.Printf("🔍 Focus check: %s/%s: success=%.2f, value=%.2f, progress=%.2f, count=%d, focus=%.2f",
			goalType, domain, successRate, avgValue, recentProgress, outcomeCount, focusScore)

		// Only include areas showing promise (focus score >= 0.5, changed from > 0.5)
		if focusScore >= 0.5 {
			focusAreas = append(focusAreas, LearningProgress{
				Domain:         domain,
				GoalType:       goalType,
				SuccessRate:    successRate,
				AvgValue:       avgValue,
				RecentProgress: recentProgress,
				FocusScore:     focusScore,
			})
		}
	}

	// Sort by focus score (highest first)
	sort.Slice(focusAreas, func(i, j int) bool {
		return focusAreas[i].FocusScore > focusAreas[j].FocusScore
	})

	return focusAreas
}

// calculateRecentProgress calculates progress improvement in recent goals
func (e *FSMEngine) calculateRecentProgress(goalType, domain string) float64 {
	outcomeKey := fmt.Sprintf("goal_outcomes:%s:%s", goalType, domain)
	outcomesData, err := e.redis.LRange(e.ctx, outcomeKey, 0, 9).Result() // Last 10 outcomes
	if err != nil || len(outcomesData) < 5 {
		return 0.5 // Neutral if not enough data
	}

	successCount := 0
	for _, outcomeData := range outcomesData {
		var outcome GoalOutcome
		if err := json.Unmarshal([]byte(outcomeData), &outcome); err == nil {
			if outcome.Success {
				successCount++
			}
		}
	}

	recentRate := float64(successCount) / float64(len(outcomesData))
	overallRate := e.getSuccessRate(goalType, domain)

	// Progress = improvement over baseline (positive if improving)
	progress := recentRate - overallRate
	if progress < 0 {
		progress = 0 // No negative progress
	}

	return progress
}

// adjustGoalGeneration adjusts goal generation to focus on promising areas
func (e *FSMEngine) adjustGoalGeneration(goals []CuriosityGoal, focusAreas []LearningProgress, domain string) []CuriosityGoal {
	if len(focusAreas) == 0 {
		return goals
	}

	// Create a map of focus scores by goal type
	focusScores := make(map[string]float64)
	for _, area := range focusAreas {
		focusScores[area.GoalType] = area.FocusScore
	}

	// Separate goals into focused and unfocused
	var focusedGoals []CuriosityGoal
	var unfocusedGoals []CuriosityGoal

	for _, goal := range goals {
		if score, ok := focusScores[goal.Type]; ok && score > 0.6 {
			// High-focus goal types get priority boost
			goal.Priority = int(float64(goal.Priority) * 1.2) // 20% boost
			if goal.Priority > 10 {
				goal.Priority = 10 // Cap at 10
			}
			focusedGoals = append(focusedGoals, goal)
		} else {
			unfocusedGoals = append(unfocusedGoals, goal)
		}
	}

	// Prioritize focused goals: 70% focused, 30% unfocused
	targetFocused := int(float64(len(goals)) * 0.7)
	if len(focusedGoals) > targetFocused {
		focusedGoals = focusedGoals[:targetFocused]
	}

	// Combine: focused goals first, then unfocused
	result := append(focusedGoals, unfocusedGoals...)

	log.Printf("🎯 Goal adjustment: %d focused goals (from promising areas), %d unfocused goals",
		len(focusedGoals), len(unfocusedGoals))

	return result
}

// markGoalAsFailed marks a goal as failed and records the outcome
func (e *FSMEngine) markGoalAsFailed(goalID string, reason string) {
	domain := e.getCurrentDomain()
	key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)

	// Find the goal and mark it as failed
	goalsData, err := e.redis.LRange(e.ctx, key, 0, 199).Result()
	if err != nil {
		log.Printf("⚠️ Failed to find goal %s to mark as failed: %v", goalID, err)
		return
	}

	for i, goalData := range goalsData {
		var goal CuriosityGoal
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			if goal.ID == goalID {
				goal.Status = "failed"
				updatedData, err := json.Marshal(goal)
				if err == nil {
					e.redis.LSet(e.ctx, key, int64(i), updatedData)
					log.Printf("❌ Marked goal %s as failed: %s", goalID, reason)
					// updateGoalStatus will record the outcome
					e.updateGoalStatus(goal)
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

	var scoredGoals []scoredCuriosityGoal
	for _, goal := range goals {
		score := e.calculateGoalScore(goal, domain)
		scoredGoals = append(scoredGoals, scoredCuriosityGoal{Goal: goal, Score: score})
	}

	sort.Slice(scoredGoals, func(i, j int) bool {
		return scoredGoals[i].Score > scoredGoals[j].Score
	})

	// Prepare LLM ranking on top candidates that pass eligibility checks
	candidates := make([]scoredCuriosityGoal, 0, minInt(5, len(scoredGoals)))
	for _, sg := range scoredGoals {
		if !e.isGoalEligible(sg.Goal, domain) {
			continue
		}
		candidates = append(candidates, sg)
		if len(candidates) >= 5 {
			break
		}
	}

	if len(candidates) == 0 {
		log.Printf("[Autonomy] No eligible goals found, using fallback")
		return goals[0]
	}

	if selected, reason, ok := e.rankGoalsWithLLM(candidates, domain); ok {
		e.context["goal_selection_reason"] = reason
		log.Printf("[Autonomy] LLM selected goal '%s' (%s)", selected.Description, reason)
		return selected
	}

	// Fallback to top heuristic candidate
	top := candidates[0].Goal
	log.Printf("[Autonomy] LLM ranking unavailable; using heuristic goal '%s'", top.Description)
	return top
}

func (e *FSMEngine) rankGoalsWithLLM(candidates []scoredCuriosityGoal, domain string) (CuriosityGoal, string, bool) {
	if len(candidates) == 0 {
		return CuriosityGoal{}, "", false
	}
	if len(candidates) == 1 {
		return candidates[0].Goal, "single candidate fallback", true
	}

	if strings.EqualFold(strings.TrimSpace(os.Getenv("FSM_DISABLE_LLM_GOAL_SELECTION")), "1") {
		return CuriosityGoal{}, "", false
	}

	base := strings.TrimSpace(os.Getenv("HDN_URL"))
	if base == "" {
		base = "http://localhost:8081" // Fixed: use correct HDN port (8081, not 8080)
	}
	url := fmt.Sprintf("%s/api/v1/interpret", strings.TrimRight(base, "/"))

	type candidatePayload struct {
		ID           string   `json:"id"`
		Type         string   `json:"type"`
		Description  string   `json:"description"`
		Priority     int      `json:"priority"`
		Heuristic    float64  `json:"heuristic_score"`
		Targets      []string `json:"targets"`
		AgeMinutes   float64  `json:"age_minutes"`
		RecentRepeat bool     `json:"recent_repeat"`
	}

	payloadCandidates := make([]candidatePayload, 0, len(candidates))
	for _, cand := range candidates {
		age := time.Since(cand.Goal.CreatedAt).Minutes()
		if age < 0 {
			age = 0
		}
		payloadCandidates = append(payloadCandidates, candidatePayload{
			ID:           cand.Goal.ID,
			Type:         cand.Goal.Type,
			Description:  cand.Goal.Description,
			Priority:     cand.Goal.Priority,
			Heuristic:    cand.Score,
			Targets:      cand.Goal.Targets,
			AgeMinutes:   age,
			RecentRepeat: e.hasGoalBeenTriedRecently(cand.Goal, domain),
		})
	}

	candidatesJSON, err := json.Marshal(payloadCandidates)
	if err != nil {
		log.Printf("⚠️ [Autonomy] Failed to marshal candidates for LLM ranking: %v", err)
		return CuriosityGoal{}, "", false
	}

	prompt := fmt.Sprintf(`You are an autonomous research planner for the domain "%s". Pick the single best curiosity goal to pursue next.
Consider the candidate goals provided as JSON.
You must balance novelty, impact, feasibility, and avoid repeating recent attempts unless necessary.
Return ONLY strict JSON with this shape:
{"selected_goal_id":"<id>","reason":"<short rationale>","scores":[{"id":"<id>","score":<0-1>,"rationale":"<why>"}]}
Candidates JSON: %s`, domain, string(candidatesJSON))

	// HDN /api/v1/interpret expects "input" field, not "text"
	bodyRequest, _ := json.Marshal(map[string]interface{}{
		"input": prompt,
		"context": map[string]string{
			"origin": "fsm", // Mark as background task for LOW priority
		},
	})
	req, _ := http.NewRequest("POST", url, bytes.NewReader(bodyRequest))
	req.Header.Set("Content-Type", "application/json")
	if pid, ok := e.context["project_id"].(string); ok && pid != "" {
		req.Header.Set("X-Project-ID", pid)
	}

	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("⚠️ [Autonomy] LLM goal ranking request failed: %v", err)
		return CuriosityGoal{}, "", false
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("⚠️ [Autonomy] LLM goal ranking status %d: %s", resp.StatusCode, string(body))
		return CuriosityGoal{}, "", false
	}

	bodyText := strings.TrimSpace(string(body))
	start := strings.Index(bodyText, "{")
	end := strings.LastIndex(bodyText, "}")
	if start >= 0 && end > start {
		bodyText = bodyText[start : end+1]
	}

	var out struct {
		SelectedGoalID string `json:"selected_goal_id"`
		Reason         string `json:"reason"`
		Scores         []struct {
			ID        string  `json:"id"`
			Score     float64 `json:"score"`
			Rationale string  `json:"rationale"`
		} `json:"scores"`
	}

	if err := json.Unmarshal([]byte(bodyText), &out); err != nil {
		log.Printf("⚠️ [Autonomy] Failed to parse LLM goal ranking response: %v body=%s", err, string(body))
		return CuriosityGoal{}, "", false
	}
	if strings.TrimSpace(out.SelectedGoalID) == "" {
		log.Printf("⚠️ [Autonomy] LLM goal ranking did not provide selected_goal_id")
		return CuriosityGoal{}, "", false
	}

	for _, cand := range candidates {
		if cand.Goal.ID == out.SelectedGoalID {
			if len(out.Scores) > 0 {
				scoreSummaries := make([]map[string]interface{}, 0, len(out.Scores))
				for _, s := range out.Scores {
					scoreSummaries = append(scoreSummaries, map[string]interface{}{
						"id":        s.ID,
						"score":     s.Score,
						"rationale": s.Rationale,
					})
				}
				e.context["goal_selection_scores"] = scoreSummaries
			}
			return cand.Goal, out.Reason, true
		}
	}

	log.Printf("⚠️ [Autonomy] LLM selected goal %s which is not in candidate list", out.SelectedGoalID)
	return CuriosityGoal{}, "", false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// calculateGoalScore calculates a priority score for a goal
// Now incorporates uncertainty models for more principled decision-making
func (e *FSMEngine) calculateGoalScore(goal CuriosityGoal, domain string) float64 {
	score := float64(goal.Priority) // Base priority (1-10)

	// Uncertainty-based adjustments
	if goal.Uncertainty != nil {
		// Apply decay if needed
		ApplyDecayToGoal(&goal)
		
		// Higher calibrated confidence increases score (more certain goals are preferred)
		confidenceBonus := goal.Uncertainty.CalibratedConfidence * 2.0
		score += confidenceBonus
		log.Printf("📊 Goal %s: uncertainty confidence bonus +%.2f (calibrated: %.3f)", 
			goal.ID, confidenceBonus, goal.Uncertainty.CalibratedConfidence)
		
		// Lower epistemic uncertainty is preferred (we know more about it)
		epistemicPenalty := goal.Uncertainty.EpistemicUncertainty * 1.0
		score -= epistemicPenalty
		log.Printf("📉 Goal %s: epistemic uncertainty penalty -%.2f (uncertainty: %.3f)", 
			goal.ID, epistemicPenalty, goal.Uncertainty.EpistemicUncertainty)
		
		// Higher stability is preferred (less volatile goals)
		stabilityBonus := goal.Uncertainty.Stability * 0.5
		score += stabilityBonus
		log.Printf("📈 Goal %s: stability bonus +%.2f (stability: %.3f)", 
			goal.ID, stabilityBonus, goal.Uncertainty.Stability)
		
		// Use uncertainty-calibrated value if available
		if goal.Value > 0 {
			valueBonus := goal.Value * 1.5
			score += valueBonus
			log.Printf("💰 Goal %s: value bonus +%.2f (value: %.3f)", goal.ID, valueBonus, goal.Value)
		}
	} else if goal.Value > 0 {
		// Fallback to value if uncertainty model not available
		valueBonus := goal.Value * 1.5
		score += valueBonus
		log.Printf("💰 Goal %s: value bonus +%.2f (value: %.3f, no uncertainty model)", goal.ID, valueBonus, goal.Value)
	}

	// Historical success bonus - goals of types that succeed more often get bonus
	successRate := e.getSuccessRate(goal.Type, domain)
	log.Printf("🔍 Scoring goal %s (type=%s): base=%d, successRate=%.2f", goal.ID, goal.Type, goal.Priority, successRate)

	if successRate > 0.5 {
		// Goals of types that succeed more than 50% get bonus
		// Scale: 0.5 -> 0 bonus, 1.0 -> +3.0 bonus
		successBonus := (successRate - 0.5) * 6.0
		score += successBonus
		log.Printf("📊 Goal %s: success rate bonus +%.2f (rate=%.2f)", goal.ID, successBonus, successRate)
	} else if successRate < 0.5 {
		log.Printf("🔍 Goal %s: success rate %.2f < 0.5, no bonus", goal.ID, successRate)
	}

	// Historical value bonus - goals of types that yield high value get bonus
	avgValue := e.getAverageValue(goal.Type, domain)
	log.Printf("🔍 Scoring goal %s: avgValue=%.2f", goal.ID, avgValue)

	if avgValue > 0.5 {
		// Goals of types that yield more than 0.5 value get bonus
		// Scale: 0.5 -> 0 bonus, 1.0 -> +2.0 bonus
		valueBonus := (avgValue - 0.5) * 4.0
		score += valueBonus
		log.Printf("💰 Goal %s: value bonus +%.2f (avg=%.2f)", goal.ID, valueBonus, avgValue)
	} else if avgValue < 0.5 {
		log.Printf("🔍 Goal %s: avg value %.2f < 0.5, no bonus", goal.ID, avgValue)
	}

	// Failure penalty for similar goals that have failed recently
	if e.hasRecentFailures(goal, domain) {
		score -= 2.0
		log.Printf("⚠️ Goal %s: recent failures penalty -2.0", goal.ID)
	}

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

	// Hypothesis testing goals are critical - they lead to workflow creation
	if goal.Type == "hypothesis_testing" {
		score += 3.0 // High bonus to prioritize hypothesis testing
		log.Printf("🧪 Goal %s: hypothesis testing bonus +3.0", goal.ID)
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

// screenCuriosityGoalsWithLLM screens curiosity goals with LLM to filter out useless ones
// This reduces GPU load by preventing execution of low-value goals
// Uses batching and rate limiting to minimize GPU usage
func (e *FSMEngine) screenCuriosityGoalsWithLLM(goals []CuriosityGoal, domain string) []CuriosityGoal {
	// Check if screening is enabled (default: enabled)
	enabled := true
	if v := strings.TrimSpace(os.Getenv("FSM_GOAL_SCREENING_ENABLED")); v != "" {
		enabled = v != "false" && v != "0"
	}
	if !enabled {
		log.Printf("ℹ️ [GOAL-SCREEN] LLM screening disabled, allowing all goals")
		return goals
	}

	if len(goals) == 0 {
		return goals
	}

	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8081"
	}
	url := fmt.Sprintf("%s/api/v1/interpret", strings.TrimRight(base, "/"))

	// Get threshold from config or default
	threshold := 0.5 // Default threshold (lower than hypothesis threshold since goals are more diverse)
	if e.config.Agent.GoalScreenThreshold > 0 {
		threshold = e.config.Agent.GoalScreenThreshold
	} else if v := strings.TrimSpace(os.Getenv("FSM_GOAL_SCREEN_THRESHOLD")); v != "" {
		if t, err := strconv.ParseFloat(v, 64); err == nil && t >= 0 && t <= 1 {
			threshold = t
		}
	}

	var approved []CuriosityGoal
	
	// Batch processing: screen goals in smaller batches to reduce GPU load
	// Process max 3 goals at a time, with delay between batches
	batchSize := 3
	if v := strings.TrimSpace(os.Getenv("FSM_GOAL_SCREEN_BATCH_SIZE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			batchSize = n
		}
	}

	// Delay between batches (default: 8 seconds to give GPU time to process)
	batchDelayMs := 8000
	if v := strings.TrimSpace(os.Getenv("FSM_GOAL_SCREEN_BATCH_DELAY_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			batchDelayMs = n
		}
	}

	// Delay between individual goals within a batch (default: 3 seconds)
	goalDelayMs := 3000
	if v := strings.TrimSpace(os.Getenv("FSM_GOAL_SCREEN_DELAY_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			goalDelayMs = n
		}
	}

	log.Printf("🎯 [GOAL-SCREEN] Screening %d goals in batches of %d (threshold: %.2f)", len(goals), batchSize, threshold)

	for i, goal := range goals {
		// Skip hypothesis testing goals - they're already screened
		if goal.Type == "hypothesis_testing" {
			approved = append(approved, goal)
			continue
		}

		// Rate limiting: delay between goals
		if i > 0 && goalDelayMs > 0 {
			time.Sleep(time.Duration(goalDelayMs) * time.Millisecond)
		}

		// Batch delay: longer delay between batches
		if i > 0 && i%batchSize == 0 && batchDelayMs > 0 {
			log.Printf("⏸️ [GOAL-SCREEN] Batch delay: %dms before next batch", batchDelayMs)
			time.Sleep(time.Duration(batchDelayMs) * time.Millisecond)
		}

		// Create prompt for goal evaluation
		prompt := fmt.Sprintf(`You are evaluating a curiosity goal. This is a SIMPLE SCORING TASK that requires NO tools, NO actions, and NO queries. Just return a JSON score.

CRITICAL: You MUST respond with type "text" containing ONLY a JSON object. Do NOT use tools. Do NOT create tasks. This is a pure evaluation task.

Rate this goal on a scale of 0.0 to 1.0:
- ACTIONABILITY: How specific and actionable is this goal? (0.0 = vague/unclear, 1.0 = clear and actionable)
- VALUE: How valuable would achieving this goal be? (0.0 = no value, 1.0 = very valuable)
- TRACTABILITY: How feasible is this goal to execute? (0.0 = impossible, 1.0 = easily achievable)

Domain: %s
Goal Type: %s
Goal Description: %s

You MUST respond with type "text" containing ONLY this JSON (no other text):
{"type": "text", "content": "{\"score\": 0.75, \"reason\": \"Brief explanation\"}"}

Or if the system requires direct JSON, return:
{"score": 0.75, "reason": "Brief explanation"}

Examples:
- High value, actionable: {"score": 0.8, "reason": "Clear, valuable, and achievable"}
- Medium value: {"score": 0.6, "reason": "Moderately useful and actionable"}
- Low value or vague: {"score": 0.3, "reason": "Too vague or low value"}

Now return ONLY the JSON score (no tools, no tasks, just the score):`, domain, goal.Type, goal.Description)

		// HDN /api/v1/interpret expects "input" field
		payload := map[string]interface{}{
			"input": prompt,
			"context": map[string]string{
				"origin": "fsm", // Mark as background task for LOW priority
			},
		}
		data, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		if pid, ok := e.context["project_id"].(string); ok && pid != "" {
			req.Header.Set("X-Project-ID", pid)
		}

		// Use async HTTP client (or sync fallback)
		ctx := context.Background()
		resp, err := Do(ctx, req)
		if err != nil {
			log.Printf("⚠️ [GOAL-SCREEN] LLM screening request failed for goal '%s': %v (allowing by default)", 
				goal.Description[:minInt(50, len(goal.Description))], err)
			approved = append(approved, goal)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("⚠️ [GOAL-SCREEN] LLM screening status %d for goal '%s' (allowing by default)", 
				resp.StatusCode, goal.Description[:minInt(50, len(goal.Description))])
			approved = append(approved, goal)
			continue
		}

		// Parse the HDN interpreter response
		// The /api/v1/interpret endpoint returns InterpretationResult or FlexibleInterpretationResult
		var interpretResp map[string]interface{}
		score := 0.0
		parseMethod := "none"

		if err := json.Unmarshal(body, &interpretResp); err == nil {
			// Try FlexibleInterpretationResult format first (text_response field)
			if textResp, ok := interpretResp["text_response"].(string); ok {
				var textObj map[string]interface{}
				if json.Unmarshal([]byte(textResp), &textObj) == nil {
					if s, ok := textObj["score"].(float64); ok {
						score = s
						parseMethod = "text_response_json"
					}
				}
			}
			
			// Try InterpretationResult format (message field might contain JSON)
			if score == 0.0 {
				if message, ok := interpretResp["message"].(string); ok {
					// Try to extract JSON from message
					jsonStart := strings.Index(message, "{")
					jsonEnd := strings.LastIndex(message, "}")
					if jsonStart != -1 && jsonEnd != -1 && jsonEnd > jsonStart {
						jsonStr := message[jsonStart : jsonEnd+1]
						var msgObj map[string]interface{}
						if json.Unmarshal([]byte(jsonStr), &msgObj) == nil {
							if s, ok := msgObj["score"].(float64); ok {
								score = s
								parseMethod = "message_json"
							}
						}
					}
				}
			}
			
			// Try FlexibleLLMResponse format (content field)
			if score == 0.0 {
				if content, ok := interpretResp["content"].(string); ok {
					var contentObj map[string]interface{}
					if json.Unmarshal([]byte(content), &contentObj) == nil {
						if s, ok := contentObj["score"].(float64); ok {
							score = s
							parseMethod = "content_json"
						}
					}
				}
			}
			
			// Try direct score field
			if score == 0.0 {
				if s, ok := interpretResp["score"].(float64); ok {
					score = s
					parseMethod = "direct_score"
				}
			}
			
			// Try tasks array (InterpretationResult format) - check first task's description
			if score == 0.0 {
				if tasks, ok := interpretResp["tasks"].([]interface{}); ok && len(tasks) > 0 {
					if task, ok := tasks[0].(map[string]interface{}); ok {
						if desc, ok := task["description"].(string); ok {
							// Try to extract score from description
							jsonStart := strings.Index(desc, "{")
							jsonEnd := strings.LastIndex(desc, "}")
							if jsonStart != -1 && jsonEnd != -1 && jsonEnd > jsonStart {
								jsonStr := desc[jsonStart : jsonEnd+1]
								var descObj map[string]interface{}
								if json.Unmarshal([]byte(jsonStr), &descObj) == nil {
									if s, ok := descObj["score"].(float64); ok {
										score = s
										parseMethod = "task_description_json"
									}
								}
							}
						}
					}
				}
			}
		}

		// Fallback: try regex/text extraction from entire response body
		if score == 0.0 {
			s := string(body)
			// Try to find JSON score pattern: "score": 0.75 or "score":0.75
			scoreRegex := regexp.MustCompile(`"score"\s*:\s*([0-9]+\.?[0-9]*)`)
			matches := scoreRegex.FindStringSubmatch(s)
			if len(matches) > 1 {
				if val, err := strconv.ParseFloat(matches[1], 64); err == nil && val >= 0 && val <= 1 {
					score = val
					parseMethod = "regex_extraction"
				}
			}
			
			// Last resort: try simple text extraction
			if score == 0.0 && strings.Contains(s, "score") {
				parts := strings.Fields(s)
				for i, part := range parts {
					if strings.Contains(strings.ToLower(part), "score") && i+1 < len(parts) {
						if val, err := strconv.ParseFloat(parts[i+1], 64); err == nil && val >= 0 && val <= 1 {
							score = val
							parseMethod = "text_extraction"
							break
						}
					}
				}
			}
		}

		log.Printf("📊 [GOAL-SCREEN] Goal '%s': score=%.2f (method=%s, threshold=%.2f)", 
			goal.Description[:minInt(60, len(goal.Description))], score, parseMethod, threshold)

		if score >= threshold {
			approved = append(approved, goal)
			log.Printf("✅ [GOAL-SCREEN] Goal APPROVED (score %.2f >= threshold %.2f)", score, threshold)
		} else {
			log.Printf("🛑 [GOAL-SCREEN] Goal FILTERED (score %.2f < threshold %.2f): %s",
				score, threshold, goal.Description[:minInt(60, len(goal.Description))])
		}
	}

	log.Printf("🎯 [GOAL-SCREEN] Screening complete: %d approved of %d goals", len(approved), len(goals))
	return approved
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

// generateHypothesisTestingGoalsForExisting creates hypothesis testing goals for existing untested hypotheses
// This ensures hypotheses created in previous cycles get tested
func (e *FSMEngine) generateHypothesisTestingGoalsForExisting(domain string) ([]CuriosityGoal, error) {
	key := fmt.Sprintf("fsm:%s:hypotheses", e.agentID)
	hypothesesData, err := e.redis.HGetAll(e.ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get hypotheses: %w", err)
	}

	// Get existing hypothesis testing goals to avoid duplicates
	goalKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	existingGoalsData, err := e.redis.LRange(e.ctx, goalKey, 0, 199).Result()
	if err != nil {
		log.Printf("Warning: Failed to get existing goals for deduplication: %v", err)
		existingGoalsData = []string{}
	}

	// Build map of existing hypothesis IDs that already have testing goals
	existingHypIDs := make(map[string]bool)
	for _, goalData := range existingGoalsData {
		var goal CuriosityGoal
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			if goal.Type == "hypothesis_testing" && len(goal.Targets) > 0 {
				existingHypIDs[goal.Targets[0]] = true
			}
		}
	}

	var goals []CuriosityGoal
	for _, hypData := range hypothesesData {
		var hypothesis map[string]interface{}
		if err := json.Unmarshal([]byte(hypData), &hypothesis); err != nil {
			continue
		}

		// Only create goals for "proposed" hypotheses (untested)
		status, _ := hypothesis["status"].(string)
		if status != "proposed" {
			continue
		}

		hypID, _ := hypothesis["id"].(string)
		if hypID == "" {
			continue
		}

		// Skip if already has a testing goal
		if existingHypIDs[hypID] {
			continue
		}

		description, _ := hypothesis["description"].(string)
		if description == "" {
			continue
		}

		// Create hypothesis testing goal with actionable description
		// If description already starts with "Test hypothesis:" or contains nested prefixes, use it as-is
		// Otherwise, create a more actionable description
		goalDesc := description
		if !strings.HasPrefix(description, "Test hypothesis:") {
			// Check if description is a follow-up hypothesis (has nested prefixes)
			if strings.Contains(description, ": ") && (strings.HasPrefix(description, "How can we better test:") ||
				strings.HasPrefix(description, "What additional evidence would support:") ||
				strings.HasPrefix(description, "What are the specific conditions for:") ||
				strings.HasPrefix(description, "What are the implications of:") ||
				strings.HasPrefix(description, "How can we extend:") ||
				strings.HasPrefix(description, "What is the opposite of:")) {
				// Extract the actual hypothesis from nested description
				parts := strings.SplitN(description, ": ", 2)
				if len(parts) == 2 {
					actualHyp := strings.TrimSpace(parts[1])
					goalDesc = fmt.Sprintf("Test and refine: %s", actualHyp)
				} else {
					goalDesc = fmt.Sprintf("Test hypothesis: %s", description)
				}
			} else {
				// Create actionable description
				goalDesc = fmt.Sprintf("Test hypothesis: %s", description)
			}
		}

		// Create uncertainty model for hypothesis testing goal
		// Estimate based on hypothesis confidence if available
		hypConfidence := 0.5 // Default
		if conf, ok := hypothesis["confidence"].(float64); ok {
			hypConfidence = conf
		}
		epistemicUncertainty := EstimateEpistemicUncertainty(0, false, false) // No direct evidence yet
		aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "hypothesis_testing")
		uncertainty := NewUncertaintyModel(hypConfidence, epistemicUncertainty, aleatoricUncertainty)
		
		goal := CuriosityGoal{
			ID:          fmt.Sprintf("hyp_test_%s", hypID),
			Type:        "hypothesis_testing",
			Description: goalDesc,
			Targets:     []string{hypID},
			Priority:    8,
			Status:      "pending",
			Domain:      domain,
			CreatedAt:   time.Now(),
			Uncertainty: uncertainty,
			Value:       uncertainty.CalibratedConfidence,
		}

		// Skip generic hypotheses - DISABLED to allow more diverse goals
		// Re-enable once hypothesis generation produces more specific hypotheses
		if false && e.isGenericHypothesisGoal(goal) {
			log.Printf("🚫 Filtered out generic hypothesis goal: %s", goal.Description)
			continue
		}

		goals = append(goals, goal)
	}

	log.Printf("🎯 Generated %d hypothesis testing goals for existing untested hypotheses", len(goals))
	return goals, nil
}

// isGenericHypothesisGoal checks if a hypothesis testing goal is generic/useless
func (e *FSMEngine) isGenericHypothesisGoal(goal CuriosityGoal) bool {
	if goal.Type != "hypothesis_testing" {
		return false
	}

	desc := strings.ToLower(goal.Description)
	
	// Check for nested vague descriptions (multiple colons indicate nesting)
	colonCount := strings.Count(desc, ":")
	if colonCount > 2 {
		// Likely a nested vague description like "Test hypothesis: How can we better test: Investigate System state: learn to discover"
		return true
	}
	
	// Generic patterns that indicate useless goals
	genericPatterns := []string{
		"apply insights from system state",
		"improve our general approach",
		"improve general performance",
		"optimize the ai capability control system",
		"if we apply insights",
		"we can improve",
		"learn to discover new",
		"discover new general opportunities",
		"investigate system state: learn",
	}
	for _, pattern := range genericPatterns {
		if strings.Contains(desc, pattern) {
			return true
		}
	}
	
	// Check for overly vague descriptions with multiple question prefixes
	vaguePrefixes := []string{
		"test hypothesis: how can we better test:",
		"test hypothesis: what additional evidence would support:",
		"test hypothesis: what are the specific conditions for:",
		"test hypothesis: what are the implications of:",
	}
	for _, prefix := range vaguePrefixes {
		if strings.HasPrefix(desc, prefix) {
			return true
		}
	}
	
	// Check if description is too vague (less than 30 chars)
	if len(goal.Description) < 30 {
		return true
	}

	return false
}
