package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// logIntelligentExecutionMetrics logs tool metrics for intelligent execution
func (ie *IntelligentExecutor) logIntelligentExecutionMetrics(ctx context.Context, req *ExecutionRequest, result *IntelligentExecutionResult) {
	if ie.toolMetrics == nil {
		return
	}

	toolID := "tool_intelligent_executor"
	toolName := "Intelligent Code Execution"

	status := "success"
	if !result.Success {
		status = "failure"
	}

	callLog := &ToolCallLog{
		ID:       fmt.Sprintf("intelligent_%d", time.Now().UnixNano()),
		ToolID:   toolID,
		ToolName: toolName,
		Parameters: map[string]interface{}{
			"task_name":   req.TaskName,
			"description": req.Description,
			"language":    req.Language,
			"context":     req.Context,
		},
		Status:      status,
		Error:       result.Error,
		Duration:    result.ExecutionTime.Milliseconds(),
		AgentID:     "intelligent_executor",
		ProjectID:   req.Context["project_id"],
		Timestamp:   time.Now(),
		Response:    result.Result,
		Permissions: []string{"code_generation", "docker_execution"},
		SafetyLevel: "medium",
	}

	if err := ie.toolMetrics.LogToolCall(ctx, callLog); err != nil {
		log.Printf("⚠️ [INTELLIGENT] Failed to log tool metrics: %v", err)
	} else {
		log.Printf("📊 [INTELLIGENT] Logged tool metrics: %s (%s)", toolName, status)
	}
}

// GetExecutionStats returns statistics about intelligent executions
func (ie *IntelligentExecutor) GetExecutionStats() map[string]interface{} {
	allCode, err := ie.ListCachedCapabilities()
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}

	stats := map[string]interface{}{
		"total_cached_capabilities": len(allCode),
		"languages":                 make(map[string]int),
		"tags":                      make(map[string]int),
		"recent_executions":         []string{},
	}

	for _, code := range allCode {
		if stats["languages"].(map[string]int)[code.Language] == 0 {
			stats["languages"].(map[string]int)[code.Language] = 0
		}
		stats["languages"].(map[string]int)[code.Language]++

		for _, tag := range code.Tags {
			if stats["tags"].(map[string]int)[tag] == 0 {
				stats["tags"].(map[string]int)[tag] = 0
			}
			stats["tags"].(map[string]int)[tag]++
		}
	}

	return stats
}

// recordExecutionEpisode records an execution episode in the self-model
func (ie *IntelligentExecutor) recordExecutionEpisode(req *ExecutionRequest, result *IntelligentExecutionResult, executionType string) {
	if ie.selfModelManager == nil {
		return
	}

	metadata := map[string]interface{}{
		"task_name":        req.TaskName,
		"description":      req.Description,
		"language":         req.Language,
		"execution_type":   executionType,
		"retry_count":      result.RetryCount,
		"execution_time":   result.ExecutionTime.String(),
		"used_cached_code": result.UsedCachedCode,
		"context":          req.Context,
	}

	decision := "execute_task"
	if result.UsedCachedCode {
		decision = "use_cached_code"
	} else if result.GeneratedCode != nil {
		decision = "generate_new_code"
	}

	err := ie.selfModelManager.RecordEpisode(
		fmt.Sprintf("Task execution: %s", req.TaskName),
		decision,
		fmt.Sprintf("Success: %v, Result: %v", result.Success, result.Result),
		result.Success,
		metadata,
	)

	if err != nil {
		log.Printf("⚠️ [SELF-MODEL] Failed to record episode: %v", err)
	} else {
		log.Printf("📝 [SELF-MODEL] Recorded execution episode for task: %s", req.TaskName)
	}

	ie.updateBeliefsFromExecution(req, result)
}

