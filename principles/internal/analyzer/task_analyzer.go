package analyzer

import (
	"encoding/json"
	"regexp"
	"strings"
)

// TaskAnalyzer analyzes tasks to extract ethical context
type TaskAnalyzer struct {
	// Patterns for detecting potentially harmful actions
	harmPatterns    []*regexp.Regexp
	stealPatterns   []*regexp.Regexp
	damagePatterns  []*regexp.Regexp
	accessPatterns  []*regexp.Regexp
	commandPatterns []*regexp.Regexp
}

// TaskAnalysis represents the result of analyzing a task
type TaskAnalysis struct {
	Action          string                 `json:"action"`
	EthicalContext  map[string]interface{} `json:"ethical_context"`
	RiskLevel       string                 `json:"risk_level"` // "low", "medium", "high"
	Confidence      float64                `json:"confidence"` // 0.0 to 1.0
	ExtractedParams map[string]interface{} `json:"extracted_params"`
	Warnings        []string               `json:"warnings"`
}

// NewTaskAnalyzer creates a new task analyzer with predefined patterns
func NewTaskAnalyzer() *TaskAnalyzer {
	return &TaskAnalyzer{
		harmPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(harm|hurt|injure|damage|destroy|kill|attack|strike|hit|punch|kick)`),
			regexp.MustCompile(`(?i)(dangerous|unsafe|risky|hazardous)`),
			regexp.MustCompile(`(?i)(weapon|knife|gun|explosive|poison)`),
		},
		stealPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(steal|take|grab|snatch|rob|burgle|pilfer|thieve)`),
			regexp.MustCompile(`(?i)(unauthorized|illegal|forbidden|prohibited)`),
			regexp.MustCompile(`(?i)(belongs to|property of|not mine|someone else)`),
		},
		damagePatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(break|smash|crush|demolish|ruin|wreck|destroy)`),
			regexp.MustCompile(`(?i)(vandalize|deface|degrade|corrupt)`),
		},
		accessPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(access|enter|break into|hack|penetrate|bypass)`),
			regexp.MustCompile(`(?i)(private|confidential|secure|restricted|classified)`),
			regexp.MustCompile(`(?i)(password|key|code|credential|authentication)`),
		},
		commandPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(obey|follow|execute|carry out|do as told|comply)`),
			regexp.MustCompile(`(?i)(order|command|instruction|directive|request)`),
		},
	}
}

// AnalyzeTask analyzes a task description and extracts ethical context
func (ta *TaskAnalyzer) AnalyzeTask(taskName, description string, context map[string]interface{}) *TaskAnalysis {
	analysis := &TaskAnalysis{
		Action:          taskName,
		EthicalContext:  make(map[string]interface{}),
		RiskLevel:       "low",
		Confidence:      0.5,
		ExtractedParams: make(map[string]interface{}),
		Warnings:        []string{},
	}

	// Combine task name and description for analysis
	fullText := strings.ToLower(taskName + " " + description)

	// Analyze for human harm
	analysis.EthicalContext["human_harm"] = ta.detectHumanHarm(fullText)
	analysis.EthicalContext["self_harm"] = ta.detectSelfHarm(fullText)
	analysis.EthicalContext["human_order"] = ta.detectHumanOrder(fullText, context)
	analysis.EthicalContext["stealing"] = ta.detectStealing(fullText)
	analysis.EthicalContext["damage"] = ta.detectDamage(fullText)
	analysis.EthicalContext["unauthorized_access"] = ta.detectUnauthorizedAccess(fullText)

	// Extract parameters from description
	analysis.ExtractedParams = ta.extractParameters(description)

	// Determine risk level
	analysis.RiskLevel = ta.calculateRiskLevel(analysis.EthicalContext)

	// Calculate confidence based on pattern matches
	analysis.Confidence = ta.calculateConfidence(analysis.EthicalContext)

	// Generate warnings
	analysis.Warnings = ta.generateWarnings(analysis.EthicalContext)

	return analysis
}

// detectHumanHarm checks if the task involves harming humans
func (ta *TaskAnalyzer) detectHumanHarm(text string) bool {
	for _, pattern := range ta.harmPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// detectSelfHarm checks if the task involves self-harm
func (ta *TaskAnalyzer) detectSelfHarm(text string) bool {
	selfHarmPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(self.*harm|harm.*self|suicide|self.*destruct)`),
		regexp.MustCompile(`(?i)(disable.*self|shut.*down.*self|destroy.*self)`),
	}

	for _, pattern := range selfHarmPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// detectHumanOrder checks if the task is in response to a human order
