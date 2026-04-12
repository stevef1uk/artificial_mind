package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	selfmodel "agi/self"

	"github.com/redis/go-redis/v9"
)

// IntelligentExecutor handles the complete workflow of:
// 1. Planning tasks using HTN and Planner/Evaluator
// 2. Generating code for unknown tasks using LLM
// 3. Testing generated code in Docker
// 4. Caching successful code for reuse
// 5. Learning from failures and improving
type IntelligentExecutor struct {
	domainManager      *DomainManager
	codeStorage        *CodeStorage
	codeGenerator      *CodeGenerator
	dockerExecutor     *SimpleDockerExecutor
	llmClient          *LLMClient
	actionManager      *ActionManager
	plannerIntegration *PlannerIntegration
	selfModelManager   *selfmodel.Manager
	toolMetrics        *ToolMetricsManager
	fileStorage        *FileStorage
	hdnBaseURL         string // For tool calling
	maxRetries         int
	validationMode     bool
	usePlanner         bool
	recentTasks        map[string]time.Time // Loop protection: track recent task executions
	learningRedis      *redis.Client        // Redis client for learning data
	ctx                context.Context      // Context for Redis operations
}

// FailurePattern tracks common failure patterns for learning
type FailurePattern struct {
	PatternType   string    `json:"pattern_type"`   // "compilation", "runtime", "logic", "validation"
	ErrorCategory string    `json:"error_category"` // "undefined", "type_mismatch", "import_error", etc.
	Language      string    `json:"language"`
	TaskCategory  string    `json:"task_category"` // Derived from task name/description
	Frequency     int       `json:"frequency"`
	SuccessRate   float64   `json:"success_rate"` // Success rate after fixes
	CommonFixes   []string  `json:"common_fixes"` // What fixes work for this pattern
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
}

// CodeGenStrategy tracks code generation strategies and their effectiveness
type CodeGenStrategy struct {
	StrategyID   string    `json:"strategy_id"`
	PromptStyle  string    `json:"prompt_style"` // "detailed", "concise", "example_based", etc.
	TaskCategory string    `json:"task_category"`
	Language     string    `json:"language"`
	SuccessRate  float64   `json:"success_rate"`
	AvgRetries   float64   `json:"avg_retries"`
	AvgQuality   float64   `json:"avg_quality"`
	UsageCount   int       `json:"usage_count"`
	LastUsed     time.Time `json:"last_used"`
}

// CodeGenLearningProgress tracks learning progress by task category and language
type CodeGenLearningProgress struct {
	TaskCategory   string  `json:"task_category"`
	Language       string  `json:"language"`
	SuccessRate    float64 `json:"success_rate"`
	AvgQuality     float64 `json:"avg_quality"`
	RecentProgress float64 `json:"recent_progress"` // Progress in last N executions
	FocusScore     float64 `json:"focus_score"`     // Should we focus here?
}

// ExecutionRequest represents a request to execute a task intelligently
type ExecutionRequest struct {
	TaskName        string            `json:"task_name"`
	Description     string            `json:"description"`
	Context         map[string]string `json:"context"`
	Language        string            `json:"language"`
	ForceRegenerate bool              `json:"force_regenerate"`
	MaxRetries      int               `json:"max_retries"`
	Timeout         int               `json:"timeout"`
	HighPriority    bool              `json:"high_priority"` // true for user requests, false for background tasks
}

// IntelligentExecutionResult represents the result of intelligent execution
type IntelligentExecutionResult struct {
	Success         bool             `json:"success"`
	Result          interface{}      `json:"result,omitempty"`
	Error           string           `json:"error,omitempty"`
	GeneratedCode   *GeneratedCode   `json:"generated_code,omitempty"`
	ExecutionTime   time.Duration    `json:"execution_time"`
	RetryCount      int              `json:"retry_count"`
	UsedCachedCode  bool             `json:"used_cached_code"`
	ValidationSteps []ValidationStep `json:"validation_steps,omitempty"`
	NewAction       *DynamicAction   `json:"new_action,omitempty"`
	WorkflowID      string           `json:"workflow_id,omitempty"`
}