// updateBeliefsFromExecution updates beliefs based on execution results
func (ie *IntelligentExecutor) updateBeliefsFromExecution(req *ExecutionRequest, result *IntelligentExecutionResult) {
	if ie.selfModelManager == nil {
		return
	}

	taskKey := fmt.Sprintf("task_%s_success_rate", req.TaskName)
	if result.Success {

		ie.selfModelManager.UpdateBelief(taskKey+"_successes",
			ie.getBeliefValue(taskKey+"_successes")+1)
	} else {

		ie.selfModelManager.UpdateBelief(taskKey+"_failures",
			ie.getBeliefValue(taskKey+"_failures")+1)
	}

	langKey := fmt.Sprintf("language_%s_success_rate", req.Language)
	if result.Success {
		ie.selfModelManager.UpdateBelief(langKey+"_successes",
			ie.getBeliefValue(langKey+"_successes")+1)
	} else {
		ie.selfModelManager.UpdateBelief(langKey+"_failures",
			ie.getBeliefValue(langKey+"_failures")+1)
	}

	execKey := fmt.Sprintf("execution_type_%s_success_rate", "traditional_execution")
	if result.Success {
		ie.selfModelManager.UpdateBelief(execKey+"_successes",
			ie.getBeliefValue(execKey+"_successes")+1)
	} else {
		ie.selfModelManager.UpdateBelief(execKey+"_failures",
			ie.getBeliefValue(execKey+"_failures")+1)
	}

	ie.selfModelManager.UpdateBelief("last_execution_time", time.Now().Unix())
	ie.selfModelManager.UpdateBelief("last_task", req.TaskName)
}

// getBeliefValue gets a belief value as an integer, defaulting to 0
func (ie *IntelligentExecutor) getBeliefValue(key string) int {
	if ie.selfModelManager == nil {
		return 0
	}

	sm, err := ie.selfModelManager.Load()
	if err != nil {
		return 0
	}

	if val, exists := sm.Beliefs[key]; exists {
		if intVal, ok := val.(float64); ok {
			return int(intVal)
		}
		if intVal, ok := val.(int); ok {
			return intVal
		}
	}
	return 0
}

// recordMonitorMetrics records metrics in the format expected by the monitor UI
func (ie *IntelligentExecutor) recordMonitorMetrics(success bool, execTime time.Duration) {

	log.Printf("📈 [INTELLIGENT] Execution completed: Success=%v, Time=%v", success, execTime)
}

