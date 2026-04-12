package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// storeHypotheses stores hypotheses in Redis for monitoring
func (e *FSMEngine) storeHypotheses(hypotheses []Hypothesis, domain string) {
	key := fmt.Sprintf("fsm:%s:hypotheses", e.agentID)

	for _, hypothesis := range hypotheses {

		hypothesisData := map[string]interface{}{
			"id":          hypothesis.ID,
			"description": hypothesis.Description,
			"domain":      hypothesis.Domain,
			"confidence":  hypothesis.Confidence,
			"status":      hypothesis.Status,
			"facts":       hypothesis.Facts,
			"constraints": hypothesis.Constraints,
			"created_at":  hypothesis.CreatedAt.Format(time.RFC3339),
		}

		if hypothesis.Uncertainty != nil {
			hypothesisData["uncertainty"] = map[string]interface{}{
				"epistemic_uncertainty": hypothesis.Uncertainty.EpistemicUncertainty,
				"aleatoric_uncertainty": hypothesis.Uncertainty.AleatoricUncertainty,
				"calibrated_confidence": hypothesis.Uncertainty.CalibratedConfidence,
				"stability":             hypothesis.Uncertainty.Stability,
				"volatility":            hypothesis.Uncertainty.Volatility,
				"last_updated":          hypothesis.Uncertainty.LastUpdated.Format(time.RFC3339),
			}
		}

		if hypothesis.CausalType != "" {
			hypothesisData["causal_type"] = hypothesis.CausalType
		}
		if len(hypothesis.CounterfactualActions) > 0 {
			hypothesisData["counterfactual_actions"] = hypothesis.CounterfactualActions
		}
		if len(hypothesis.InterventionGoals) > 0 {
			hypothesisData["intervention_goals"] = hypothesis.InterventionGoals
		}

		data, err := json.Marshal(hypothesisData)
		if err != nil {
			log.Printf("Warning: Failed to marshal hypothesis: %v", err)
			continue
		}

		e.redis.HSet(e.ctx, key, hypothesis.ID, data)
	}

	e.redis.Expire(e.ctx, key, 24*time.Hour)

	log.Printf("📝 Stored %d hypotheses in Redis", len(hypotheses))
}