// ValidationStep represents a step in the validation process
type ValidationStep struct {
	Step     string            `json:"step"`
	Success  bool              `json:"success"`
	Message  string            `json:"message"`
	Duration time.Duration     `json:"duration"`
	Code     string            `json:"code,omitempty"`
	Output   string            `json:"output,omitempty"`
	Error    string            `json:"error,omitempty"`
	Files    map[string][]byte `json:"files,omitempty"`
}

func NewIntelligentExecutor(
	domainManager *DomainManager,
	codeStorage *CodeStorage,
	codeGenerator *CodeGenerator,
	dockerExecutor *SimpleDockerExecutor,
	llmClient *LLMClient,
	actionManager *ActionManager,
	plannerIntegration *PlannerIntegration,
	selfModelManager *selfmodel.Manager,
	toolMetrics *ToolMetricsManager,
	fileStorage *FileStorage,
	hdnBaseURL string,
	redisAddr string,
) *IntelligentExecutor {
	// Initialize Redis client for learning data if address provided
	var learningRedis *redis.Client
	if redisAddr != "" {
		learningRedis = redis.NewClient(&redis.Options{
			Addr: redisAddr,
		})
	}

	return &IntelligentExecutor{
		domainManager:      domainManager,
		codeStorage:        codeStorage,
		codeGenerator:      codeGenerator,
		dockerExecutor:     dockerExecutor,
		llmClient:          llmClient,
		actionManager:      actionManager,
		plannerIntegration: plannerIntegration,
		selfModelManager:   selfModelManager,
		toolMetrics:        toolMetrics,
		fileStorage:        fileStorage,
		hdnBaseURL:         hdnBaseURL,
		maxRetries:         3,
		validationMode:     true,
		usePlanner:         plannerIntegration != nil,
		learningRedis:      learningRedis,
		ctx:                context.Background(),
		recentTasks:        make(map[string]time.Time),
	}
}

// ParameterCompatibility represents the result of parameter compatibility checking
type ParameterCompatibility struct {
	IsCompatible bool    `json:"is_compatible"`
	Status       string  `json:"status"`
	Reason       string  `json:"reason"`
	Confidence   float64 `json:"confidence"`
}

// normalizeLanguageAndCode infers a correct runtime when saved language is unsupported and
// the code contains a recognizable header (e.g., leading "python\n"). Returns normalized language and code.
func normalizeLanguageAndCode(savedLanguage, code string) (string, string) {
	supported := map[string]bool{"python": true, "javascript": true, "bash": true, "sh": true, "go": true}
	lang := strings.ToLower(strings.TrimSpace(savedLanguage))
	if supported[lang] {
		return lang, code
	}
	if strings.HasPrefix(code, "python\n") {
		return "python", strings.TrimPrefix(code, "python\n")
	}
	if lang == "" {
		lang = "python"
	}
	return lang, code
}

// isCodeUnsafeStatic performs a lightweight static scan to block obviously dangerous behavior
func isCodeUnsafeStatic(code string, language string, ctx map[string]string) string {
	lower := strings.ToLower(code)

	dangerous := []string{
		"os.system(", "subprocess.popen", "subprocess.call", "subprocess.run",
		"shutil.rmtree", "eval(", "exec(", "__import__(", "open('/",
		"socket.",

		"docker run", "docker exec", "docker build", "docker ps", "docker stop", "docker start",
		"docker rm", "docker rmi", "docker pull", "docker push", "docker compose",
		"podman run", "podman exec", "kubectl ",
	}

	allowReq := false
	if v := strings.TrimSpace(os.Getenv("ALLOW_REQUESTS")); v == "1" || strings.EqualFold(v, "true") {
		allowReq = true
	}
	if ctx != nil {
		if v, ok := ctx["allow_requests"]; ok && (strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1") {
			allowReq = true
		}
	}
	if !allowReq {

		dangerous = append(dangerous, "requests.", "urllib.request", "wget ", "curl ")
	}
	for _, pat := range dangerous {
		if strings.Contains(lower, pat) {
			return "contains dangerous pattern: " + pat
		}
	}

	return ""
}