// generatePerformanceReport creates a performance comparison report for multiple programs
func (ie *IntelligentExecutor) generatePerformanceReport(timings []map[string]interface{}, programs []ChainedProgram, outputs []string) string {
	var report strings.Builder

	report.WriteString("=" + strings.Repeat("=", 70) + "\n")
	report.WriteString("PERFORMANCE COMPARISON REPORT\n")
	report.WriteString("=" + strings.Repeat("=", 70) + "\n\n")
	report.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	report.WriteString("EXECUTION TIMINGS:\n")
	report.WriteString("-" + strings.Repeat("-", 70) + "\n")

	for i, timing := range timings {

		programName, _ := timing["program"].(string)
		if programName == "" {
			programName = fmt.Sprintf("Program %d", i+1)
		}
		language, _ := timing["language"].(string)
		if language == "" {
			language = "unknown"
		}
		durationMs, _ := timing["duration_ms"].(int64)
		durationNs, _ := timing["duration_ns"].(int64)
		success, _ := timing["success"].(bool)

		report.WriteString(fmt.Sprintf("\nProgram %d: %s (%s)\n", i+1, programName, language))
		report.WriteString(fmt.Sprintf("  Status: %s\n", map[bool]string{true: "SUCCESS", false: "FAILED"}[success]))

		usingExtracted := false
		if usingExtractedVal, ok := timing["using_extracted_time"]; ok {
			if val, ok := usingExtractedVal.(bool); ok {
				usingExtracted = val
			}
		}
		var totalMs int64
		hasTotal := false
		if totalMsVal, ok := timing["total_duration_ms"]; ok {
			if val, ok := totalMsVal.(int64); ok {
				totalMs = val
				hasTotal = true
			}
		}

		if usingExtracted && hasTotal && totalMs > durationMs {
			report.WriteString(fmt.Sprintf("  Algorithm Duration: %d ms (%.2f seconds)\n", durationMs, float64(durationMs)/1000.0))
			report.WriteString(fmt.Sprintf("  Algorithm Duration: %d nanoseconds\n", durationNs))
			report.WriteString(fmt.Sprintf("  Total Execution Time: %d ms (includes compilation/Docker overhead)\n", totalMs))
			report.WriteString("  Note: Algorithm time is the actual sorting performance, not total execution overhead\n")
		} else if !usingExtracted && hasTotal {

			report.WriteString(fmt.Sprintf("  Duration: %d ms (%.2f seconds)\n", durationMs, float64(durationMs)/1000.0))
			report.WriteString(fmt.Sprintf("  Duration: %d nanoseconds\n", durationNs))
			report.WriteString("  Note: Program did not print execution time, showing total execution time\n")
		} else {
			report.WriteString(fmt.Sprintf("  Duration: %d ms (%.2f seconds)\n", durationMs, float64(durationMs)/1000.0))
			report.WriteString(fmt.Sprintf("  Duration: %d nanoseconds\n", durationNs))
		}
	}

	if len(timings) >= 2 {
		report.WriteString("\n" + "=" + strings.Repeat("=", 70) + "\n")
		report.WriteString("PERFORMANCE COMPARISON:\n")
		report.WriteString("=" + strings.Repeat("=", 70) + "\n\n")

		ns1, _ := timings[0]["duration_ns"].(int64)
		ns2, _ := timings[1]["duration_ns"].(int64)
		ms1, _ := timings[0]["duration_ms"].(int64)
		ms2, _ := timings[1]["duration_ms"].(int64)
		usingExtracted1, _ := timings[0]["using_extracted_time"].(bool)
		usingExtracted2, _ := timings[1]["using_extracted_time"].(bool)

		lang1, _ := timings[0]["language"].(string)
		if lang1 == "" {
			lang1 = "unknown"
		}
		lang2, _ := timings[1]["language"].(string)
		if lang2 == "" {
			lang2 = "unknown"
		}

		compareExtracted := usingExtracted1 && usingExtracted2

		if compareExtracted {

			if ns1 == 0 && ns2 == 0 {
				report.WriteString("Both programs show 0 execution time (timing may not have been captured)\n")
			} else if ns1 == 0 {
				report.WriteString(fmt.Sprintf("%s: timing not captured from output, %s: %d ns (%.6f ms)\n", lang1, lang2, ns2, float64(ns2)/1000000.0))
			} else if ns2 == 0 {
				report.WriteString(fmt.Sprintf("%s: timing not captured from output, %s: %d ns (%.6f ms)\n", lang2, lang1, ns1, float64(ns1)/1000000.0))
			} else if ns1 < ns2 {
				diff := float64(ns2-ns1) / float64(ns1) * 100
				report.WriteString(fmt.Sprintf("%s executed %.2f%% faster than %s\n", lang1, diff, lang2))
				report.WriteString(fmt.Sprintf("%s: %d ns (%.6f ms)\n", lang1, ns1, float64(ns1)/1000000.0))
				report.WriteString(fmt.Sprintf("%s: %d ns (%.6f ms)\n", lang2, ns2, float64(ns2)/1000000.0))
			} else if ns2 < ns1 {
				diff := float64(ns1-ns2) / float64(ns2) * 100
				report.WriteString(fmt.Sprintf("%s executed %.2f%% faster than %s\n", lang2, diff, lang1))
				report.WriteString(fmt.Sprintf("%s: %d ns (%.6f ms)\n", lang1, ns1, float64(ns1)/1000000.0))
				report.WriteString(fmt.Sprintf("%s: %d ns (%.6f ms)\n", lang2, ns2, float64(ns2)/1000000.0))
			} else {
				report.WriteString(fmt.Sprintf("Both programs executed in the same time: %d ns (%.6f ms)\n", ns1, float64(ns1)/1000000.0))
			}
		} else {

			if !usingExtracted1 && !usingExtracted2 {
				report.WriteString("Note: Comparing total execution times (algorithm timings not extracted from output)\n")
			} else if usingExtracted1 {
				report.WriteString(fmt.Sprintf("Note: %s has algorithm timing (%d ns), %s using total execution time\n", lang1, ns1, lang2))
			} else {
				report.WriteString(fmt.Sprintf("Note: %s has algorithm timing (%d ns), %s using total execution time\n", lang2, ns2, lang1))
			}

			if ms1 == 0 && ms2 == 0 {
				report.WriteString("Both programs show 0 execution time\n")
			} else if ms1 < ms2 {
				diff := float64(ms2-ms1) / float64(ms1) * 100
				report.WriteString(fmt.Sprintf("%s executed %.2f%% faster than %s\n", lang1, diff, lang2))
			} else if ms2 < ms1 {
				diff := float64(ms1-ms2) / float64(ms2) * 100
				report.WriteString(fmt.Sprintf("%s executed %.2f%% faster than %s\n", lang2, diff, lang1))
			} else {
				report.WriteString(fmt.Sprintf("Both programs executed in the same time: %d ms\n", ms1))
			}
			report.WriteString(fmt.Sprintf("%s: %d ms\n", lang1, ms1))
			report.WriteString(fmt.Sprintf("%s: %d ms\n", lang2, ms2))
		}
	}

	if len(outputs) > 0 {
		report.WriteString("\n" + "=" + strings.Repeat("=", 70) + "\n")
		report.WriteString("PROGRAM OUTPUTS:\n")
		report.WriteString("=" + strings.Repeat("=", 70) + "\n\n")
		for i, output := range outputs {
			report.WriteString(fmt.Sprintf("Program %d Output:\n", i+1))
			report.WriteString(output)
			report.WriteString("\n\n")
		}
	}

	report.WriteString("\n" + "=" + strings.Repeat("=", 70) + "\n")
	report.WriteString("END OF REPORT\n")
	report.WriteString("=" + strings.Repeat("=", 70) + "\n")

	return report.String()
}