// createHypothesisTestingGoals creates curiosity goals for testing hypotheses
func (e *FSMEngine) createHypothesisTestingGoals(hypotheses []Hypothesis, domain string) {

	approved := e.screenHypothesesWithLLM(hypotheses, domain)

	seenHypothesisDesc := map[string]bool{}
	var uniqueApproved []Hypothesis
	for _, h := range approved {
		key := strings.ToLower(strings.TrimSpace(h.Description))
		if key == "" {
			uniqueApproved = append(uniqueApproved, h)
			continue
		}
		if seenHypothesisDesc[key] {
			continue
		}
		seenHypothesisDesc[key] = true
		uniqueApproved = append(uniqueApproved, h)
	}

	goalKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	existing := map[string]CuriosityGoal{}
	if existingGoalsData, err := e.redis.LRange(e.ctx, goalKey, 0, 199).Result(); err == nil {
		for _, gd := range existingGoalsData {
			var g CuriosityGoal
			if err := json.Unmarshal([]byte(gd), &g); err == nil {
				k := e.createDedupKey(g)
				existing[k] = g
			}
		}
	}

	newGoals := 0
	filteredCount := 0
	for _, hypothesis := range uniqueApproved {

		if hypothesis.CausalType != "" && len(hypothesis.InterventionGoals) > 0 {

			for i, interventionGoal := range hypothesis.InterventionGoals {
				// Create uncertainty model for intervention goal
				var uncertainty *UncertaintyModel
				if hypothesis.Uncertainty != nil {

					uncertainty = NewUncertaintyModel(
						hypothesis.Uncertainty.CalibratedConfidence,
						clamp(hypothesis.Uncertainty.EpistemicUncertainty+0.1, 0.0, 1.0),
						hypothesis.Uncertainty.AleatoricUncertainty,
					)
				} else {
					epistemicUncertainty := EstimateEpistemicUncertainty(len(hypothesis.Facts), false, false)
					aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "intervention_testing")
					uncertainty = NewUncertaintyModel(hypothesis.Confidence, epistemicUncertainty, aleatoricUncertainty)
				}

				priority := 9
				if hypothesis.CausalType == "experimentally_testable_relation" {
					priority = 10
				}

				goal := CuriosityGoal{
					ID:          fmt.Sprintf("intervention_%s_%d", hypothesis.ID, i),
					Type:        "intervention_testing",
					Description: interventionGoal,
					Targets:     []string{hypothesis.ID},
					Priority:    priority,
					Status:      "pending",
					Domain:      domain,
					CreatedAt:   time.Now(),
					Uncertainty: uncertainty,
					Value:       uncertainty.CalibratedConfidence * 1.1,
				}

				dedupKey := e.createDedupKey(goal)
				if _, exists := existing[dedupKey]; !exists {
					goalData, _ := json.Marshal(goal)
					e.redis.LPush(e.ctx, goalKey, goalData)
					e.redis.LTrim(e.ctx, goalKey, 0, 199)
					existing[dedupKey] = goal
					newGoals++

					if e.goalManager != nil {
						_ = e.goalManager.PostCuriosityGoal(goal, "hypothesis_testing")
					}

					log.Printf("🔬 [CAUSAL] Created intervention goal: %s", interventionGoal[:min(60, len(interventionGoal))])
				}
			}
		}

		hypDesc := hypothesis.Description
		goalDesc := hypDesc

		if strings.Contains(hypDesc, ": ") {
			parts := strings.SplitN(hypDesc, ": ", 2)
			if len(parts) == 2 {
				prefix := strings.ToLower(parts[0])

				if strings.Contains(prefix, "how can we better test") ||
					strings.Contains(prefix, "what additional evidence") ||
					strings.Contains(prefix, "what are the specific conditions") ||
					strings.Contains(prefix, "what are the implications") ||
					strings.Contains(prefix, "how can we extend") ||
					strings.Contains(prefix, "what is the opposite") {

					actualHyp := strings.TrimSpace(parts[1])
					goalDesc = fmt.Sprintf("Test and refine: %s", actualHyp)
				} else {
					goalDesc = fmt.Sprintf("Test hypothesis: %s", hypDesc)
				}
			} else {
				goalDesc = fmt.Sprintf("Test hypothesis: %s", hypDesc)
			}
		} else {
			goalDesc = fmt.Sprintf("Test hypothesis: %s", hypDesc)
		}

		// Create uncertainty model for hypothesis testing goal
		// Use hypothesis uncertainty if available, otherwise estimate
		var uncertainty *UncertaintyModel
		if hypothesis.Uncertainty != nil {

			uncertainty = NewUncertaintyModel(
				hypothesis.Uncertainty.CalibratedConfidence,
				hypothesis.Uncertainty.EpistemicUncertainty,
				hypothesis.Uncertainty.AleatoricUncertainty,
			)
			uncertainty.Stability = hypothesis.Uncertainty.Stability
			uncertainty.Volatility = hypothesis.Uncertainty.Volatility
		} else {

			epistemicUncertainty := EstimateEpistemicUncertainty(len(hypothesis.Facts), false, false)
			aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "hypothesis_testing")
			uncertainty = NewUncertaintyModel(hypothesis.Confidence, epistemicUncertainty, aleatoricUncertainty)
		}

		priority := 8
		if len(hypothesis.InterventionGoals) > 0 {
			priority = 7
		}

		goal := CuriosityGoal{
			ID:          fmt.Sprintf("hyp_test_%s", hypothesis.ID),
			Type:        "hypothesis_testing",
			Description: goalDesc,
			Targets:     []string{hypothesis.ID},
			Priority:    priority,
			Status:      "pending",
			Domain:      domain,
			CreatedAt:   time.Now(),
			Uncertainty: uncertainty,
			Value:       uncertainty.CalibratedConfidence,
		}

		if len(approved) == 0 || len(uniqueApproved) == 0 {

			if false && e.isGenericHypothesisGoal(goal) {
				filteredCount++
				log.Printf("🚫 Filtered out generic hypothesis goal (no LLM approval): %s", goal.Description)
				continue
			}
		} else {

			log.Printf("✅ Allowing LLM-approved hypothesis through (score >= threshold): %s", hypothesis.Description[:min(80, len(hypothesis.Description))])
		}

		k := e.createDedupKey(goal)
		if _, exists := existing[k]; exists {
			continue
		}
		if goalData, err := json.Marshal(goal); err == nil {
			_ = e.redis.LPush(e.ctx, goalKey, goalData).Err()
			_ = e.redis.LTrim(e.ctx, goalKey, 0, 199).Err()
			existing[k] = goal
			newGoals++

			if e.goalManager != nil {
				_ = e.goalManager.PostCuriosityGoal(goal, "hypothesis_testing")
			}
		}
	}

	log.Printf("🎯 Created %d hypothesis testing goals (after LLM screening, deduped, filtered %d generic)", newGoals, filteredCount)
}

