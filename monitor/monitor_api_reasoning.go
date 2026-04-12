package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// getReasoningTraces retrieves reasoning traces for a domain
func (m *MonitorService) getReasoningTraces(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		domain = "General"
	}

	key := fmt.Sprintf("reasoning:traces:%s", domain)
	traces, err := m.redisClient.LRange(context.Background(), key, 0, 9).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch reasoning traces"})
		return
	}

	var reasoningTraces []map[string]interface{}
	for _, traceData := range traces {
		var trace map[string]interface{}
		if err := json.Unmarshal([]byte(traceData), &trace); err == nil {
			reasoningTraces = append(reasoningTraces, trace)
		}
	}

	c.JSON(http.StatusOK, gin.H{"traces": reasoningTraces})
}

// getBeliefs retrieves beliefs for a domain
func (m *MonitorService) getBeliefs(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		domain = "General"
	}

	key := fmt.Sprintf("reasoning:beliefs:%s", domain)
	beliefs, err := m.redisClient.LRange(context.Background(), key, 0, 9).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch beliefs"})
		return
	}

	var beliefList []map[string]interface{}
	for _, beliefData := range beliefs {
		var belief map[string]interface{}
		if err := json.Unmarshal([]byte(beliefData), &belief); err == nil {
			beliefList = append(beliefList, belief)
		}
	}

	c.JSON(http.StatusOK, gin.H{"beliefs": beliefList})
}

// getHypotheses retrieves hypotheses for a domain
func (m *MonitorService) getHypotheses(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		domain = "General"
	}

	key := fmt.Sprintf("fsm:agent_1:hypotheses")
	hypotheses, err := m.redisClient.HGetAll(context.Background(), key).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch hypotheses"})
		return
	}

	var hypothesisList []map[string]interface{}
	for _, hypothesisData := range hypotheses {
		var hypothesis map[string]interface{}
		if err := json.Unmarshal([]byte(hypothesisData), &hypothesis); err == nil {

			if domain == "General" || hypothesis["domain"] == domain {
				hypothesisList = append(hypothesisList, hypothesis)
			}
		}
	}

	sort.Slice(hypothesisList, func(i, j int) bool {
		ti, _ := hypothesisList[i]["created_at"].(string)
		tj, _ := hypothesisList[j]["created_at"].(string)
		return ti > tj
	})

	if len(hypothesisList) > 20 {
		hypothesisList = hypothesisList[:20]
	}

	c.JSON(http.StatusOK, gin.H{"hypotheses": hypothesisList})
}

// getCuriosityGoals retrieves curiosity goals for a domain
func (m *MonitorService) getCuriosityGoals(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		domain = "General"
	}

	key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	goals, err := m.redisClient.LRange(context.Background(), key, 0, 9).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch curiosity goals"})
		return
	}

	var goalList []map[string]interface{}
	for _, goalData := range goals {
		var goal map[string]interface{}
		if err := json.Unmarshal([]byte(goalData), &goal); err == nil {
			goalList = append(goalList, goal)
		}
	}

	seen := map[string]bool{}
	var deduped []map[string]interface{}
	for _, g := range goalList {
		t := ""
		if v, ok := g["type"].(string); ok {
			t = v
		}
		d := ""
		if v, ok := g["description"].(string); ok {
			d = v
		}
		k := strings.ToLower(strings.TrimSpace(t + ":" + d))
		if k == ":" || k == "" {
			if v, ok := g["id"].(string); ok && v != "" {
				k = v
			}
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		deduped = append(deduped, g)
	}

	c.JSON(http.StatusOK, gin.H{"goals": deduped})
}

// getReasoningExplanations retrieves reasoning explanations for a goal
func (m *MonitorService) getReasoningExplanations(c *gin.Context) {
	goal := c.Param("goal")
	if goal == "" {
		goal = "general"
	}

	key := fmt.Sprintf("reasoning:explanations:%s", goal)
	explanations, err := m.redisClient.LRange(context.Background(), key, 0, 9).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch explanations"})
		return
	}

	var explanationList []map[string]interface{}
	for _, expData := range explanations {
		var explanation map[string]interface{}
		if err := json.Unmarshal([]byte(expData), &explanation); err == nil {
			explanationList = append(explanationList, explanation)
		}
	}

	c.JSON(http.StatusOK, gin.H{"explanations": explanationList})
}

