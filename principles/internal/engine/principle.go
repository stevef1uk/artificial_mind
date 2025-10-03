package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type JsonRule struct {
	Name        string `json:"name"`
	Priority    int    `json:"priority"`
	Action      string `json:"action"`    // "*" = all actions
	Condition   string `json:"condition"` // simple conditions like key==value, key!=value, or AND
	DenyMessage string `json:"deny_message"`
}

type DynamicPrinciple struct {
	rules []JsonRule
}

// Load principles from JSON file
func LoadPrinciplesFromFile(path string) (*DynamicPrinciple, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rules []JsonRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}

	// Sort by priority (lower number = higher priority)
	for i := 0; i < len(rules)-1; i++ {
		for j := i + 1; j < len(rules); j++ {
			if rules[i].Priority > rules[j].Priority {
				rules[i], rules[j] = rules[j], rules[i]
			}
		}
	}

	// Debug: Print loaded rules
	fmt.Printf("Loaded %d rules:\n", len(rules))
	for i, rule := range rules {
		fmt.Printf("  %d: Action='%s', Condition='%s', Message='%s'\n", i, rule.Action, rule.Condition, rule.DenyMessage)
	}

	return &DynamicPrinciple{rules: rules}, nil
}

// Evaluate a dynamic rule
func (dp *DynamicPrinciple) Check(action string, params, context map[string]interface{}) (bool, []string) {
	reasons := []string{}

	for _, r := range dp.rules {
		// Check if rule applies to this action
		if r.Action != "*" && r.Action != action {
			continue
		}

		// Evaluate condition
		conditionMet := r.Condition == "" || evaluateCondition(r.Condition, context)

		if conditionMet {
			reasons = append(reasons, r.DenyMessage)
			// Return immediately on first matching rule (highest priority)
			return false, reasons
		}
	}

	return len(reasons) == 0, reasons
}

// Simple condition evaluator: supports key==value, key!=value, AND
func evaluateCondition(cond string, context map[string]interface{}) bool {
	// Handle empty condition
	if strings.TrimSpace(cond) == "" {
		return true
	}

	clauses := strings.Split(cond, "&&")
	for _, c := range clauses {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		var key, val string
		if strings.Contains(c, "==") {
			parts := strings.Split(c, "==")
			if len(parts) != 2 {
				return false
			}
			key = strings.TrimSpace(parts[0])
			val = strings.TrimSpace(parts[1])
			if ctxVal, ok := context[key]; !ok || fmt.Sprintf("%v", ctxVal) != val {
				return false
			}
		} else if strings.Contains(c, "!=") {
			parts := strings.Split(c, "!=")
			if len(parts) != 2 {
				return false
			}
			key = strings.TrimSpace(parts[0])
			val = strings.TrimSpace(parts[1])
			if ctxVal, ok := context[key]; ok && fmt.Sprintf("%v", ctxVal) == val {
				return false
			}
		} else {
			// Unknown condition format, skip
			return false
		}
	}
	return true
}