// screenHypothesesWithLLM calls the HDN interpreter to rate hypotheses and filters by threshold
func (e *FSMEngine) screenHypothesesWithLLM(hypotheses []Hypothesis, domain string) []Hypothesis {
	if len(hypotheses) == 0 {
		return hypotheses
	}

	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8081"
	}
	url := fmt.Sprintf("%s/api/v1/interpret", strings.TrimRight(base, "/"))

	var approved []Hypothesis
	threshold := e.config.Agent.HypothesisScreenThreshold
	if threshold == 0 {
		threshold = 0.6
	}

	for _, h := range hypotheses {

		causalTypeInfo := ""
		if h.CausalType != "" {
			causalTypeInfo = fmt.Sprintf("\nCausal Type: %s", h.CausalType)
			if len(h.InterventionGoals) > 0 {
				causalTypeInfo += fmt.Sprintf("\nIntervention Goals Available: %d", len(h.InterventionGoals))
			}
		}

		prompt := fmt.Sprintf(`You are evaluating a hypothesis. This is a SIMPLE SCORING TASK that requires NO tools, NO actions, and NO queries. Just return a JSON score.

CRITICAL: You MUST respond with type "text" containing ONLY a JSON object. Do NOT use tools. Do NOT create tasks. This is a pure evaluation task.

Rate this hypothesis on a scale of 0.0 to 1.0:
- IMPACT: How valuable would confirming this hypothesis be? (0.0 = no value, 1.0 = very valuable)
- TRACTABILITY: How testable/verifiable is this hypothesis? (0.0 = untestable, 1.0 = easily testable)
- CAUSAL REASONING: If this is a causal hypothesis (not just correlation), give higher score for testability. Causal hypotheses with intervention goals are more valuable.

Domain: %s
Hypothesis: %s%s

You MUST respond with type "text" containing ONLY this JSON (no other text):
{"type": "text", "content": "{\"score\": 0.75, \"reason\": \"Brief explanation\"}"}

Or if the system requires direct JSON, return:
{"score": 0.75, "reason": "Brief explanation"}

Examples:
- High impact, testable: {"score": 0.8, "reason": "High value and easily testable"}
- Medium impact, testable: {"score": 0.6, "reason": "Moderate value, testable"}
- Low impact or untestable: {"score": 0.3, "reason": "Low value or difficult to test"}

Now return ONLY the JSON score (no tools, no tasks, just the score):`, domain, h.Description, causalTypeInfo)

		payload := map[string]interface{}{
			"input": prompt,
			"context": map[string]string{
				"origin": "fsm",
			},
		}
		data, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Type", "application/json")
		if pid, ok := e.context["project_id"].(string); ok && pid != "" {
			req.Header.Set("X-Project-ID", pid)
		}

		delayMs := 5000
		if v := strings.TrimSpace(os.Getenv("FSM_LLM_REQUEST_DELAY_MS")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				delayMs = n
			}
		}
		if delayMs > 0 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}

		ctx := context.Background()
		resp, err := Do(ctx, req)
		if err != nil {
			log.Printf("⚠️ [HYP-SCREEN] LLM screening request failed: %v (allowing by default)", err)
			approved = append(approved, h)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		bodyPreview := string(body)
		if len(bodyPreview) > 500 {
			bodyPreview = bodyPreview[:500] + "..."
		}
		log.Printf("🔍 [HYP-SCREEN] Raw LLM response for hypothesis '%s': Status=%d, Body=%s",
			h.Description[:min(50, len(h.Description))], resp.StatusCode, bodyPreview)

		if resp.StatusCode != http.StatusOK {
			log.Printf("⚠️ [HYP-SCREEN] LLM screening status %d: %s (allowing by default)", resp.StatusCode, string(body))
			approved = append(approved, h)
			continue
		}

		// Parse the HDN interpreter response (structured format)
		var interpretResp map[string]interface{}
		score := 0.0
		parseMethod := "none"

		if err := json.Unmarshal(body, &interpretResp); err != nil {
			log.Printf("⚠️ [HYP-SCREEN] Failed to parse response JSON: %v", err)
		} else {
			// HDN returns structured response with tasks/message fields
			// Extract JSON score from task descriptions or message
			var scoreJSON string

			if tasks, ok := interpretResp["tasks"].([]interface{}); ok && len(tasks) > 0 {
				for _, t := range tasks {
					if task, ok := t.(map[string]interface{}); ok {

						if desc, ok := task["description"].(string); ok && desc != "" {

							if strings.Contains(desc, "{") && strings.Contains(desc, "score") {
								scoreJSON = desc
								log.Printf("🔍 [HYP-SCREEN] Found task description with potential JSON: %s", desc[:min(200, len(desc))])
								break
							}
						}
					}
				}
			}

			if scoreJSON == "" {
				if textResp, ok := interpretResp["text_response"].(string); ok && textResp != "" {
					scoreJSON = textResp
					log.Printf("🔍 [HYP-SCREEN] Found text_response field: %s", textResp[:min(100, len(textResp))])
				}
			}

			if scoreJSON == "" {
				if metadata, ok := interpretResp["metadata"].(map[string]interface{}); ok {
					if textResp, ok := metadata["text_response"].(string); ok && textResp != "" {
						scoreJSON = textResp
						log.Printf("🔍 [HYP-SCREEN] Found text_response in metadata: %s", textResp[:min(100, len(textResp))])
					}
				}
			}

			if scoreJSON == "" {
				if msg, ok := interpretResp["message"].(string); ok && msg != "" && msg != "Text response provided" {
					scoreJSON = msg
					log.Printf("🔍 [HYP-SCREEN] Found message field: %s", msg[:min(100, len(msg))])
				}
			}

			if scoreJSON != "" {
				start := strings.Index(scoreJSON, "{")
				end := strings.LastIndex(scoreJSON, "}")
				if start >= 0 && end > start {
					scoreJSON = scoreJSON[start : end+1]
				}
			}

			if scoreJSON != "" {
				var scoreObj map[string]interface{}
				if err := json.Unmarshal([]byte(scoreJSON), &scoreObj); err == nil {
					parseMethod = "json_extracted"
					if v, ok := scoreObj["score"].(float64); ok {
						score = v
						log.Printf("✅ [HYP-SCREEN] Parsed score from extracted JSON: %.2f (reason: %v)", score, scoreObj["reason"])
					} else {

						if v, ok := scoreObj["rating"].(float64); ok {
							score = v
							log.Printf("✅ [HYP-SCREEN] Found 'rating' field instead: %.2f", score)
						} else if v, ok := scoreObj["value"].(float64); ok {
							score = v
							log.Printf("✅ [HYP-SCREEN] Found 'value' field instead: %.2f", score)
						} else {
							log.Printf("⚠️ [HYP-SCREEN] Extracted JSON but no score field. Keys: %v", getMapKeys(scoreObj))
						}
					}
				} else {
					log.Printf("⚠️ [HYP-SCREEN] Failed to parse extracted JSON: %v", err)
				}
			}

			if score == 0.0 {
				if v, ok := interpretResp["score"].(float64); ok {
					score = v
					parseMethod = "json_direct"
					log.Printf("✅ [HYP-SCREEN] Found score at top level: %.2f", score)
				}
			}
		}

		if score == 0.0 {

			parseMethod = "text_fallback"
			s := string(body)
			log.Printf("⚠️ [HYP-SCREEN] JSON parsing failed, trying text extraction from response body")

			if strings.Contains(s, "score") || strings.Contains(s, "rating") {

				parts := strings.Fields(s)
				for i, part := range parts {
					if (strings.Contains(strings.ToLower(part), "score") ||
						strings.Contains(strings.ToLower(part), "rating")) &&
						i+1 < len(parts) {
						// Next part might be the number
						var val float64
						if _, err := fmt.Sscanf(parts[i+1], "%f", &val); err == nil && val >= 0 && val <= 1 {
							score = val
							log.Printf("✅ [HYP-SCREEN] Extracted score from text after keyword: %.2f", score)
							break
						}
					}
				}
			}

			if score == 0.0 {
				for i := 0; i < len(s); i++ {
					if s[i] >= '0' && s[i] <= '9' {
						var val float64
						if _, err := fmt.Sscanf(s[i:], "%f", &val); err == nil && val >= 0 && val <= 1 {
							score = val
							log.Printf("✅ [HYP-SCREEN] Extracted score from text (simple scan): %.2f", score)
							break
						}
					}
				}
			}
		}

		log.Printf("📊 [HYP-SCREEN] Final score: %.2f (method: %s, threshold: %.2f) for hypothesis: %s",
			score, parseMethod, threshold, h.Description[:min(80, len(h.Description))])

		if score >= threshold {
			approved = append(approved, h)
			log.Printf("✅ [HYP-SCREEN] Hypothesis APPROVED (score %.2f >= threshold %.2f)", score, threshold)
		} else {
			log.Printf("🛑 [HYP-SCREEN] Hypothesis FILTERED (score %.2f < threshold %.2f): %s",
				score, threshold, h.Description[:min(80, len(h.Description))])
		}
	}

	return approved
}

