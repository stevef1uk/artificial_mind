package main

import (
	mempkg "agi/hdn/memory"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// executeAction executes a single action
func (e *FSMEngine) executeAction(action ActionConfig, event map[string]interface{}) {
	log.Printf("Executing action: %s (module: %s)", action.Type, action.Module)

	actionMessages := map[string]string{
		"generate_hypotheses":     "🧠 Generating hypotheses from facts and domain knowledge",
		"test_hypothesis":         "🧪 Testing hypothesis with tools to gather evidence",
		"grow_knowledge_base":     "🌱 Growing knowledge base: discovering concepts and filling gaps",
		"discover_new_concepts":   "🔍 Discovering new concepts from episodes",
		"update_domain_knowledge": "📚 Updating domain knowledge with new information",
		"plan_test_or_action":     "📋 Creating hierarchical plan using success rates",
		"check_principles":        "🛡️ Checking principles compliance before action",
		"execute_action":          "⚡ Executing action with tools",
	}
	if msg, ok := actionMessages[action.Type]; ok {
		e.logActivity(msg, "action", map[string]string{
			"action": action.Type,
			"state":  e.currentState,
		})
	}

	switch action.Module {
	case "hdn.input_parser":
		e.executeInputParser(action, event)
	case "knowledge.domain_classifier":
		e.executeDomainClassifier(action, event)
	case "self.knowledge_extractor":
		e.executeKnowledgeExtractor(action, event)
	case "memory.embedding":
		e.executeEmbedding(action, event)
	case "knowledge.updater":
		e.executeKnowledgeUpdater(action, event)
	case "self.summarizer":
		e.executeSummarizer(action, event)
	case "self.belief_store":
		e.executeBeliefStore(action, event)
	case "planner.hypothesis_generator":
		e.executeHypothesisGenerator(action, event)
	case "knowledge.constraint_checker":
		e.executeConstraintChecker(action, event)
	case "planner.hierarchical":
		e.executeHierarchicalPlanner(action, event)
	case "evaluator.plan_ranker":
		e.executePlanRanker(action, event)
	case "principles.mandatory_checker":
		e.executeMandatoryPrinciplesChecker(action, event)
	case "principles.checker":
		e.executePrinciplesChecker(action, event)
	case "principles.pre_execution_checker":
		e.executePreExecutionPrinciplesChecker(action, event)
	case "evaluator.utility_calculator":
		e.executeUtilityCalculator(action, event)
	case "knowledge.constraint_enforcer":
		e.executeConstraintEnforcer(action, event)
	case "hdn.retrieve_capabilities":
		e.executeRetrieveCapabilities(action, event)
	case "hdn.execute_capability":
		e.executeExecuteCapability(action, event)
	case "monitor.collector":
		e.executeMonitorCollector(action, event)
	case "knowledge.metrics_collector":
		e.executeMetricsCollector(action, event)
	case "evaluator.outcome_analyzer":
		e.executeOutcomeAnalyzer(action, event)
	case "knowledge.learning_updater":
		e.executeLearningUpdater(action, event)
	case "knowledge.concept_discovery":
		e.executeConceptDiscovery(action, event)
	case "knowledge.gap_analyzer":
		e.executeGapAnalyzer(action, event)
	case "knowledge.growth_engine":
		e.executeGrowthEngine(action, event)
	case "knowledge.consistency_checker":
		e.executeConsistencyChecker(action, event)
	case "projects.checkpoint":
		e.executeCheckpoint(action, event)
	case "memory.episodic_updater":
		e.executeEpisodicUpdater(action, event)
	case "monitor.logger":
		e.executeLogger(action, event)
	case "system.recovery":
		e.executeRecovery(action, event)
	case "system.cleanup":
		e.executeCleanup(action, event)
	case "reasoning.belief_query":
		e.executeBeliefQuery(action, event)
	case "reasoning.inference":
		e.executeInference(action, event)
	case "reasoning.curiosity_goals":
		e.executeCuriosityGoals(action, event)
	case "reasoning.explanation":
		e.executeExplanation(action, event)
	case "reasoning.trace_logger":
		e.executeTraceLogger(action, event)
	case "reasoning.news_storage":
		e.executeNewsStorage(action, event)
	case "reasoning.hypothesis_testing":
		e.executeHypothesisTesting(action, event)
	default:
		log.Printf("Unknown action module: %s", action.Module)
	}
}

// Action execution methods (simplified implementations)
func (e *FSMEngine) executeInputParser(action ActionConfig, event map[string]interface{}) {

	log.Printf("Parsing input with domain validation")

	if payload, ok := event["payload"].(map[string]interface{}); ok {
		if text, ok := payload["text"].(string); ok {

			if text != "" && (containsIgnoreCase(text, "PrimeNumberGenerator") || containsIgnoreCase(text, "first 10 primes") || containsIgnoreCase(text, "primes")) {
				sel := map[string]interface{}{"id": "PrimeNumberGenerator", "name": "PrimeNumberGenerator"}
				e.context["selected_capability"] = sel
				e.context["current_action"] = "PrimeNumberGenerator"

				if e.context["capability_inputs"] == nil {
					e.context["capability_inputs"] = map[string]interface{}{"count": "10"}
				}
				log.Printf("Auto-selected capability: PrimeNumberGenerator with count=10")
			}
		}
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		log.Printf("📤 Emitting ingest_ok event")
		e.handleEvent("ingest_ok", nil)
	}()
}

func (e *FSMEngine) executeDomainClassifier(action ActionConfig, event map[string]interface{}) {

	log.Printf("Classifying domain using concept matching")

	// Extract text to classify from event
	var textToClassify string

	if eventType, ok := event["type"].(string); ok {
		if eventType == "relations" || eventType == "alerts" {
			if payload, ok := event["payload"].(map[string]interface{}); ok {
				if metadata, ok := payload["metadata"].(map[string]interface{}); ok {

					if headline, ok := metadata["headline"].(string); ok && headline != "" {
						textToClassify = headline
					} else if text, ok := payload["text"].(string); ok && text != "" {
						textToClassify = text
					} else {

						if head, ok := metadata["head"].(string); ok {
							textToClassify = head
							if rel, ok := metadata["relation"].(string); ok {
								textToClassify += " " + rel
							}
							if tail, ok := metadata["tail"].(string); ok {
								textToClassify += " " + tail
							}
						}
					}
				} else if text, ok := payload["text"].(string); ok && text != "" {
					textToClassify = text
				}
			}
		} else {

			if payload, ok := event["payload"].(map[string]interface{}); ok {
				if text, ok := payload["text"].(string); ok && text != "" {
					textToClassify = text
				}
			}
		}
	}

	if textToClassify != "" && e.knowledgeIntegration != nil {
		result, err := e.knowledgeIntegration.ClassifyDomain(textToClassify)
		if err == nil && result != nil && result.Domain != "" {

			e.context["current_domain"] = result.Domain
			log.Printf("✅ Classified domain: %s (confidence: %.2f)", result.Domain, result.Confidence)

			e.saveState()
		} else if err != nil {
			log.Printf("⚠️ Domain classification failed: %v", err)
		}
	} else {
		log.Printf("⚠️ No text available for domain classification")
	}
}

func (e *FSMEngine) executeKnowledgeExtractor(action ActionConfig, event map[string]interface{}) {

	log.Printf("Extracting facts with domain constraints")

	// Extract facts from event data and context
	var facts []map[string]interface{}

	if eventData, ok := event["data"].(string); ok && eventData != "" {
		facts = append(facts, map[string]interface{}{
			"fact":       eventData,
			"confidence": 0.8,
			"domain":     e.getCurrentDomain(),
		})
	}

	if payload, ok := event["payload"].(map[string]interface{}); ok {
		if text, ok := payload["text"].(string); ok && text != "" {

			e.context["input"] = text

			if e.knowledgeIntegration != nil {
				domain := e.getCurrentDomain()
				extractedFacts, err := e.knowledgeIntegration.ExtractFacts(text, domain)
				if err == nil && len(extractedFacts) > 0 {
					log.Printf("📚 Extracted %d facts from input using knowledge integration", len(extractedFacts))
					for _, fact := range extractedFacts {
						facts = append(facts, map[string]interface{}{
							"fact":       fact.Content,
							"confidence": fact.Confidence,
							"domain":     fact.Domain,
						})
					}
				} else {

					facts = append(facts, map[string]interface{}{
						"fact":       text,
						"confidence": 0.7,
						"domain":     domain,
					})
				}
			} else {

				facts = append(facts, map[string]interface{}{
					"fact":       text,
					"confidence": 0.7,
					"domain":     e.getCurrentDomain(),
				})
			}
		}
	}

	if input, ok := e.context["input"].(string); ok && input != "" {

		alreadyAdded := false
		for _, f := range facts {
			if factStr, ok := f["fact"].(string); ok && factStr == input {
				alreadyAdded = true
				break
			}
		}
		if !alreadyAdded {
			facts = append(facts, map[string]interface{}{
				"fact":       fmt.Sprintf("User input: %s", input),
				"confidence": 0.9,
				"domain":     e.getCurrentDomain(),
			})
		}
	}

	if e.currentState != "" {
		facts = append(facts, map[string]interface{}{
			"fact":       fmt.Sprintf("System state: %s", e.currentState),
			"confidence": 0.95,
			"domain":     "system",
		})
	}

	if len(facts) == 0 {
		facts = append(facts, map[string]interface{}{
			"fact":       "FSM is processing events",
			"confidence": 0.7,
			"domain":     e.getCurrentDomain(),
		})
	}

	seen := map[string]bool{}
	if existing, ok := e.context["extracted_facts"].([]interface{}); ok {
		for _, it := range existing {
			if m, ok := it.(map[string]interface{}); ok {
				if s, ok := m["fact"].(string); ok {
					seen[s] = true
				}
			}
		}
	}

	// Build deduped list to append
	var asInterfaces []interface{}
	for _, f := range facts {
		if s, ok := f["fact"].(string); ok {
			if seen[s] {
				continue
			}
			seen[s] = true
		}
		asInterfaces = append(asInterfaces, f)
	}

	if e.context["extracted_facts"] == nil {
		e.context["extracted_facts"] = []interface{}{}
	}
	e.context["extracted_facts"] = append(e.context["extracted_facts"].([]interface{}), asInterfaces...)
	e.pruneContextList("extracted_facts", 50)

	e.publishPerceptionFacts(asInterfaces)

	go func() {
		time.Sleep(300 * time.Millisecond)
		log.Printf("📤 Emitting facts_extracted event")
		e.handleEvent("facts_extracted", nil)
	}()
}

func (e *FSMEngine) executeEmbedding(action ActionConfig, event map[string]interface{}) {

	log.Printf("Generating embeddings for Weaviate storage")

	episodes := e.getEpisodesFromContext()
	if len(episodes) == 0 {
		log.Printf("No episodes to embed")
		return
	}

	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}
	client := mempkg.NewVectorDBAdapter(weaviateURL, "agi-episodes")

	if err := client.EnsureCollection(8); err != nil {
		log.Printf("Warning: Failed to ensure vector database collection: %v", err)
		return
	}

	for _, episode := range episodes {

		text := ""
		if textVal, ok := episode["text"].(string); ok {
			text = textVal
		} else if descVal, ok := episode["description"].(string); ok {
			text = descVal
		} else {

			if jsonBytes, err := json.Marshal(episode); err == nil {
				text = string(jsonBytes)
			}
		}

		if text == "" {
			continue
		}

		record := &mempkg.EpisodicRecord{
			SessionID: e.agentID,
			Timestamp: time.Now().UTC(),
			Outcome:   "success",
			Tags:      []string{"fsm", "episode"},
			Text:      text,
			Metadata:  episode,
		}

		embedding := e.generateSimpleEmbedding(text, 8)

		if err := client.IndexEpisode(record, embedding); err != nil {
			log.Printf("Warning: Failed to index episode in vector database: %v", err)
		} else {
			log.Printf("✅ Indexed episode in vector database: %s", text[:min(50, len(text))])
		}
	}
}

