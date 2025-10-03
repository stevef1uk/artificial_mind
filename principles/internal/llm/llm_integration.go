package llm

import (
	"encoding/json"
	"fmt"
	"principles/internal/analyzer"
	"principles/internal/mapper"
	"strings"
)

// LLMIntegration provides integration with LLM systems for task analysis
type LLMIntegration struct {
	analyzer *analyzer.TaskAnalyzer
	mapper   *mapper.DynamicActionMapper
}

// LLMClient interface for LLM communication
type LLMClient interface {
	AnalyzeTask(taskName, description string, context map[string]interface{}) (string, error)
	GenerateTaskPlan(goal string, context map[string]interface{}) ([]mapper.LLMTask, error)
	RefineTask(task mapper.LLMTask, feedback []string) (mapper.LLMTask, error)
}

// NewLLMIntegration creates a new LLM integration
func NewLLMIntegration(principlesAPIURL string) *LLMIntegration {
	return &LLMIntegration{
		analyzer: analyzer.NewTaskAnalyzer(),
		mapper:   mapper.NewDynamicActionMapper(principlesAPIURL),
	}
}

// AnalyzeTaskWithLLM uses LLM to analyze a task for ethical implications
func (li *LLMIntegration) AnalyzeTaskWithLLM(llmClient LLMClient, taskName, description string, context map[string]interface{}) (*analyzer.TaskAnalysis, error) {
	// First, do basic pattern-based analysis
	basicAnalysis := li.analyzer.AnalyzeTask(taskName, description, context)

	// If confidence is low or risk is high, use LLM for deeper analysis
	if basicAnalysis.Confidence < 0.7 || basicAnalysis.RiskLevel == "high" {
		llmPrompt := li.buildAnalysisPrompt(taskName, description, context, basicAnalysis)

		llmResponse, err := llmClient.AnalyzeTask(taskName, llmPrompt, context)
		if err != nil {
			return basicAnalysis, fmt.Errorf("LLM analysis failed: %v", err)
		}

		// Parse LLM response and enhance analysis
		enhancedAnalysis := li.enhanceAnalysisWithLLM(basicAnalysis, llmResponse)
		return enhancedAnalysis, nil
	}

	return basicAnalysis, nil
}

// GenerateEthicalTaskPlan generates a task plan with ethical considerations
func (li *LLMIntegration) GenerateEthicalTaskPlan(llmClient LLMClient, goal string, context map[string]interface{}) ([]mapper.LLMTask, error) {
	// Generate initial plan
	plan, err := llmClient.GenerateTaskPlan(goal, context)
	if err != nil {
		return nil, fmt.Errorf("failed to generate task plan: %v", err)
	}

	// Validate each task in the plan
	validatedPlan := make([]mapper.LLMTask, 0, len(plan))

	for _, task := range plan {
		// Check if task is ethically allowed
		result := li.mapper.CheckTask(task)

		if result.Allowed {
			validatedPlan = append(validatedPlan, task)
		} else {
			// Try to refine the task
			refinedTask, refineErr := li.refineTaskWithLLM(llmClient, task, result.Reasons)
			if refineErr != nil {
				// If refinement fails, skip this task
				continue
			}

			// Check refined task
			refinedResult := li.mapper.CheckTask(refinedTask)
			if refinedResult.Allowed {
				validatedPlan = append(validatedPlan, refinedTask)
			}
		}
	}

	return validatedPlan, nil
}

// refineTaskWithLLM uses LLM to refine a blocked task
func (li *LLMIntegration) refineTaskWithLLM(llmClient LLMClient, task mapper.LLMTask, reasons []string) (mapper.LLMTask, error) {
	refinedDescription, err := llmClient.RefineTask(task, reasons)
	if err != nil {
		return task, fmt.Errorf("LLM refinement failed: %v", err)
	}

	// Update task with refined description
	task.Description = refinedDescription.Description
	task.Context = refinedDescription.Context

	return task, nil
}

// buildAnalysisPrompt creates a prompt for LLM task analysis
func (li *LLMIntegration) buildAnalysisPrompt(taskName, description string, context map[string]interface{}, analysis *analyzer.TaskAnalysis) string {
	prompt := fmt.Sprintf(`
Analyze the following task for ethical implications:

Task: %s
Description: %s
Context: %v

Current Analysis:
- Risk Level: %s
- Confidence: %.2f
- Human Harm: %v
- Stealing: %v
- Damage: %v
- Unauthorized Access: %v

Please provide additional ethical analysis focusing on:
1. Potential harm to humans
2. Legal or ethical violations
3. Safety concerns
4. Privacy implications
5. Recommendations for making the task safer

Respond in JSON format with fields: human_harm, self_harm, human_order, stealing, damage, unauthorized_access, safety_concerns, recommendations
`, taskName, description, context, analysis.RiskLevel, analysis.Confidence,
		analysis.EthicalContext["human_harm"], analysis.EthicalContext["stealing"],
		analysis.EthicalContext["damage"], analysis.EthicalContext["unauthorized_access"])

	return prompt
}

