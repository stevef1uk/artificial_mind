package main

import (
	"fmt"
	"log"
	"strings"
)

// findCachedCode searches for previously generated and validated code with parameter compatibility
func (ie *IntelligentExecutor) findCachedCode(taskName, description, language string) (*GeneratedCode, error) {

	if strings.HasPrefix(taskName, "code_") {
		log.Printf("🔍 [INTELLIGENT] Searching for cached code by ID: %s (language: %s)", taskName, language)

		allLanguages := []string{"python", "go", "javascript", "java", "cpp"}
		for _, lang := range allLanguages {
			allResults, err := ie.codeStorage.SearchCode("", lang, []string{"intelligent_execution", "validated"})
			if err != nil {
				log.Printf("🔍 [INTELLIGENT] Search error for language %s: %v", lang, err)
				continue
			}
			log.Printf("🔍 [INTELLIGENT] Found %d results for language %s", len(allResults), lang)
			for _, result := range allResults {
				log.Printf("🔍 [INTELLIGENT] Checking result: ID=%s, TaskName=%s, Language=%s, Executable=%v",
					result.Code.ID, result.Code.TaskName, result.Code.Language, result.Code.Executable)
				if result.Code.ID == taskName && result.Code.Executable {
					log.Printf("🔍 [INTELLIGENT] Found cached code by ID: %s (ID: %s, Language: %s)", result.Code.TaskName, result.Code.ID, result.Code.Language)
					return result.Code, nil
				}
			}
		}
	}

	results, err := ie.codeStorage.SearchCode(taskName, language, []string{"intelligent_execution", "validated"})
	if err == nil && len(results) > 0 {
		for _, result := range results {
			if result.Code.Executable && result.Code.Language == language {
				log.Printf("🔍 [INTELLIGENT] Found cached code: %s (ID: %s)", result.Code.TaskName, result.Code.ID)
				return result.Code, nil
			}
		}
	}

	return nil, fmt.Errorf("no cached code found")
}

// findCompatibleCachedCode searches for cached code that's compatible with the current request parameters
func (ie *IntelligentExecutor) findCompatibleCachedCode(req *ExecutionRequest) (*GeneratedCode, error) {
	log.Printf("🔍 [INTELLIGENT] Searching for compatible cached code for task: %s", req.TaskName)

	results, err := ie.codeStorage.SearchCode(req.TaskName, "", []string{"intelligent_execution", "validated"})
	if err != nil {
		log.Printf("🔍 [INTELLIGENT] Search error: %v", err)
		return nil, err
	}

	// Filter results to only include exact task name matches
	var exactMatches []CodeSearchResult
	for _, result := range results {
		if result.Code.TaskName == req.TaskName {
			exactMatches = append(exactMatches, result)
		}
	}

	if len(exactMatches) == 0 {
		log.Printf("🔍 [INTELLIGENT] No exact cached code found for task: %s", req.TaskName)
		return nil, fmt.Errorf("no exact cached code found")
	}

	log.Printf("🔍 [INTELLIGENT] Found %d exact cached code entries for task: %s", len(exactMatches), req.TaskName)

	for _, result := range exactMatches {
		if !result.Code.Executable {
			continue
		}

		codeLower := strings.ToLower(result.Code.Code)
		if strings.Contains(codeLower, "/api/v1/tools/tool_mcp_query_neo4j/invoke") {
			log.Printf("🚫 [INTELLIGENT] Rejecting cached code (ID: %s) - uses deprecated tool endpoint", result.Code.ID)
			continue
		}

		reqDescLower := strings.ToLower(req.Description)
		reqTaskLower := strings.ToLower(req.TaskName)
		isCurrentTaskHypothesis := strings.HasPrefix(reqTaskLower, "test hypothesis:") ||
			strings.HasPrefix(reqDescLower, "test hypothesis:") ||
			(req.Context != nil && req.Context["hypothesis_testing"] == "true")

		cachedTaskNameLower := strings.ToLower(result.Code.TaskName)
		cachedDescLower := ""
		if result.Code.Description != "" {
			cachedDescLower = strings.ToLower(result.Code.Description)
		}
		cachedIsHypothesis := strings.HasPrefix(cachedTaskNameLower, "test hypothesis:") ||
			strings.HasPrefix(cachedDescLower, "test hypothesis:")

		hasHypothesisPatterns := strings.Contains(codeLower, "hypothesis_test_report") ||
			strings.Contains(codeLower, "hypothesis = \"") ||
			(strings.Contains(codeLower, "test hypothesis:") && strings.Contains(codeLower, "extract meaningful terms")) ||
			strings.Contains(codeLower, "hypothesis test report") ||
			strings.Contains(codeLower, "hypothesis = \"if we apply insights") ||
			strings.Contains(codeLower, "system state: learn")

		if !isCurrentTaskHypothesis && (cachedIsHypothesis || hasHypothesisPatterns) {
			log.Printf("🚫 [INTELLIGENT] Rejecting cached code (ID: %s, Task: %s) - cached code is for hypothesis testing but current task is not", result.Code.ID, result.Code.TaskName)
			log.Printf("🚫 [INTELLIGENT] Current task: %s, Description: %s", req.TaskName, req.Description[:min(100, len(req.Description))])
			continue
		}

		compatibility := ie.checkParameterCompatibility(result.Code, req)
		log.Printf("🔍 [INTELLIGENT] Compatibility check for %s (ID: %s): %s",
			result.Code.TaskName, result.Code.ID, compatibility.Status)

		if compatibility.IsCompatible {
			log.Printf("✅ [INTELLIGENT] Found compatible cached code: %s (ID: %s) - %s",
				result.Code.TaskName, result.Code.ID, compatibility.Reason)
			return result.Code, nil
		}
	}

	log.Printf("❌ [INTELLIGENT] No compatible cached code found for task: %s", req.TaskName)
	return nil, fmt.Errorf("no compatible cached code found")
}