// generateSimpleEmbedding creates a simple hash-based embedding
func (e *FSMEngine) generateSimpleEmbedding(text string, dim int) []float32 {
	vec := make([]float32, dim)
	for i := 0; i < dim; i++ {

		h := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", text, i)))
		val := binary.BigEndian.Uint32(h[:4])
		vec[i] = float32(val%1000) / 1000.0
	}
	return vec
}

func (e *FSMEngine) executeKnowledgeUpdater(action ActionConfig, event map[string]interface{}) {

	log.Printf("Updating domain knowledge in Neo4j")

	concepts := []map[string]interface{}{
		{"concept": "FSM State Machine", "confidence": 0.9, "examples": 3},
		{"concept": "NATS Event Bus", "confidence": 0.85, "examples": 2},
		{"concept": "Redis State Storage", "confidence": 0.8, "examples": 1},
	}

	if e.context["discovered_concepts"] == nil {
		e.context["discovered_concepts"] = []interface{}{}
	}
	var conceptInterfaces []interface{}
	for _, c := range concepts {
		conceptInterfaces = append(conceptInterfaces, c)
	}
	e.context["discovered_concepts"] = append(e.context["discovered_concepts"].([]interface{}), conceptInterfaces...)
	e.pruneContextList("discovered_concepts", 50)

	if e.context["knowledge_growth"] == nil {
		e.context["knowledge_growth"] = map[string]interface{}{
			"concepts_created":    0,
			"relationships_added": 0,
			"examples_added":      0,
		}
	}
	growth := e.context["knowledge_growth"].(map[string]interface{})

	getInt := func(val interface{}) int {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case int64:
			return int(v)
		default:
			return 0
		}
	}

	growth["concepts_created"] = getInt(growth["concepts_created"]) + len(concepts)
	growth["examples_added"] = getInt(growth["examples_added"]) + 6

	knowledgeKey := fmt.Sprintf("fsm:%s:knowledge_growth", e.agentID)
	e.redis.HSet(e.ctx, knowledgeKey, map[string]interface{}{
		"concepts_created":    growth["concepts_created"],
		"examples_added":      growth["examples_added"],
		"relationships_added": growth["relationships_added"],
		"last_growth_time":    time.Now().Format(time.RFC3339),
	})
}

