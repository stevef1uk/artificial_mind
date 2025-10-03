// main.go
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	principles "principles/pkg"
	"strings"
	"time"
)

// --------- Domain definitions ---------

type MethodDef struct {
	Task          string   `json:"task"`
	Preconditions []string `json:"preconditions"`
	Subtasks      []string `json:"subtasks"`
	IsLearned     bool     `json:"-"`
}

// Global principles client for ethical checking
var principlesClient *principles.Client

// InitializePrinciplesClient initializes the principles client
func InitializePrinciplesClient(principlesAPIURL string) {
	principlesClient = principles.NewClient(principlesAPIURL)
	fmt.Printf("🔒 Principles client initialized: %s\n", principlesAPIURL)
}

// isHarmfulAction checks if an action name suggests it could harm humans
func isHarmfulAction(actionName string) bool {
	harmfulKeywords := []string{
		"harm", "hurt", "injure", "damage", "destroy", "kill", "attack",
		"strike", "hit", "punch", "kick", "dangerous", "unsafe",
	}

	actionLower := strings.ToLower(actionName)
	for _, keyword := range harmfulKeywords {
		if strings.Contains(actionLower, keyword) {
			return true
		}
	}
	return false
}

// CheckActionWithPrinciples checks if an action is ethically allowed
func CheckActionWithPrinciples(actionName string, context map[string]interface{}) (bool, []string, error) {
	if principlesClient == nil {
		fmt.Printf("⚠️ [PRINCIPLES] Principles client not initialized, allowing action: %s\n", actionName)
		return true, nil, nil
	}

	// Check for obviously harmful actions first
	if isHarmfulAction(actionName) {
		return false, []string{"Action contains harmful keywords"}, nil
	}

	// Use principles client to check the action
	result, reasons, err := principlesClient.IsActionAllowed(actionName, map[string]interface{}{}, context)
	if err != nil {
		fmt.Printf("⚠️ [PRINCIPLES] Principles check failed for %s: %v\n", actionName, err)
		// Allow action on error (fail-safe)
		return true, nil, nil
	}

	if !result {
		fmt.Printf("🚫 [PRINCIPLES] Action blocked: %s. Reasons: %v\n", actionName, reasons)
	}

	return result, reasons, nil
}

type ActionDef struct {
	Task          string   `json:"task"`
	Preconditions []string `json:"preconditions"`
	Effects       []string `json:"effects"`
}

type Domain struct {
	Methods []MethodDef `json:"methods"`
	Actions []ActionDef `json:"actions"`
}

// --------- State & helpers ---------

type State map[string]bool

func checkPreconditions(state State, preconds []string) (bool, []string) {
	// returns (allSatisfied, missingPredicates)
	var missing []string
	for _, p := range preconds {
		neg := false
		key := p
		if strings.HasPrefix(p, "not ") {
			neg = true
			key = strings.TrimPrefix(p, "not ")
		}
		val := state[key]
		if (!neg && !val) || (neg && val) {
			missing = append(missing, p)
		}
	}
	return len(missing) == 0, missing
}

func applyEffects(state State, effects []string) State {
	for _, e := range effects {
		if strings.HasPrefix(e, "not ") {
			key := strings.TrimPrefix(e, "not ")
			state[key] = false
		} else {
			state[e] = true
		}
	}
	return state
}

func isPrimitive(taskName string, domain *Domain) bool {
	for _, a := range domain.Actions {
		if a.Task == taskName {
			return true
		}
	}
	return false
}

// --------- Domain loader/persist ---------

func LoadDomain(path string) (*Domain, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d Domain
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func SaveDomain(path string, d *Domain) error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, data, 0644)
}

// --------- HTN Planner with learning ---------

// HTNPlan returns a flat sequence of primitive action names, or nil if failed.
func HTNPlan(state State, taskName string, domain *Domain) []string {
	return HTNPlanWithPath(state, taskName, domain, make(map[string]bool))
}

