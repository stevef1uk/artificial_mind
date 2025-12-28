// FILE: planner.go
package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// -----------------------------
// Types
// -----------------------------

// Option represents a plan option for evaluation
type Option struct {
	TaskName string  `json:"task_name"`
	Language string  `json:"language"`
	Score    float64 `json:"score"`
}

type Goal struct {
	ID     string                 `json:"id"`
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

type Capability struct {
	ID          string                 `json:"id"`
	TaskName    string                 `json:"task_name"`
	Entrypoint  string                 `json:"entrypoint"`
	Language    string                 `json:"language"`
	InputSig    map[string]string      `json:"input_signature"`
	Outputs     []string               `json:"outputs"`
	Preconds    []string               `json:"preconditions"`
	Effects     map[string]interface{} `json:"effects"`
	Score       float64                `json:"score"`
	CreatedAt   time.Time              `json:"created_at"`
	LastUsed    time.Time              `json:"last_used"`
	Validation  map[string]interface{} `json:"validation"`
	Permissions []string               `json:"permissions"`
}

type PlanStep struct {
	CapabilityID  string                 `json:"capability_id"`
	Args          map[string]interface{} `json:"args"`
	EstimatedCost float64                `json:"estimated_cost"`
	Confidence    float64                `json:"confidence"`
}

type Plan struct {
	ID               string     `json:"id"`
	Goal             Goal       `json:"goal"`
	Steps            []PlanStep `json:"steps"`
	EstimatedUtility float64    `json:"estimated_utility"`
	PrinciplesRisk   float64    `json:"principles_risk"`
	Score            float64    `json:"score"`
}

// Episode stored in memory for audit
type Episode struct {
	ID              string      `json:"id"`
	Timestamp       time.Time   `json:"timestamp"`
	UserRequest     string      `json:"user_request"`
	StructuredEvent interface{} `json:"structured_event"`
	SelectedPlan    Plan        `json:"selected_plan"`
	DecisionTrace   []string    `json:"decision_trace"`
	Result          interface{} `json:"result"`
	PrinciplesCheck interface{} `json:"principles_check"`
}

// Configurable evaluator weights
type EvaluatorConfig struct {
	WUtil float64
	WCost float64
	WRisk float64
	WConf float64
}

// Evaluator provides base evaluation functionality
type Evaluator struct {
	config EvaluatorConfig
}

// NewEvaluator creates a new base evaluator
func NewEvaluator() *Evaluator {
	return &Evaluator{
		config: EvaluatorConfig{WUtil: 4, WCost: 1, WRisk: 10, WConf: 2},
	}
}

// ScoreOption evaluates a plan option using basic principles
func (e *Evaluator) ScoreOption(opt Option) (float64, error) {
	// Basic scoring based on language preference and task complexity
	score := 0.0

	// Language preference scoring
	switch opt.Language {
	case "python":
		score += 1.0
	case "go":
		score += 0.8
	case "javascript":
		score += 0.6
	case "bash":
		score += 0.4
	default:
		score += 0.2
	}

	// Task complexity scoring (simplified)
	if opt.TaskName != "" {
		score += 0.5
	}

	return score, nil
}

// -----------------------------
// Interfaces
// -----------------------------

type Executor interface {
	ExecutePlan(ctx context.Context, p Plan, workflowID string) (interface{}, error)
}

// -----------------------------
// Planner struct
// -----------------------------

type Planner struct {
	ctx           context.Context
	redis         *redis.Client
	evalCfg       EvaluatorConfig
	principlesURL string
	executor      Executor
}

func NewPlanner(ctx context.Context, r *redis.Client, exec Executor, principlesURL string) *Planner {
	return &Planner{
		ctx:           ctx,
		redis:         r,
		evalCfg:       EvaluatorConfig{WUtil: 4, WCost: 1, WRisk: 10, WConf: 2},
		principlesURL: principlesURL,
		executor:      exec,
	}
}

// -----------------------------
// Redis helpers
// -----------------------------

func (p *Planner) capabilityKey(id string) string { return fmt.Sprintf("capability:%s", id) }

func (p *Planner) SaveCapability(ctx context.Context, cap Capability) error {
	if cap.ID == "" {
		cap.ID = uuid.New().String()
	}
	cap.CreatedAt = time.Now().UTC()
	b, err := json.Marshal(cap)
	if err != nil {
		return err
	}
	return p.redis.Set(ctx, p.capabilityKey(cap.ID), b, 0).Err()
}

func (p *Planner) GetCapability(ctx context.Context, id string) (*Capability, error) {
	res, err := p.redis.Get(ctx, p.capabilityKey(id)).Result()
	if err != nil {
		return nil, err
	}
	var cap Capability
	if err := json.Unmarshal([]byte(res), &cap); err != nil {
		return nil, err
	}
	return &cap, nil
}

func (p *Planner) ListCapabilities(ctx context.Context) ([]Capability, error) {
	// naive: use KEYS (ok for small setups). In production use a set index.
	keys, err := p.redis.Keys(ctx, "capability:*").Result()
	if err != nil {
		return nil, err
	}
	caps := make([]Capability, 0, len(keys))
	for _, k := range keys {
		v, err := p.redis.Get(ctx, k).Result()
		if err != nil {
			continue
		}
		var c Capability
		if err := json.Unmarshal([]byte(v), &c); err != nil {
			continue
		}
		caps = append(caps, c)
	}
	return caps, nil
}

// -----------------------------
// Capability matcher (very simple signature match)
// -----------------------------

func (p *Planner) FindMatchingCapabilities(ctx context.Context, goal Goal) ([]Capability, error) {
	caps, err := p.ListCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	out := []Capability{}
	for _, c := range caps {
		// simple heuristic: task name contains goal type or input signature covers params
		if c.TaskName == goal.Type || containsIgnoreCase(c.TaskName, goal.Type) {
			out = append(out, c)
			continue
		}
		// check inputSig keys subset of goal params
		match := true
		for k := range c.InputSig {
			if _, ok := goal.Params[k]; !ok {
				match = false
				break
			}
		}
		if match {
			out = append(out, c)
		}
	}
	
	// 🧠 INTELLIGENCE: Sort by learned success rate to prefer capabilities that work well
	if len(out) > 1 {
		// Enhance each capability with learned success rate
		type scoredCap struct {
			cap   Capability
			score float64
		}
		scored := make([]scoredCap, len(out))
		for i, cap := range out {
			successRate := p.getCapabilitySuccessRate(ctx, cap.ID, cap.TaskName)
			// Combine base score with learned success rate
			// Base score (0-1) + success rate bonus (0-2)
			totalScore := cap.Score + (successRate * 2.0)
			scored[i] = scoredCap{cap: cap, score: totalScore}
		}
		
		// Sort by total score (descending)
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].score > scored[j].score
		})
		
		// Extract sorted capabilities
		out = make([]Capability, len(scored))
		for i, sc := range scored {
			out[i] = sc.cap
		}
	}
	
	return out, nil
}