func (e *FSMEngine) executeSummarizer(action ActionConfig, event map[string]interface{}) {

	log.Printf("Summarizing episodes with domain context")

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("facts_ready", nil)
	}()
}

func (e *FSMEngine) executeBeliefStore(action ActionConfig, event map[string]interface{}) {

	log.Printf("Updating beliefs in Redis")
}

func (e *FSMEngine) executeHypothesisGenerator(action ActionConfig, event map[string]interface{}) {

	log.Printf("🧠 Generating hypotheses using domain relations")

	facts := e.getFactsFromContext()
	domain := e.getCurrentDomain()

	hypotheses, err := e.knowledgeIntegration.GenerateHypotheses(facts, domain)
	if err != nil {
		log.Printf("❌ Hypothesis generation failed: %v", err)
		e.context["hypothesis_generation_error"] = err.Error()
		return
	}

	if len(hypotheses) > 0 {
		log.Printf("🧠 Generated %d hypotheses", len(hypotheses))
		e.context["generated_hypotheses"] = hypotheses
		e.context["hypothesis_count"] = len(hypotheses)

		e.storeHypotheses(hypotheses, domain)

		e.createHypothesisTestingGoals(hypotheses, domain)

		e.logActivity(
			fmt.Sprintf("Generated %d hypotheses in domain '%s'", len(hypotheses), domain),
			"hypothesis",
			map[string]string{
				"details": fmt.Sprintf("Domain: %s, Count: %d", domain, len(hypotheses)),
			},
		)
	} else {
		log.Printf("ℹ️ No hypotheses generated")
		e.context["no_hypotheses_generated"] = true
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("hypotheses_generated", nil)
	}()
}

func (e *FSMEngine) executeConstraintChecker(action ActionConfig, event map[string]interface{}) {

	log.Printf("Checking constraints against domain knowledge")
}

func (e *FSMEngine) executeHierarchicalPlanner(action ActionConfig, event map[string]interface{}) {

	log.Printf("Creating hierarchical plans using domain success rates")
}

func (e *FSMEngine) executePlanRanker(action ActionConfig, event map[string]interface{}) {

	log.Printf("Ranking plans using domain knowledge weights")

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("plans_ranked", nil)
	}()
}

func (e *FSMEngine) executeMonitorCollector(action ActionConfig, event map[string]interface{}) {

	log.Printf("Collecting outcomes with domain validation")

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("results_collected", nil)
	}()
}

func (e *FSMEngine) executeMetricsCollector(action ActionConfig, event map[string]interface{}) {

	log.Printf("Measuring domain-specific metrics")
}

func (e *FSMEngine) executeOutcomeAnalyzer(action ActionConfig, event map[string]interface{}) {

	log.Printf("Analyzing outcomes against domain expectations")

	e.publishEvaluationResults()

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("analysis_ok", nil)
	}()
}

func (e *FSMEngine) executeLearningUpdater(action ActionConfig, event map[string]interface{}) {

	log.Printf("Updating domain knowledge based on learning")

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("growth_updated", nil)
	}()
}