// gatherHypothesisEvidence gathers evidence to test a hypothesis
func (e *FSMEngine) gatherHypothesisEvidence(hypothesis map[string]interface{}, domain string) ([]map[string]interface{}, error) {
	var evidence []map[string]interface{}

	description, ok := hypothesis["description"].(string)
	if !ok {
		return nil, fmt.Errorf("hypothesis missing description")
	}

	query := fmt.Sprintf("Find information related to: %s", description)
	beliefs, err := e.reasoning.QueryBeliefs(query, domain)
	if err != nil {
		log.Printf("Warning: Failed to query beliefs for hypothesis: %v", err)
	} else {

		for _, belief := range beliefs {
			evidence = append(evidence, map[string]interface{}{
				"type":       "belief",
				"content":    belief.Statement,
				"confidence": belief.Confidence,
				"source":     "knowledge_base",
				"relevance":  e.calculateRelevance(description, belief.Statement),
			})
		}
	}

	contradictionQuery := fmt.Sprintf("Find information that contradicts: %s", description)
	contradictions, err := e.reasoning.QueryBeliefs(contradictionQuery, domain)
	if err == nil {
		for _, contradiction := range contradictions {
			evidence = append(evidence, map[string]interface{}{
				"type":       "contradiction",
				"content":    contradiction.Statement,
				"confidence": contradiction.Confidence,
				"source":     "knowledge_base",
				"relevance":  e.calculateRelevance(description, contradiction.Statement),
			})
		}
	}

	if len(evidence) == 0 {
		evidence = append(evidence, map[string]interface{}{
			"type":       "synthetic",
			"content":    fmt.Sprintf("No specific evidence found for hypothesis: %s", description),
			"confidence": 0.5,
			"source":     "synthetic",
			"relevance":  0.5,
		})
	}

	return evidence, nil
}

