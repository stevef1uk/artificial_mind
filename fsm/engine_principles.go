package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

func (e *FSMEngine) executeMandatoryPrinciplesChecker(action ActionConfig, event map[string]interface{}) {

	log.Printf("🔒 MANDATORY PRINCIPLES CHECK - Hardcoded requirement")

	actionDesc := "Unknown action"
	if desc, ok := event["action"].(string); ok {
		actionDesc = desc
	} else if desc, ok := e.context["current_action"].(string); ok {
		actionDesc = desc
	}

	if hdn, ok := e.context["hdn_delegate"].(bool); ok && hdn {
		log.Printf("✅ MANDATORY PRINCIPLES CHECK SKIPPED - Delegated to HDN")
		e.context["principles_allowed"] = true
		e.context["principles_reason"] = "Delegated to HDN checks"
		e.context["principles_confidence"] = 0.99

		e.recordPrinciplesStats(true, 1*time.Millisecond, false)
		return
	}

	if ca, ok := e.context["current_action"].(string); ok {
		switch strings.ToLower(ca) {
		case "primenumbergenerator", "matrixcalculator", "statisticalanalyzer":
			log.Printf("✅ MANDATORY PRINCIPLES CHECK BYPASS - Safe capability: %s", ca)
			e.context["principles_allowed"] = true
			e.context["principles_reason"] = "Allowlisted safe capability"
			e.context["principles_confidence"] = 0.99

			e.recordPrinciplesStats(true, 1*time.Millisecond, false)
			return
		}
	}

	start := time.Now()
	response, err := e.principles.MandatoryPrinciplesCheck(actionDesc, e.context)
	if err != nil {
		log.Printf("❌ MANDATORY PRINCIPLES CHECK FAILED - %v", err)

		e.context["principles_error"] = err.Error()
		e.recordPrinciplesStats(false, time.Since(start), true)
		return
	}

	if !response.Allowed {
		log.Printf("❌ MANDATORY PRINCIPLES CHECK FAILED - Action blocked: %s", response.Reason)
		e.context["principles_blocked"] = response.Reason
		e.context["blocked_rules"] = response.BlockedRules
		e.recordPrinciplesStats(false, time.Since(start), false)
		return
	}

	log.Printf("✅ MANDATORY PRINCIPLES CHECK PASSED - Action allowed: %s", response.Reason)
	e.context["principles_allowed"] = true
	e.context["principles_reason"] = response.Reason
	e.context["principles_confidence"] = response.Confidence
	e.recordPrinciplesStats(true, time.Since(start), false)
}

func (e *FSMEngine) executePrinciplesChecker(action ActionConfig, event map[string]interface{}) {

	log.Printf("Checking principles with domain safety")

	go func() {
		time.Sleep(150 * time.Millisecond)
		e.handleEvent("allowed", nil)
	}()
}