// isRequestUnsafeStatic blocks obviously dangerous intents before generation
func isRequestUnsafeStatic(req *ExecutionRequest) string {
	lowerTask := strings.ToLower(req.TaskName + " " + req.Description)

	patterns := []string{
		"delete all files", "wipe all files", "format disk", "rm -rf /",
		"destroy database", "exfiltrate", "ransomware", "dd if=/dev/zero",
		"deletes all files", "deleting all files", "delete all file",
	}
	for _, p := range patterns {
		if strings.Contains(lowerTask, p) {
			return "request contains disallowed destructive intent: " + p
		}
	}

	adultPatterns := []string{
		"inappropriate content", "adults only", "adult content", "porn", "xxx",
		"nsfw", "explicit sexual", "erotic",
		"inappropriate", "for adults only", "adults-only",
	}
	for _, p := range adultPatterns {
		if strings.Contains(lowerTask, p) {
			return "request contains disallowed inappropriate content intent: " + p
		}
	}

	if req.Context != nil {
		tgt := strings.ToLower(strings.TrimSpace(req.Context["target"]))
		op := strings.ToLower(strings.TrimSpace(req.Context["operation"]))
		if (tgt == "all_files" || strings.Contains(tgt, "all file")) &&
			(op == "delete" || op == "wipe" || op == "destroy") {
			return "request attempts destructive operation on all files"
		}

		ctype := strings.ToLower(strings.TrimSpace(req.Context["content_type"]))
		audience := strings.ToLower(strings.TrimSpace(req.Context["audience"]))
		if strings.Contains(ctype, "inappropriate") || strings.Contains(ctype, "adult") || audience == "adults" || audience == "adults only" {
			return "request attempts to generate inappropriate/adult content"
		}
	}
	return ""
}

// inferLanguageFromIntelligentRequest tries to determine the intended programming language
// from the task name, description, and context. Returns empty string if unknown.
func inferLanguageFromRequest(req *IntelligentExecutionRequest) string {

	desc := strings.ToLower(strings.TrimSpace(req.Description))

	if strings.Contains(desc, "rust") || strings.Contains(desc, "rust program") ||
		strings.Contains(desc, "rust code") || strings.Contains(desc, ".rs") ||
		strings.Contains(desc, " in rust") || strings.Contains(desc, "write a rust") ||
		strings.Contains(desc, "create a rust") || strings.Contains(desc, "build a rust") {
		return "rust"
	}

	if strings.Contains(desc, " go ") || strings.HasPrefix(desc, "go ") || strings.HasSuffix(desc, " in go") ||
		strings.Contains(desc, " in golang") || strings.Contains(desc, "golang") ||
		strings.Contains(desc, "main.go") || strings.Contains(desc, "go program") ||
		strings.Contains(desc, "go code") || strings.Contains(desc, "go script") ||
		strings.Contains(desc, "write a go") || strings.Contains(desc, "create a go") ||
		strings.Contains(desc, "build a go") || strings.Contains(desc, "develop a go") ||
		strings.Contains(desc, ".go") || strings.Contains(desc, "golang program") ||
		strings.Contains(desc, "golang code") || strings.Contains(desc, "golang script") {
		return "go"
	}

	task := strings.ToLower(strings.TrimSpace(req.TaskName))

	if strings.Contains(task, "rust") || strings.Contains(task, ".rs") {
		return "rust"
	}

	if strings.Contains(task, "go ") || strings.Contains(task, " golang") ||
		strings.Contains(task, ".go") || strings.Contains(task, "golang") {
		return "go"
	}

	if lang, ok := req.Context["language"]; ok && strings.TrimSpace(lang) != "" {
		return strings.ToLower(strings.TrimSpace(lang))
	}

	return ""
}

// filterCodegenContext removes non-functional keys that cause the LLM to emit unused variables
// or irrelevant code. It keeps only keys likely needed for computation or file hints.
func filterCodegenContext(in map[string]string) map[string]string {
	if len(in) == 0 {
		return in
	}

	drop := map[string]bool{
		"session_id":         true,
		"project_id":         true,
		"artifact_names":     true,
		"save_code_filename": true,
		"artifacts_wrapper":  true,
		"force_regenerate":   true,

		"saveCodeFilename": true,
		"artifacts":        true,
		"artifactsWrapper": true,
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if drop[strings.TrimSpace(strings.ToLower(k))] {
			continue
		}
		out[k] = v
	}
	return out
}