// buildRefinementPrompt creates a prompt for task refinement
func (li *LLMIntegration) buildRefinementPrompt(task mapper.LLMTask, reasons []string) string {
	prompt := fmt.Sprintf(`
The following task was blocked for ethical reasons:

Task: %s
Description: %s
Blocking Reasons: %v

Please provide a refined version of this task that:
1. Achieves the same goal
2. Avoids the ethical violations mentioned
3. Maintains safety and legality
4. Is more specific and clear

Respond with a JSON object containing: task_name, description, context, sub_tasks
`, task.TaskName, task.Description, reasons)

	return prompt
}

// enhanceAnalysisWithLLM enhances basic analysis with LLM insights
func (li *LLMIntegration) enhanceAnalysisWithLLM(basicAnalysis *analyzer.TaskAnalysis, llmResponse string) *analyzer.TaskAnalysis {
	// Try to parse LLM response as JSON
	var llmAnalysis map[string]interface{}
	if err := json.Unmarshal([]byte(llmResponse), &llmAnalysis); err != nil {
		// If JSON parsing fails, use text analysis
		return li.enhanceAnalysisWithText(basicAnalysis, llmResponse)
	}

	// Update analysis with LLM insights
	enhanced := *basicAnalysis
	enhanced.Confidence = 0.9 // High confidence in LLM analysis

	// Update ethical context with LLM analysis
	if humanHarm, ok := llmAnalysis["human_harm"].(bool); ok {
		enhanced.EthicalContext["human_harm"] = humanHarm
	}
	if selfHarm, ok := llmAnalysis["self_harm"].(bool); ok {
		enhanced.EthicalContext["self_harm"] = selfHarm
	}
	if humanOrder, ok := llmAnalysis["human_order"].(bool); ok {
		enhanced.EthicalContext["human_order"] = humanOrder
	}
	if stealing, ok := llmAnalysis["stealing"].(bool); ok {
		enhanced.EthicalContext["stealing"] = stealing
	}
	if damage, ok := llmAnalysis["damage"].(bool); ok {
		enhanced.EthicalContext["damage"] = damage
	}
	if access, ok := llmAnalysis["unauthorized_access"].(bool); ok {
		enhanced.EthicalContext["unauthorized_access"] = access
	}

	// Add LLM-specific insights
	if safetyConcerns, ok := llmAnalysis["safety_concerns"].(string); ok {
		enhanced.Warnings = append(enhanced.Warnings, "LLM Safety Concerns: "+safetyConcerns)
	}
	if recommendations, ok := llmAnalysis["recommendations"].(string); ok {
		enhanced.Warnings = append(enhanced.Warnings, "LLM Recommendations: "+recommendations)
	}

	// Recalculate risk level
	enhanced.RiskLevel = li.calculateEnhancedRiskLevel(enhanced.EthicalContext)

	return &enhanced
}

// enhanceAnalysisWithText enhances analysis using text-based LLM response
func (li *LLMIntegration) enhanceAnalysisWithText(basicAnalysis *analyzer.TaskAnalysis, llmResponse string) *analyzer.TaskAnalysis {
	enhanced := *basicAnalysis
	enhanced.Confidence = 0.8 // Medium confidence in text analysis

	// Look for keywords in LLM response
	response := strings.ToLower(llmResponse)

	if strings.Contains(response, "harm") || strings.Contains(response, "dangerous") {
		enhanced.EthicalContext["human_harm"] = true
	}
	if strings.Contains(response, "steal") || strings.Contains(response, "unauthorized") {
		enhanced.EthicalContext["stealing"] = true
	}
	if strings.Contains(response, "damage") || strings.Contains(response, "break") {
		enhanced.EthicalContext["damage"] = true
	}
	if strings.Contains(response, "access") || strings.Contains(response, "private") {
		enhanced.EthicalContext["unauthorized_access"] = true
	}

	// Add LLM response as warning
	enhanced.Warnings = append(enhanced.Warnings, "LLM Analysis: "+llmResponse)

	return &enhanced
}

// calculateEnhancedRiskLevel calculates risk level for enhanced analysis
func (li *LLMIntegration) calculateEnhancedRiskLevel(context map[string]interface{}) string {
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

	if riskScore >= 3 {
		return "high"
	} else if riskScore >= 1 {
		return "medium"
	}
	return "low"
}
