package api

import (
	"encoding/json"
	"net/http"
	"principles/internal/engine"
)

type ActionRequest struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	Context map[string]interface{} `json:"context"`
}

type ActionResponse struct {
	Result  string   `json:"result,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
}

func MakeHandler(e *engine.Engine, perform func(map[string]interface{}) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		result, reasons := e.Execute(req.Action, req.Params, req.Context, perform)
		resp := ActionResponse{Result: result, Reasons: reasons}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// Plan check request/response types
type PlanCheckRequest struct {
	Plan interface{} `json:"plan"`
}

type PlanCheckResponse struct {
	Blocked bool   `json:"blocked"`
	Reason  string `json:"reason"`
}

// MakePlanCheckHandler creates a handler for the /check-plan endpoint
func MakePlanCheckHandler(e *engine.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req PlanCheckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// Extract plan information for principles check
		planData, ok := req.Plan.(map[string]interface{})
		if !ok {
			http.Error(w, "invalid plan format", http.StatusBadRequest)
			return
		}

		// Extract goal and steps from plan
		goal, goalOk := planData["goal"].(map[string]interface{})
		steps, stepsOk := planData["steps"].([]interface{})

		if !goalOk || !stepsOk {
			http.Error(w, "invalid plan structure", http.StatusBadRequest)
			return
		}

		// Check if the plan violates any principles
		blocked, reason := checkPlanAgainstPrinciples(goal, steps)

		resp := PlanCheckResponse{
			Blocked: blocked,
			Reason:  reason,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// checkPlanAgainstPrinciples performs the actual principles check
func checkPlanAgainstPrinciples(goal map[string]interface{}, steps []interface{}) (bool, string) {
	// Extract goal type and parameters
	goalType, _ := goal["type"].(string)
	goalParams, _ := goal["params"].(map[string]interface{})

	// Check for malicious actions
	if isMaliciousAction(goalType, goalParams) {
		return true, "Action violates safety principles: potentially harmful operation detected"
	}

	// Check each step in the plan
	for _, step := range steps {
		stepMap, ok := step.(map[string]interface{})
		if !ok {
			continue
		}

		// Extract capability ID and arguments
		capabilityID, _ := stepMap["capability_id"].(string)
		args, _ := stepMap["args"].(map[string]interface{})

		// Check if this step is malicious
		if isMaliciousStep(capabilityID, args) {
			return true, "Step violates safety principles: potentially harmful operation detected"
		}
	}

	return false, ""
}

// isMaliciousAction checks if a goal action is malicious
func isMaliciousAction(goalType string, params map[string]interface{}) bool {
	// Check for file deletion operations
	if containsIgnoreCase(goalType, "delete") || containsIgnoreCase(goalType, "remove") {
		return true
	}

	// Check for system modification operations
	if containsIgnoreCase(goalType, "system") || containsIgnoreCase(goalType, "admin") {
		return true
	}

	// Check for inappropriate content generation
	if containsIgnoreCase(goalType, "inappropriate") || containsIgnoreCase(goalType, "adult") {
		return true
	}

	// Check parameters for dangerous operations
	for key, value := range params {
		if containsIgnoreCase(key, "delete") || containsIgnoreCase(key, "remove") {
			return true
		}
		if str, ok := value.(string); ok {
			if containsIgnoreCase(str, "delete") || containsIgnoreCase(str, "remove") {
				return true
			}
		}
	}

	return false
}

// isMaliciousStep checks if a plan step is malicious
func isMaliciousStep(capabilityID string, args map[string]interface{}) bool {
	// Check capability ID for malicious patterns
	if containsIgnoreCase(capabilityID, "delete") || containsIgnoreCase(capabilityID, "remove") {
		return true
	}

	// Check arguments for dangerous operations
	for key, value := range args {
		if containsIgnoreCase(key, "delete") || containsIgnoreCase(key, "remove") {
			return true
		}
		if str, ok := value.(string); ok {
			if containsIgnoreCase(str, "delete") || containsIgnoreCase(str, "remove") {
				return true
			}
		}
	}

	return false
}

// containsIgnoreCase checks if a string contains another string (case insensitive)
func containsIgnoreCase(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}

	// Simple case-insensitive contains check
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// toLower converts a character to lowercase
func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
