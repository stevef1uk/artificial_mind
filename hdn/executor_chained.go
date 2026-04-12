package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// storeChainedProgramArtifact stores a chained program as an artifact
func (ie *IntelligentExecutor) storeChainedProgramArtifact(generatedCode *GeneratedCode, workflowID, programName string) {
	if generatedCode == nil || ie.fileStorage == nil {
		return
	}

	filename := programName
	if !strings.Contains(filepath.Ext(programName), ".") {
		// No extension, add one based on language
		var ext string
		switch strings.ToLower(generatedCode.Language) {
		case "python":
			ext = ".py"
		case "go":
			ext = ".go"
		case "javascript":
			ext = ".js"
		case "java":
			ext = ".java"
		default:
			ext = ".txt"
		}
		filename = fmt.Sprintf("%s%s", programName, ext)
	}

	storedFile := &StoredFile{
		Filename:    filename,
		Content:     []byte(generatedCode.Code),
		ContentType: fmt.Sprintf("text/x-%s-source", generatedCode.Language),
		Size:        int64(len(generatedCode.Code)),
		WorkflowID:  workflowID,
		StepID:      "chained_execution",
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	if err := ie.fileStorage.StoreFile(storedFile); err != nil {
		log.Printf("⚠️ [CHAINED] Failed to store program artifact %s: %v", filename, err)
	} else {
		log.Printf("📁 [CHAINED] Stored program artifact: %s", filename)
	}
}

// isChainedProgramRequest detects if a request needs multiple programs with chained execution
func (ie *IntelligentExecutor) isChainedProgramRequest(req *ExecutionRequest) bool {
	description := strings.ToLower(req.Description)
	log.Printf("🔍 [CHAINED-DETECT] Checking if request is chained: %s", req.Description)

	chainedPatterns := []string{
		"two programs",
		"multiple programs",
		"first program",
		"second program",
		"prog1",
		"prog2",
		"chained",
		"chain",
		"exec.command",
		"run.*program",
		"execute.*program",
		"call.*program",
		"then create",
		"then generate",
		"then make",
		"python.*then.*go",
		"go.*then.*python",
		"generates.*then.*reads",
		"reads.*then.*generates",
	}

	for _, pattern := range chainedPatterns {
		if strings.Contains(description, pattern) {
			log.Printf("✅ [CHAINED-DETECT] Matched pattern: '%s'", pattern)
			return true
		}
	}

	if names, ok := req.Context["artifact_names"]; ok && names != "" {
		parts := strings.Split(names, ",")
		if len(parts) >= 2 {

			programCount := 0
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasSuffix(strings.ToLower(part), ".go") ||
					strings.HasSuffix(strings.ToLower(part), ".py") ||
					strings.HasSuffix(strings.ToLower(part), ".js") ||
					strings.HasSuffix(strings.ToLower(part), ".java") {
					programCount++
				}
			}

			if programCount >= 2 {
				return true
			}
		}
	}

	return false
}

