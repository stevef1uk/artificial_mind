package conversational

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
)

// IntentParser analyzes natural language input to determine user intent
type IntentParser struct {
	llmClient LLMClientInterface
}

// Intent represents the parsed intent from user input
type Intent struct {
	Type            string                 `json:"type"`
	Goal            string                 `json:"goal"`
	Confidence      float64                `json:"confidence"`
	Entities        map[string]string      `json:"entities"`
	OriginalMessage string                 `json:"original_message"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// LLMClientInterface defines the interface for LLM operations
type LLMClientInterface interface {
	GenerateResponse(ctx context.Context, prompt string, maxTokens int) (string, error)
	ClassifyText(ctx context.Context, text string, categories []string) (string, float64, error)
	ExtractEntities(ctx context.Context, text string, entityTypes []string) (map[string]string, error)
}

// NewIntentParser creates a new intent parser
func NewIntentParser(llmClient LLMClientInterface) *IntentParser {
	return &IntentParser{
		llmClient: llmClient,
	}
}

// ParseIntent analyzes user input to determine intent
func (ip *IntentParser) ParseIntent(ctx context.Context, message string, context map[string]string) (*Intent, error) {
	log.Printf("🧠 [INTENT-PARSER] Analyzing message: %s", message)

	// Step 0: Check for hardcoded personal patterns first (highest priority)
	personalPatterns := []string{
		"remember",
		"my name is",
		"i prefer",
		"i live in",
		"call me",
		"i'm known as",
		"i am known as",
		"born in",
		"i work at",
		"my birthday",
		"my children",
		"my favorite",
		"is my name",
		"are my children",
	}
	for _, p := range personalPatterns {
		if strings.Contains(strings.ToLower(message), p) {
			return &Intent{
				Type:            "personal_update",
				Confidence:      0.99,
				Goal:            "Save the following personal information to long-term memory: " + message,
				OriginalMessage: message,
				Entities:        map[string]string{"content": message},
			}, nil
		}
	}

	// Step 1: Classify the type of intent
	intentType, confidence, err := ip.classifyIntent(ctx, message)
	if err != nil {
		log.Printf("⚠️ [INTENT-PARSER] Intent classification failed, using fallback: %v", err)
		intentType = "general_conversation"
		confidence = 0.5
	}

	// Step 2: Extract entities from the message
	entities, err := ip.extractEntities(ctx, message, intentType)
	if err != nil {
		log.Printf("⚠️ [INTENT-PARSER] Entity extraction failed: %v", err)
		entities = make(map[string]string)
	}

	// Step 3: Generate goal statement
	goal, err := ip.generateGoal(ctx, message, intentType, entities)
	if err != nil {
		log.Printf("⚠️ [INTENT-PARSER] Goal generation failed: %v", err)
		goal = message // Fallback to original message
	}

	// Step 4: Apply rule-based refinements
	intentType, confidence = ip.applyRuleBasedRefinements(message, intentType, confidence)

	return &Intent{
		Type:            intentType,
		Goal:            goal,
		Confidence:      confidence,
		Entities:        entities,
		OriginalMessage: message,
		Metadata: map[string]interface{}{
			"parsed_at":    "now",
			"context_keys": len(context),
		},
	}, nil
}

// classifyIntent uses LLM to classify the intent type
func (ip *IntentParser) classifyIntent(ctx context.Context, message string) (string, float64, error) {
	categories := []string{
		"query",                // Asking for information
		"task",                 // Requesting action execution
		"plan",                 // Requesting planning/strategy
		"learn",                // Requesting learning/teaching
		"explain",              // Requesting explanation
		"general_conversation", // General chat
		"help",                 // Requesting help
		"debug",                // Debugging/technical support
		"personal_update",      // Sharing personal information to be remembered
	}

	// Use LLM for classification
	intentType, confidence, err := ip.llmClient.ClassifyText(ctx, message, categories)
	if err != nil {
		// Fallback to rule-based classification
		return ip.ruleBasedClassification(message), 0.6, nil
	}

	return intentType, confidence, nil
}

// ruleBasedClassification provides fallback classification using patterns
func (ip *IntentParser) ruleBasedClassification(message string) string {
	message = strings.ToLower(message)

	// Personal update patterns
	personalPatterns := []string{
		`remember`,
		`my name is`,
		`i prefer`,
		`call me`,
		`i'm known as`,
		`i am known as`,
		`born in`,
		`i live in`,
		`i work at`,
		`my birthday`,
	}
	for _, pattern := range personalPatterns {
		if strings.Contains(message, pattern) {
			return "personal_update"
		}
	}

	// Query patterns
	queryPatterns := []string{
		`what is`,
		`what are`,
		`how does`,
		`how do`,
		`tell me about`,
		`explain`,
		`describe`,
		`define`,
		`what's the`,
		`can you tell me`,
	}
	for _, pattern := range queryPatterns {
		if matched, _ := regexp.MatchString(pattern, message); matched {
			return "query"
		}
	}

	// Task patterns
	taskPatterns := []string{
		`do this`,
		`execute`,
		`run`,
		`perform`,
		`create`,
		`build`,
		`make`,
		`generate`,
		`calculate`,
		`compute`,
		`scrape`,
		`fetch`,
		`get data from`,
		`download`,
		`retrieve`,
		`can you scrape`,
		`can you fetch`,
		`can you get`,
	}
	for _, pattern := range taskPatterns {
		if matched, _ := regexp.MatchString(pattern, message); matched {
			return "task"
		}
	}

	// Plan patterns
	planPatterns := []string{
		`plan`,
		`strategy`,
		`approach`,
		`how should`,
		`what steps`,
		`roadmap`,
		`schedule`,
		`timeline`,
	}
	for _, pattern := range planPatterns {
		if matched, _ := regexp.MatchString(pattern, message); matched {
			return "plan"
		}
	}

	// Learn patterns
	learnPatterns := []string{
		`learn`,
		`teach`,
		`study`,
		`understand`,
		`research`,
		`investigate`,
		`explore`,
	}
	for _, pattern := range learnPatterns {
		if matched, _ := regexp.MatchString(pattern, message); matched {
			return "learn"
		}
	}

	// Help patterns
	helpPatterns := []string{
		`help`,
		`assist`,
		`support`,
		`guide`,
		`tutorial`,
		`how to`,
	}
	for _, pattern := range helpPatterns {
		if matched, _ := regexp.MatchString(pattern, message); matched {
			return "help"
		}
	}

	return "general_conversation"
}

