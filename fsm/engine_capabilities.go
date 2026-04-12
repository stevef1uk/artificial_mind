package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// HDN capability integration
func (e *FSMEngine) executeRetrieveCapabilities(action ActionConfig, event map[string]interface{}) {

	e.context["hdn_inflight"] = true
	e.context["hdn_started_at"] = time.Now().Format(time.RFC3339)

	go func() {
		defer func() { e.context["hdn_inflight"] = false }()

		base := os.Getenv("HDN_URL")
		if base == "" {
			base = "http://localhost:8080"
		}
		url := fmt.Sprintf("%s/api/v1/intelligent/capabilities", base)
		log.Printf("HDN: GET %s", url)

		req, _ := http.NewRequest("GET", url, nil)
		if pid, ok := e.context["project_id"].(string); ok && pid != "" {
			req.Header.Set("X-Project-ID", pid)
		}
		// Simple retry with backoff (1s, 2s, 4s)
		var resp *http.Response
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			resp, err = (&http.Client{Timeout: 30 * time.Second}).Do(req)
			if err == nil {
				break
			}
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("⚠️ HDN capabilities fetch attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
			time.Sleep(backoff)
		}
		if err != nil {
			log.Printf("❌ HDN capabilities fetch failed after retries: %v", err)
			e.context["hdn_last_error"] = err.Error()
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			log.Printf("❌ HDN capabilities fetch status %d: %s", resp.StatusCode, string(body))
			e.context["hdn_last_error"] = fmt.Sprintf("status %d", resp.StatusCode)
			return
		}

		// Tolerant parse: accept either array or object containing an array (prefer "capabilities" key)
		var caps []map[string]interface{}
		if err := json.Unmarshal(body, &caps); err != nil {
			var obj map[string]interface{}
			if err2 := json.Unmarshal(body, &obj); err2 != nil {
				log.Printf("❌ HDN capabilities parse error: %v (body len=%d)", err, len(body))
				e.context["hdn_last_error"] = err.Error()
				return
			}

			if raw, ok := obj["capabilities"]; ok {
				if list, ok := raw.([]interface{}); ok {
					for _, it := range list {
						if m, ok2 := it.(map[string]interface{}); ok2 {
							caps = append(caps, m)
						}
					}
				}
			}

			if len(caps) == 0 {
				for _, v := range obj {
					if list, ok := v.([]interface{}); ok {
						for _, it := range list {
							if m, ok2 := it.(map[string]interface{}); ok2 {
								caps = append(caps, m)
							}
						}
						break
					}
				}
			}
		}
		if len(caps) == 0 {
			log.Printf("ℹ️ HDN capabilities response contained no items")
		}
		arr := make([]interface{}, 0, len(caps))
		for _, c := range caps {
			arr = append(arr, c)
		}
		e.context["candidate_capabilities"] = arr
		if len(caps) > 0 {
			e.context["selected_capability"] = caps[0]
			if name, ok := caps[0]["name"].(string); ok && name != "" {
				e.context["current_action"] = name
			} else if id, ok := caps[0]["id"].(string); ok && id != "" {
				e.context["current_action"] = id
			}
		}
		e.handleEvent("capabilities_retrieved", nil)
	}()
}