// checkParameterCompatibility checks if cached code is compatible with current request parameters
func (ie *IntelligentExecutor) checkParameterCompatibility(cachedCode *GeneratedCode, req *ExecutionRequest) ParameterCompatibility {
	log.Printf("🔍 [COMPATIBILITY] Checking compatibility for %s (ID: %s)", cachedCode.TaskName, cachedCode.ID)

	originalContext := cachedCode.Context
	currentContext := req.Context

	log.Printf("🔍 [COMPATIBILITY] Original context: %+v", originalContext)
	log.Printf("🔍 [COMPATIBILITY] Current context: %+v", currentContext)

	if strings.TrimSpace(req.Language) != "" && !strings.EqualFold(cachedCode.Language, req.Language) {
		return ParameterCompatibility{
			IsCompatible: false,
			Status:       "language_mismatch",
			Reason:       fmt.Sprintf("Cached language '%s' != requested '%s'", cachedCode.Language, req.Language),
			Confidence:   0.0,
		}
	}

	if oc, ok1 := originalContext["project_id"]; ok1 {
		if cc, ok2 := currentContext["project_id"]; ok2 {
			if strings.TrimSpace(oc) != "" && strings.TrimSpace(cc) != "" && oc != cc {
				return ParameterCompatibility{
					IsCompatible: false,
					Status:       "project_mismatch",
					Reason:       fmt.Sprintf("Cached project_id '%s' != current '%s'", oc, cc),
					Confidence:   0.0,
				}
			}
		}
	}

	if ie.contextsAreEqual(originalContext, currentContext) {
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "exact_match",
			Reason:       "Parameters match exactly",
			Confidence:   1.0,
		}
	}

	if ie.isMathematicalTask(cachedCode.TaskName) {
		compatibility := ie.checkMathematicalCompatibility(cachedCode, req)
		if compatibility.IsCompatible {
			return compatibility
		}
	}

	if ie.isStringBasedTask(cachedCode.TaskName) {
		compatibility := ie.checkStringBasedCompatibility(cachedCode, req)
		if compatibility.IsCompatible {
			return compatibility
		}
	}

	compatibility := ie.checkStructuralCompatibility(cachedCode, req)
	if compatibility.IsCompatible {
		return compatibility
	}

	return ParameterCompatibility{
		IsCompatible: false,
		Status:       "incompatible",
		Reason:       "Parameters are fundamentally different",
		Confidence:   0.0,
	}
}

// contextsAreEqual checks if two context maps are equal
func (ie *IntelligentExecutor) contextsAreEqual(ctx1, ctx2 map[string]string) bool {
	if len(ctx1) != len(ctx2) {
		return false
	}

	for k, v1 := range ctx1 {
		if v2, exists := ctx2[k]; !exists || v1 != v2 {
			return false
		}
	}

	return true
}

// isMathematicalTask checks if a task is mathematical in nature
func (ie *IntelligentExecutor) isMathematicalTask(taskName string) bool {
	mathKeywords := []string{"prime", "matrix", "statistics", "calculate", "compute", "math", "number", "sum", "multiply", "divide", "add", "subtract"}
	taskLower := strings.ToLower(taskName)

	for _, keyword := range mathKeywords {
		if strings.Contains(taskLower, keyword) {
			return true
		}
	}
	return false
}

// isStringBasedTask checks if a task is string-based in nature
func (ie *IntelligentExecutor) isStringBasedTask(taskName string) bool {
	stringKeywords := []string{"text", "string", "parse", "format", "replace", "split", "join", "search", "find"}
	taskLower := strings.ToLower(taskName)

	for _, keyword := range stringKeywords {
		if strings.Contains(taskLower, keyword) {
			return true
		}
	}
	return false
}