// extractEntities extracts relevant entities from the message
func (ip *IntentParser) extractEntities(ctx context.Context, message string, intentType string) (map[string]string, error) {
	entityTypes := []string{
		"query",       // The actual question or query
		"topic",       // The subject matter
		"domain",      // The knowledge domain
		"task",        // The specific task to perform
		"objective",   // The goal or objective
		"constraints", // Any constraints or requirements
		"context",     // Additional context
		"level",       // Complexity or detail level
		"source",      // Information source
	}

	// Use LLM for entity extraction
	entities, err := ip.llmClient.ExtractEntities(ctx, message, entityTypes)
	if err != nil {
		// Fallback to rule-based extraction
		return ip.ruleBasedEntityExtraction(message, intentType), nil
	}

	return entities, nil
}

// ruleBasedEntityExtraction provides fallback entity extraction
func (ip *IntentParser) ruleBasedEntityExtraction(message string, intentType string) map[string]string {
	entities := make(map[string]string)

	// Extract query for query intents
	if intentType == "query" {
		// Remove common question words to get the core query
		query := message
		questionWords := []string{"what is", "what are", "how does", "how do", "tell me about", "explain", "describe", "define", "what's the", "can you tell me"}
		for _, word := range questionWords {
			if strings.Contains(strings.ToLower(query), word) {
				query = strings.TrimSpace(strings.Replace(strings.ToLower(query), word, "", 1))
				break
			}
		}
		entities["query"] = query
		entities["topic"] = query
	}

	// Extract task for task intents
	if intentType == "task" {
		entities["task"] = message
		entities["objective"] = message
	}

	// Extract objective for plan intents
	if intentType == "plan" {
		entities["objective"] = message
	}

	// Extract topic for learn intents
	if intentType == "learn" {
		entities["topic"] = message
		entities["source"] = "user_input"
	}

	// Extract concept for explain intents
	if intentType == "explain" {
		entities["concept"] = message
		entities["level"] = "general"
	}

	return entities
}