// ChainedProgram represents a single program in a chained execution
type ChainedProgram struct {
	Name        string
	Description string
	Context     map[string]string
	Language    string
}

// detectLanguageFromText detects programming language from text description
// This is a helper function for chained program detection
func detectLanguageFromText(text string) string {
	textLower := strings.ToLower(text)

	if strings.Contains(textLower, "rust") || strings.Contains(textLower, ".rs") {
		return "rust"
	}

	if strings.Contains(textLower, " go ") || strings.HasPrefix(textLower, "go ") ||
		strings.Contains(textLower, "golang") || strings.Contains(textLower, ".go") {
		return "go"
	}

	if strings.Contains(textLower, "python") || strings.Contains(textLower, ".py") {
		return "python"
	}

	if strings.Contains(textLower, "javascript") || strings.Contains(textLower, "js") ||
		strings.Contains(textLower, "node") || strings.Contains(textLower, ".js") {
		return "javascript"
	}

	if strings.Contains(textLower, "java") && !strings.Contains(textLower, "javascript") ||
		strings.Contains(textLower, ".java") {
		return "java"
	}

	return "python"
}

// extractJSONFromOutput extracts clean JSON from program output, removing env vars and other noise
// extractTimingFromOutput extracts algorithm execution time from program output
// Looks for patterns like "took: 123ns", "duration: 456ms", "Time: 789 microseconds", etc.
func extractTimingFromOutput(output string, language string) int64 {
	if output == "" {
		return 0
	}

	patterns := []*regexp.Regexp{

		regexp.MustCompile(`(?i)execution\s+time[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)(ns|us|ms|s|h|m)\b`),

		regexp.MustCompile(`(?i)execution\s+time[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s+(ns|nanoseconds|nanosecond)`),

		regexp.MustCompile(`(?i)execution\s+time[:\s]+((?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?)\s*(ns|nanoseconds|nanosecond|us|microseconds|microsecond|ms|milliseconds|millisecond|s|seconds|second)`),

		regexp.MustCompile(`(?i)(?:took|duration|time|elapsed)[:\s]+((?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?|\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ns|nanoseconds|nanosecond|us|microseconds|microsecond|ms|milliseconds|millisecond|s|seconds|second)`),

		regexp.MustCompile(`(?i)execution\s+time[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(s|seconds|second|ms|milliseconds|millisecond|us|microseconds|microsecond|ns|nanoseconds|nanosecond)`),

		regexp.MustCompile(`(?i)(?:took|duration|time|elapsed)[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ms|milliseconds|millisecond|s|seconds|second|us|microseconds|microsecond|ns|nanoseconds|nanosecond)`),

		regexp.MustCompile(`(?i)(?:sorting\s+time|algorithm\s+time)[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ns|nanoseconds|nanosecond|ms|milliseconds|millisecond|s|seconds|second|us|microseconds|microsecond)`),
	}

	// Try each pattern and find ALL matches, then use the LAST one
	// This handles cases where timing is printed multiple times (e.g., in loops)
	var lastMatch []string

	for _, pattern := range patterns {

		allMatches := pattern.FindAllStringSubmatch(output, -1)
		if len(allMatches) > 0 {

			match := allMatches[len(allMatches)-1]
			if len(match) >= 3 {
				lastMatch = match
			}
		}
	}

	if len(lastMatch) >= 3 {
		valueStr := lastMatch[1]
		unit := strings.ToLower(lastMatch[2])

		var nanoseconds int64

		if matched, _ := regexp.MatchString(`^(?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?$`, valueStr); matched {

			if duration, err := time.ParseDuration(valueStr); err == nil {
				nanoseconds = duration.Nanoseconds()
				log.Printf("🔍 [TIMING-EXTRACT] Parsed Go Duration string '%s' = %d ns", valueStr, nanoseconds)
				if nanoseconds > 0 {
					return nanoseconds
				}
			} else {
				log.Printf("⚠️ [TIMING-EXTRACT] Failed to parse Go Duration string '%s': %v", valueStr, err)
			}
		}

		// Try parsing as a numeric value (handles scientific notation)
		var value float64

		if parsed, err := strconv.ParseFloat(valueStr, 64); err == nil {
			value = parsed
		} else {
			log.Printf("⚠️ [TIMING-EXTRACT] Failed to parse timing value '%s': %v", valueStr, err)
			return 0
		}

		switch unit {
		case "ns", "nanoseconds", "nanosecond":
			nanoseconds = int64(value)
		case "us", "microseconds", "microsecond":
			nanoseconds = int64(value * 1000)
		case "ms", "milliseconds", "millisecond":
			nanoseconds = int64(value * 1000000)
		case "s", "seconds", "second":
			nanoseconds = int64(value * 1000000000)
		default:
			return 0
		}

		if nanoseconds > 0 {
			log.Printf("🔍 [TIMING-EXTRACT] Found timing: %s %s = %d ns (from last occurrence)", valueStr, unit, nanoseconds)
			return nanoseconds
		}
	}

	return 0
}