// getCapabilitySuccessRate retrieves learned success rate for a capability
func (p *Planner) getCapabilitySuccessRate(ctx context.Context, capabilityID, taskName string) float64 {
	if p.redis == nil {
		return 0.0
	}
	
	// Try capability-specific success rate first
	key := fmt.Sprintf("capability_success_rate:%s", capabilityID)
	rateStr, err := p.redis.Get(ctx, key).Result()
	if err == nil {
		if rate, err := strconv.ParseFloat(rateStr, 64); err == nil {
			return rate
		}
	}
	
	// Fallback to task-name-based success rate
	if taskName != "" {
		taskKey := fmt.Sprintf("task_success_rate:%s", taskName)
		rateStr, err := p.redis.Get(ctx, taskKey).Result()
		if err == nil {
			if rate, err := strconv.ParseFloat(rateStr, 64); err == nil {
				return rate
			}
		}
	}
	
	return 0.0 // No data available
}

func containsIgnoreCase(a, b string) bool {
	return len(a) >= len(b) && (a == b || (len(b) > 0 && (stringContainsFold(a, b))))
}

func stringContainsFold(s, substr string) bool { // simple helper
	return len(substr) > 0 && (len(s) >= len(substr) && (indexFold(s, substr) >= 0))
}

func indexFold(s, substr string) int {
	// rudimentary case-insensitive index
	S := []rune(s)
	sub := []rune(substr)
	for i := 0; i <= len(S)-len(sub); i++ {
		ok := true
		for j := 0; j < len(sub); j++ {
			if toLower(S[i+j]) != toLower(sub[j]) {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// -----------------------------
// Plan generation & scoring
// -----------------------------

func (p *Planner) GeneratePlans(ctx context.Context, goal Goal) ([]Plan, error) {
	caps, err := p.FindMatchingCapabilities(ctx, goal)
	if err != nil {
		return nil, err
	}
	plans := []Plan{}
	for _, c := range caps {
		step := PlanStep{CapabilityID: c.ID, Args: goal.Params, EstimatedCost: 1.0, Confidence: c.Score}
		pl := Plan{ID: uuid.New().String(), Goal: goal, Steps: []PlanStep{step}, EstimatedUtility: 0.8, PrinciplesRisk: 0.0}
		plans = append(plans, pl)
	}
	// Optionally: call LLM to get suggestions (omitted here; hook point)
	return plans, nil
}

func (p *Planner) scorePlan(plan Plan) float64 {
	// deterministic scoring: utility/cost/risk/confidence
	util := plan.EstimatedUtility
	cost := 0.0
	for _, s := range plan.Steps {
		cost += s.EstimatedCost
	}
	risk := plan.PrinciplesRisk
	conf := 0.0
	for _, s := range plan.Steps {
		conf += s.Confidence
	}
	if len(plan.Steps) > 0 {
		conf = conf / float64(len(plan.Steps))
	}
	// normalized
	score := p.evalCfg.WUtil*util - p.evalCfg.WCost*cost - p.evalCfg.WRisk*risk + p.evalCfg.WConf*conf
	return score
}

func (p *Planner) ScoreAndSortPlans(plans []Plan) []Plan {
	for i := range plans {
		plans[i].Score = p.scorePlan(plans[i])
	}
	sort.Slice(plans, func(i, j int) bool { // descending
		if plans[i].Score == plans[j].Score {
			return plans[i].EstimatedUtility > plans[j].EstimatedUtility
		}
		return plans[i].Score > plans[j].Score
	})
	return plans
}

// -----------------------------
// Principles check
// -----------------------------

func (p *Planner) CheckPlanAgainstPrinciples(ctx context.Context, plan Plan) (blocked bool, reason string, err error) {
	// Simple HTTP request to principles server. Assumes POST /check-plan
	body := map[string]interface{}{"plan": plan}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", p.principlesURL+"/check-plan", bytesReader(b))
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false, "", fmt.Errorf("principles server returned %d", resp.StatusCode)
	}
	var out struct {
		Blocked bool   `json:"blocked"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, "", err
	}
	return out.Blocked, out.Reason, nil
}

// helper for bytes reader without importing bytes package at top-level (to keep imports tidy in this skeleton)
func bytesReader(b []byte) *readerShim { return &readerShim{b: b, idx: 0} }

type readerShim struct {
	b   []byte
	idx int
}

func (r *readerShim) Read(p []byte) (n int, err error) {
	if r.idx >= len(r.b) {
		return 0, errors.New("EOF")
	}
	n = copy(p, r.b[r.idx:])
	r.idx += n
	return n, nil
}
func (r *readerShim) Close() error { return nil }

// -----------------------------
// Plan selection and execution
// -----------------------------

func (p *Planner) PlanAndSelect(ctx context.Context, goal Goal) (*Plan, error) {
	plans, err := p.GeneratePlans(ctx, goal)
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return nil, errors.New("no candidate capabilities")
	}
	plans = p.ScoreAndSortPlans(plans)
	// try each plan until one passes principles
	for _, pl := range plans {
		blocked, _, err := p.CheckPlanAgainstPrinciples(ctx, pl)
		if err != nil {
			return nil, err
		}
		if blocked {
			// log and continue
			continue
		}
		return &pl, nil
	}
	return nil, errors.New("no plan passed principles checks")
}

func (p *Planner) ExecutePlan(ctx context.Context, plan Plan, userRequest string) (*Episode, error) {
	res, err := p.executor.ExecutePlan(ctx, plan, "")
	e := &Episode{ID: uuid.New().String(), Timestamp: time.Now().UTC(), UserRequest: userRequest, SelectedPlan: plan, Result: res}
	// store episode to redis
	b, _ := json.Marshal(e)
	if err := p.redis.Set(ctx, fmt.Sprintf("episode:%s", e.ID), b, 0).Err(); err != nil {
		// non-fatal: still return result
	}
	if err != nil {
		return e, err
	}
	return e, nil
}

// -----------------------------
// Utilities
// -----------------------------

func (p *Planner) LoadEpisode(ctx context.Context, id string) (*Episode, error) {
	v, err := p.redis.Get(ctx, fmt.Sprintf("episode:%s", id)).Result()
	if err != nil {
		return nil, err
	}
	var e Episode
	if err := json.Unmarshal([]byte(v), &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// end of planner.go