// checkMathematicalCompatibility checks compatibility for mathematical tasks
func (ie *IntelligentExecutor) checkMathematicalCompatibility(cachedCode *GeneratedCode, req *ExecutionRequest) ParameterCompatibility {

	if cachedCode.TaskName != req.TaskName {
		return ParameterCompatibility{
			IsCompatible: false,
			Status:       "mathematical_incompatible",
			Reason:       fmt.Sprintf("Different mathematical tasks: '%s' vs '%s'", cachedCode.TaskName, req.TaskName),
			Confidence:   0.0,
		}
	}

	criticalParams := []string{"operation", "method", "type", "mode", "algorithm"}
	for _, param := range criticalParams {
		if originalVal, hasOriginal := cachedCode.Context[param]; hasOriginal {
			if currentVal, hasCurrent := req.Context[param]; hasCurrent {
				if originalVal != currentVal {
					return ParameterCompatibility{
						IsCompatible: false,
						Status:       "mathematical_incompatible",
						Reason:       fmt.Sprintf("Critical parameter '%s' differs: '%s' vs '%s'", param, originalVal, currentVal),
						Confidence:   0.0,
					}
				}
			}
		}
	}

	originalKeys := make(map[string]bool)
	for k := range cachedCode.Context {
		originalKeys[k] = true
	}

	currentKeys := make(map[string]bool)
	for k := range req.Context {
		currentKeys[k] = true
	}

	commonKeys := 0
	for k := range originalKeys {
		if currentKeys[k] {
			commonKeys++
		}
	}

	keyOverlapRatio := float64(commonKeys) / float64(len(originalKeys))

	if keyOverlapRatio >= 0.8 {
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "mathematical_compatible",
			Reason:       fmt.Sprintf("Mathematical task with %d%% parameter overlap", int(keyOverlapRatio*100)),
			Confidence:   keyOverlapRatio,
		}
	}

	if ie.hasCompatibleMathematicalParameters(cachedCode.Context, req.Context) {
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "mathematical_parameter_compatible",
			Reason:       "Mathematical parameters are compatible (different values, same structure)",
			Confidence:   0.8,
		}
	}

	return ParameterCompatibility{
		IsCompatible: false,
		Status:       "mathematical_incompatible",
		Reason:       "Mathematical parameters are not compatible",
		Confidence:   0.0,
	}
}

// hasCompatibleMathematicalParameters checks if mathematical parameters are compatible
func (ie *IntelligentExecutor) hasCompatibleMathematicalParameters(original, current map[string]string) bool {

	mathParams := []string{"count", "number", "size", "length", "input", "value", "n", "limit", "max", "min"}

	for _, param := range mathParams {
		if _, hasOriginal := original[param]; hasOriginal {
			if _, hasCurrent := current[param]; hasCurrent {

				return true
			}
		}
	}

	return false
}

// checkStringBasedCompatibility checks compatibility for string-based tasks
func (ie *IntelligentExecutor) checkStringBasedCompatibility(cachedCode *GeneratedCode, req *ExecutionRequest) ParameterCompatibility {

	originalKeys := make(map[string]bool)
	for k := range cachedCode.Context {
		originalKeys[k] = true
	}

	currentKeys := make(map[string]bool)
	for k := range req.Context {
		currentKeys[k] = true
	}

	commonKeys := 0
	for k := range originalKeys {
		if currentKeys[k] {
			commonKeys++
		}
	}

	keyOverlapRatio := float64(commonKeys) / float64(len(originalKeys))

	if keyOverlapRatio >= 0.9 {
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "string_compatible",
			Reason:       fmt.Sprintf("String task with %d%% parameter overlap", int(keyOverlapRatio*100)),
			Confidence:   keyOverlapRatio,
		}
	}

	return ParameterCompatibility{
		IsCompatible: false,
		Status:       "string_incompatible",
		Reason:       "String task parameters are not compatible",
		Confidence:   0.0,
	}
}

// checkStructuralCompatibility checks if the parameter structure is compatible
func (ie *IntelligentExecutor) checkStructuralCompatibility(cachedCode *GeneratedCode, req *ExecutionRequest) ParameterCompatibility {

	originalKeys := make(map[string]bool)
	for k := range cachedCode.Context {
		originalKeys[k] = true
	}

	currentKeys := make(map[string]bool)
	for k := range req.Context {
		currentKeys[k] = true
	}

	commonKeys := 0
	for k := range originalKeys {
		if currentKeys[k] {
			commonKeys++
		}
	}

	newKeys := 0
	for k := range currentKeys {
		if !originalKeys[k] {
			newKeys++
		}
	}

	totalKeys := len(originalKeys) + newKeys
	if totalKeys == 0 {
		return ParameterCompatibility{
			IsCompatible: false,
			Status:       "no_parameters",
			Reason:       "No parameters to compare",
			Confidence:   0.0,
		}
	}

	compatibilityScore := float64(commonKeys) / float64(totalKeys)

	if compatibilityScore >= 0.7 {
		return ParameterCompatibility{
			IsCompatible: true,
			Status:       "structurally_compatible",
			Reason:       fmt.Sprintf("Structural compatibility: %d%%", int(compatibilityScore*100)),
			Confidence:   compatibilityScore,
		}
	}

	return ParameterCompatibility{
		IsCompatible: false,
		Status:       "structurally_incompatible",
		Reason:       "Parameter structure is not compatible",
		Confidence:   0.0,
	}
}

// ListCachedCapabilities returns all cached capabilities
func (ie *IntelligentExecutor) ListCachedCapabilities() ([]*GeneratedCode, error) {
	return ie.codeStorage.ListAllCode()
}