func extractJSONFromOutput(output string) string {
	if output == "" {
		return ""
	}

	lines := strings.Split(output, "\n")
	var jsonLines []string
	inJSON := false

	for _, line := range lines {
		lineTrimmed := strings.TrimSpace(line)

		if lineTrimmed == "" {
			continue
		}

		if matched, _ := regexp.MatchString(`^[A-Z_][A-Z0-9_]*=['"]`, lineTrimmed); matched {
			continue
		}

		if strings.Contains(lineTrimmed, "Warning: Permanently added") ||
			strings.Contains(lineTrimmed, "Host key verification") {
			continue
		}

		if strings.HasPrefix(lineTrimmed, "{") || strings.HasPrefix(lineTrimmed, "[") {
			inJSON = true
			jsonLines = append(jsonLines, lineTrimmed)

			if strings.HasSuffix(lineTrimmed, "}") || strings.HasSuffix(lineTrimmed, "]") {
				break
			}
			continue
		}

		if inJSON {
			jsonLines = append(jsonLines, lineTrimmed)
			if strings.HasSuffix(lineTrimmed, "}") || strings.HasSuffix(lineTrimmed, "]") {
				break
			}
		}
	}

	if len(jsonLines) == 0 {

		startIdx := -1
		var openChar, closeChar byte
		for i := 0; i < len(output); i++ {
			if output[i] == '{' {
				startIdx = i
				openChar = '{'
				closeChar = '}'
				break
			} else if output[i] == '[' {
				startIdx = i
				openChar = '['
				closeChar = ']'
				break
			}
		}

		if startIdx >= 0 {

			depth := 0
			endIdx := -1
			for i := startIdx; i < len(output); i++ {
				if output[i] == openChar {
					depth++
				} else if output[i] == closeChar {
					depth--
					if depth == 0 {
						endIdx = i
						break
					}
				}
			}
			if endIdx > startIdx {
				extracted := output[startIdx : endIdx+1]
				// Validate extracted JSON
				var test interface{}
				if err := json.Unmarshal([]byte(extracted), &test); err == nil {
					return extracted
				}
			}
		}

		return ""
	}

	jsonStr := strings.Join(jsonLines, "\n")

	// Validate it's valid JSON
	var test interface{}
	if err := json.Unmarshal([]byte(jsonStr), &test); err == nil {
		return jsonStr
	}

	startIdx := -1
	for i := 0; i < len(jsonStr); i++ {
		if jsonStr[i] == '{' || jsonStr[i] == '[' {
			startIdx = i
			break
		}
	}
	if startIdx >= 0 {

		openChar := jsonStr[startIdx]
		var closeChar byte = '}'
		if openChar == '[' {
			closeChar = ']'
		}
		depth := 0
		endIdx := -1
		for i := startIdx; i < len(jsonStr); i++ {
			if jsonStr[i] == openChar {
				depth++
			} else if jsonStr[i] == closeChar {
				depth--
				if depth == 0 {
					endIdx = i
					break
				}
			}
		}
		if endIdx > startIdx {
			extracted := jsonStr[startIdx : endIdx+1]
			// Validate extracted JSON
			var test2 interface{}
			if err := json.Unmarshal([]byte(extracted), &test2); err == nil {
				return extracted
			}
		}
	}

	return ""
}

// truncateString truncates a string to max length for logging
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