// testHypothesisWithTools tests a hypothesis by creating and using tools
func (e *FSMEngine) testHypothesisWithTools(hypothesis map[string]interface{}, domain string) ([]map[string]interface{}, error) {
	var evidence []map[string]interface{}

	description, ok := hypothesis["description"].(string)
	if !ok {
		return nil, fmt.Errorf("hypothesis missing description")
	}

	log.Printf("🔧 Creating tools to test hypothesis: %s", description)

	toolName := fmt.Sprintf("hypothesis_tester_%d", time.Now().UnixNano())
	toolDescription := fmt.Sprintf("Test the hypothesis: %s", description)

	toolCode, err := e.generateHypothesisTestingTool(toolName, toolDescription, description, domain)
	if err != nil {
		log.Printf("Warning: Failed to generate tool for hypothesis testing: %v", err)

		return e.gatherHypothesisEvidence(hypothesis, domain)
	}

	toolResult, err := e.executeHypothesisTestingTool(toolCode, description, domain)
	if err != nil {
		log.Printf("Warning: Failed to execute hypothesis testing tool: %v", err)

		return e.gatherHypothesisEvidence(hypothesis, domain)
	}

	evidence = append(evidence, map[string]interface{}{
		"type":       "tool_result",
		"content":    toolResult.Result,
		"confidence": toolResult.Confidence,
		"source":     "hypothesis_testing_tool",
		"relevance":  1.0,
		"tool_name":  toolName,
		"success":    toolResult.Success,
	})

	knowledgeEvidence, err := e.gatherHypothesisEvidence(hypothesis, domain)
	if err == nil {
		evidence = append(evidence, knowledgeEvidence...)
	}

	return evidence, nil
}

// generateHypothesisTestingTool creates a tool to test a specific hypothesis
func (e *FSMEngine) generateHypothesisTestingTool(toolName, toolDescription, hypothesis, domain string) (string, error) {
	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	url := fmt.Sprintf("%s/api/v1/learn/llm", base)

	prompt := fmt.Sprintf(`Create a Python tool to test this hypothesis: "%s"

The tool should:
1. Gather relevant data or information
2. Perform analysis or calculations
3. Return a result that supports or refutes the hypothesis
4. Include confidence score (0.0 to 1.0)

Domain: %s
Tool name: %s

Return only the Python code for the tool function.`, hypothesis, domain, toolName)

	payload := map[string]interface{}{
		"task_name":   toolName,
		"description": toolDescription,
		"context": map[string]interface{}{
			"hypothesis": hypothesis,
			"domain":     domain,
			"prompt":     prompt,
		},
		"use_llm": true,
	}

	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if pid, ok := e.context["project_id"].(string); ok && pid != "" {
		req.Header.Set("X-Project-ID", pid)
	}

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tool generation failed with status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if code, ok := result["code"].(string); ok {
		return code, nil
	}

	return "", fmt.Errorf("no code generated in response")
}

