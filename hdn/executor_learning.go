package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// categorizeFailure categorizes a failure error into a pattern type and category
func (ie *IntelligentExecutor) categorizeFailure(errorMsg string, language string) (patternType, errorCategory string) {
	errorLower := strings.ToLower(errorMsg)

	if strings.Contains(errorLower, "undefined") ||
		strings.Contains(errorLower, "imported and not used") ||
		strings.Contains(errorLower, "declared but not used") ||
		strings.Contains(errorLower, "cannot find package") {
		patternType = "compilation"
	} else if strings.Contains(errorLower, "panic") ||
		strings.Contains(errorLower, "runtime error") ||
		strings.Contains(errorLower, "index out of range") ||
		strings.Contains(errorLower, "nil pointer") {
		patternType = "runtime"
	} else if strings.Contains(errorLower, "type") && strings.Contains(errorLower, "mismatch") ||
		strings.Contains(errorLower, "cannot use") ||
		strings.Contains(errorLower, "assignment mismatch") {
		patternType = "type_error"
	} else {
		patternType = "validation"
	}

	if strings.Contains(errorLower, "undefined") {
		errorCategory = "undefined_symbol"
	} else if strings.Contains(errorLower, "import") {
		errorCategory = "import_error"
	} else if strings.Contains(errorLower, "type") {
		errorCategory = "type_mismatch"
	} else if strings.Contains(errorLower, "assignment mismatch") {
		errorCategory = "assignment_mismatch"
	} else if strings.Contains(errorLower, "unused") || strings.Contains(errorLower, "not used") {
		errorCategory = "unused_import"
	} else {
		errorCategory = "other"
	}

	return patternType, errorCategory
}

// recordFailurePattern records a failure pattern for learning
func (ie *IntelligentExecutor) recordFailurePattern(validationResult ValidationStep, req *ExecutionRequest) {
	if ie.learningRedis == nil {
		return
	}

	patternType, errorCategory := ie.categorizeFailure(validationResult.Error, req.Language)
	taskCategory := ie.deriveTaskCategory(req.TaskName, req.Description)

	patternKey := fmt.Sprintf("failure_pattern:%s:%s:%s", patternType, errorCategory, req.Language)
	patternData, err := ie.learningRedis.Get(ie.ctx, patternKey).Result()

	var pattern FailurePattern
	if err == nil && patternData != "" {
		json.Unmarshal([]byte(patternData), &pattern)
	} else {
		pattern = FailurePattern{
			PatternType:   patternType,
			ErrorCategory: errorCategory,
			Language:      req.Language,
			TaskCategory:  taskCategory,
			Frequency:     0,
			SuccessRate:   0.0,
			CommonFixes:   []string{},
			FirstSeen:     time.Now(),
		}
	}

	pattern.Frequency++
	pattern.LastSeen = time.Now()

	patternDataJSON, _ := json.Marshal(pattern)
	ie.learningRedis.Set(ie.ctx, patternKey, patternDataJSON, 30*24*time.Hour)

	log.Printf("📊 [LEARNING] Recorded failure pattern: %s/%s (frequency: %d)", patternType, errorCategory, pattern.Frequency)
}

// learnFromValidationFailure learns from validation failures to improve future code generation
func (ie *IntelligentExecutor) learnFromValidationFailure(validationResult ValidationStep, req *ExecutionRequest) {
	if ie.learningRedis == nil {
		return
	}

	ie.recordFailurePattern(validationResult, req)

	patternType, errorCategory := ie.categorizeFailure(validationResult.Error, req.Language)
	preventionKey := fmt.Sprintf("prevention_hint:%s:%s:%s", patternType, errorCategory, req.Language)

	hint := ie.generatePreventionHint(validationResult.Error, req.Language)
	ie.learningRedis.Set(ie.ctx, preventionKey, hint, 30*24*time.Hour)
}

// generatePreventionHint generates a prevention hint based on error message
func (ie *IntelligentExecutor) generatePreventionHint(errorMsg, language string) string {
	errorLower := strings.ToLower(errorMsg)

	if strings.Contains(errorLower, "undefined") {
		return "Check for missing imports or typos in function/variable names"
	} else if strings.Contains(errorLower, "imported and not used") {
		return "Remove unused imports - they cause compilation errors"
	} else if strings.Contains(errorLower, "assignment mismatch") {
		return "Check function return values - json.Unmarshal returns only error, not ([]byte, error)"
	} else if strings.Contains(errorLower, "type") && strings.Contains(errorLower, "mismatch") {
		return "Check type assertions - JSON numbers are float64, not int64"
	}

	return "Review error message carefully and fix all issues"
}