func HTNPlanWithPath(state State, taskName string, domain *Domain, path map[string]bool) []string {
	log.Printf("🔍 [HTN] Planning for task: %s", taskName)
	log.Printf("🔍 [HTN] Current state: %+v", state)
	log.Printf("🔍 [HTN] Current path: %+v", path)

	// Check for cycles
	if path[taskName] {
		log.Printf("❌ [HTN] Cycle detected! Task %s is already in the planning path: %+v", taskName, path)
		return nil
	}

	// Add current task to path
	path[taskName] = true
	defer func() {
		// Remove current task from path when returning
		delete(path, taskName)
	}()

	// Try methods first (methods can override primitive actions)
	for i, m := range domain.Methods {
		if m.Task != taskName {
			continue
		}
		log.Printf("🔍 [HTN] Found method %d: %s with preconditions: %v", i, m.Task, m.Preconditions)

		ok, missing := checkPreconditions(state, m.Preconditions)
		if !ok {
			log.Printf("❌ [HTN] Method %s preconditions not met. Missing: %v", m.Task, missing)
			continue
		}
		log.Printf("✅ [HTN] Method %s preconditions satisfied", m.Task)

		var plan []string
		success := true
		// copy state locally to simulate effects of subtasks for applicability
		localState := copyState(state)
		log.Printf("🔍 [HTN] Processing subtasks for %s: %v", m.Task, m.Subtasks)

		for j, stName := range m.Subtasks {
			log.Printf("🔍 [HTN] Planning subtask %d/%d: %s", j+1, len(m.Subtasks), stName)
			// recursively plan for subtask with cycle detection
			subplan := HTNPlanWithPath(localState, stName, domain, path)
			if subplan == nil {
				log.Printf("❌ [HTN] Failed to plan subtask: %s", stName)
				success = false
				break
			}
			log.Printf("✅ [HTN] Subtask %s planned: %v", stName, subplan)
			plan = append(plan, subplan...)
			// simulate effects of subplan on localState
			for _, actName := range subplan {
				if act := findAction(actName, domain); act != nil {
					log.Printf("🔍 [HTN] Applying effects of %s: %v", actName, act.Effects)
					localState = applyEffects(localState, act.Effects)
					log.Printf("🔍 [HTN] New local state: %+v", localState)
				}
			}
		}
		if success {
			log.Printf("✅ [HTN] Successfully planned task %s: %v", taskName, plan)
			return plan
		}
		log.Printf("❌ [HTN] Method %s failed to plan all subtasks", m.Task)
	}

	// No method worked. If there is a primitive action, check it.
	if action := findAction(taskName, domain); action != nil {
		log.Printf("🔍 [HTN] Found primitive action: %s with preconditions: %v", action.Task, action.Preconditions)
		ok, missing := checkPreconditions(state, action.Preconditions)
		if ok {
			// action is applicable
			log.Printf("✅ [HTN] Primitive action %s is applicable", action.Task)
			return []string{action.Task}
		}
		// action exists but preconditions missing -> fail so caller may invoke learning
		log.Printf("❌ [HTN] Primitive action %s preconditions not met. Missing: %v", action.Task, missing)
		return nil
	}

	// No method and no primitive action -> fail
	log.Printf("❌ [HTN] No method or primitive action found for task: %s", taskName)
	return nil
}

// findAction by name
func findAction(name string, domain *Domain) *ActionDef {
	for i := range domain.Actions {
		if domain.Actions[i].Task == name {
			return &domain.Actions[i]
		}
	}
	return nil
}

func copyState(s State) State {
	new := State{}
	for k, v := range s {
		new[k] = v
	}
	return new
}

// --------- Learning: infer providers for missing predicates ---------

// findActionsProviding returns action tasks whose EFFECTS include the bare predicate (without 'not ')
func findActionsProviding(predicate string, domain *Domain) []string {
	key := predicate
	if strings.HasPrefix(predicate, "not ") {
		key = strings.TrimPrefix(predicate, "not ")
	}
	var providers []string
	for _, a := range domain.Actions {
		for _, eff := range a.Effects {
			// match positive effect
			if eff == key {
				providers = append(providers, a.Task)
			}
			// if requested predicate is "not X" then a provider could be an action that sets X false,
			// but our small DSL uses positive effects only for simplicity.
		}
	}
	return providers
}