// executeHypothesisTestingTool executes a tool to test a hypothesis
func (e *FSMEngine) executeHypothesisTestingTool(toolCode, hypothesis, domain string) (ToolResult, error) {
	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	url := fmt.Sprintf("%s/api/v1/intelligent/execute", base)

	payload := map[string]interface{}{
		"task_name":   "hypothesis_testing",
		"description": fmt.Sprintf("Test hypothesis: %s", hypothesis),
		"language":    "python",
		"context": map[string]interface{}{
			"hypothesis": hypothesis,
			"domain":     domain,
			"code":       toolCode,
		},
		"force_regenerate": false,
	}

	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if pid, ok := e.context["project_id"].(string); ok && pid != "" {
		req.Header.Set("X-Project-ID", pid)
	}

	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return ToolResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ToolResult{}, fmt.Errorf("tool execution failed with status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ToolResult{}, err
	}

	toolResult := ToolResult{
		Success:    true,
		Confidence: 0.5,
		Result:     "Tool executed successfully",
	}

	if success, ok := result["success"].(bool); ok {
		toolResult.Success = success
	}

	if confidence, ok := result["confidence"].(float64); ok {
		toolResult.Confidence = confidence
	}

	if resultText, ok := result["result"].(string); ok {
		toolResult.Result = resultText
	} else if output, ok := result["output"].(string); ok {
		toolResult.Result = output
	}

	return toolResult, nil
}

// evaluateHypothesis evaluates a hypothesis based on gathered evidence
func (e *FSMEngine) evaluateHypothesis(hypothesis map[string]interface{}, evidence []map[string]interface{}, domain string) HypothesisTestResult {

	totalConfidence := 0.0
	supportingEvidence := 0
	contradictingEvidence := 0

	for _, piece := range evidence {
		confidence, _ := piece["confidence"].(float64)
		relevance, _ := piece["relevance"].(float64)
		evidenceType, _ := piece["type"].(string)

		weightedConfidence := confidence * relevance
		totalConfidence += weightedConfidence

		if evidenceType == "belief" {
			supportingEvidence++
		} else if evidenceType == "contradiction" {
			contradictingEvidence++
		}
	}

	avgConfidence := 0.5
	if len(evidence) > 0 {
		avgConfidence = totalConfidence / float64(len(evidence))
	}

	// Determine status based on evidence
	var status string
	var evaluation string

	if supportingEvidence > contradictingEvidence && avgConfidence > 0.8 {
		status = "confirmed"
		evaluation = fmt.Sprintf("Hypothesis supported by %d pieces of evidence with %.2f confidence", supportingEvidence, avgConfidence)
	} else if contradictingEvidence > supportingEvidence && avgConfidence < 0.3 {
		status = "refuted"
		evaluation = fmt.Sprintf("Hypothesis contradicted by %d pieces of evidence with %.2f confidence", contradictingEvidence, avgConfidence)
	} else {
		status = "inconclusive"
		evaluation = fmt.Sprintf("Insufficient evidence to confirm or refute hypothesis (supporting: %d, contradicting: %d, confidence: %.2f)", supportingEvidence, contradictingEvidence, avgConfidence)
	}

	return HypothesisTestResult{
		Status:     status,
		Confidence: avgConfidence,
		Evaluation: evaluation,
		Evidence:   evidence,
	}
}

// calculateRelevance calculates how relevant a piece of evidence is to a hypothesis
func (e *FSMEngine) calculateRelevance(hypothesis, evidence string) float64 {

	hypothesisWords := strings.Fields(strings.ToLower(hypothesis))
	evidenceWords := strings.Fields(strings.ToLower(evidence))

	matches := 0
	for _, hWord := range hypothesisWords {
		for _, eWord := range evidenceWords {
			if hWord == eWord && len(hWord) > 3 {
				matches++
				break
			}
		}
	}

	if len(hypothesisWords) == 0 {
		return 0.0
	}

	relevance := float64(matches) / float64(len(hypothesisWords))
	if relevance > 1.0 {
		relevance = 1.0
	}

	return relevance
}

// processHypothesisResult handles the consequences of hypothesis testing results
func (e *FSMEngine) processHypothesisResult(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	description := hypothesis["description"].(string)

	log.Printf("🔄 Processing hypothesis result: %s (status: %s, confidence: %.2f)", description, result.Status, result.Confidence)

	switch result.Status {
	case "confirmed":
		e.handleConfirmedHypothesis(hypothesis, result, domain)
	case "refuted":
		e.handleRefutedHypothesis(hypothesis, result, domain)
	case "inconclusive":
		e.handleInconclusiveHypothesis(hypothesis, result, domain)
	default:
		log.Printf("⚠️ Unknown hypothesis status: %s", result.Status)
	}
}