// getPreventionHintsForTask retrieves learned prevention hints for a task
// This is where the system demonstrates intelligence by using what it learned
func (ie *IntelligentExecutor) getPreventionHintsForTask(req *ExecutionRequest) []string {
	if ie.learningRedis == nil {
		log.Printf("⚠️  [INTELLIGENCE] getPreventionHintsForTask: learningRedis is nil for task %s (language: %s)", req.TaskName, req.Language)
		return []string{}
	}
	log.Printf("🧠 [INTELLIGENCE] getPreventionHintsForTask: Searching for hints (task: %s, language: %s)", req.TaskName, req.Language)

	taskCategory := ie.deriveTaskCategory(req.TaskName, req.Description)
	var hints []string

	patternTypes := []string{"compilation", "runtime", "type_error", "validation"}
	searchedKeys := []string{}
	for _, patternType := range patternTypes {

		errorCategories := []string{"undefined", "type_mismatch", "import_error", "syntax_error"}
		for _, errorCategory := range errorCategories {
			preventionKey := fmt.Sprintf("prevention_hint:%s:%s:%s", patternType, errorCategory, req.Language)
			searchedKeys = append(searchedKeys, preventionKey)
			hint, err := ie.learningRedis.Get(ie.ctx, preventionKey).Result()
			if err == nil && hint != "" {

				patternKey := fmt.Sprintf("failure_pattern:%s:%s:%s", patternType, errorCategory, req.Language)
				patternData, err := ie.learningRedis.Get(ie.ctx, patternKey).Result()
				if err == nil && patternData != "" {
					var pattern FailurePattern
					if json.Unmarshal([]byte(patternData), &pattern) == nil {

						if pattern.Frequency >= 2 {
							hints = append(hints, hint)
							log.Printf("🧠 [INTELLIGENCE] Retrieved learned prevention hint: %s (seen %d times)", hint, pattern.Frequency)
						} else {
							log.Printf("🧠 [INTELLIGENCE] Found hint but frequency too low: %s (frequency: %d, need >= 2)", preventionKey, pattern.Frequency)
						}
					}
				} else {
					log.Printf("🧠 [INTELLIGENCE] Found hint but no matching pattern: %s", preventionKey)
				}
			}
		}
	}
	if len(hints) == 0 && len(searchedKeys) > 0 {
		log.Printf("🧠 [INTELLIGENCE] Searched %d keys for prevention hints, found 0 matching hints for task %s (language: %s)", len(searchedKeys), req.TaskName, req.Language)
	}

	strategyKey := fmt.Sprintf("codegen_strategy:%s:%s", taskCategory, req.Language)
	strategyData, err := ie.learningRedis.Get(ie.ctx, strategyKey).Result()
	if err == nil && strategyData != "" {
		var strategy CodeGenStrategy
		if json.Unmarshal([]byte(strategyData), &strategy) == nil {

			if strategy.SuccessRate > 0.7 && strategy.AvgQuality > 0.6 && strategy.UsageCount >= 3 {
				hints = append(hints, fmt.Sprintf("Previous successful approach: %s (success rate: %.0f%%, avg retries: %.1f)",
					strategy.PromptStyle, strategy.SuccessRate*100, strategy.AvgRetries))
				log.Printf("🧠 [INTELLIGENCE] Using learned successful strategy: %s (%.0f%% success)", strategy.PromptStyle, strategy.SuccessRate*100)
			}
		}
	}

	return hints
}

// deriveTaskCategory derives a task category from task name and description
func (ie *IntelligentExecutor) deriveTaskCategory(taskName, description string) string {
	combined := strings.ToLower(taskName + " " + description)

	if strings.Contains(combined, "json") || strings.Contains(combined, "parse") {
		return "json_processing"
	} else if strings.Contains(combined, "file") || strings.Contains(combined, "read") || strings.Contains(combined, "write") {
		return "file_operations"
	} else if strings.Contains(combined, "http") || strings.Contains(combined, "api") || strings.Contains(combined, "request") {
		return "http_operations"
	} else if strings.Contains(combined, "calculate") || strings.Contains(combined, "math") || strings.Contains(combined, "compute") {
		return "calculation"
	} else if strings.Contains(combined, "transform") || strings.Contains(combined, "convert") {
		return "data_transformation"
	}

	return "general"
}

// identifyFocusAreas identifies areas showing promise for focused learning
func (ie *IntelligentExecutor) identifyFocusAreas() []CodeGenLearningProgress {
	if ie.learningRedis == nil {
		return []CodeGenLearningProgress{}
	}

	var focusAreas []CodeGenLearningProgress

	pattern := "codegen_strategy:*"
	keys, err := ie.learningRedis.Keys(ie.ctx, pattern).Result()
	if err != nil {
		return focusAreas
	}

	for _, key := range keys {
		strategyData, err := ie.learningRedis.Get(ie.ctx, key).Result()
		if err != nil {
			continue
		}

		var strategy CodeGenStrategy
		if err := json.Unmarshal([]byte(strategyData), &strategy); err != nil {
			continue
		}

		focusScore := strategy.SuccessRate*0.5 + strategy.AvgQuality*0.3 - (strategy.AvgRetries/10.0)*0.2

		if focusScore > 0.5 && strategy.UsageCount >= 3 {
			progress := CodeGenLearningProgress{
				TaskCategory:   strategy.TaskCategory,
				Language:       strategy.Language,
				SuccessRate:    strategy.SuccessRate,
				AvgQuality:     strategy.AvgQuality,
				RecentProgress: 0.0,
				FocusScore:     focusScore,
			}
			focusAreas = append(focusAreas, progress)
		}
	}

	return focusAreas
}

// assessCodeQuality assesses the quality of generated code
func (ie *IntelligentExecutor) assessCodeQuality(code *GeneratedCode, retryCount int) float64 {
	quality := 1.0

	quality -= float64(retryCount) * 0.2

	lines := strings.Count(code.Code, "\n")
	if lines > 5 && lines < 100 {
		quality += 0.1
	} else if lines > 100 {
		quality -= 0.1
	}

	if quality < 0 {
		quality = 0
	}
	if quality > 1 {
		quality = 1
	}

	return quality
}