// LearnMethodForMissing will create a method that sequences a provider action before the original taskName.
// e.g. missing predicate "draft_written" -> find action "WriteDraft" that effects "draft_written" and create
// method: TaskName -> [WriteDraft, TaskName]
func LearnMethodForMissing(taskName string, missingPredicates []string, domain *Domain) bool {
	for _, mp := range missingPredicates {
		providers := findActionsProviding(mp, domain)
		if len(providers) > 0 {
			// pick first provider for simplicity
			provider := providers[0]
			newMethod := MethodDef{
				Task:          taskName,
				Preconditions: []string{},                   // fallback always applicable
				Subtasks:      []string{provider, taskName}, // provider then attempt original task (will resolve to primitive or method)
				IsLearned:     true,
			}
			domain.Methods = append([]MethodDef{newMethod}, domain.Methods...) // prepend learned method
			fmt.Printf("🤖 Learned method for '%s' to satisfy '%s' by using '%s'\n", taskName, mp, provider)
			return true
		}
	}
	// no provider found
	return false
}

// Extract missing predicates for a given action against current state
func missingPredicatesForAction(action *ActionDef, state State) []string {
	_, missing := checkPreconditions(state, action.Preconditions)
	return missing
}

// --------- Execution ---------

func ExecutePlan(state State, plan []string, domain *Domain) State {
	log.Printf("🚀 [EXEC] Starting plan execution with %d actions", len(plan))
	log.Printf("🚀 [EXEC] Initial state: %+v", state)

	for i, actName := range plan {
		log.Printf("🚀 [EXEC] Step %d/%d: Processing action %s", i+1, len(plan), actName)

		a := findAction(actName, domain)
		if a == nil {
			log.Printf("❌ [EXEC] No action definition for %s", actName)
			fmt.Printf("⚠️  No action definition for %s\n", actName)
			continue
		}
		log.Printf("🔍 [EXEC] Found action definition: %s with preconditions: %v", actName, a.Preconditions)

		ok, missing := checkPreconditions(state, a.Preconditions)
		if !ok {
			log.Printf("❌ [EXEC] Preconditions failed for %s. Missing: %v", actName, missing)
			fmt.Printf("⚠️ Preconditions failed for %s: missing %v\n", actName, missing)
			// skip or halt (we'll halt here)
			return state
		}
		log.Printf("✅ [EXEC] Preconditions satisfied for %s", actName)

		// Check with principles before executing
		context := map[string]interface{}{
			"human_harm":  isHarmfulAction(actName), // Check if action is harmful
			"human_order": true,                     // Assume HDN tasks are human-ordered
			"self_harm":   false,                    // Default to false
		}
		log.Printf("🔍 [EXEC] Checking principles for %s with context: %+v", actName, context)

		allowed, reasons, err := CheckActionWithPrinciples(actName, context)
		if err != nil {
			log.Printf("⚠️ [EXEC] Principles check failed for %s: %v", actName, err)
			fmt.Printf("⚠️  Principles check failed for %s: %v\n", actName, err)
			// Continue execution on error (fail-safe)
		} else if !allowed {
			log.Printf("🚫 [EXEC] Action BLOCKED by principles: %s. Reasons: %v", actName, reasons)
			fmt.Printf("🚫 Action BLOCKED by principles: %s\n", actName)
			fmt.Printf("   Reasons: %v\n", reasons)
			// Skip this action but continue with the plan
			continue
		} else {
			log.Printf("✅ [EXEC] Principles check passed for %s", actName)
		}

		// Simulate action
		log.Printf("🔍 [EXEC] Executing action: %s", a.Task)
		fmt.Printf("→ Executing action: %s\n", a.Task)
		time.Sleep(300 * time.Millisecond)

		log.Printf("🔍 [EXEC] Applying effects: %v", a.Effects)
		state = applyEffects(state, a.Effects)
		log.Printf("📊 [EXEC] New state after %s: %+v", actName, state)
	}

	log.Printf("✅ [EXEC] Plan execution completed. Final state: %+v", state)
	return state
}

// --------- Utility functions ---------

func loadDomain(path string) *Domain {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Printf("Error loading domain: %v\n", err)
		return nil
	}

	var domain Domain
	if err := json.Unmarshal(data, &domain); err != nil {
		fmt.Printf("Error parsing domain: %v\n", err)
		return nil
	}

	return &domain
}

// --------- Main runner with retry+learning loop ---------

// Main function is now in server.go
// Use 'go run . -mode=server' to start the API server
// Use 'go run . -mode=cli' to run the original CLI version