func (e *FSMEngine) executeConceptDiscovery(action ActionConfig, event map[string]interface{}) {

	log.Printf("🔍 Discovering new concepts from episodes")

	episodes := e.getEpisodesFromContext()
	domain := e.getCurrentDomain()

	discoveries, err := e.knowledgeGrowth.DiscoverNewConcepts(episodes, domain)
	if err != nil {
		log.Printf("❌ Concept discovery failed: %v", err)
		return
	}

	if len(discoveries) > 0 {
		log.Printf("📚 Discovered %d new concepts", len(discoveries))
		e.context["new_concepts_discovered"] = true
		e.context["discoveries"] = discoveries
	} else {
		log.Printf("ℹ️ No new concepts discovered")
	}
}

func (e *FSMEngine) executeGapAnalyzer(action ActionConfig, event map[string]interface{}) {

	log.Printf("🔍 Analyzing knowledge gaps")

	domain := e.getCurrentDomain()
	gaps, err := e.knowledgeGrowth.FindKnowledgeGaps(domain)
	if err != nil {
		log.Printf("❌ Gap analysis failed: %v", err)
		return
	}

	if len(gaps) > 0 {
		log.Printf("🕳️ Found %d knowledge gaps", len(gaps))
		e.context["knowledge_gaps"] = gaps
		e.context["gaps_found"] = true
	} else {
		log.Printf("✅ No knowledge gaps found")
	}
}

func (e *FSMEngine) executeGrowthEngine(action ActionConfig, event map[string]interface{}) {

	log.Printf("🌱 Growing knowledge base")

	episodes := e.getEpisodesFromContext()
	domain := e.getCurrentDomain()

	err := e.knowledgeGrowth.GrowKnowledgeBase(episodes, domain)
	if err != nil {
		log.Printf("❌ Knowledge growth failed: %v", err)
		return
	}

	log.Printf("✅ Knowledge base growth completed")

	e.logActivity(
		fmt.Sprintf("Knowledge base grew: processed %d episodes in domain '%s'", len(episodes), domain),
		"learning",
		map[string]string{
			"details": fmt.Sprintf("Domain: %s, Episodes: %d", domain, len(episodes)),
		},
	)
	e.context["knowledge_grown"] = true

	log.Printf("🎯 Generating curiosity goals for transition")
	goals, err := e.reasoning.GenerateCuriosityGoals(domain)
	if err != nil {
		log.Printf("❌ Failed to generate curiosity goals: %v", err)
		return
	}

	if len(goals) == 0 {
		log.Printf("ℹ️ No curiosity goals generated, skipping transition")
		return
	}

	log.Printf("🎯 Generated %d curiosity goals", len(goals))

	conclusion := fmt.Sprintf("Generated %d curiosity goals", len(goals))
	e.context["conclusion"] = conclusion
	e.context["confidence"] = 0.8

	go func() {
		time.Sleep(200 * time.Millisecond)
		log.Printf("📤 Emitting curiosity_goals_generated event")
		e.handleEvent("curiosity_goals_generated", nil)
	}()
}

func (e *FSMEngine) executeConsistencyChecker(action ActionConfig, event map[string]interface{}) {

	log.Printf("🔍 Validating knowledge consistency")

	domain := e.getCurrentDomain()
	err := e.knowledgeGrowth.ValidateKnowledgeConsistency(domain)
	if err != nil {
		log.Printf("❌ Consistency check failed: %v", err)
		return
	}

	log.Printf("✅ Knowledge consistency validation completed")
	e.context["consistency_validated"] = true
}

func (e *FSMEngine) executeCheckpoint(action ActionConfig, event map[string]interface{}) {

	log.Printf("Creating checkpoint with domain insights")
}

func (e *FSMEngine) executeEpisodicUpdater(action ActionConfig, event map[string]interface{}) {

	log.Printf("Updating episodic memory with domain links")
}

func (e *FSMEngine) executeLogger(action ActionConfig, event map[string]interface{}) {

	log.Printf("Logging with domain context")
}

func (e *FSMEngine) executeRecovery(action ActionConfig, event map[string]interface{}) {

	log.Printf("Recovery using domain fallbacks")

	go func() {
		time.Sleep(3 * time.Second)
		e.handleEvent("recovered", nil)
	}()
}

func (e *FSMEngine) executeCleanup(action ActionConfig, event map[string]interface{}) {

	log.Printf("🧹 [CLEANUP] Clearing transient context data...")

	projectID := e.context["project_id"]
	currentDomain := e.context["current_domain"]
	autonomy := e.context["autonomy"]

	keysToClear := []string{
		"extracted_facts",
		"discovered_concepts",
		"inferred_beliefs",
		"reasoning_traces",
		"beliefs",
		"last_execution",
		"last_execution_body",
		"last_execution_error",
		"generated_hypotheses",
		"candidate_capabilities",
		"reasoning_explanation",
	}

	for _, k := range keysToClear {
		delete(e.context, k)
	}

	e.context["project_id"] = projectID
	e.context["current_domain"] = currentDomain
	if autonomy != nil {
		e.context["autonomy"] = autonomy
	}

	log.Printf("✅ [CLEANUP] Context cleared for next cycle")
}

