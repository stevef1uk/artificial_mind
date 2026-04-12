package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"
)

// validateCode tests the generated code in Docker
func (ie *IntelligentExecutor) validateCode(ctx context.Context, code *GeneratedCode, req *ExecutionRequest, workflowID string) ValidationStep {
	start := time.Now()
	log.Printf("🧪 [VALIDATION] Testing code for task: %s", code.TaskName)
	descPreview := req.Description
	if len(descPreview) > 100 {
		descPreview = descPreview[:100] + "..."
	}
	log.Printf("🔍 [VALIDATION] Request task: %s, description: %s", req.TaskName, descPreview)

	descLower := strings.ToLower(req.Description)
	taskLower := strings.ToLower(req.TaskName)
	if strings.Contains(descLower, "tool_http_get") || strings.Contains(descLower, "tool_html_scraper") ||
		strings.Contains(descLower, "use tool") || strings.Contains(taskLower, "tool_http_get") ||
		strings.Contains(taskLower, "tool_html_scraper") || strings.Contains(taskLower, "use tool") {
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		req.Context["allow_requests"] = "true"
		log.Printf("🔓 [VALIDATION] Allowing HTTP requests for tool-calling task")
	}

	if strings.HasPrefix(descLower, "test hypothesis:") || strings.HasPrefix(taskLower, "test hypothesis:") ||
		strings.HasPrefix(descLower, "test hypothesis by gathering evidence:") ||
		strings.Contains(descLower, "hypothesis testing") || strings.Contains(taskLower, "hypothesis testing") ||
		strings.Contains(descLower, "test hypothesis by gathering evidence") {
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		req.Context["allow_requests"] = "true"
		log.Printf("🔓 [VALIDATION] Allowing HTTP requests for hypothesis testing task")
	}

	if strings.Contains(descLower, "query neo4j") || strings.Contains(taskLower, "query neo4j") ||
		strings.Contains(descLower, "query knowledge base") || strings.Contains(taskLower, "query knowledge base") ||
		strings.Contains(descLower, "knowledge base") && (strings.Contains(descLower, "query") || strings.Contains(descLower, "search")) {
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		req.Context["allow_requests"] = "true"
		log.Printf("🔓 [VALIDATION] Allowing HTTP requests for knowledge base query task")
	}

	if strings.Contains(descLower, "tool_generate_image") || strings.Contains(taskLower, "tool_generate_image") ||
		strings.Contains(descLower, "generate image") || strings.Contains(descLower, "create image") ||
		strings.Contains(descLower, "draw an image") || strings.Contains(descLower, "draw a picture") {
		if req.Context == nil {
			req.Context = make(map[string]string)
		}
		req.Context["allow_requests"] = "true"
		log.Printf("🔓 [VALIDATION] Allowing HTTP requests for image generation task")
	}

	if req.Context != nil {
		if v, ok := req.Context["hypothesis_testing"]; ok && (strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1") {
			req.Context["allow_requests"] = "true"
			log.Printf("🔓 [VALIDATION] Allowing HTTP requests (context flag: hypothesis_testing=true)")
		}
	}

	if unsafeReason := isCodeUnsafeStatic(code.Code, code.Language, req.Context); unsafeReason != "" {
		return ValidationStep{
			Step:     "static_safety_check",
			Success:  false,
			Message:  "Code rejected by safety policy",
			Duration: time.Since(start),
			Code:     code.Code,
			Error:    unsafeReason,
		}
	}

	env := map[string]string{}

	skipKeys := map[string]bool{
		"session_id":         true,
		"project_id":         true,
		"artifact_names":     true,
		"save_code_filename": true,
		"saveCodeFilename":   true,
		"artifacts":          true,
		"artifactsWrapper":   true,
		"artifacts_wrapper":  true,
		"force_regenerate":   true,
		"prefer_traditional": true,
	}
	for k, v := range req.Context {
		if v != "" && !skipKeys[strings.TrimSpace(strings.ToLower(k))] {
			env[k] = v
		}
	}

	env["QUIET"] = "1"

	executionMethod := strings.TrimSpace(os.Getenv("EXECUTION_METHOD"))

	forceDocker := code.Language == "rust" || code.Language == "java"

	if forceDocker {
		log.Printf("🐳 [VALIDATION] Forcing Docker executor for %s (not available on RPI host)", code.Language)
	}

	useSSH := !forceDocker && (executionMethod == "ssh" || (executionMethod == "" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "aarch64" || os.Getenv("ENABLE_ARM64_TOOLS") == "true")))

	// Pass HDN_URL to validation environment so generated code can call tool APIs if needed
	// IMPORTANT: Use host.docker.internal for Docker, but use Kubernetes service DNS for SSH
	var hdnURL string
	if ie.hdnBaseURL != "" {
		hdnURL = ie.hdnBaseURL
	} else if url := os.Getenv("HDN_URL"); url != "" {
		hdnURL = url
	} else {
		hdnURL = "http://localhost:8080"
	}

	if !useSSH && strings.Contains(hdnURL, "localhost") {
		hdnURL = strings.Replace(hdnURL, "localhost", "host.docker.internal", -1)
		log.Printf("🌐 [VALIDATION] Updated HDN_URL for Docker: %s", hdnURL)
	} else if useSSH {

		nodePort := os.Getenv("HDN_NODEPORT")
		if nodePort != "" {

			if strings.Contains(hdnURL, "hdn-server-rpi58.agi.svc.cluster.local") {

				hdnURL = fmt.Sprintf("http://hdn-server-rpi58.agi.svc.cluster.local:%s", nodePort)
			} else if strings.Contains(hdnURL, "localhost") {

				hdnURL = fmt.Sprintf("http://hdn-server-rpi58.agi.svc.cluster.local:%s", nodePort)
			} else {

				hdnURL = strings.Replace(hdnURL, ":8080", fmt.Sprintf(":%s", nodePort), -1)
			}
			log.Printf("🌐 [VALIDATION] Using NodePort %s with service DNS for SSH (resolves to node IP via /etc/hosts): %s", nodePort, hdnURL)
		} else if strings.Contains(hdnURL, "localhost") {

			if k8sService := os.Getenv("HDN_K8S_SERVICE"); k8sService != "" {
				hdnURL = strings.Replace(hdnURL, "localhost:8080", k8sService, -1)
				log.Printf("🌐 [VALIDATION] Using Kubernetes service DNS for SSH: %s", hdnURL)
			} else {

				hdnURL = strings.Replace(hdnURL, "localhost:8080", "hdn-server-rpi58.agi.svc.cluster.local:8080", -1)
				log.Printf("🌐 [VALIDATION] Using Kubernetes service DNS (ClusterIP) for SSH: %s", hdnURL)
			}
		} else {
			log.Printf("🌐 [VALIDATION] Using HDN_URL for SSH execution: %s", hdnURL)
		}
	}

	env["HDN_URL"] = hdnURL

	if allowReq, ok := req.Context["allow_requests"]; ok && allowReq == "true" {
		env["allow_requests"] = "true"
	}

	log.Printf("🔍 [EXEC] Generated code:")
	log.Printf("--- START CODE ---")
	log.Printf("%s", code.Code)
	log.Printf("--- END CODE ---")

	var result *DockerExecutionResponse
	var err error

	if useSSH {
		log.Printf("🧪 [VALIDATION] Using SSH executor for validation")
		result, err = ie.executeWithSSHTool(ctx, code.Code, code.Language, env, true, workflowID)
	} else {
		log.Printf("🧪 [VALIDATION] Using Docker executor for validation")

		if ie.dockerExecutor == nil {
			return ValidationStep{
				Step:     "docker_execution",
				Success:  false,
				Error:    "docker executor unavailable",
				Message:  "Docker execution failed",
				Duration: time.Since(start),
				Code:     code.Code,
			}
		}

		dockerReq := &DockerExecutionRequest{
			Language:     code.Language,
			Code:         code.Code,
			Timeout:      600,
			Environment:  env,
			IsValidation: true,
		}

		if prevOutput, ok := req.Context["previous_output"]; ok && prevOutput != "" {
			dockerReq.Input = prevOutput
			log.Printf("📥 [VALIDATION] Passing previous_output as stdin: %s", prevOutput)
		}

		result, err = ie.dockerExecutor.ExecuteCode(ctx, dockerReq)
		if err != nil {
			result = &DockerExecutionResponse{Success: false, Error: err.Error(), ExitCode: 1}
		}
	}

	validationStep := ValidationStep{
		Step:     "docker_execution",
		Success:  result.Success,
		Duration: time.Since(start),
		Code:     code.Code,
	}

	if err != nil {
		validationStep.Error = err.Error()
		validationStep.Message = "Docker execution failed"
		log.Printf("❌ [VALIDATION] Docker execution failed: %v", err)
		return validationStep
	}

	if !result.Success {
		validationStep.Error = result.Error
		validationStep.Message = "Code execution failed"

		validationStep.Output = result.Output
		log.Printf("❌ [VALIDATION] Code execution failed: %s", result.Error)
		log.Printf("📊 [VALIDATION] Output (may contain compilation errors): %s", result.Output)
		return validationStep
	}

	validationStep.Output = result.Output
	validationStep.Message = "Code execution successful"
	validationStep.Files = result.Files

	if strings.TrimSpace(result.Output) == "" {

		isChainedProgram := strings.HasPrefix(req.TaskName, "prog") ||
			strings.HasPrefix(req.TaskName, "chained_prog") ||
			strings.HasPrefix(req.TaskName, "program_") ||
			strings.Contains(strings.ToLower(req.Description), "create") && strings.Contains(strings.ToLower(req.Description), "program")

		hasFiles := len(result.Files) > 0

		if isChainedProgram && hasFiles {
			log.Printf("✅ [VALIDATION] Chained program executed successfully and created files (no output required)")

		} else if isChainedProgram {
			log.Printf("⚠️ [VALIDATION] Chained program executed successfully but no output (allowing success for file generation)")

		} else {

			shouldHaveOutput := strings.Contains(strings.ToLower(req.Description), "print") ||
				strings.Contains(strings.ToLower(req.Description), "output") ||
				strings.Contains(strings.ToLower(req.Description), "result") ||
				strings.Contains(strings.ToLower(req.Description), "calculate") ||
				strings.Contains(strings.ToLower(req.Description), "generate") ||
				strings.Contains(strings.ToLower(req.Description), "return") ||
				strings.Contains(strings.ToLower(req.Description), "prime") ||
				strings.Contains(strings.ToLower(req.Description), "statistic") ||
				strings.Contains(strings.ToLower(req.Description), "matrix")

			if shouldHaveOutput {
				log.Printf("❌ [VALIDATION] Code executed successfully but produced no output (task requires output)")
				validationStep.Success = false
				validationStep.Error = "Code executed successfully but produced no output"
				validationStep.Message = "Code execution succeeded but no output was produced"
				return validationStep
			}
		}
	}

	log.Printf("✅ [VALIDATION] Code execution successful")
	log.Printf("📊 [VALIDATION] Output: %s", result.Output)

	return validationStep
}