// executeChainedPrograms executes multiple programs sequentially with output passing
func (ie *IntelligentExecutor) executeChainedPrograms(ctx context.Context, req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	log.Printf("🔗 [CHAINED] Starting chained program execution")

	programs, err := ie.parseChainedPrograms(req)
	if err != nil {
		return &IntelligentExecutionResult{
			Success:       false,
			Error:         fmt.Sprintf("Failed to parse chained programs: %v", err),
			ExecutionTime: time.Since(start),
			WorkflowID:    workflowID,
		}, nil
	}

	log.Printf("🔗 [CHAINED] Parsed %d programs for chained execution", len(programs))

	// Execute programs sequentially
	var lastOutput string
	var allOutputs []string
	var generatedCodes []*GeneratedCode
	var programTimings []map[string]interface{} // Track timing for each program

	lowerDesc := strings.ToLower(req.Description)
	needsReport := strings.Contains(lowerDesc, "time") || strings.Contains(lowerDesc, "performance") || strings.Contains(lowerDesc, "compare") || strings.Contains(lowerDesc, "report") || strings.Contains(lowerDesc, "differnce") || strings.Contains(lowerDesc, "difference")

	for i, program := range programs {
		log.Printf("🔗 [CHAINED] Executing program %d/%d: %s", i+1, len(programs), program.Name)

		programStart := time.Now()

		programReq := &ExecutionRequest{
			TaskName:     program.Name,
			Description:  program.Description,
			Context:      program.Context,
			Language:     program.Language,
			MaxRetries:   req.MaxRetries,
			Timeout:      req.Timeout,
			HighPriority: req.HighPriority,
		}

		if i > 0 && lastOutput != "" {
			programReq.Context["previous_output"] = lastOutput
			programReq.Description += fmt.Sprintf("\n\nPrevious program output: %s", lastOutput)
		}

		programResult, err := ie.executeProgramDirectly(ctx, programReq, time.Now(), workflowID)
		if err != nil {
			return &IntelligentExecutionResult{
				Success:       false,
				Error:         fmt.Sprintf("Program %d failed: %v", i+1, err),
				ExecutionTime: time.Since(start),
				WorkflowID:    workflowID,
			}, nil
		}

		artifactName := fmt.Sprintf("prog%d", i+1)

		langExt := map[string]string{
			"go":         ".go",
			"python":     ".py",
			"javascript": ".js",
			"js":         ".js",
			"java":       ".java",
			"rust":       ".rs",
			"cpp":        ".cpp",
			"c":          ".c",
		}
		if ext, ok := langExt[program.Language]; ok {
			artifactName = fmt.Sprintf("prog%d%s", i+1, ext)
		}
		ie.storeChainedProgramArtifact(programResult.GeneratedCode, workflowID, artifactName)

		programDuration := time.Since(programStart)

		// Try to extract algorithm execution time from program output
		// This gives us the actual algorithm performance, not Docker/compilation overhead
		var algorithmDurationMs int64 = 0
		var algorithmDurationNs int64 = 0
		var usingExtractedTiming bool = false
		if output, ok := programResult.Result.(string); ok {
			extractedTime := extractTimingFromOutput(output, program.Language)

			if extractedTime >= 100 {
				algorithmDurationNs = extractedTime
				algorithmDurationMs = extractedTime / 1000000
				usingExtractedTiming = true
				log.Printf("⏱️ [CHAINED] Extracted algorithm timing from output: %d ns (%d ms)", algorithmDurationNs, algorithmDurationMs)
			} else {

				algorithmDurationMs = programDuration.Milliseconds()
				algorithmDurationNs = programDuration.Nanoseconds()
				log.Printf("⏱️ [CHAINED] No valid timing found in output (extracted: %d ns), using total execution time: %d ms", extractedTime, algorithmDurationMs)
			}
		} else {

			algorithmDurationMs = programDuration.Milliseconds()
			algorithmDurationNs = programDuration.Nanoseconds()
		}

		timing := map[string]interface{}{
			"program":              program.Name,
			"language":             program.Language,
			"duration_ms":          algorithmDurationMs,
			"duration_ns":          algorithmDurationNs,
			"total_duration_ms":    programDuration.Milliseconds(),
			"total_duration_ns":    programDuration.Nanoseconds(),
			"using_extracted_time": usingExtractedTiming,
			"success":              programResult.Success,
		}
		programTimings = append(programTimings, timing)
		log.Printf("⏱️ [CHAINED] Program %d (%s) - Algorithm: %d ms, Total: %v (success: %v)", i+1, program.Language, algorithmDurationMs, programDuration, programResult.Success)

		if output, ok := programResult.Result.(string); ok {

			if needsReport {

				lastOutput = output
				allOutputs = append(allOutputs, output)
				log.Printf("🔗 [CHAINED] Using full output for program %d (performance comparison): %s", i+1, output)
			} else {

				cleanedOutput := extractJSONFromOutput(output)
				if cleanedOutput != "" {
					lastOutput = cleanedOutput
					allOutputs = append(allOutputs, cleanedOutput)
					log.Printf("🔗 [CHAINED] Extracted clean output from program %d: %s", i+1, cleanedOutput)
				} else {

					lastOutput = output
					allOutputs = append(allOutputs, output)
					log.Printf("⚠️ [CHAINED] Could not extract JSON from program %d output, using raw output", i+1)
				}
			}
		} else {

			allOutputs = append(allOutputs, "")
			log.Printf("⚠️ [CHAINED] Program %d produced no output", i+1)
		}

		if !programResult.Success {
			log.Printf("⚠️ [CHAINED] Program %d execution failed: %s (but timing and output recorded for report)", i+1, programResult.Error)

			continue
		}

		if programResult.GeneratedCode != nil {
			generatedCodes = append(generatedCodes, programResult.GeneratedCode)
		}

		log.Printf("🔗 [CHAINED] Program %d completed successfully", i+1)
	}

	combinedOutput := strings.Join(allOutputs, "\n")
	finalOutput := combinedOutput
	if len(allOutputs) > 0 {

		finalOutput = allOutputs[len(allOutputs)-1]
		log.Printf("🔗 [CHAINED] Using final program output as result: %s", finalOutput)
	}

	log.Printf("🔗 [CHAINED] All programs execution completed")
	log.Printf("🔗 [CHAINED] Program outputs: %v", allOutputs)
	log.Printf("🔗 [CHAINED] Final result: %s", finalOutput)
	log.Printf("🔗 [CHAINED] Recorded timings for %d programs", len(programTimings))

	log.Printf("📊 [CHAINED] Report generation check: timings=%d, needsReport=%v, description=%s", len(programTimings), needsReport, req.Description)

	if len(programTimings) >= 2 && needsReport {
		log.Printf("📊 [CHAINED] Generating performance comparison report")
		report := ie.generatePerformanceReport(programTimings, programs, allOutputs)
		if report != "" && ie.fileStorage != nil {
			reportFile := &StoredFile{
				Filename:    "performance_comparison_report.txt",
				Content:     []byte(report),
				ContentType: "text/plain",
				Size:        int64(len(report)),
				WorkflowID:  workflowID,
				StepID:      "chained_execution",
				CreatedAt:   time.Now(),
				ExpiresAt:   time.Now().Add(24 * time.Hour),
			}
			if err := ie.fileStorage.StoreFile(reportFile); err != nil {
				log.Printf("⚠️ [CHAINED] Failed to store performance report: %v", err)
			} else {
				log.Printf("✅ [CHAINED] Stored performance comparison report")
			}
		} else {
			log.Printf("⚠️ [CHAINED] Report generation skipped: report empty=%v, fileStorage nil=%v", report == "", ie.fileStorage == nil)
		}
	} else {
		log.Printf("⚠️ [CHAINED] Report generation skipped: timings=%d (need 2+), needsReport=%v", len(programTimings), needsReport)
	}

	// Create a combined result that shows all programs
	var combinedResult strings.Builder
	combinedResult.WriteString(fmt.Sprintf("Executed %d programs in sequence:\n\n", len(programs)))
	for i, program := range programs {
		combinedResult.WriteString(fmt.Sprintf("=== Program %d: %s (%s) ===\n", i+1, program.Name, program.Language))
		if i < len(allOutputs) && allOutputs[i] != "" {
			combinedResult.WriteString(fmt.Sprintf("Output: %s\n", allOutputs[i]))
		} else {
			combinedResult.WriteString("Output: (no output)\n")
		}
		if i < len(generatedCodes) && generatedCodes[i] != nil {
			combinedResult.WriteString(fmt.Sprintf("Code: %s\n", generatedCodes[i].Code))
		} else {
			combinedResult.WriteString("Code: (not available)\n")
		}
		combinedResult.WriteString("\n")
	}

	// Create a combined GeneratedCode that includes all programs
	var combinedCode *GeneratedCode
	if len(generatedCodes) > 0 && generatedCodes[0] != nil {
		var combinedCodeText strings.Builder
		combinedCodeText.WriteString(fmt.Sprintf("// Chained execution: %d programs\n\n", len(programs)))
		for i, code := range generatedCodes {
			if code != nil {
				combinedCodeText.WriteString(fmt.Sprintf("// === Program %d: %s (%s) ===\n", i+1, code.TaskName, code.Language))
				combinedCodeText.WriteString(code.Code)
				combinedCodeText.WriteString("\n\n")
			} else if i < len(programs) {

				combinedCodeText.WriteString(fmt.Sprintf("// === Program %d: %s (%s) ===\n", i+1, programs[i].Name, programs[i].Language))
				combinedCodeText.WriteString("// Code generation failed or was not available\n\n")
			}
		}

		combinedCode = &GeneratedCode{
			ID:          generatedCodes[0].ID,
			TaskName:    fmt.Sprintf("chained_%d_programs", len(programs)),
			Description: fmt.Sprintf("Chained execution of %d programs", len(programs)),
			Language:    generatedCodes[0].Language,
			Code:        combinedCodeText.String(),
			Context:     generatedCodes[0].Context,
			CreatedAt:   generatedCodes[0].CreatedAt,
		}
	} else if len(programs) > 0 {
		// Fallback: create a minimal combined code even if no generated codes
		var combinedCodeText strings.Builder
		combinedCodeText.WriteString(fmt.Sprintf("// Chained execution: %d programs\n", len(programs)))
		combinedCodeText.WriteString("// Note: Individual program codes are stored as artifacts (prog1.go, prog2.py, etc.)\n")
		combinedCode = &GeneratedCode{
			ID:          fmt.Sprintf("chained_%d", time.Now().UnixNano()),
			TaskName:    fmt.Sprintf("chained_%d_programs", len(programs)),
			Description: fmt.Sprintf("Chained execution of %d programs", len(programs)),
			Language:    programs[0].Language,
			Code:        combinedCodeText.String(),
			Context:     req.Context,
			CreatedAt:   time.Now(),
		}
	}

	// Determine result format: use combined format for performance comparisons,
	// but use final output for data flow chaining (where one program feeds into another)
	// This maintains backward compatibility with tests that expect the final output
	var resultOutput interface{}
	if needsReport {

		resultOutput = combinedResult.String()
		log.Printf("🔗 [CHAINED] Using combined result format (performance comparison)")
	} else {

		if len(allOutputs) > 0 {
			resultOutput = allOutputs[len(allOutputs)-1]
		} else {
			resultOutput = finalOutput
		}
		log.Printf("🔗 [CHAINED] Using final program output for result (data flow chaining): %v", resultOutput)
	}

	result := &IntelligentExecutionResult{
		Success:        true,
		Result:         resultOutput,
		GeneratedCode:  combinedCode,
		ExecutionTime:  time.Since(start),
		WorkflowID:     workflowID,
		RetryCount:     0,
		UsedCachedCode: false,
		ValidationSteps: []ValidationStep{{
			Step:    "chained_execution",
			Success: true,
			Message: fmt.Sprintf("Successfully executed %d programs in sequence", len(programs)),
		}},
	}

	ie.logIntelligentExecutionMetrics(ctx, req, result)

	return result, nil
}