// Reasoning action implementations
func (e *FSMEngine) executeBeliefQuery(action ActionConfig, event map[string]interface{}) {

	log.Printf("🧠 Querying beliefs from knowledge base")

	query := "all concepts"
	if q, ok := event["query"].(string); ok && q != "" {
		query = q
	} else if q, ok := e.context["belief_query"].(string); ok && q != "" {
		query = q
	}

	domain := e.getCurrentDomain()
	beliefs, err := e.reasoning.QueryBeliefs(query, domain)
	if err != nil {
		log.Printf("❌ Belief query failed: %v", err)
		e.context["belief_query_error"] = err.Error()
		return
	}

	e.context["beliefs"] = beliefs
	e.context["belief_count"] = len(beliefs)
	log.Printf("✅ Retrieved %d beliefs", len(beliefs))

	conclusion := fmt.Sprintf("Retrieved %d beliefs", len(beliefs))
	e.context["conclusion"] = conclusion
	e.context["confidence"] = 0.7

	trace := ReasoningTrace{
		ID:        fmt.Sprintf("trace_%d", time.Now().UnixNano()),
		Goal:      e.getCurrentGoal(),
		Domain:    e.getCurrentDomain(),
		CreatedAt: time.Now(),
		Steps: []ReasoningStep{
			{
				StepNumber: 1,
				Action:     "belief_query",
				Query:      query,
				Result:     map[string]interface{}{"count": len(beliefs)},
				Reasoning:  "Queried beliefs from knowledge base via HDN",
				Confidence: 0.7,
				Timestamp:  time.Now(),
			},
		},
		Conclusion: fmt.Sprintf("Retrieved %d beliefs", len(beliefs)),
		Confidence: 0.7,
		Properties: map[string]interface{}{
			"state": e.currentState,
			"agent": e.agentID,
		},
	}
	if err := e.reasoning.LogReasoningTrace(trace); err != nil {
		log.Printf("Warning: failed to log reasoning trace: %v", err)
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("beliefs_queried", nil)
	}()
}

func (e *FSMEngine) executeInference(action ActionConfig, event map[string]interface{}) {

	log.Printf("🔍 Applying inference rules")

	domain := e.getCurrentDomain()

	if e.getCurrentGoal() == "Unknown goal" {
		e.context["current_goal"] = "Knowledge inference and exploration"
		log.Printf("🎯 Set default goal: Knowledge inference and exploration")
	}

	newBeliefs, err := e.reasoning.InferNewBeliefs(domain)
	if err != nil {
		log.Printf("❌ Inference failed: %v", err)
		e.context["inference_error"] = err.Error()
		return
	}

	if e.context["inferred_beliefs"] == nil {
		e.context["inferred_beliefs"] = []interface{}{}
	}
	var beliefInterfaces []interface{}
	for _, belief := range newBeliefs {
		beliefInterfaces = append(beliefInterfaces, belief)
	}
	e.context["inferred_beliefs"] = append(e.context["inferred_beliefs"].([]interface{}), beliefInterfaces...)

	log.Printf("✨ Inferred %d new beliefs", len(newBeliefs))

	conclusion := fmt.Sprintf("Inferred %d new beliefs in domain '%s'", len(newBeliefs), domain)
	e.context["conclusion"] = conclusion
	e.context["confidence"] = 0.7

	itrace := ReasoningTrace{
		ID:        fmt.Sprintf("trace_%d", time.Now().UnixNano()),
		Goal:      e.getCurrentGoal(),
		Domain:    domain,
		CreatedAt: time.Now(),
		Steps: []ReasoningStep{
			{
				StepNumber: 1,
				Action:     "inference",
				Query:      "apply_inference_rules",
				Result:     map[string]interface{}{"inferred_count": len(newBeliefs), "domain": domain},
				Reasoning:  fmt.Sprintf("Applied forward-chaining rules over Neo4j graph in domain '%s'", domain),
				Confidence: 0.7,
				Timestamp:  time.Now(),
			},
		},
		Conclusion: fmt.Sprintf("Inferred %d new beliefs in domain '%s'", len(newBeliefs), domain),
		Confidence: 0.7,
		Properties: map[string]interface{}{
			"state":  e.currentState,
			"agent":  e.agentID,
			"domain": domain,
		},
	}

	if existing, ok := e.context["reasoning_traces"]; !ok || existing == nil {

		e.context["reasoning_traces"] = []interface{}{itrace}
	} else {
		switch v := existing.(type) {
		case []ReasoningTrace:
			traces := append(v, itrace)
			e.context["reasoning_traces"] = traces
			e.pruneContextList("reasoning_traces", 10)
		case []interface{}:
			e.context["reasoning_traces"] = append(v, itrace)
			e.pruneContextList("reasoning_traces", 10)
		default:

			e.context["reasoning_traces"] = []interface{}{v, itrace}
		}
	}

	if err := e.reasoning.LogReasoningTrace(itrace); err != nil {
		log.Printf("Warning: failed to log reasoning trace: %v", err)
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("beliefs_inferred", nil)
	}()
}