func (ta *TaskAnalyzer) detectHumanOrder(text string, context map[string]interface{}) bool {
	// Check context first
	if order, ok := context["human_order"].(bool); ok {
		return order
	}

	// Check text for command patterns
	for _, pattern := range ta.commandPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// detectStealing checks if the task involves stealing
func (ta *TaskAnalyzer) detectStealing(text string) bool {
	for _, pattern := range ta.stealPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// detectDamage checks if the task involves damage
func (ta *TaskAnalyzer) detectDamage(text string) bool {
	for _, pattern := range ta.damagePatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// detectUnauthorizedAccess checks if the task involves unauthorized access
func (ta *TaskAnalyzer) detectUnauthorizedAccess(text string) bool {
	for _, pattern := range ta.accessPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// extractParameters extracts parameters from task description
func (ta *TaskAnalyzer) extractParameters(description string) map[string]interface{} {
	params := make(map[string]interface{})

	// Extract common parameters using regex
	patterns := map[string]*regexp.Regexp{
		"location": regexp.MustCompile(`(?i)(?:to|at|in|from)\s+([a-zA-Z0-9\s]+)`),
		"item":     regexp.MustCompile(`(?i)(?:the|a|an)\s+([a-zA-Z0-9\s]+?)(?:\s|$|,|\.)`),
		"target":   regexp.MustCompile(`(?i)(?:target|aim|focus on)\s+([a-zA-Z0-9\s]+)`),
		"speed":    regexp.MustCompile(`(?i)(?:at|with)\s+(fast|slow|normal|high|low)\s+(?:speed|pace)`),
		"urgency":  regexp.MustCompile(`(?i)(urgent|immediate|asap|quickly|emergency)`),
	}

	for key, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(description, -1)
		if len(matches) > 0 {
			params[key] = matches[0][1]
		}
	}

	return params
}

// calculateRiskLevel determines the risk level based on ethical context
func (ta *TaskAnalyzer) calculateRiskLevel(context map[string]interface{}) string {
	riskScore := 0

	if harm, ok := context["human_harm"].(bool); ok && harm {
		riskScore += 3
	}
	if steal, ok := context["stealing"].(bool); ok && steal {
		riskScore += 2
	}
	if damage, ok := context["damage"].(bool); ok && damage {
		riskScore += 2
	}
	if access, ok := context["unauthorized_access"].(bool); ok && access {
		riskScore += 2
	}
	if selfHarm, ok := context["self_harm"].(bool); ok && selfHarm {
		riskScore += 1
	}

	if riskScore >= 3 {
		return "high"
	} else if riskScore >= 1 {
		return "medium"
	}
	return "low"
}

// calculateConfidence calculates confidence in the analysis
func (ta *TaskAnalyzer) calculateConfidence(context map[string]interface{}) float64 {
	confidence := 0.5 // Base confidence

	// Increase confidence based on clear indicators
	for _, value := range context {
		if boolVal, ok := value.(bool); ok && boolVal {
			confidence += 0.1
		}
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// generateWarnings generates warnings based on ethical context
func (ta *TaskAnalyzer) generateWarnings(context map[string]interface{}) []string {
	warnings := []string{}

	if harm, ok := context["human_harm"].(bool); ok && harm {
		warnings = append(warnings, "This task may harm humans")
	}
	if steal, ok := context["stealing"].(bool); ok && steal {
		warnings = append(warnings, "This task involves stealing or taking unauthorized items")
	}
	if damage, ok := context["damage"].(bool); ok && damage {
		warnings = append(warnings, "This task may cause damage")
	}
	if access, ok := context["unauthorized_access"].(bool); ok && access {
		warnings = append(warnings, "This task involves unauthorized access")
	}

	return warnings
}

// ToJSON converts the analysis to JSON
func (ta *TaskAnalysis) ToJSON() ([]byte, error) {
	return json.MarshalIndent(ta, "", "  ")
}