// recordSuccessfulExecution records a successful execution for learning
func (ie *IntelligentExecutor) recordSuccessfulExecution(req *ExecutionRequest, result *IntelligentExecutionResult, code *GeneratedCode) {

	trivialPatterns := []string{
		"create example.txt",
		"create example",
		"list directory and create",
		"list current directory",
	}
	descLower := strings.ToLower(req.Description)
	for _, pattern := range trivialPatterns {
		if strings.Contains(descLower, pattern) {
			log.Printf("🚫 [INTELLIGENT] Skipping capability storage for trivial task: %s", pattern)
			return
		}
	}
	if ie.learningRedis == nil {
		return
	}

	taskCategory := ie.deriveTaskCategory(req.TaskName, req.Description)

	strategyKey := fmt.Sprintf("codegen_strategy:%s:%s", taskCategory, req.Language)
	strategyData, err := ie.learningRedis.Get(ie.ctx, strategyKey).Result()

	var strategy CodeGenStrategy
	if err == nil && strategyData != "" {
		json.Unmarshal([]byte(strategyData), &strategy)
	} else {
		strategy = CodeGenStrategy{
			StrategyID:   fmt.Sprintf("strategy_%s_%s", taskCategory, req.Language),
			PromptStyle:  "default",
			TaskCategory: taskCategory,
			Language:     req.Language,
			SuccessRate:  0.0,
			AvgRetries:   0.0,
			AvgQuality:   0.0,
			UsageCount:   0,
			LastUsed:     time.Now(),
		}
	}

	strategy.UsageCount++
	strategy.LastUsed = time.Now()

	alpha := 0.1
	strategy.SuccessRate = alpha*1.0 + (1-alpha)*strategy.SuccessRate

	strategy.AvgRetries = alpha*float64(result.RetryCount) + (1-alpha)*strategy.AvgRetries

	quality := 1.0 - (float64(result.RetryCount) / 5.0)
	if quality < 0 {
		quality = 0
	}
	strategy.AvgQuality = alpha*quality + (1-alpha)*strategy.AvgQuality

	strategyDataJSON, _ := json.Marshal(strategy)
	ie.learningRedis.Set(ie.ctx, strategyKey, strategyDataJSON, 30*24*time.Hour)

	log.Printf("📊 [LEARNING] Recorded successful execution: %s/%s (success_rate: %.2f%%, retries: %.1f)",
		taskCategory, req.Language, strategy.SuccessRate*100, strategy.AvgRetries)

	ie.considerToolCreationFromExecution(req, result, code)
}

// recordFailedExecution records a failed execution for learning
func (ie *IntelligentExecutor) recordFailedExecution(req *ExecutionRequest, result *IntelligentExecutionResult) {
	if ie.learningRedis == nil {
		return
	}

	taskCategory := ie.deriveTaskCategory(req.TaskName, req.Description)

	strategyKey := fmt.Sprintf("codegen_strategy:%s:%s", taskCategory, req.Language)
	strategyData, err := ie.learningRedis.Get(ie.ctx, strategyKey).Result()

	if err == nil && strategyData != "" {
		var strategy CodeGenStrategy
		json.Unmarshal([]byte(strategyData), &strategy)

		alpha := 0.1
		strategy.SuccessRate = alpha*0.0 + (1-alpha)*strategy.SuccessRate
		strategy.UsageCount++
		strategy.LastUsed = time.Now()

		strategyDataJSON, _ := json.Marshal(strategy)
		ie.learningRedis.Set(ie.ctx, strategyKey, strategyDataJSON, 30*24*time.Hour)
	}

	log.Printf("📊 [LEARNING] Recorded failed execution: %s/%s", taskCategory, req.Language)
}