// generateGoal creates a clear goal statement from the intent
func (ip *IntentParser) generateGoal(ctx context.Context, message string, intentType string, entities map[string]string) (string, error) {
	// Create a prompt for goal generation
	prompt := fmt.Sprintf(`
Based on the user's message and intent, generate a clear, actionable goal statement.

User Message: "%s"
Intent Type: %s
Entities: %s

Generate a goal statement that clearly describes what the AI should accomplish.
The goal should be specific, measurable, and actionable.

Goal:`, message, intentType, fmt.Sprintf("%v", entities))

	// Use LLM to generate goal
	goal, err := ip.llmClient.GenerateResponse(ctx, prompt, 100)
	if err != nil {
		// Fallback to template-based goal generation
		return ip.templateBasedGoalGeneration(message, intentType, entities), nil
	}

	return strings.TrimSpace(goal), nil
}

// templateBasedGoalGeneration provides fallback goal generation
func (ip *IntentParser) templateBasedGoalGeneration(message string, intentType string, entities map[string]string) string {
	switch intentType {
	case "query":
		if query, exists := entities["query"]; exists {
			return fmt.Sprintf("Answer the question: %s", query)
		}
		return fmt.Sprintf("Provide information about: %s", message)

	case "task":
		if task, exists := entities["task"]; exists {
			return fmt.Sprintf("Execute the task: %s", task)
		}
		return fmt.Sprintf("Perform the requested action: %s", message)

	case "plan":
		if objective, exists := entities["objective"]; exists {
			return fmt.Sprintf("Create a plan for: %s", objective)
		}
		return fmt.Sprintf("Develop a strategy for: %s", message)

	case "learn":
		if topic, exists := entities["topic"]; exists {
			return fmt.Sprintf("Learn about: %s", topic)
		}
		return fmt.Sprintf("Acquire knowledge about: %s", message)

	case "explain":
		if concept, exists := entities["concept"]; exists {
			return fmt.Sprintf("Explain: %s", concept)
		}
		return fmt.Sprintf("Provide explanation for: %s", message)

	case "help":
		return fmt.Sprintf("Provide help with: %s", message)

	case "debug":
		return fmt.Sprintf("Debug or troubleshoot: %s", message)

	default:
		return fmt.Sprintf("Respond helpfully to: %s", message)
	}
}

// applyRuleBasedRefinements applies additional rule-based refinements
func (ip *IntentParser) applyRuleBasedRefinements(message string, intentType string, confidence float64) (string, float64) {
	message = strings.ToLower(message)

	// Override misclassifications: "What is X?" should always be "query" to use knowledge base
	queryOverridePatterns := []string{
		`^what is `,
		`^what are `,
		`^what's `,
		`^what was `,
		`^what were `,
		`^tell me about `,
		`^explain `,
		`^describe `,
		`^define `,
		`^what does `,
		`^how does `,
		`^how do `,
		`^was my `,
		`^did i `,
		`^do i `,
		`^am i `,
		`^what was my `,
		`^where did i `,
		`^who did i `,
	}
	for _, pattern := range queryOverridePatterns {
		if matched, _ := regexp.MatchString(pattern, message); matched {
			if intentType != "query" {
				log.Printf("🔧 [INTENT-PARSER] Overriding intent from '%s' to 'query' for pattern: %s", intentType, pattern)
			}
			return "query", 0.95
		}
	}

	// Check for high-confidence patterns
	highConfidencePatterns := map[string][]string{
		"query": {
			`what is `,
			`what are `,
			`how does `,
			`tell me about `,
		},
		"task": {
			`do this`,
			`execute `,
			`run `,
			`create `,
		},
		"plan": {
			`plan `,
			`strategy `,
			`how should `,
		},
	}

	if patterns, exists := highConfidencePatterns[intentType]; exists {
		for _, pattern := range patterns {
			if matched, _ := regexp.MatchString(pattern, message); matched {
				confidence = 0.9
				break
			}
		}
	}

	// Check for low-confidence patterns that might indicate wrong classification
	lowConfidencePatterns := map[string][]string{
		"query": {
			`do this`,
			`execute `,
			`run `,
		},
		"task": {
			`what is `,
			`tell me about `,
			`explain `,
		},
	}

	if patterns, exists := lowConfidencePatterns[intentType]; exists {
		for _, pattern := range patterns {
			if matched, _ := regexp.MatchString(pattern, message); matched {
				confidence = 0.3
				break
			}
		}
	}

	// If confidence is very low, fall back to general conversation
	if confidence < 0.3 {
		intentType = "general_conversation"
		confidence = 0.5
	}

	return intentType, confidence
}