// handleConfirmedHypothesis processes a confirmed hypothesis
func (e *FSMEngine) handleConfirmedHypothesis(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	description := hypothesis["description"].(string)
	hypothesisID := hypothesis["id"].(string)

	log.Printf("✅ Hypothesis confirmed: %s", description)

	epistemicUncertainty := EstimateEpistemicUncertainty(len(result.Evidence), true, false)
	aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "")
	uncertainty := NewUncertaintyModel(result.Confidence, epistemicUncertainty, aleatoricUncertainty)

	uncertainty.Reinforce(result.Confidence)

	belief := Belief{
		ID:          fmt.Sprintf("belief_from_hyp_%s", hypothesisID),
		Statement:   description,
		Confidence:  uncertainty.CalibratedConfidence,
		Domain:      domain,
		Source:      "hypothesis_testing",
		Evidence:    []string{hypothesisID},
		CreatedAt:   time.Now(),
		LastUpdated: time.Now(),
		Uncertainty: uncertainty,
		Properties: map[string]interface{}{
			"original_hypothesis_id": hypothesisID,
			"testing_method":         "tool_based",
			"evidence_count":         len(result.Evidence),
		},
	}

	beliefKey := fmt.Sprintf("reasoning:beliefs:%s", domain)
	beliefData, _ := json.Marshal(belief)
	e.redis.LPush(e.ctx, beliefKey, beliefData)
	e.redis.LTrim(e.ctx, beliefKey, 0, 199)

	e.updateDomainKnowledgeFromHypothesis(hypothesis, result, domain)

	e.generateWorkflowsFromHypothesis(hypothesis, result, domain)

	e.generateFollowUpHypotheses(hypothesis, result, domain, "confirmed")

	episode := map[string]interface{}{
		"id":          fmt.Sprintf("ep_hyp_confirmed_%d", time.Now().UnixNano()),
		"type":        "hypothesis_confirmed",
		"description": fmt.Sprintf("Confirmed hypothesis: %s", description),
		"domain":      domain,
		"confidence":  result.Confidence,
		"evidence":    result.Evidence,
		"timestamp":   time.Now().Format(time.RFC3339),
		"properties": map[string]interface{}{
			"hypothesis_id": hypothesisID,
			"learning_type": "hypothesis_confirmation",
		},
	}

	episodeKey := e.getRedisKey("episodes")
	episodeData, _ := json.Marshal(episode)
	e.redis.LPush(e.ctx, episodeKey, episodeData)
	e.redis.LTrim(e.ctx, episodeKey, 0, 99)

	log.Printf("📚 Stored confirmed hypothesis as belief and learning episode")
}

// handleRefutedHypothesis processes a refuted hypothesis
func (e *FSMEngine) handleRefutedHypothesis(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	description := hypothesis["description"].(string)
	hypothesisID := hypothesis["id"].(string)

	log.Printf("❌ Hypothesis refuted: %s", description)

	episode := map[string]interface{}{
		"id":          fmt.Sprintf("ep_hyp_refuted_%d", time.Now().UnixNano()),
		"type":        "hypothesis_refuted",
		"description": fmt.Sprintf("Refuted hypothesis: %s", description),
		"domain":      domain,
		"confidence":  result.Confidence,
		"evidence":    result.Evidence,
		"timestamp":   time.Now().Format(time.RFC3339),
		"properties": map[string]interface{}{
			"hypothesis_id":     hypothesisID,
			"learning_type":     "hypothesis_refutation",
			"refutation_reason": result.Evaluation,
		},
	}

	episodeKey := e.getRedisKey("episodes")
	episodeData, _ := json.Marshal(episode)
	e.redis.LPush(e.ctx, episodeKey, episodeData)
	e.redis.LTrim(e.ctx, episodeKey, 0, 99)

	e.generateFollowUpHypotheses(hypothesis, result, domain, "refuted")

	e.updateDomainConstraintsFromRefutation(hypothesis, result, domain)

	log.Printf("📚 Stored refuted hypothesis as learning episode and generated alternatives")
}