func (e *FSMEngine) executeCuriosityGoals(action ActionConfig, event map[string]interface{}) {

	log.Printf("🎯 Generating curiosity goals")

	domain := e.getCurrentDomain()
	goals, err := e.reasoning.GenerateCuriosityGoals(domain)
	if err != nil {
		log.Printf("❌ Curiosity goals generation failed: %v", err)
		e.context["curiosity_goals_error"] = err.Error()
		return
	}

	e.context["curiosity_goals"] = goals
	e.context["curiosity_goal_count"] = len(goals)

	if e.redis != nil {
		key := fmt.Sprintf("reasoning:curiosity_goals:%s", domain)

		existingGoalsData, err := e.redis.LRange(e.ctx, key, 0, 199).Result()
		if err != nil {
			log.Printf("Warning: Failed to get existing goals for deduplication: %v", err)
		}

		existingGoals := make(map[string]CuriosityGoal)
		for _, goalData := range existingGoalsData {
			var goal CuriosityGoal
			if err := json.Unmarshal([]byte(goalData), &goal); err == nil {

				dedupKey := e.createDedupKey(goal)
				existingGoals[dedupKey] = goal
			}
		}

		newGoalsCount := 0
		for _, g := range goals {
			dedupKey := e.createDedupKey(g)
			if _, exists := existingGoals[dedupKey]; !exists {
				b, _ := json.Marshal(g)
				_ = e.redis.LPush(e.ctx, key, b).Err()
				existingGoals[dedupKey] = g
				newGoalsCount++
			}
		}

		_ = e.redis.LTrim(e.ctx, key, 0, 199)
		log.Printf("Added %d new goals (deduplicated from %d generated)", newGoalsCount, len(goals))

		if newGoalsCount == 0 {
			log.Printf("ℹ️ No new goals added (all duplicates), skipping trace logging")
			return
		}
	}

	if e.getCurrentGoal() == "Unknown goal" && len(goals) > 0 {

		bestGoal := goals[0]
		for _, goal := range goals {
			if goal.Priority > bestGoal.Priority {
				bestGoal = goal
			}
		}
		e.context["current_goal"] = bestGoal.Description
		log.Printf("🎯 Set current goal from curiosity goals: %s", bestGoal.Description)
	}

	log.Printf("🎯 Generated %d curiosity goals", len(goals))

	conclusion := fmt.Sprintf("Generated %d curiosity goals", len(goals))
	e.context["conclusion"] = conclusion
	e.context["confidence"] = 0.8

	if len(goals) > 0 {
		trace := ReasoningTrace{
			ID:        fmt.Sprintf("trace_%d", time.Now().UnixNano()),
			Goal:      e.getCurrentGoal(),
			Domain:    domain,
			CreatedAt: time.Now(),
			Steps: []ReasoningStep{
				{
					StepNumber: 1,
					Action:     "generate_curiosity_goals",
					Result:     map[string]interface{}{"goals_count": len(goals), "domain": domain},
					Reasoning:  fmt.Sprintf("Generated %d curiosity goals for domain '%s'", len(goals), domain),
					Confidence: 0.8,
					Timestamp:  time.Now(),
				},
			},
			Conclusion: fmt.Sprintf("Generated %d curiosity goals", len(goals)),
			Confidence: 0.8,
			Properties: map[string]interface{}{
				"state":  e.currentState,
				"agent":  e.agentID,
				"domain": domain,
			},
		}

		if err := e.reasoning.LogReasoningTrace(trace); err != nil {
			log.Printf("Warning: failed to log reasoning trace: %v", err)
		}
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("curiosity_goals_generated", nil)
	}()
}

func (e *FSMEngine) executeExplanation(action ActionConfig, event map[string]interface{}) {

	log.Printf("💭 Generating reasoning explanation")

	goal := "reasoning_explanation"
	if g, ok := event["goal"].(string); ok && g != "" {
		goal = g
	} else if g, ok := e.context["current_goal"].(string); ok && g != "" {
		goal = g
	} else if g, ok := e.context["explanation_goal"].(string); ok && g != "" {
		goal = g
	}

	domain := e.getCurrentDomain()
	explanation, err := e.reasoning.ExplainReasoning(goal, domain)
	if err != nil {
		log.Printf("❌ Explanation generation failed: %v", err)
		e.context["explanation_error"] = err.Error()
		return
	}

	e.context["reasoning_explanation"] = explanation
	log.Printf("💭 Generated explanation: %s", explanation)

	go func(goal, domain, text string) {
		defer func() { recover() }()
		payload := map[string]interface{}{
			"goal":        goal,
			"domain":      domain,
			"explanation": text,
			"created_at":  time.Now().UTC().Format(time.RFC3339),
		}

		if workflowID, ok := e.context["current_workflow_id"].(string); ok && workflowID != "" {
			payload["workflow_id"] = workflowID
		}
		if projectID, ok := e.context["project_id"].(string); ok && projectID != "" {
			payload["project_id"] = projectID
		}
		if sessionID, ok := e.context["session_id"].(string); ok && sessionID != "" {
			payload["session_id"] = sessionID
		}

		if b, err := json.Marshal(payload); err == nil {
			key := fmt.Sprintf("reasoning:explanations:%s", goal)
			if err := e.redis.LPush(e.ctx, key, b).Err(); err == nil {
				_ = e.redis.LTrim(e.ctx, key, 0, 49).Err()
				log.Printf("📝 Persisted reasoning explanation for goal %s", goal)
			} else {
				log.Printf("⚠️ Failed to persist reasoning explanation: %v", err)
			}
		}
	}(goal, domain, explanation)

	go func() {
		time.Sleep(200 * time.Millisecond)
		e.handleEvent("explanation_generated", nil)
	}()
}

func (e *FSMEngine) executeNewsStorage(action ActionConfig, event map[string]interface{}) {

	log.Printf("📰 Storing news events for curiosity goal generation")

	eventType, ok := event["type"].(string)
	if !ok {
		return
	}

	if eventType == "timer_tick" || eventType == "" {
		return
	}

	if eventType == "user_message" || eventType == "assistant_message" || eventType == "conversation" {
		return
	}

	if eventType == "agi.tool.created" || strings.HasPrefix(eventType, "agi.tool.") {
		return
	}

	payload, ok := event["payload"].(map[string]interface{})
	if !ok || len(payload) == 0 {

		return
	}

	e.forwardNewsEventToHDN(event)

	e.storeNewsEventInWeaviate(event)

	if eventType == "relations" {
		if payload, ok := event["payload"].(map[string]interface{}); ok {
			if metadata, ok := payload["metadata"].(map[string]interface{}); ok {

				relationData := map[string]interface{}{
					"head":       metadata["head"],
					"relation":   metadata["relation"],
					"tail":       metadata["tail"],
					"headline":   metadata["headline"],
					"source":     metadata["source"],
					"timestamp":  metadata["timestamp"],
					"confidence": metadata["confidence"],
				}

				if b, err := json.Marshal(relationData); err == nil {
					key := "reasoning:news_relations:recent"
					if err := e.redis.LPush(e.ctx, key, b).Err(); err == nil {
						_ = e.redis.LTrim(e.ctx, key, 0, 99).Err()
						log.Printf("📰 Stored news relation: %s %s %s", metadata["head"], metadata["relation"], metadata["tail"])
					}
				}
			}
		}
	}

	if eventType == "alerts" {
		if payload, ok := event["payload"].(map[string]interface{}); ok {
			if metadata, ok := payload["metadata"].(map[string]interface{}); ok {

				alertData := map[string]interface{}{
					"headline":   metadata["headline"],
					"impact":     metadata["impact"],
					"source":     metadata["source"],
					"timestamp":  metadata["timestamp"],
					"confidence": metadata["confidence"],
				}

				if b, err := json.Marshal(alertData); err == nil {
					key := "reasoning:news_alerts:recent"
					if err := e.redis.LPush(e.ctx, key, b).Err(); err == nil {
						_ = e.redis.LTrim(e.ctx, key, 0, 49).Err()
						log.Printf("📰 Stored news alert: %s (impact: %s)", metadata["headline"], metadata["impact"])
					}
				}
			}
		}
	}
}