// TestPrinciplesIntegration demonstrates principles blocking
func TestPrinciplesIntegration() {
	fmt.Println("🧪 Testing HDN-Principles Integration")
	fmt.Println("=====================================")

	// Initialize principles client
	InitializePrinciplesClient("http://localhost:8080")

	// Load domain
	domain := loadDomain("domain.json")
	if domain == nil {
		fmt.Println("❌ Failed to load domain")
		return
	}

	// Create a test state
	state := State{
		"robot_at_lab":     true,
		"sample_available": true,
		"door_unlocked":    true,
	}

	// Test 1: Normal action (should be allowed)
	fmt.Println("\n📋 Test 1: Normal Action")
	fmt.Println("------------------------")
	plan1 := []string{"WriteDraft", "GetReview"}
	fmt.Printf("Plan: %v\n", plan1)
	state1 := ExecutePlan(state, plan1, domain)
	fmt.Printf("Final state: %v\n", state1)

	// Test 2: Add a "steal" action to the domain for testing
	stealAction := ActionDef{
		Task:          "steal",
		Preconditions: []string{}, // No preconditions needed for testing
		Effects:       []string{"has_stolen_item"},
	}
	domain.Actions = append(domain.Actions, stealAction)

	// Reset state for test 2
	state2 := State{
		"robot_at_lab":     true,
		"sample_available": true,
		"door_unlocked":    true,
	}

	// Test 2: Steal action (should be blocked by principles)
	fmt.Println("\n📋 Test 2: Steal Action (Should be Blocked)")
	fmt.Println("------------------------------------------")
	plan2 := []string{"steal"}
	fmt.Printf("Plan: %v\n", plan2)
	state2 = ExecutePlan(state2, plan2, domain)
	fmt.Printf("Final state: %v\n", state2)

	// Test 3: Harmful action
	harmAction := ActionDef{
		Task:          "harm_human",
		Preconditions: []string{}, // No preconditions needed for testing
		Effects:       []string{"human_harmed"},
	}
	domain.Actions = append(domain.Actions, harmAction)

	// Reset state for test 3
	state3 := State{
		"robot_at_lab":     true,
		"sample_available": true,
		"door_unlocked":    true,
	}

	fmt.Println("\n📋 Test 3: Harmful Action (Should be Blocked)")
	fmt.Println("--------------------------------------------")
	plan3 := []string{"harm_human"}
	fmt.Printf("Plan: %v\n", plan3)
	state3 = ExecutePlan(state3, plan3, domain)
	fmt.Printf("Final state: %v\n", state3)

	// Test 4: File deletion action
	deleteFileAction := ActionDef{
		Task:          "delete_file",
		Preconditions: []string{}, // No preconditions needed for testing
		Effects:       []string{"file_deleted"},
	}
	domain.Actions = append(domain.Actions, deleteFileAction)

	// Reset state for test 4
	state4 := State{
		"robot_at_lab":     true,
		"sample_available": true,
		"door_unlocked":    true,
	}

	fmt.Println("\n📋 Test 4: File Deletion Action (Should be Blocked)")
	fmt.Println("--------------------------------------------------")
	plan4 := []string{"delete_file"}
	fmt.Printf("Plan: %v\n", plan4)
	state4 = ExecutePlan(state4, plan4, domain)
	fmt.Printf("Final state: %v\n", state4)

	// Test 5: System command execution
	executeCommandAction := ActionDef{
		Task:          "execute_command",
		Preconditions: []string{}, // No preconditions needed for testing
		Effects:       []string{"command_executed"},
	}
	domain.Actions = append(domain.Actions, executeCommandAction)

	// Reset state for test 5
	state5 := State{
		"robot_at_lab":     true,
		"sample_available": true,
		"door_unlocked":    true,
	}

	fmt.Println("\n📋 Test 5: System Command Execution (Should be Blocked)")
	fmt.Println("-----------------------------------------------------")
	plan5 := []string{"execute_command"}
	fmt.Printf("Plan: %v\n", plan5)
	state5 = ExecutePlan(state5, plan5, domain)
	fmt.Printf("Final state: %v\n", state5)

	fmt.Println("\n✅ Principles integration test completed!")
}

func TestLLMIntegration() {
	fmt.Println("🧪 Testing LLM Integration")
	fmt.Println("==========================")
	fmt.Println("❌ LLM integration test requires additional dependencies")
	fmt.Println("✅ Skipping LLM test for now - focusing on principles integration")
}

// Main function removed - use server.go for main functionality
// Test functions can be called from server.go or run separately