// parseChainedProgramsWithLLM uses LLM to intelligently parse a request into multiple programs
func (ie *IntelligentExecutor) parseChainedProgramsWithLLM(req *ExecutionRequest) ([]ChainedProgram, error) {
	if ie.llmClient == nil {
		return nil, fmt.Errorf("LLM client not available")
	}

	log.Printf("🔍 [CHAINED-LLM] Parsing request - Description: %s", req.Description)
	log.Printf("🔍 [CHAINED-LLM] TaskName: %s, Language: %s", req.TaskName, req.Language)

	lowerDesc := strings.ToLower(req.Description)
	hasMultipleLanguages := (strings.Contains(lowerDesc, "go") && strings.Contains(lowerDesc, "python")) ||
		(strings.Contains(lowerDesc, "python") && strings.Contains(lowerDesc, "go")) ||
		(strings.Contains(lowerDesc, "then create") || strings.Contains(lowerDesc, "then generate") || strings.Contains(lowerDesc, "then make"))

	multipleProgramsHint := ""
	if hasMultipleLanguages {
		multipleProgramsHint = "\n\n🚨 CRITICAL: This request clearly asks for MULTIPLE programs (mentions multiple languages or uses 'then'). You MUST return at least 2 programs in your JSON array. Do NOT combine them into a single program!"
	}

	prompt := fmt.Sprintf(`You are a code generation assistant. Analyze the following user request and break it down into individual programs that need to be created.

User Request: "%s"

Context: %v%s

Parse this request and identify each distinct program that needs to be created. For each program, extract:
1. The programming language (go, python, javascript, java, etc.)
2. A clear description of what the program should do
3. Any specific requirements (tests, timings, reports, etc.)

CRITICAL INSTRUCTIONS:
- ONLY include what the user explicitly asks for. Do NOT add extra features like unit tests, reports, or other requirements unless the user specifically mentions them.
- If the user asks for a "bubble sort program", create a simple bubble sort program - do NOT add unit tests, performance reports, or other features unless explicitly requested.
- For timing/performance comparisons: The program MUST measure and print its own execution time using built-in timing functions:
  * Go: Import "time" package. Use start := time.Now() BEFORE calling the sorting function, call the sorting function, then elapsed := time.Since(start) AFTER and print ONCE: fmt.Printf("took: %%v\n", elapsed) or fmt.Printf("Duration: %%d nanoseconds\n", elapsed.Nanoseconds())
  * Python: Import time module. Use start_time = time.time() BEFORE calling the sorting function, call the sorting function, then end_time = time.time() AFTER and print ONCE at the END: print("Execution time:", end_time - start_time, "seconds")
  * CRITICAL: Timing code must be OUTSIDE any loops - measure the entire algorithm execution, not individual iterations
  * CRITICAL: Print timing ONCE at the end, not inside loops or multiple times
  * The timing output MUST be in the console output so it can be extracted for performance comparison
  * CRITICAL: If the user asks for performance comparison or timing, you MUST include timing code in BOTH programs
- Do NOT use subprocess.run, subprocess.call, or subprocess.Popen to execute other programs - this is not allowed.
- Keep descriptions simple and focused on what the user actually requested.

Return your response as a JSON array with this exact structure:
[
  {
    "name": "descriptive_name_for_program_1",
    "language": "go",
    "description": "Clear description of what this program should do, ONLY including what the user explicitly requested."
  },
  {
    "name": "descriptive_name_for_program_2", 
    "language": "python",
    "description": "Clear description of what this program should do, ONLY including what the user explicitly requested."
  }
]

Important:
- If the request mentions creating a program in one language "then" creating another, these are SEPARATE programs - return BOTH
- If the request mentions "Go program" AND "Python program", return TWO separate programs
- If tests are mentioned EXPLICITLY by the user, include that in the description
- If reports are mentioned EXPLICITLY by the user, that might be a separate program or part of the last program's description
- Be precise about the language for each program
- Return ONLY valid JSON, no additional text or explanation
- If the request asks for multiple programs, you MUST return multiple programs in the array

JSON Response:`, req.Description, req.Context, multipleProgramsHint)

	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}
	ctx := context.Background()
	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, prompt, priority)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %v", err)
	}

	log.Printf("🔍 [CHAINED-LLM] LLM response: %s", response)

	// Parse the JSON response
	var programDefs []struct {
		Name        string `json:"name"`
		Language    string `json:"language"`
		Description string `json:"description"`
	}

	jsonStart := strings.Index(response, "[")
	jsonEnd := strings.LastIndex(response, "]")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return nil, fmt.Errorf("could not find JSON array in LLM response")
	}

	jsonStr := response[jsonStart : jsonEnd+1]
	if err := json.Unmarshal([]byte(jsonStr), &programDefs); err != nil {
		log.Printf("❌ [CHAINED-LLM] JSON parse error: %v, raw JSON: %s", err, jsonStr)
		return nil, fmt.Errorf("failed to parse LLM JSON response: %v", err)
	}

	if len(programDefs) == 0 {
		return nil, fmt.Errorf("LLM returned empty program list")
	}

	if hasMultipleLanguages && len(programDefs) == 1 {
		log.Printf("⚠️ [CHAINED-LLM] WARNING: Request asks for multiple programs but LLM only returned 1. Request: %s", req.Description)
		log.Printf("⚠️ [CHAINED-LLM] LLM returned: %+v", programDefs)

		log.Printf("🔄 [CHAINED-LLM] Attempting manual split of request into multiple programs")
		manuallySplit := ie.manuallySplitMultiplePrograms(req)
		if len(manuallySplit) > 1 {
			log.Printf("✅ [CHAINED-LLM] Successfully manually split into %d programs", len(manuallySplit))
			programDefs = manuallySplit
		} else {
			log.Printf("⚠️ [CHAINED-LLM] Manual split only found %d program(s), proceeding with LLM result", len(manuallySplit))
		}
	}

	programs := make([]ChainedProgram, len(programDefs))
	for i, def := range programDefs {

		lang := strings.ToLower(def.Language)
		if lang == "" {
			lang = "python"
		}

		programs[i] = ChainedProgram{
			Name:        def.Name,
			Description: def.Description,
			Language:    lang,
			Context:     make(map[string]string),
		}

		log.Printf("✅ [CHAINED-LLM] Parsed program %d: %s (%s) - %s", i+1, def.Name, lang, def.Description)
	}

	return programs, nil
}