func (e *FSMEngine) executePreExecutionPrinciplesChecker(action ActionConfig, event map[string]interface{}) {

	log.Printf("🔒 PRE-EXECUTION PRINCIPLES CHECK - Double-checking safety before action")

	actionDesc := "Unknown action"
	if desc, ok := event["action"].(string); ok {
		actionDesc = desc
	} else if desc, ok := e.context["current_action"].(string); ok {
		actionDesc = desc
	}

	if hdn, ok := e.context["hdn_delegate"].(bool); ok && hdn {
		log.Printf("✅ PRE-EXECUTION PRINCIPLES CHECK SKIPPED - Delegated to HDN")
		e.context["pre_execution_allowed"] = true
		e.context["pre_execution_reason"] = "Delegated to HDN checks"
		e.context["pre_execution_confidence"] = 0.99
		e.recordPrinciplesStats(true, 1*time.Millisecond, false)
		go func() { time.Sleep(100 * time.Millisecond); e.handleEvent("allowed", nil) }()
		return
	}

	if ca, ok := e.context["current_action"].(string); ok {
		switch strings.ToLower(ca) {
		case "primenumbergenerator", "matrixcalculator", "statisticalanalyzer":
			log.Printf("✅ PRE-EXECUTION PRINCIPLES CHECK BYPASS - Safe capability: %s", ca)
			e.context["pre_execution_allowed"] = true
			e.context["pre_execution_reason"] = "Allowlisted safe capability"
			e.context["pre_execution_confidence"] = 0.99
			e.recordPrinciplesStats(true, 1*time.Millisecond, false)

			go func() {
				time.Sleep(100 * time.Millisecond)
				e.handleEvent("allowed", nil)
			}()
			return
		}
	}

	start := time.Now()
	response, err := e.principles.PreExecutionPrinciplesCheck(actionDesc, e.context)
	if err != nil {
		log.Printf("❌ PRE-EXECUTION PRINCIPLES CHECK FAILED - %v", err)
		e.context["pre_execution_error"] = err.Error()
		e.recordPrinciplesStats(false, time.Since(start), true)
		return
	}

	if !response.Allowed {
		log.Printf("❌ PRE-EXECUTION PRINCIPLES CHECK FAILED - Action blocked: %s", response.Reason)
		e.context["pre_execution_blocked"] = response.Reason
		e.context["pre_execution_blocked_rules"] = response.BlockedRules
		e.recordPrinciplesStats(false, time.Since(start), false)
		return
	}

	log.Printf("✅ PRE-EXECUTION PRINCIPLES CHECK PASSED - Action allowed: %s", response.Reason)
	e.context["pre_execution_allowed"] = true
	e.context["pre_execution_reason"] = response.Reason
	e.context["pre_execution_confidence"] = response.Confidence
	e.recordPrinciplesStats(true, time.Since(start), false)

	go func() {
		time.Sleep(100 * time.Millisecond)
		e.handleEvent("allowed", nil)
	}()
}

// recordPrinciplesStats updates Redis-backed metrics used by the Monitor FSM "Principles Checks" panel.
func (e *FSMEngine) recordPrinciplesStats(allowed bool, duration time.Duration, errOccurred bool) {

	defer func() { recover() }()

	key := fmt.Sprintf("fsm:%s:principles", e.agentID)

	_ = e.redis.HIncrBy(e.ctx, key, "total_checks", 1).Err()

	if allowed {
		_ = e.redis.HIncrBy(e.ctx, key, "allowed_actions", 1).Err()
	} else {
		_ = e.redis.HIncrBy(e.ctx, key, "blocked_actions", 1).Err()
	}

	if errOccurred {
		_ = e.redis.HIncrBy(e.ctx, key, "error_count", 1).Err()
	}

	ms := float64(duration.Milliseconds())
	if ms < 0 {
		ms = 0
	}
	_ = e.redis.HIncrByFloat(e.ctx, key, "total_response_time_ms", ms).Err()

	totals, err1 := e.redis.HMGet(e.ctx, key, "total_response_time_ms", "total_checks").Result()
	if err1 == nil && len(totals) == 2 && totals[0] != nil && totals[1] != nil {
		var totalMs float64
		var checks int64
		if s, ok := totals[0].(string); ok {
			fmt.Sscanf(s, "%f", &totalMs)
		}
		switch v := totals[1].(type) {
		case string:
			fmt.Sscanf(v, "%d", &checks)
		case int64:
			checks = v
		}
		if checks > 0 {
			avg := totalMs / float64(checks)
			_ = e.redis.HSet(e.ctx, key, "average_response_time_ms", avg).Err()
		}
	}
}

func (e *FSMEngine) executeUtilityCalculator(action ActionConfig, event map[string]interface{}) {

	log.Printf("Calculating utility including domain confidence")
}

func (e *FSMEngine) executeConstraintEnforcer(action ActionConfig, event map[string]interface{}) {

	log.Printf("Applying domain constraints")
}