// handleInconclusiveHypothesis processes an inconclusive hypothesis
func (e *FSMEngine) handleInconclusiveHypothesis(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {
	description := hypothesis["description"].(string)
	hypothesisID := hypothesis["id"].(string)

	log.Printf("❓ Hypothesis inconclusive: %s", description)

	episode := map[string]interface{}{
		"id":          fmt.Sprintf("ep_hyp_inconclusive_%d", time.Now().UnixNano()),
		"type":        "hypothesis_inconclusive",
		"description": fmt.Sprintf("Inconclusive hypothesis: %s", description),
		"domain":      domain,
		"confidence":  result.Confidence,
		"evidence":    result.Evidence,
		"timestamp":   time.Now().Format(time.RFC3339),
		"properties": map[string]interface{}{
			"hypothesis_id": hypothesisID,
			"learning_type": "hypothesis_inconclusive",
			"reason":        result.Evaluation,
		},
	}

	episodeKey := e.getRedisKey("episodes")
	episodeData, _ := json.Marshal(episode)
	e.redis.LPush(e.ctx, episodeKey, episodeData)
	e.redis.LTrim(e.ctx, episodeKey, 0, 99)

	e.generateFollowUpHypotheses(hypothesis, result, domain, "inconclusive")

	log.Printf("📚 Stored inconclusive hypothesis for future refinement")
}

// updateDomainKnowledgeFromHypothesis updates domain knowledge with confirmed hypothesis
func (e *FSMEngine) updateDomainKnowledgeFromHypothesis(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {

	insight := map[string]interface{}{
		"type":        "confirmed_hypothesis",
		"description": hypothesis["description"],
		"domain":      domain,
		"confidence":  result.Confidence,
		"evidence":    result.Evidence,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	insightKey := fmt.Sprintf("fsm:%s:domain_insights", e.agentID)
	insightData, _ := json.Marshal(insight)
	e.redis.LPush(e.ctx, insightKey, insightData)
	e.redis.LTrim(e.ctx, insightKey, 0, 99)
}

// updateDomainConstraintsFromRefutation updates domain constraints based on refuted hypothesis
func (e *FSMEngine) updateDomainConstraintsFromRefutation(hypothesis map[string]interface{}, result HypothesisTestResult, domain string) {

	constraint := map[string]interface{}{
		"type":        "refuted_hypothesis",
		"description": fmt.Sprintf("Avoid similar to: %s", hypothesis["description"]),
		"domain":      domain,
		"reason":      result.Evaluation,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	constraintKey := fmt.Sprintf("fsm:%s:domain_constraints", e.agentID)
	constraintData, _ := json.Marshal(constraint)
	e.redis.LPush(e.ctx, constraintKey, constraintData)
	e.redis.LTrim(e.ctx, constraintKey, 0, 99)
}

// generateFollowUpHypotheses generates new hypotheses based on testing results
func (e *FSMEngine) generateFollowUpHypotheses(originalHypothesis map[string]interface{}, result HypothesisTestResult, domain, resultType string) {

	description := originalHypothesis["description"].(string)

	var followUpDescriptions []string

	switch resultType {
	case "confirmed":

		followUpDescriptions = []string{
			fmt.Sprintf("What are the implications of: %s", description),
			fmt.Sprintf("How can we extend: %s", description),
			fmt.Sprintf("What are the limitations of: %s", description),
		}
	case "refuted":

		followUpDescriptions = []string{
			fmt.Sprintf("What is the opposite of: %s", description),
			fmt.Sprintf("What are alternative explanations for the same phenomenon as: %s", description),
			fmt.Sprintf("What are the boundary conditions where: %s might not apply", description),
		}
	case "inconclusive":

		coreHyp := description
		if strings.Contains(description, ": ") {
			parts := strings.SplitN(description, ": ", 2)
			if len(parts) == 2 {

				prefix := strings.ToLower(parts[0])
				if strings.Contains(prefix, "how can we better test") ||
					strings.Contains(prefix, "what additional evidence") ||
					strings.Contains(prefix, "what are the specific conditions") {

					coreHyp = strings.TrimSpace(parts[1])
				}
			}
		}

		followUpDescriptions = []string{
			fmt.Sprintf("Design a specific test to validate: %s", coreHyp),
			fmt.Sprintf("Identify concrete evidence needed to support: %s", coreHyp),
			fmt.Sprintf("Determine the specific conditions where: %s applies", coreHyp),
		}
	}

	for i, followUpDesc := range followUpDescriptions {

		epistemicUncertainty := EstimateEpistemicUncertainty(1, false, false)
		aleatoricUncertainty := EstimateAleatoricUncertainty(domain, "")
		uncertainty := NewUncertaintyModel(0.6, epistemicUncertainty, aleatoricUncertainty)

		followUpHypothesis := Hypothesis{
			ID:          fmt.Sprintf("followup_%s_%d_%d", resultType, time.Now().UnixNano(), i),
			Description: followUpDesc,
			Domain:      domain,
			Confidence:  uncertainty.CalibratedConfidence,
			Status:      "proposed",
			Facts:       []string{originalHypothesis["id"].(string)},
			Constraints: []string{"Must follow domain safety principles"},
			CreatedAt:   time.Now(),
			Uncertainty: uncertainty,
		}

		key := fmt.Sprintf("fsm:%s:hypotheses", e.agentID)
		hypothesisData := map[string]interface{}{
			"id":                followUpHypothesis.ID,
			"description":       followUpHypothesis.Description,
			"domain":            followUpHypothesis.Domain,
			"confidence":        followUpHypothesis.Confidence,
			"status":            followUpHypothesis.Status,
			"facts":             followUpHypothesis.Facts,
			"constraints":       followUpHypothesis.Constraints,
			"created_at":        followUpHypothesis.CreatedAt.Format(time.RFC3339),
			"parent_hypothesis": originalHypothesis["id"],
			"follow_up_type":    resultType,
		}

		data, _ := json.Marshal(hypothesisData)
		e.redis.HSet(e.ctx, key, followUpHypothesis.ID, data)
	}

	log.Printf("🔬 Generated %d follow-up hypotheses for %s result", len(followUpDescriptions), resultType)
}