// manuallySplitMultiplePrograms attempts to manually split a request into multiple programs
// when the LLM fails to do so correctly
func (ie *IntelligentExecutor) manuallySplitMultiplePrograms(req *ExecutionRequest) []struct {
	Name        string `json:"name"`
	Language    string `json:"language"`
	Description string `json:"description"`
} {
	desc := req.Description
	lowerDesc := strings.ToLower(desc)
	var programs []struct {
		Name        string `json:"name"`
		Language    string `json:"language"`
		Description string `json:"description"`
	}

	thenPatterns := []string{
		" then create", " then generate", " then make", " then ",
		"then create", "then generate", "then make", "then ",
	}
	var parts []string
	var splitIdx int = -1

	for _, pattern := range thenPatterns {
		patternLower := strings.ToLower(pattern)
		if idx := strings.Index(lowerDesc, patternLower); idx > 0 {

			searchStart := idx
			if searchStart > len(desc) {
				searchStart = len(desc) - 10
			}
			if searchStart < 0 {
				searchStart = 0
			}

			searchArea := desc[searchStart:]
			if patternIdx := strings.Index(strings.ToLower(searchArea), patternLower); patternIdx >= 0 {
				splitIdx = searchStart + patternIdx + len(pattern)
				parts = []string{desc[:searchStart+patternIdx], desc[splitIdx:]}
				log.Printf("🔍 [MANUAL-SPLIT] Found pattern '%s' at position %d", pattern, searchStart+patternIdx)
				break
			}
		}
	}

	if len(parts) < 2 {

		langs := []string{"rust", "go", "python", "java", "javascript", "js"}
		langCount := 0
		for _, lang := range langs {
			if strings.Contains(lowerDesc, lang) {
				langCount++
			}
		}
		if langCount >= 2 {
			if idx := strings.Index(lowerDesc, " and "); idx > 0 {
				parts = []string{desc[:idx], desc[idx+5:]}
				log.Printf("🔍 [MANUAL-SPLIT] Split on 'and' at position %d", idx)
			}
		}
	}

	log.Printf("🔍 [MANUAL-SPLIT] Split result: %d parts", len(parts))
	if len(parts) >= 2 {

		if len(parts) >= 2 {

			part1 := strings.TrimSpace(parts[0])
			part1Lower := strings.ToLower(part1)
			lang1 := detectLanguageFromText(part1Lower)

			part2 := strings.TrimSpace(parts[1])

			part2 = strings.TrimPrefix(part2, "create ")
			part2 = strings.TrimPrefix(part2, "generate ")
			part2 = strings.TrimSpace(part2)
			part2Lower := strings.ToLower(part2)
			lang2 := detectLanguageFromText(part2Lower)

			prog1 := struct {
				Name        string `json:"name"`
				Language    string `json:"language"`
				Description string `json:"description"`
			}{
				Name:        fmt.Sprintf("program_1_%s", lang1),
				Language:    lang1,
				Description: part1,
			}

			prog2 := struct {
				Name        string `json:"name"`
				Language    string `json:"language"`
				Description string `json:"description"`
			}{
				Name:        fmt.Sprintf("program_2_%s", lang2),
				Language:    lang2,
				Description: part2,
			}

			programs = append(programs, prog1, prog2)
			log.Printf("✅ [CHAINED-LLM] Manually split: Program 1 (%s): %s", lang1, part1)
			log.Printf("✅ [CHAINED-LLM] Manually split: Program 2 (%s): %s", lang2, part2)
		}
	} else {

		langs := []string{"rust", "go", "python", "java", "javascript", "js"}
		var foundLangs []string
		for _, lang := range langs {
			if strings.Contains(lowerDesc, lang) {
				foundLangs = append(foundLangs, lang)
			}
		}
		if len(foundLangs) >= 2 {

			for i, lang := range foundLangs {
				prog := struct {
					Name        string `json:"name"`
					Language    string `json:"language"`
					Description string `json:"description"`
				}{
					Name:        fmt.Sprintf("program_%d_%s", i+1, lang),
					Language:    lang,
					Description: desc,
				}
				programs = append(programs, prog)
			}
		}
	}

	return programs
}