func (e *FSMEngine) executeExecuteCapability(action ActionConfig, event map[string]interface{}) {

	log.Printf("HDN: executing selected domain capability")

	// Select capability
	var selected map[string]interface{}
	if sel, ok := e.context["selected_capability"].(map[string]interface{}); ok {
		selected = sel
	} else if list, ok := e.context["candidate_capabilities"].([]interface{}); ok && len(list) > 0 {
		if m, ok2 := list[0].(map[string]interface{}); ok2 {
			selected = m
			e.context["selected_capability"] = selected
		}
	}

	base := os.Getenv("HDN_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	url := fmt.Sprintf("%s/api/v1/interpret/execute", base)

	ctx := map[string]string{"origin": "fsm"}
	if pid, ok := e.context["project_id"].(string); ok && pid != "" {
		ctx["project_id"] = pid
	}

	e.context["hdn_delegate"] = true

	desc := "Execute a task"
	if selected != nil {

		if v, ok := selected["description"].(string); ok && v != "" {
			desc = v
		} else if v, ok := selected["name"].(string); ok && v != "" {
			desc = fmt.Sprintf("Execute %s", v)
		}
	} else {

		if currentGoal := e.getCurrentGoal(); currentGoal != "" && currentGoal != "Unknown goal" {
			desc = currentGoal
			log.Printf("🎯 Using current curiosity goal as execution input: %s", desc)
		} else {
			log.Printf("❌ No capability selected and no current goal")
			return
		}
	}

	descLower := strings.ToLower(desc)
	if strings.Contains(descLower, "file") || strings.Contains(descLower, "read") || strings.Contains(descLower, "write") {
		if !strings.Contains(descLower, "tool") {
			desc = desc + " (prefer using file tools like tool_file_read, tool_file_write, or tool_ls)"
		}
	} else if strings.Contains(descLower, "http") || strings.Contains(descLower, "url") || strings.Contains(descLower, "fetch") || strings.Contains(descLower, "scrape") {
		if !strings.Contains(descLower, "tool") {
			desc = desc + " (prefer using tool_http_get or tool_html_scraper)"
		}
	} else if strings.Contains(descLower, "execute") || strings.Contains(descLower, "command") || strings.Contains(descLower, "run") {
		if !strings.Contains(descLower, "tool") {
			desc = desc + " (prefer using tool_exec or appropriate tool if available)"
		}
	}

	payload := map[string]interface{}{
		"input":      desc,
		"context":    ctx,
		"session_id": fmt.Sprintf("fsm_%s_%d", e.agentID, time.Now().UnixNano()),
	}
	if inp := e.context["capability_inputs"]; inp != nil {

		if c, ok := payload["context"].(map[string]string); ok {

			if inputsStr, ok := inp.(string); ok {
				c["inputs"] = inputsStr
			}
		}
	}
	data, _ := json.Marshal(payload)
	log.Printf("HDN: POST %s (input=%s)", url, desc)

	go func() {

		e.recordToolUsage("invoked", desc, map[string]interface{}{"selected": selected})
		req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if pid, ok := e.context["project_id"].(string); ok && pid != "" {
			req.Header.Set("X-Project-ID", pid)
		}
		// Retry with backoff on transient failures
		var resp *http.Response
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			resp, err = (&http.Client{Timeout: 300 * time.Second}).Do(req)
			if err == nil {
				break
			}
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("⚠️ HDN execute attempt %d failed: %v (retrying in %s)", attempt, err, backoff)
			time.Sleep(backoff)
		}
		if err != nil {
			log.Printf("❌ HDN execute failed after retries: %v", err)
			e.context["last_execution_error"] = err.Error()
			e.recordToolUsage("failed", desc, map[string]interface{}{"error": err.Error()})
			go e.persistExecutionEpisode(map[string]interface{}{"error": err.Error()}, false)
			e.handleEvent("execution_failed", nil)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			log.Printf("❌ HDN execute status %d: %s", resp.StatusCode, string(body))
			e.context["last_execution_status"] = resp.StatusCode
			e.context["last_execution_body"] = string(body)
			e.recordToolUsage("failed", desc, map[string]interface{}{"status": resp.StatusCode, "body": string(body)})
			go e.persistExecutionEpisode(map[string]interface{}{"status": resp.StatusCode, "body": string(body)}, false)
			e.handleEvent("execution_failed", nil)
			return
		}
		var out map[string]interface{}
		if err := json.Unmarshal(body, &out); err != nil {
			log.Printf("❌ HDN execute parse error: %v", err)
			e.context["last_execution_body"] = string(body)
			e.recordToolUsage("failed", desc, map[string]interface{}{"parse_error": err.Error()})
			go e.persistExecutionEpisode(map[string]interface{}{"body": string(body), "parse_error": err.Error()}, false)
			e.handleEvent("execution_failed", nil)
			return
		}

		if s, ok := out["success"].(bool); ok && !s {
			errMsg := "execution reported success=false"
			if em, ok := out["error"].(string); ok && em != "" {
				errMsg = em
			}
			log.Printf("❌ HDN execute reported failure: %s", errMsg)
			e.context["last_execution_error"] = errMsg
			e.recordToolUsage("failed", desc, map[string]interface{}{"error": errMsg})
			go e.persistExecutionEpisode(out, false)
			e.handleEvent("execution_failed", nil)
			return
		}

		if _, ok := out["result"]; !ok {
			if v, ok := out["output"]; ok {
				out["result"] = v
			} else if v, ok := out["data"]; ok {
				out["result"] = v
			}
		}

		e.context["last_execution"] = out

		if workflowID, ok := out["workflow_id"].(string); ok && workflowID != "" {
			e.context["current_workflow_id"] = workflowID
			log.Printf("📝 Captured workflow_id: %s", workflowID)
		}

		e.recordToolUsage("result", desc, map[string]interface{}{"result": out})
		go e.persistExecutionEpisode(out, true)
		e.handleEvent("execution_finished", nil)
	}()
}