// fixCodeWithLLM attempts to fix code based on validation feedback
func (ie *IntelligentExecutor) fixCodeWithLLM(originalCode *GeneratedCode, validationResult ValidationStep, req *ExecutionRequest) (*GeneratedCode, error) {
	log.Printf("🔧 [FIX] Attempting to fix code using LLM feedback")

	fixPrompt := ie.buildFixPrompt(originalCode, validationResult, req)

	priority := PriorityLow
	if req.HighPriority {
		priority = PriorityHigh
	}
	ctx := context.Background()
	response, err := ie.llmClient.callLLMWithContextAndPriority(ctx, fixPrompt, priority)
	if err != nil {
		return nil, fmt.Errorf("LLM fix call failed: %v", err)
	}

	fixedCode, err := ie.codeGenerator.extractCodeFromResponse(response, originalCode.Language)
	if err != nil {
		return nil, fmt.Errorf("failed to extract fixed code: %v", err)
	}
	if fixedCode == "" {
		return nil, fmt.Errorf("failed to extract fixed code from LLM response")
	}

	fixedGeneratedCode := &GeneratedCode{
		ID:          fmt.Sprintf("code_%d", time.Now().UnixNano()),
		TaskName:    originalCode.TaskName,
		Description: originalCode.Description,
		Language:    originalCode.Language,
		Code:        fixedCode,
		Context:     originalCode.Context,
		CreatedAt:   time.Now(),
		Tags:        append(originalCode.Tags, "fixed"),
		Executable:  true,
	}

	log.Printf("✅ [FIX] Generated fixed code")
	return fixedGeneratedCode, nil
}