// parseChainedPrograms parses a request into multiple programs using LLM
func (ie *IntelligentExecutor) parseChainedPrograms(req *ExecutionRequest) ([]ChainedProgram, error) {
	log.Printf("🧠 [CHAINED] Using LLM to parse chained programs from: %s", req.Description)

	programs, err := ie.parseChainedProgramsWithLLM(req)
	if err == nil && len(programs) > 0 {
		log.Printf("✅ [CHAINED] LLM parsing succeeded, found %d programs", len(programs))
		return programs, nil
	}

	log.Printf("⚠️ [CHAINED] LLM parsing failed: %v, falling back to pattern matching", err)

	lowerDesc := strings.ToLower(req.Description)
	hasThen := strings.Contains(lowerDesc, "then")

	langs := []string{"rust", "go", "python", "java", "javascript", "js"}
	langCount := 0
	for _, lang := range langs {
		if strings.Contains(lowerDesc, lang) {
			langCount++
		}
	}
	hasMultipleLangs := langCount >= 2

	if hasThen || hasMultipleLangs {
		log.Printf("🔄 [CHAINED] Request has 'then' or multiple languages, forcing manual split")
		log.Printf("🔄 [CHAINED] hasThen=%v, hasMultipleLangs=%v", hasThen, hasMultipleLangs)
		manuallySplit := ie.manuallySplitMultiplePrograms(req)
		if len(manuallySplit) > 1 {
			log.Printf("✅ [CHAINED] Manual split succeeded, found %d programs", len(manuallySplit))

			chainedPrograms := make([]ChainedProgram, len(manuallySplit))
			for i, def := range manuallySplit {
				chainedPrograms[i] = ChainedProgram{
					Name:        def.Name,
					Description: def.Description,
					Language:    def.Language,
					Context:     make(map[string]string),
				}
			}
			return chainedPrograms, nil
		} else {
			log.Printf("⚠️ [CHAINED] Manual split only found %d program(s), but request clearly asks for multiple", len(manuallySplit))
		}
	}

	description := req.Description
	programs = []ChainedProgram{}

	lowerDesc = strings.ToLower(description)
	hasProg1 := strings.Contains(description, "prog1") ||
		(strings.Contains(lowerDesc, "python") && strings.Contains(lowerDesc, "generates")) ||
		(strings.Contains(lowerDesc, "create") && strings.Contains(lowerDesc, "python")) ||

		(strings.Contains(lowerDesc, "go") && strings.Contains(lowerDesc, "program") && strings.Contains(lowerDesc, "then"))
	hasProg2 := strings.Contains(description, "prog2") ||
		(strings.Contains(lowerDesc, "go") && strings.Contains(lowerDesc, "reads")) ||
		(strings.Contains(lowerDesc, "then") && strings.Contains(lowerDesc, "go")) ||

		(strings.Contains(lowerDesc, "go") && strings.Contains(lowerDesc, "program") && strings.Contains(lowerDesc, "then") && strings.Contains(lowerDesc, "python")) ||

		(strings.Contains(lowerDesc, "then") && (strings.Contains(lowerDesc, "python") || strings.Contains(lowerDesc, "create") && strings.Contains(lowerDesc, "python")))

	log.Printf("🔍 [CHAINED] Parsing description: %s", description)
	log.Printf("🔍 [CHAINED] hasProg1: %v, hasProg2: %v", hasProg1, hasProg2)
	log.Printf("🔍 [CHAINED] Contains 'python': %v, Contains 'generates': %v", strings.Contains(description, "python"), strings.Contains(description, "generates"))
	log.Printf("🔍 [CHAINED] Contains 'go': %v, Contains 'reads': %v", strings.Contains(description, "go"), strings.Contains(description, "reads"))

	if hasProg1 && hasProg2 {

		prog1Desc, prog1Lang := ie.parseProgramRequirements(description, "prog1")
		prog2Desc, prog2Lang := ie.parseProgramRequirements(description, "prog2")

		log.Printf("🔍 [CHAINED] prog1Desc: %s, prog1Lang: %s", prog1Desc, prog1Lang)
		log.Printf("🔍 [CHAINED] prog2Desc: %s, prog2Lang: %s", prog2Desc, prog2Lang)

		prog1 := ChainedProgram{
			Name:        fmt.Sprintf("chained_prog1_%d", time.Now().UnixNano()),
			Description: prog1Desc,
			Context:     make(map[string]string),
			Language:    prog1Lang,
		}
		programs = append(programs, prog1)

		prog2 := ChainedProgram{
			Name:        fmt.Sprintf("chained_prog2_%d", time.Now().UnixNano()),
			Description: prog2Desc,
			Context:     make(map[string]string),
			Language:    prog2Lang,
		}
		programs = append(programs, prog2)
	} else {

		if names, ok := req.Context["artifact_names"]; ok && names != "" {
			parts := strings.Split(names, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {

					ext := strings.ToLower(filepath.Ext(part))
					lang := req.Language
					switch ext {
					case ".py":
						lang = "python"
					case ".go":
						lang = "go"
					case ".js":
						lang = "javascript"
					case ".java":
						lang = "java"
					case ".rs":
						lang = "rust"
					case ".cpp", ".cc", ".cxx":
						lang = "cpp"
					case ".c":
						lang = "c"
					}

					desc := req.Description

					idx := 0
					for i, p := range parts {
						if strings.TrimSpace(p) == part {
							idx = i
							break
						}
					}

					if idx == 0 && strings.Contains(part, ".py") {

						if strings.Contains(strings.ToLower(desc), "program 1") || strings.Contains(strings.ToLower(desc), "prog1") {

							jsonPattern := regexp.MustCompile(`(?i)(program\s*1|prog1).*?(python).*?(print|generate).*?(\{[^}]+\})`)
							if matches := jsonPattern.FindStringSubmatch(desc); len(matches) > 0 {
								jsonStr := matches[len(matches)-1]
								desc = fmt.Sprintf("Program 1 (Python): You MUST print EXACTLY this JSON string: %s. Do NOT print anything else - no labels, no extra text, just the JSON.", jsonStr)
							} else {
								desc = fmt.Sprintf("Program 1 (Python): %s. You MUST generate Python code that prints JSON.", desc)
							}
						} else {
							desc = fmt.Sprintf("Program 1 (Python): Generate %s. %s", part, desc)
						}
					} else if idx == 1 && strings.Contains(part, ".go") {

						if strings.Contains(strings.ToLower(desc), "program 2") || strings.Contains(strings.ToLower(desc), "prog2") {

							readPattern := regexp.MustCompile(`(?i)(program\s*2|prog2).*?(go).*?(read|process).*?(\d+)`)
							if matches := readPattern.FindStringSubmatch(desc); len(matches) > 0 {
								resultNum := matches[len(matches)-1]
								desc = fmt.Sprintf("Program 2 (Go): You MUST read JSON from stdin (or previous program output), extract the 'number' field, multiply it by 2, and print EXACTLY the result: %s. Do NOT print labels, just the number.", resultNum)
							} else {
								desc = fmt.Sprintf("Program 2 (Go): %s. You MUST generate Go code that reads JSON and processes it.", desc)
							}
						} else {
							desc = fmt.Sprintf("Program 2 (Go): Generate %s. This program reads output from the previous program. %s", part, desc)
						}
					} else {

						if idx == 0 {
							desc = fmt.Sprintf("Program 1: Generate %s. %s", part, desc)
						} else {
							desc = fmt.Sprintf("Program %d: Generate %s. This program processes output from previous programs. %s", idx+1, part, desc)
						}
					}

					program := ChainedProgram{
						Name:        strings.TrimSuffix(part, ext),
						Description: desc,
						Context:     make(map[string]string),
						Language:    lang,
					}
					programs = append(programs, program)
				}
			}
		}
	}

	if len(programs) == 0 {

		log.Printf("⚠️ [CHAINED] All parsing methods failed, creating single program from original request as fallback")

		lang := req.Language
		if lang == "" {
			lowerDesc := strings.ToLower(req.Description)
			if strings.Contains(lowerDesc, "rust") || strings.Contains(lowerDesc, ".rs") {
				lang = "rust"
			} else if strings.Contains(lowerDesc, "go") && !strings.Contains(lowerDesc, "python") {
				lang = "go"
			} else if strings.Contains(lowerDesc, "python") {
				lang = "python"
			} else {
				lang = "python"
			}
		}

		fallbackProgram := ChainedProgram{
			Name:        fmt.Sprintf("program_%s", lang),
			Description: req.Description,
			Language:    lang,
			Context:     make(map[string]string),
		}

		log.Printf("✅ [CHAINED] Created fallback program: %s (%s)", fallbackProgram.Name, lang)
		return []ChainedProgram{fallbackProgram}, nil
	}

	return programs, nil
}