// getReasoningDomains lists domains that currently have beliefs or curiosity goals
func (m *MonitorService) getReasoningDomains(c *gin.Context) {
	// Use SCAN to avoid blocking Redis for large keyspaces
	type result struct {
		Domains          []string `json:"domains"`
		BeliefDomains    []string `json:"belief_domains"`
		CuriosityDomains []string `json:"curiosity_domains"`
	}

	unique := func(items []string) []string {
		seen := map[string]bool{}
		out := make([]string, 0, len(items))
		for _, it := range items {
			if it == "" || seen[it] {
				continue
			}
			seen[it] = true
			out = append(out, it)
		}
		return out
	}

	scanDomains := func(pattern string, prefix string) []string {
		var cursor uint64
		domains := []string{}
		for i := 0; i < 50; i++ {
			keys, cur, err := m.redisClient.Scan(context.Background(), cursor, pattern, 100).Result()
			if err != nil {
				break
			}
			for _, k := range keys {
				if strings.HasPrefix(k, prefix) {
					d := strings.TrimPrefix(k, prefix)
					if d != "" {
						domains = append(domains, d)
					}
				}
			}
			cursor = cur
			if cursor == 0 {
				break
			}
		}
		return unique(domains)
	}

	beliefs := scanDomains("reasoning:beliefs:*", "reasoning:beliefs:")
	curiosity := scanDomains("reasoning:curiosity_goals:*", "reasoning:curiosity_goals:")

	neo4jDomains := []string{}
	if m.hdnURL != "" {

		searchURL := fmt.Sprintf("%s/api/v1/knowledge/concepts?limit=1000", m.hdnURL)
		if resp, err := http.Get(searchURL); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var conceptResult struct {
					Concepts []struct {
						Domain string `json:"domain"`
					} `json:"concepts"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&conceptResult); err == nil {
					domainSet := make(map[string]bool)
					for _, concept := range conceptResult.Concepts {
						domain := strings.TrimSpace(concept.Domain)

						if domain != "" && domain != "General" && !isSourceIdentifier(domain) {
							domainSet[domain] = true
						}
					}
					for domain := range domainSet {
						neo4jDomains = append(neo4jDomains, domain)
					}
				}
			}
		}
	}

	union := append([]string{}, beliefs...)
	union = append(union, curiosity...)
	union = append(union, neo4jDomains...)

	filtered := make([]string, 0, len(union))
	for _, d := range union {
		if !isSourceIdentifier(d) {
			filtered = append(filtered, d)
		}
	}
	union = unique(filtered)

	c.JSON(http.StatusOK, result{Domains: union, BeliefDomains: beliefs, CuriosityDomains: curiosity})
}

// getReflection provides comprehensive introspection of the system's current mental state
func (m *MonitorService) getReflection(c *gin.Context) {
	ctx := context.Background()
	domain := c.DefaultQuery("domain", "General")
	limit := 10

	reflection := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"domain":    domain,
	}

	fsmThinking := map[string]interface{}{}
	if resp, err := http.Get(m.fsmURL + "/thinking"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var thinking map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&thinking) == nil {
				fsmThinking = thinking
			}
		}
	}
	reflection["fsm_thinking"] = fsmThinking

	tracesKey := fmt.Sprintf("reasoning:traces:%s", domain)
	traces, _ := m.redisClient.LRange(ctx, tracesKey, 0, 9).Result()
	var reasoningTraces []map[string]interface{}
	for _, traceData := range traces {
		var trace map[string]interface{}
		if json.Unmarshal([]byte(traceData), &trace) == nil {
			reasoningTraces = append(reasoningTraces, trace)
		}
	}
	reflection["reasoning_traces"] = reasoningTraces

	beliefsKey := fmt.Sprintf("reasoning:beliefs:%s", domain)
	beliefs, _ := m.redisClient.LRange(ctx, beliefsKey, 0, int64(limit-1)).Result()
	var beliefList []map[string]interface{}
	for _, beliefData := range beliefs {
		var belief map[string]interface{}
		if json.Unmarshal([]byte(beliefData), &belief) == nil {
			beliefList = append(beliefList, belief)
		}
	}
	reflection["beliefs"] = beliefList

	curiosityKey := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)
	goals, _ := m.redisClient.LRange(ctx, curiosityKey, 0, int64(limit-1)).Result()
	var goalList []map[string]interface{}
	for _, goalData := range goals {
		var goal map[string]interface{}
		if json.Unmarshal([]byte(goalData), &goal) == nil {
			goalList = append(goalList, goal)
		}
	}
	reflection["curiosity_goals"] = goalList

	hypothesesKey := "fsm:agent_1:hypotheses"
	hypotheses, _ := m.redisClient.HGetAll(ctx, hypothesesKey).Result()
	var hypothesisList []map[string]interface{}
	count := 0
	for _, hypothesisData := range hypotheses {
		if count >= limit {
			break
		}
		var hypothesis map[string]interface{}
		if json.Unmarshal([]byte(hypothesisData), &hypothesis) == nil {
			if domain == "General" || hypothesis["domain"] == domain {
				hypothesisList = append(hypothesisList, hypothesis)
				count++
			}
		}
	}
	reflection["hypotheses"] = hypothesisList

	if resp, err := http.Get(m.hdnURL + "/api/v1/tools/calls/recent?limit=" + fmt.Sprintf("%d", limit)); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var toolCalls map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&toolCalls) == nil {
				reflection["recent_tool_calls"] = toolCalls["calls"]
			}
		}
	}

	if resp, err := http.Get(m.hdnURL + "/api/v1/memory/summary"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var summary map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&summary) == nil {
				reflection["working_memory"] = summary
			}
		}
	}

	if resp, err := http.Get(m.goalMgrURL + "/goals/agent_1/active"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var goals map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&goals) == nil {
				reflection["active_goals"] = goals
			}
		}
	}

	explanationsKey := "reasoning:explanations"
	explanations, _ := m.redisClient.LRange(ctx, explanationsKey, 0, int64(limit-1)).Result()
	var explanationList []map[string]interface{}
	for _, expData := range explanations {
		var exp map[string]interface{}
		if json.Unmarshal([]byte(expData), &exp) == nil {
			explanationList = append(explanationList, exp)
		}
	}
	reflection["explanations"] = explanationList

	reflection["summary"] = map[string]interface{}{
		"reasoning_traces_count": len(reasoningTraces),
		"beliefs_count":          len(beliefList),
		"curiosity_goals_count":  len(goalList),
		"hypotheses_count":       len(hypothesisList),
		"explanations_count":     len(explanationList),
		"fsm_state":              fsmThinking["current_state"],
		"fsm_thinking_focus":     fsmThinking["thinking_focus"],
	}

	c.JSON(http.StatusOK, reflection)
}

// getRecentExplanations retrieves recent explanations across all goals
func (m *MonitorService) getRecentExplanations(c *gin.Context) {
	ctx := context.Background()

	keys, err := m.redisClient.Keys(ctx, "reasoning:explanations:*").Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch explanation keys"})
		return
	}

	var allExplanations []map[string]interface{}

	maxKeys := 5
	if len(keys) > maxKeys {
		keys = keys[:maxKeys]
	}
	for _, key := range keys {
		explanations, err := m.redisClient.LRange(ctx, key, 0, 2).Result()
		if err != nil {
			continue
		}

		for _, expData := range explanations {
			var explanation map[string]interface{}
			if err := json.Unmarshal([]byte(expData), &explanation); err == nil {
				allExplanations = append(allExplanations, explanation)
			}
		}
	}

	sort.Slice(allExplanations, func(i, j int) bool {
		timeI, _ := allExplanations[i]["generated_at"].(string)
		timeJ, _ := allExplanations[j]["generated_at"].(string)
		return timeI > timeJ
	})

	if len(allExplanations) > 20 {
		allExplanations = allExplanations[:20]
	}

	c.JSON(http.StatusOK, gin.H{"explanations": allExplanations})
}