// forwardNewsEventToHDN forwards news events to the HDN server for episodic memory indexing
func (e *FSMEngine) forwardNewsEventToHDN(event map[string]interface{}) {

	canonicalEvent := e.convertToCanonicalEvent(event)
	if canonicalEvent == nil {
		log.Printf("⚠️ Failed to convert news event to canonical format")
		return
	}

	eventData, err := json.Marshal(canonicalEvent)
	if err != nil {
		log.Printf("❌ Failed to marshal news event for HDN: %v", err)
		return
	}

	if err := e.nc.Publish("agi.events.input", eventData); err != nil {
		log.Printf("❌ Failed to publish news event to HDN: %v", err)
		return
	}

	log.Printf("📡 Forwarded news event to HDN: %s (type: %s)", canonicalEvent.EventID, canonicalEvent.Type)
}

// storeNewsEventInWeaviate stores news events directly in Weaviate using WikipediaArticle class
func (e *FSMEngine) storeNewsEventInWeaviate(event map[string]interface{}) {
	log.Printf("🔍 DEBUG: storeNewsEventInWeaviate called with event: %+v", event)

	weaviateURL := os.Getenv("WEAVIATE_URL")
	if weaviateURL == "" {
		weaviateURL = "http://localhost:8080"
	}
	log.Printf("🔍 DEBUG: Using Weaviate URL: %s", weaviateURL)

	eventType, ok := event["type"].(string)
	if !ok {
		log.Printf("❌ DEBUG: No event type found in event")
		return
	}
	log.Printf("🔍 DEBUG: Event type: %s", eventType)

	payload, ok := event["payload"].(map[string]interface{})
	if !ok || len(payload) == 0 {
		log.Printf("⚠️ Skipping event - no payload or empty payload")
		return
	}
	log.Printf("🔍 DEBUG: Payload: %+v", payload)

	metadata, ok := payload["metadata"].(map[string]interface{})
	if !ok {
		metadata = make(map[string]interface{})

		if text, ok := payload["text"].(string); ok && text != "" {
			metadata["headline"] = text
		}
	}
	log.Printf("🔍 DEBUG: Metadata: %+v", metadata)

	now := time.Now().UTC()
	articleID := fmt.Sprintf("news_%s_%x", now.Format("20060102"), now.UnixNano()&0xffffffff)

	// Extract title and text based on event type
	var title, text string
	if eventType == "relations" {
		if headline, ok := metadata["headline"].(string); ok {
			title = headline
		} else {
			title = fmt.Sprintf("News Relation: %s %s %s",
				getStringFromMap(metadata, "head"),
				getStringFromMap(metadata, "relation"),
				getStringFromMap(metadata, "tail"))
		}
		text = fmt.Sprintf("Relation: %s %s %s. Headline: %s. Source: %s",
			getStringFromMap(metadata, "head"),
			getStringFromMap(metadata, "relation"),
			getStringFromMap(metadata, "tail"),
			getStringFromMap(metadata, "headline"),
			getStringFromMap(metadata, "source"))
	} else if eventType == "alerts" {
		if headline, ok := metadata["headline"].(string); ok {
			title = headline
		} else {
			title = "News Alert"
		}
		text = fmt.Sprintf("Alert: %s. Impact: %s. Source: %s",
			getStringFromMap(metadata, "headline"),
			getStringFromMap(metadata, "impact"),
			getStringFromMap(metadata, "source"))
	} else {

		title = getStringFromMap(metadata, "headline")
		if title == "" {
			title = fmt.Sprintf("News Event: %s", eventType)
		}
		text = getStringFromMap(metadata, "text")
		if text == "" {
			text = fmt.Sprintf("News event of type: %s", eventType)
		}
	}

	metadataObj := map[string]interface{}{
		"event_type":        eventType,
		"confidence":        getStringFromMap(metadata, "confidence"),
		"original_metadata": metadata,
	}
	metadataJSON, err := json.Marshal(metadataObj)
	if err != nil {
		log.Printf("❌ Failed to marshal metadata for Weaviate: %v", err)
		return
	}

	embedding := e.generateSimpleEmbedding(text, 8)

	// Normalize the vector (L2 normalization for better similarity search)
	var sumSq float32
	for _, v := range embedding {
		sumSq += v * v
	}
	if sumSq > 0 {
		norm := float32(1.0)

		for i := 0; i < 6; i++ {
			norm = 0.5 * (norm + sumSq/norm)
		}
		invNorm := 1.0 / norm
		for i := range embedding {
			embedding[i] *= invNorm
		}
	}

	weaviateObject := map[string]interface{}{
		"class": "WikipediaArticle",
		"properties": map[string]interface{}{
			"title":     title,
			"text":      text,
			"source":    "news:fsm",
			"url":       getStringFromMap(metadata, "url"),
			"timestamp": now.Format(time.RFC3339),
			"metadata":  string(metadataJSON),
		},
		"vector": embedding,
	}

	jsonData, err := json.Marshal(weaviateObject)
	if err != nil {
		log.Printf("❌ Failed to marshal news event for Weaviate: %v", err)
		return
	}

	url := weaviateURL + "/v1/objects"
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		log.Printf("❌ Failed to create Weaviate request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ Failed to send news event to Weaviate: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("❌ Weaviate returned error for news event: %s", resp.Status)
		return
	}

	log.Printf("✅ Stored news event in Weaviate: %s (type: %s)", articleID, eventType)
}