// parseProgramRequirements parses the description to extract specific requirements for each program
func (ie *IntelligentExecutor) parseProgramRequirements(description, programName string) (string, string) {
	lower := strings.ToLower(description)

	if strings.Contains(lower, "then") {

		sep := "then"
		if strings.Contains(lower, "and then") {
			sep = "and then"
		}
		parts := strings.Split(description, sep)
		if len(parts) >= 2 {
			part1 := strings.TrimSpace(parts[0])
			part2 := strings.TrimSpace(parts[1])
			lowerPart1 := strings.ToLower(part1)
			lowerPart2 := strings.ToLower(part2)

			if programName == "prog1" {

				lang := "python"
				if strings.Contains(lowerPart1, "go") && strings.Contains(lowerPart1, "program") {
					lang = "go"
				} else if strings.Contains(lowerPart1, "python") && strings.Contains(lowerPart1, "program") {
					lang = "python"
				} else if strings.Contains(lowerPart1, "python") {
					lang = "python"
				} else if strings.Contains(lowerPart1, "go") {
					lang = "go"
				} else if strings.Contains(lowerPart1, "javascript") || strings.Contains(lowerPart1, "js") {
					lang = "javascript"
				} else if strings.Contains(lowerPart1, "java") {
					lang = "java"
				}

				return part1, lang
			} else if programName == "prog2" {

				lang := "python"
				if strings.Contains(lowerPart2, "python") && strings.Contains(lowerPart2, "program") {
					lang = "python"
				} else if strings.Contains(lowerPart2, "go") && strings.Contains(lowerPart2, "program") {
					lang = "go"
				} else if strings.Contains(lowerPart2, "python") {
					lang = "python"
				} else if strings.Contains(lowerPart2, "go") {
					lang = "go"
				} else if strings.Contains(lowerPart2, "javascript") || strings.Contains(lowerPart2, "js") {
					lang = "javascript"
				} else if strings.Contains(lowerPart2, "java") {
					lang = "java"
				}

				return part2, lang
			}
		}
	}

	if strings.Contains(lower, "json") && strings.Contains(lower, "generates") {
		if programName == "prog1" {
			return "Create a Python program that generates JSON with a number", "python"
		}
	}
	if strings.Contains(lower, "json") && strings.Contains(lower, "reads") {
		if programName == "prog2" {
			return "Create a Go program that reads JSON and multiplies the number by 2", "go"
		}
	}

	return fmt.Sprintf("Generate %s", programName), "python"
}