// buildFixPrompt creates a prompt for fixing code
func (ie *IntelligentExecutor) buildFixPrompt(originalCode *GeneratedCode, validationResult ValidationStep, req *ExecutionRequest) string {

	languageGuidance := ""
	if originalCode.Language == "javascript" || originalCode.Language == "js" {
		languageGuidance = `
🚨 CRITICAL FOR JAVASCRIPT CODE FIXES:
- Read the compilation error message CAREFULLY - it tells you exactly what's wrong!
- Common JavaScript errors:
  * "Identifier 'X' has already been declared" - Variable X is declared twice (e.g., "let x;" then "let x = [];")
    - FIX: Remove the duplicate declaration - if you already declared it with "let x, y, z;", don't declare it again with "let x = [];"
    - Use assignment instead: "x = [];" (not "let x = [];")
  * "SyntaxError: Cannot use import statement outside a module" - Don't use ES6 import syntax in Node.js without package.json
    - FIX: Use "require()" instead of "import", or use CommonJS syntax
  * "ReferenceError: X is not defined" - Variable used before declaration or typo
  * "TypeError: Cannot read property 'X' of undefined" - Object is undefined before accessing property
- If you see "has already been declared":
  - Check if the variable was declared in a "let x, y, z;" statement at the top
  - If yes, use assignment "x = value;" instead of redeclaring "let x = value;"
  - Example: If you have "let mean, median, mode, stdDev;" at the top, use "mode = [];" not "let mode = [];"
- CRITICAL: Read data from environment variables using process.env, NOT hardcode values!
  - WRONG: "const data = [1, 2, 3, 4, 5];" (hardcoded!)
  - CORRECT: "const dataStr = process.env.data || process.env.input || ''; const data = dataStr.split(',').map(Number);"
  - The context provides parameters like 'data' or 'input' - read them from process.env!
- After fixing, verify:
  - ✅ No duplicate variable declarations
  - ✅ All variables are properly declared before use
  - ✅ Data is read from process.env, not hardcoded
  - ✅ Code uses console.log() for output (not print())
`
	} else if originalCode.Language == "go" {

		isMatrixOp := false
		if req != nil && req.Context != nil {
			if req.Context["matrix1"] != "" || req.Context["matrix2"] != "" {
				isMatrixOp = true
			}
		}
		if !isMatrixOp && req != nil {
			descLower := strings.ToLower(req.Description)
			if strings.Contains(descLower, "matrix") {
				isMatrixOp = true
			}
		}

		matrixGuidance := ""
		if isMatrixOp {
			matrixGuidance = `

🚨🚨🚨 CRITICAL FOR GO MATRIX OPERATIONS - READ CAREFULLY 🚨🚨🚨:
1. **READ MATRICES FROM ENV VARS**: You MUST use os.Getenv("matrix1") and os.Getenv("matrix2"). Parse JSON string "[[1,2],[3,4]]" using encoding/json. DO NOT hardcode matrices.
2. **REQUIRED IMPORTS**: You MUST import "encoding/json", "fmt", and "os".
3. **OUTPUT FORMAT (CRITICAL)**: You MUST print each ROW on a SEPARATE line using fmt.Println().
   - WRONG: fmt.Println(result) (prints [[6 8] [10 12]] on ONE line - FAILS VALIDATION!)
   - CORRECT: Use a loop: for i := 0; i < len(result); i++ { fmt.Println(result[i]) }
   - Expected output: [6 8] on first line, [10 12] on second line.
4. **VALIDATION FAILURE**: If validation failed because output doesn't match expected pattern, check:
   - Are you printing the entire matrix with one fmt.Println? (WRONG - prints on one line)
   - Are you printing each row separately? (CORRECT - each row on its own line)
   - Did you read matrices from environment variables? (REQUIRED - do not hardcode)
`
		}

		languageGuidance = `
🚨 GO FIX RULES (fix ALL errors in ONE pass):
- "undefined: X" → Add missing import (json→encoding/json, os.Getenv→os, fmt.Println→fmt, io.ReadAll→io+os)
- "X declared/imported and not used" → REMOVE it (Go treats unused as ERROR)
- "assignment mismatch: 2 vars but X returns 1" → json.Unmarshal returns ONLY error, NOT (value, error)!
- json.Unmarshal: err := json.Unmarshal(bytes, &data) (NOT jsonBytes, _ := ...)
- io.ReadAll: bytes, err := io.ReadAll(os.Stdin) (returns 2 values)
- JSON numbers are float64, NOT int64: data["key"].(float64) then convert to int
- Fix ALL errors at once - don't fix one at a time!` + matrixGuidance
	} else if originalCode.Language == "rust" {
		languageGuidance = `
🚨 CRITICAL FOR RUST CODE FIXES:
- Read the compilation error message CAREFULLY - Rust's borrow checker is very specific!
- Common Rust compilation errors:
  * "cannot borrow X as mutable, as it is not declared as mutable" - Variable needs mut keyword
    - FIX: Change "let x = ..." to "let mut x = ..."
    - If it's already mut, check if you're trying to borrow it incorrectly
  * "cannot borrow X as mutable because it is also borrowed as immutable" - Conflicting borrows
    - FIX: Restructure code to avoid simultaneous mutable and immutable borrows
    - Use scopes to limit borrow lifetimes: "{ let borrow = &x; ... }" then "x.mut_method()"
  * "cannot move out of X which is behind a shared reference" - Trying to move from a reference
    - FIX: Clone the value: "let y = x.clone();" or use references instead of moving
  * "expected X, found Y" - Type mismatch
    - FIX: Check types match - use ".to_string()", ".parse()", or explicit type conversions
  * "use of moved value: X" - Value was moved and can't be used again
    - FIX: Use references "&X" instead of moving, or clone if needed
  * "mismatched types" - Function expects different type
    - FIX: Check function signature and provide correct type
- For Box type mutable borrows:
  - WRONG: "increment_age(&mut person_box);" when person_box is Box<Person>
  - CORRECT: "increment_age(&mut *person_box);" (dereference then borrow)
  - OR: "let person = &mut *person_box; increment_age(person);"
- For Box type immutable borrows:
  - WRONG: "print_person(person_box);" when person_box is Box<Person>
  - CORRECT: "print_person(&*person_box);" or "print_person(&person_box);"
- If you see "cannot borrow as mutable":
  1. Check if the variable is declared with "mut": "let mut x = ..."
  2. If it's a Box, dereference first: "&mut *box_value"
  3. If it's already borrowed immutably, end that borrow before mutable borrow
- After fixing, verify:
  - ✅ All variables that need mutation are declared with "mut"
  - ✅ Box values are properly dereferenced before borrowing: "&mut *box" or "&*box"
  - ✅ No conflicting borrows (mutable and immutable at the same time)
  - ✅ No use of moved values
  - ✅ Types match function signatures
`
	}

	return fmt.Sprintf(`You are an expert programmer. The following code failed to execute properly and needs to be fixed.

Original Task: %s
Description: %s
Language: %s

Original Code:
`+"```"+`%s
%s
`+"```"+`

Error Details:
- Step: %s
- Error: %s
- Output: %s

Context:
%s
%s

🚨 CRITICAL FIXING INSTRUCTIONS:
1. Read ALL errors in the error message above - fix ALL of them in ONE revision!
2. If you see compilation errors, fix ALL of them before returning code!
3. If you see "imported and not used" errors, remove those imports BUT also check if you need to ADD other imports!
4. If you see "undefined" errors, add the missing imports!
5. After fixing, verify the code will compile and run successfully!
6. Make sure the code actually produces output - if the task requires reading JSON and printing a field, ensure the code reads from stdin and prints the result!

Please fix the code to make it work correctly. Return ONLY the fixed code wrapped in markdown code blocks like this:
`+"```"+`%s
// Your fixed code here
`+"```"+`

Fixed code:`,
		originalCode.TaskName,
		originalCode.Description,
		originalCode.Language,
		originalCode.Language,
		originalCode.Code,
		validationResult.Step,
		validationResult.Error,
		validationResult.Output,
		ie.formatContext(req.Context),
		languageGuidance,
		originalCode.Language)
}