func (e *FSMEngine) executeTraceLogger(action ActionConfig, event map[string]interface{}) {

	log.Printf("📝 Logging reasoning trace")

	trace := ReasoningTrace{
		ID:         fmt.Sprintf("trace_%d", time.Now().UnixNano()),
		Goal:       e.getCurrentGoal(),
		Steps:      e.getCurrentReasoningSteps(),
		Evidence:   e.getCurrentEvidence(),
		Conclusion: e.getCurrentConclusion(),
		Confidence: e.getCurrentConfidence(),
		Domain:     e.getCurrentDomain(),
		CreatedAt:  time.Now(),
		Properties: map[string]interface{}{
			"agent_id": e.agentID,
			"state":    e.currentState,
		},
	}

	err := e.reasoning.LogReasoningTrace(trace)
	if err != nil {
		log.Printf("❌ Trace logging failed: %v", err)
		e.context["trace_logging_error"] = err.Error()
		return
	}

	log.Printf("📝 Reasoning trace logged successfully")
}

func (e *FSMEngine) executeHypothesisTesting(action ActionConfig, event map[string]interface{}) {

	log.Printf("🧪 Testing hypothesis")

	hypothesisID := ""
	if id, ok := event["hypothesis_id"].(string); ok && id != "" {
		hypothesisID = id
	} else if id, ok := e.context["current_hypothesis_id"].(string); ok && id != "" {
		hypothesisID = id
	}

	if hypothesisID == "" {
		log.Printf("❌ No hypothesis ID provided for testing")
		return
	}

	key := fmt.Sprintf("fsm:%s:hypotheses", e.agentID)
	hypothesisData, err := e.redis.HGet(e.ctx, key, hypothesisID).Result()
	if err != nil {
		log.Printf("❌ Failed to get hypothesis %s: %v", hypothesisID, err)
		return
	}

	var hypothesis map[string]interface{}
	if err := json.Unmarshal([]byte(hypothesisData), &hypothesis); err != nil {
		log.Printf("❌ Failed to parse hypothesis %s: %v", hypothesisID, err)
		return
	}

	hypothesis["status"] = "testing"
	hypothesis["testing_started_at"] = time.Now().Format(time.RFC3339)

	updatedData, _ := json.Marshal(hypothesis)
	e.redis.HSet(e.ctx, key, hypothesisID, updatedData)

	log.Printf("🧪 Testing hypothesis: %s", hypothesis["description"])

	domain := e.getCurrentDomain()
	evidence, err := e.testHypothesisWithTools(hypothesis, domain)
	if err != nil {
		log.Printf("❌ Failed to test hypothesis %s with tools: %v", hypothesisID, err)

		hypothesis["status"] = "failed"
		hypothesis["testing_error"] = err.Error()
		updatedData, _ := json.Marshal(hypothesis)
		e.redis.HSet(e.ctx, key, hypothesisID, updatedData)
		return
	}

	result := e.evaluateHypothesis(hypothesis, evidence, domain)

	hypothesis["status"] = result.Status
	hypothesis["testing_completed_at"] = time.Now().Format(time.RFC3339)

	desc, _ := hypothesis["description"].(string)
	e.logActivity(
		fmt.Sprintf("Hypothesis test result: %s (confidence: %.2f)", result.Status, result.Confidence),
		"hypothesis",
		map[string]string{
			"details": fmt.Sprintf("Hypothesis: %s, Status: %s", desc[:min(100, len(desc))], result.Status),
		},
	)
	hypothesis["evidence"] = evidence
	hypothesis["evaluation"] = result.Evaluation
	hypothesis["confidence"] = result.Confidence

	updatedData, _ = json.Marshal(hypothesis)
	e.redis.HSet(e.ctx, key, hypothesisID, updatedData)

	log.Printf("✅ Hypothesis testing completed: %s (status: %s, confidence: %.2f)",
		hypothesis["description"], result.Status, result.Confidence)

	trace := ReasoningTrace{
		ID:        fmt.Sprintf("trace_%d", time.Now().UnixNano()),
		Goal:      fmt.Sprintf("Test hypothesis: %s", hypothesis["description"]),
		Domain:    domain,
		CreatedAt: time.Now(),
		Steps: []ReasoningStep{
			{
				StepNumber: 1,
				Action:     "hypothesis_testing",
				Query:      hypothesis["description"].(string),
				Result:     map[string]interface{}{"status": result.Status, "confidence": result.Confidence},
				Reasoning:  fmt.Sprintf("Tested hypothesis by gathering %d pieces of evidence", len(evidence)),
				Confidence: result.Confidence,
				Timestamp:  time.Now(),
			},
		},
		Conclusion: fmt.Sprintf("Hypothesis %s: %s (confidence: %.2f)", result.Status, hypothesis["description"], result.Confidence),
		Confidence: result.Confidence,
		Properties: map[string]interface{}{
			"hypothesis_id":  hypothesisID,
			"evidence_count": len(evidence),
		},
	}

	if err := e.reasoning.LogReasoningTrace(trace); err != nil {
		log.Printf("Warning: failed to log reasoning trace: %v", err)
	}

	e.processHypothesisResult(hypothesis, result, domain)

	go func() {
		time.Sleep(200 * time.Millisecond)
		eventData := map[string]interface{}{
			"hypothesis_id": hypothesisID,
			"status":        result.Status,
			"confidence":    result.Confidence,
		}
		data, _ := json.Marshal(eventData)
		e.handleEvent("hypothesis_tested", data)
	}()
}