// recordToolUsage appends a usage record in Redis and optionally publishes NATS (handled by other services)
func (e *FSMEngine) recordToolUsage(kind string, tool string, extra map[string]interface{}) {
	defer func() { recover() }()
	rec := map[string]interface{}{
		"ts":       time.Now().Format(time.RFC3339),
		"agent_id": e.agentID,
		"type":     kind,
		"tool":     tool,
	}
	for k, v := range extra {
		rec[k] = v
	}
	b, _ := json.Marshal(rec)
	keys := []string{
		fmt.Sprintf("tools:%s:usage_history", e.agentID),
		"tools:global:usage_history",
	}
	for _, k := range keys {
		_ = e.redis.LPush(e.ctx, k, b).Err()
		_ = e.redis.LTrim(e.ctx, k, 0, 199).Err()
	}
}

// persistExecutionEpisode stores an execution result as an episode in Redis and updates knowledge growth snapshots.
func (e *FSMEngine) persistExecutionEpisode(result map[string]interface{}, success bool) {
	defer func() { recover() }()

	ep := map[string]interface{}{
		"id":        fmt.Sprintf("ep_%d", time.Now().UnixNano()),
		"timestamp": time.Now().Format(time.RFC3339),
		"outcome":   map[bool]string{true: "success", false: "failed"}[success],
	}
	if pid, ok := e.context["project_id"].(string); ok && pid != "" {
		ep["project_id"] = pid
	}
	if sel, ok := e.context["selected_capability"].(map[string]interface{}); ok {
		if n, ok := sel["name"].(string); ok && n != "" {
			ep["summary"] = fmt.Sprintf("Executed %s", n)
		} else if id, ok2 := sel["id"].(string); ok2 && id != "" {
			ep["summary"] = fmt.Sprintf("Executed %s", id)
		}
	}

	ep["result"] = result

	keys := []string{
		fmt.Sprintf("fsm:%s:episodes", e.agentID),
		e.getRedisKey("episodes"),
	}
	data, _ := json.Marshal(ep)
	for _, k := range keys {
		if k == "" {
			continue
		}
		_ = e.redis.LPush(e.ctx, k, data).Err()
		_ = e.redis.LTrim(e.ctx, k, 0, 99).Err()
	}

	_ = e.redis.Set(e.ctx, fmt.Sprintf("fsm:%s:last_activity", e.agentID), time.Now().Format(time.RFC3339), 0).Err()

	kgKey := fmt.Sprintf("fsm:%s:knowledge_growth_timeline", e.agentID)
	kg := KnowledgeGrowthStats{LastGrowthTime: time.Now()}
	if b, err := json.Marshal(kg); err == nil {
		_ = e.redis.LPush(e.ctx, kgKey, b).Err()
		_ = e.redis.LTrim(e.ctx, kgKey, 0, 199).Err()
	}
}