// formatContext formats context map for display
func (ie *IntelligentExecutor) formatContext(context map[string]string) string {
	if len(context) == 0 {
		return "No additional context"
	}

	var parts []string
	for k, v := range context {
		parts = append(parts, fmt.Sprintf("- %s: %s", k, v))
	}
	return strings.Join(parts, "\n")
}

// inferLanguageFromRequest tries to determine the intended programming language
// from the task name, description, and context. Returns empty string if unknown.
func (ie *IntelligentExecutor) inferLanguageFromRequest(req *ExecutionRequest) string {

	if lang, ok := req.Context["language"]; ok && strings.TrimSpace(lang) != "" {
		return strings.ToLower(strings.TrimSpace(lang))
	}

	desc := strings.ToLower(strings.TrimSpace(req.Description))

	if strings.Contains(desc, "rust") || strings.Contains(desc, "rust program") ||
		strings.Contains(desc, "rust code") || strings.Contains(desc, ".rs") ||
		strings.Contains(desc, " in rust") || strings.Contains(desc, "create a rust") ||
		strings.Contains(desc, "write a rust") || strings.Contains(desc, "build a rust") {
		return "rust"
	}

	if strings.Contains(desc, " go ") || strings.HasPrefix(desc, "go ") || strings.HasSuffix(desc, " in go") ||
		strings.Contains(desc, " in golang") || strings.Contains(desc, "golang") ||
		strings.Contains(desc, "main.go") || strings.Contains(desc, "go program") ||
		strings.Contains(desc, "go code") || strings.Contains(desc, ".go") {
		return "go"
	}

	if strings.Contains(desc, "python") || strings.Contains(desc, "py script") ||
		strings.Contains(desc, "python program") || strings.Contains(desc, "python code") ||
		strings.Contains(desc, ".py") {
		return "python"
	}

	task := strings.ToLower(strings.TrimSpace(req.TaskName))

	if strings.Contains(task, "rust") || strings.Contains(task, ".rs") {
		return "rust"
	}

	if strings.Contains(task, "python") || strings.Contains(task, ".py") {
		return "python"
	}

	if strings.Contains(task, "go ") || strings.Contains(task, " golang") ||
		strings.Contains(task, ".go") || strings.Contains(task, "golang") {
		return "go"
	}

	return ""
}

// ExecutePrimeNumbersExample demonstrates the intelligent execution with prime numbers
func (ie *IntelligentExecutor) ExecutePrimeNumbersExample(ctx context.Context, count int) (*IntelligentExecutionResult, error) {
	req := &ExecutionRequest{
		TaskName:    "CalculatePrimes",
		Description: fmt.Sprintf("Calculate the first %d prime numbers", count),
		Context: map[string]string{
			"count": fmt.Sprintf("%d", count),
			"input": fmt.Sprintf("%d", count),
		},
		Language:        "python",
		ForceRegenerate: false,
		MaxRetries:      3,
		Timeout:         600,
	}

	return ie.ExecuteTaskIntelligently(ctx, req)
}
