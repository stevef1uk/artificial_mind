package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Goal struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func normalizeDescription(desc string) string {
	normalized := strings.ToLower(strings.TrimSpace(desc))
	
	// For behavior loop descriptions, extract just the transition type
	// "Potential behavior loop detected: transition 'perceive->idle' occurred 28 times"
	// becomes "behavior loop detected: transition perceive->idle"
	if strings.Contains(normalized, "behavior loop detected") {
		// Extract the transition part
		if idx := strings.Index(normalized, "transition '"); idx >= 0 {
			start := idx + len("transition '")
			if endIdx := strings.Index(normalized[start:], "'"); endIdx >= 0 {
				transition := normalized[start : start+endIdx]
				return fmt.Sprintf("behavior loop: %s", transition)
			}
		}
	}
	
	// For other inconsistency descriptions, remove numeric details
	// Remove "occurred X times" or similar patterns
	normalized = regexp.MustCompile(`occurred \d+ times`).ReplaceAllString(normalized, "")
	normalized = regexp.MustCompile(`count:\s*\d+`).ReplaceAllString(normalized, "")
	
	return strings.TrimSpace(normalized)
}

func fetchGoals(goalMgrURL, agentID string) ([]Goal, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("%s/goals/%s/active", goalMgrURL, agentID)
	
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch goals: %v", err)
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	
	var goals []Goal
	if err := json.Unmarshal(body, &goals); err == nil {
		return goals, nil
	}
	
	var wrapped struct {
		Goals []Goal `json:"goals"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		return wrapped.Goals, nil
	}
	
	return nil, fmt.Errorf("failed to parse goals response")
}

func deleteGoal(goalMgrURL, goalID string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("%s/goal/%s", goalMgrURL, goalID)
	
	req, _ := http.NewRequest("DELETE", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete goal: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("delete failed (status %d): %s", resp.StatusCode, string(body))
}

func main() {
	goalMgrURL := flag.String("url", "http://localhost:8090", "Goal Manager URL")
	agentID := flag.String("agent", "agent_1", "Agent ID")
	dryRun := flag.Bool("dry-run", false, "Show what would be deleted without actually deleting")
	flag.Parse()
	
	log.Printf("🧹 Starting duplicate goal cleanup...")
	log.Printf("Goal Manager URL: %s", *goalMgrURL)
	log.Printf("Agent ID: %s", *agentID)
	
	goals, err := fetchGoals(*goalMgrURL, *agentID)
	if err != nil {
		log.Fatalf("❌ Failed to fetch goals: %v", err)
	}
	
	if len(goals) == 0 {
		log.Printf("✅ No active goals found. Nothing to clean up.")
		return
	}
	
	log.Printf("📊 Found %d active goals", len(goals))
	
	seenDescriptions := make(map[string]Goal)
	var duplicates []Goal
	
	for _, g := range goals {
		normalized := normalizeDescription(g.Description)
		
		if existing, ok := seenDescriptions[normalized]; ok {
			if g.UpdatedAt.After(existing.UpdatedAt) {
				duplicates = append(duplicates, existing)
				seenDescriptions[normalized] = g
			} else {
				duplicates = append(duplicates, g)
			}
		} else {
			seenDescriptions[normalized] = g
		}
	}
	
	if len(duplicates) == 0 {
		log.Printf("✅ No duplicates found!")
		return
	}
	
	log.Printf("🗑️  Found %d duplicate goals to clean up:", len(duplicates))
	for _, g := range duplicates {
		log.Printf("  - %s: %s (updated: %s)", g.ID, g.Description, g.UpdatedAt.Format(time.RFC3339))
	}
	
	if *dryRun {
		log.Printf("🏁 Dry run complete. Use without --dry-run to actually delete.")
		return
	}
	
	deleted := 0
	for _, g := range duplicates {
		if err := deleteGoal(*goalMgrURL, g.ID); err != nil {
			log.Printf("❌ Failed to delete %s: %v", g.ID, err)
		} else {
			log.Printf("✅ Deleted: %s", g.ID)
			deleted++
		}
	}
	
	log.Printf("\n✅ Cleanup complete! Deleted %d duplicate goals.", deleted)
	
	log.Printf("\n📊 Remaining active goals:")
	remaining, _ := fetchGoals(*goalMgrURL, *agentID)
	for _, g := range remaining {
		log.Printf("  - %s: %s", g.ID, g.Description)
	}
}