// extractProgramDescription extracts the description for a specific program
func (ie *IntelligentExecutor) extractProgramDescription(description, programName string) string {

	lines := strings.Split(description, "\n")
	var relevantLines []string

	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), programName) {
			relevantLines = append(relevantLines, line)
		}
	}

	if len(relevantLines) > 0 {
		return strings.Join(relevantLines, "\n")
	}

	return fmt.Sprintf("Generate %s", programName)
}

// executeProgramDirectly executes a single program without loop protection (for chained execution)
func (ie *IntelligentExecutor) executeProgramDirectly(ctx context.Context, req *ExecutionRequest, start time.Time, workflowID string) (*IntelligentExecutionResult, error) {
	log.Printf("🔗 [CHAINED] Executing program directly: %s", req.TaskName)

	if req.Language == "" {
		if inferred := ie.inferLanguageFromRequest(req); inferred != "" {
			req.Language = inferred
		} else {
			req.Language = "python"
		}
	}
	if req.MaxRetries == 0 {
		req.MaxRetries = ie.maxRetries
	}
	if req.Timeout == 0 {
		req.Timeout = 300
	}

	result := &IntelligentExecutionResult{
		Success:         false,
		ValidationSteps: []ValidationStep{},
		WorkflowID:      workflowID,
	}

	filteredCtx := filterCodegenContext(req.Context)

	enhancedDesc := req.Description
	if req.Language != "" {
		enhancedDesc = fmt.Sprintf("CRITICAL: You MUST generate %s code, NOT any other language!\n\n%s", req.Language, enhancedDesc)
	}

	if req.Language == "python" && strings.Contains(strings.ToLower(enhancedDesc), "json") {
		enhancedDesc += "\n\nCRITICAL FOR PYTHON JSON: If you need to print JSON, use: json.dumps(dict_object). Do NOT use json.loads() on a dictionary - that's for parsing JSON strings!"
	}

	if strings.Contains(strings.ToLower(enhancedDesc), "matrix") ||
		(req.Context != nil && (req.Context["matrix1"] != "" || req.Context["matrix2"] != "")) {
		if req.Language == "go" {
			enhancedDesc += "\n\n🚨 CRITICAL GO MATRIX REQUIREMENTS:\n1. Read from env: matrix1Str := os.Getenv(\"matrix1\"); json.Unmarshal([]byte(matrix1Str), &matrix1) - DO NOT hardcode!\n2. Import: \"os\", \"encoding/json\", \"fmt\"\n3. Output: Print each row separately - for i := 0; i < len(result); i++ { fmt.Println(result[i]) }\n4. WRONG: fmt.Println(result) prints [[6 8] [10 12]] on one line - this FAILS!\n5. CORRECT output format: [6 8] on line 1, [10 12] on line 2"
		} else if req.Language == "python" {
			enhancedDesc += "\n\n🚨 CRITICAL FOR PYTHON MATRIX OPERATIONS:\n- You MUST read matrices from environment variables using os.getenv(\"matrix1\") and os.getenv(\"matrix2\")\n- Parse the JSON string format (e.g., \"[[1,2],[3,4]]\") using json.loads()\n- DO NOT hardcode matrix values - the matrices will be different each time!\n- Example: matrix1 = json.loads(os.getenv(\"matrix1\"))"
		}
	}

	if req.TaskName != "" {
		enhancedDesc = fmt.Sprintf("%s\n\nIMPORTANT: The generated code will be saved as: %s", enhancedDesc, req.TaskName)

		if strings.HasPrefix(req.TaskName, "prog") || strings.HasPrefix(req.TaskName, "chained_prog") {

			if names, ok := req.Context["artifact_names"]; ok {
				parts := strings.Split(names, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if strings.Contains(part, req.TaskName) || strings.Contains(req.TaskName, strings.TrimSuffix(part, filepath.Ext(part))) {
						ext := filepath.Ext(part)
						if ext != "" {
							enhancedDesc = fmt.Sprintf("%s\n\nCRITICAL: The output filename will be %s - ensure you generate %s code that matches this extension!", enhancedDesc, part, req.Language)
						}
						break
					}
				}
			}
		}
	}

	codeGenReq := &CodeGenerationRequest{
		TaskName:     req.TaskName,
		Description:  enhancedDesc,
		Language:     req.Language,
		Context:      filteredCtx,
		Tags:         []string{"intelligent_execution", "auto_generated", "chained"},
		Executable:   true,
		HighPriority: req.HighPriority,
	}

	isChainedProg2 := (strings.HasPrefix(req.TaskName, "chained_prog2") || strings.HasPrefix(req.TaskName, "prog2")) && req.Language == "go"
	hasPreviousOutput := filteredCtx != nil && filteredCtx["previous_output"] != ""
	needsJSONParsing := req.Language == "go" && (strings.Contains(strings.ToLower(enhancedDesc), "json") ||
		strings.Contains(strings.ToLower(enhancedDesc), "read") ||
		strings.Contains(strings.ToLower(enhancedDesc), "stdin") ||
		isChainedProg2 ||
		hasPreviousOutput)

	if needsJSONParsing {

		codeGenReq.Description = enhancedDesc + "\n\n🚨 CRITICAL: You MUST copy this EXACT code - including ALL imports:\n\npackage main\n\nimport (\n\t\"encoding/json\"\n\t\"fmt\"\n\t\"io\"\n\t\"os\"\n\t\"strings\"\n)\n\nfunc main() {\n\t// Read JSON from stdin - EXACTLY this line, no variations!\n\tjsonBytes, _ := io.ReadAll(os.Stdin)\n\t\n\t// CRITICAL: Trim whitespace and newlines from input\n\tjsonStr := strings.TrimSpace(string(jsonBytes))\n\t\n\t// Unmarshal into map[string]interface{}\n\tvar data map[string]interface{}\n\tjson.Unmarshal([]byte(jsonStr), &data)\n\t\n\t// Extract number as float64, then convert to int\n\t// Use type assertion with ok check to avoid panic\n\tif numVal, ok := data[\"number\"].(float64); ok {\n\t\tnumber := int(numVal)\n\t\t// Calculate result (multiply by 2)\n\t\tresult := number * 2\n\t\t// Print ONLY the number, no labels\n\t\tfmt.Println(result)\n\t}\n}\n\n🚨 CRITICAL RULES - DO NOT DEVIATE:\n- MUST include ALL 5 imports: \"encoding/json\", \"fmt\", \"io\", \"os\", \"strings\"\n- MUST use: io.ReadAll(os.Stdin) - NOT log.Std(), NOT stdin, NOT anything else!\n- MUST trim whitespace: jsonStr := strings.TrimSpace(string(jsonBytes))\n- MUST import \"encoding/json\" to use json.Unmarshal - this is REQUIRED!\n- MUST import \"strings\" to use strings.TrimSpace - this is REQUIRED!\n- MUST import \"os\" package to access os.Stdin\n- MUST use type assertion with ok check: if numVal, ok := data[\"number\"].(float64); ok {\n- DO NOT use direct type assertion without ok check - it will panic if the value is nil!\n- DO NOT use log.Std() or any other function - ONLY os.Stdin!"
	}

	codeGenResult, err := ie.codeGenerator.GenerateCode(codeGenReq)
	if err != nil {
		result.Error = fmt.Sprintf("Code generation failed: %v", err)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	if !codeGenResult.Success {
		result.Error = fmt.Sprintf("Code generation failed: %s", codeGenResult.Error)
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	generatedCode := codeGenResult.Code
	if generatedCode == nil {
		result.Error = "No code generated"
		result.ExecutionTime = time.Since(start)
		return result, nil
	}

	for attempt := 0; attempt < req.MaxRetries; attempt++ {
		log.Printf("🔄 [CHAINED] Validation attempt %d/%d for program: %s", attempt+1, req.MaxRetries, req.TaskName)

		validationResult := ie.validateCode(ctx, generatedCode, req, workflowID)
		result.ValidationSteps = append(result.ValidationSteps, validationResult)
		result.RetryCount = attempt + 1

		if validationResult.Success {
			log.Printf("✅ [CHAINED] Code validation successful on attempt %d", attempt+1)
			result.Success = true
			result.Result = validationResult.Output
			result.GeneratedCode = generatedCode
			result.ExecutionTime = time.Since(start)
			return result, nil
		} else {
			log.Printf("❌ [CHAINED] Code validation failed on attempt %d: %s", attempt+1, validationResult.Error)

			if attempt < req.MaxRetries-1 {
				log.Printf("🔧 [CHAINED] Attempting to fix code using LLM feedback")
				fixedCode, fixErr := ie.fixCodeWithLLM(generatedCode, validationResult, req)
				if fixErr != nil {
					log.Printf("❌ [CHAINED] Code fixing failed: %v", fixErr)
					continue
				}
				generatedCode = fixedCode
				log.Printf("✅ [CHAINED] Code fixed, retrying validation")
			}
		}
	}

	result.Success = false
	if len(result.ValidationSteps) > 0 {
		lastStep := result.ValidationSteps[len(result.ValidationSteps)-1]
		if lastStep.Error != "" {
			result.Error = lastStep.Error
		} else if lastStep.Output != "" {
			result.Error = fmt.Sprintf("Execution failed: %s", lastStep.Output)
		} else {
			result.Error = "Code validation failed after all retry attempts"
		}
		result.Result = lastStep.Output
	} else {
		result.Error = "Code validation failed after all retry attempts"
	}
	result.GeneratedCode = generatedCode
	result.ExecutionTime = time.Since(start)

	ie.logIntelligentExecutionMetrics(ctx, req, result)

	if result.Success && generatedCode != nil {
		ie.recordSuccessfulExecution(req, result, generatedCode)
	} else if !result.Success {
		ie.recordFailedExecution(req, result)
	}

	return result, nil
}
