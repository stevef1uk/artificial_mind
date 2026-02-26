package selfmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// Goal represents a managed objective for an agent
type PolicyGoal struct {
	ID          string                 `json:"id"`
	AgentID     string                 `json:"agent_id"`
	Description string                 `json:"description"`
	Origin      string                 `json:"origin"`
	Priority    string                 `json:"priority"`
	Status      string                 `json:"status"`
	Confidence  float64                `json:"confidence"`
	Deadline    string                 `json:"deadline,omitempty"`
	Metrics     map[string]interface{} `json:"metrics,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"` // Additional context (e.g., source, domain, curiosity_id)
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// GoalManager manages goal lifecycle, scoring, and exposure via Redis/NATS
type GoalManager struct {
	nc      *nats.Conn
	redis   *redis.Client
	agentID string
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewGoalManager(nc *nats.Conn, rdb *redis.Client, agentID string) *GoalManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &GoalManager{nc: nc, redis: rdb, agentID: agentID, ctx: ctx, cancel: cancel}
}

func (gm *GoalManager) Start() error {
	// Subscribe to triggers
	if _, err := gm.nc.Subscribe("agi.perception.fact", gm.handleFact); err != nil {
		return fmt.Errorf("subscribe perception: %w", err)
	}
	if _, err := gm.nc.Subscribe("agi.evaluation.result", gm.handleEvaluation); err != nil {
		return fmt.Errorf("subscribe evaluation: %w", err)
	}
	if _, err := gm.nc.Subscribe("agi.user.goal", gm.handleUserGoal); err != nil {
		return fmt.Errorf("subscribe user goal: %w", err)
	}
	return nil
}

func (gm *GoalManager) Stop() {
	gm.cancel()
}

// --- NATS Handlers ---

func (gm *GoalManager) handleUserGoal(m *nats.Msg) {
	fmt.Printf("🐛 DEBUG: Received user goal event: %s\n", string(m.Data))
	var g PolicyGoal
	if err := json.Unmarshal(m.Data, &g); err != nil {
		fmt.Printf("🐛 DEBUG: Error unmarshaling user goal: %v\n", err)
		return
	}
	if g.ID == "" {
		g.ID = fmt.Sprintf("g_%d", time.Now().UTC().UnixNano())
	}
	g.AgentID = gm.agentID
	if g.Status == "" {
		g.Status = "active"
	}
	// Reuse CreateGoal path (deduplication & events)
	if _, err := gm.CreateGoal(g); err != nil {
		return
	}
}

func (gm *GoalManager) handleFact(m *nats.Msg) {
	fmt.Printf("🐛 DEBUG: Received fact event: %s\n", string(m.Data))
	var fact map[string]interface{}
	if err := json.Unmarshal(m.Data, &fact); err != nil {
		fmt.Printf("🐛 DEBUG: Error unmarshaling fact: %v\n", err)
		return
	}
	// Example rule: spawn reduce-error-rate goal when error_rate > 5%
	if rate, ok := asFloat64(fact["error_rate"]); ok && rate > 0.05 {
		g := PolicyGoal{
			ID:          fmt.Sprintf("g_%d", time.Now().UTC().UnixNano()),
			AgentID:     gm.agentID,
			Description: "Reduce API error rate below 1%",
			Origin:      "perception:errors",
			Priority:    "high",
			Status:      "active",
			Confidence:  0.8,
			Metrics: map[string]interface{}{
				"current_error_rate": rate,
				"target_error_rate":  0.01,
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		_ = gm.saveGoal(&g)
		_ = gm.publishEvent("agi.goal.created", g)
	}
}

func (gm *GoalManager) handleEvaluation(m *nats.Msg) {
	fmt.Printf("🐛 DEBUG: Received evaluation event: %s\n", string(m.Data))
	var eval map[string]interface{}
	if err := json.Unmarshal(m.Data, &eval); err != nil {
		fmt.Printf("🐛 DEBUG: Error unmarshaling evaluation: %v\n", err)
		return
	}
	if rate, ok := asFloat64(eval["error_rate"]); ok {
		// check active goals for achievement
		ids, err := gm.redis.SMembers(gm.ctx, gm.keyActiveSet()).Result()
		if err != nil {
			return
		}
		for _, gid := range ids {
			g, err := gm.loadGoal(gid)
			if err != nil {
				continue
			}
			if g.Status != "active" {
				continue
			}
			if g.Metrics != nil {
				if target, ok := asFloat64(g.Metrics["target_error_rate"]); ok && rate <= target {
					g.Status = "achieved"
					g.UpdatedAt = time.Now().UTC()
					_ = gm.saveGoal(g)
					_ = gm.archiveGoal(g.ID)
					_ = gm.publishEvent("agi.goal.achieved", *g)
				}
			}
		}
	}
}

// --- Persistence & Scoring ---

func (gm *GoalManager) keyActiveSet() string {
	return fmt.Sprintf("goals:%s:active", gm.agentID)
}

func (gm *GoalManager) keyHistorySet() string {
	return fmt.Sprintf("goals:%s:history", gm.agentID)
}

func (gm *GoalManager) keyGoal(id string) string {
	return fmt.Sprintf("goal:%s", id)
}

func (gm *GoalManager) keyPriorityZSet() string {
	return fmt.Sprintf("goals:%s:priorities", gm.agentID)
}

func (gm *GoalManager) saveGoal(g *PolicyGoal) error {
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	if err := gm.redis.Set(gm.ctx, gm.keyGoal(g.ID), b, 0).Err(); err != nil {
		return err
	}
	_ = gm.redis.SAdd(gm.ctx, gm.keyActiveSet(), g.ID).Err()
	// Update scoring
	score := gm.scoreGoal(*g)
	_ = gm.redis.ZAdd(gm.ctx, gm.keyPriorityZSet(), redis.Z{Score: score, Member: g.ID}).Err()
	return nil
}

func (gm *GoalManager) loadGoal(id string) (*PolicyGoal, error) {
	v, err := gm.redis.Get(gm.ctx, gm.keyGoal(id)).Result()
	if err != nil {
		return nil, err
	}
	var g PolicyGoal
	if err := json.Unmarshal([]byte(v), &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (gm *GoalManager) archiveGoal(id string) error {
	_ = gm.redis.SRem(gm.ctx, gm.keyActiveSet(), id).Err()
	_ = gm.redis.SAdd(gm.ctx, gm.keyHistorySet(), id).Err()
	_ = gm.redis.ZRem(gm.ctx, gm.keyPriorityZSet(), id).Err()
	return nil
}

func (gm *GoalManager) scoreGoal(g PolicyGoal) float64 {
	// Very simple mapping: high=3, medium=2, low=1; score = priority * confidence
	var importance float64 = 1
	switch g.Priority {
	case "high":
		importance = 3
	case "medium":
		importance = 2
	case "low":
		importance = 1
	}
	return importance * g.Confidence
}

func (gm *GoalManager) publishEvent(subject string, g PolicyGoal) error {
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return gm.nc.Publish(subject, b)
}

// --- Helpers ---

func asFloat64(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// --- Public API helpers for REST layer ---

// ListActiveGoals returns the current active goals for this agent (sorted by priority desc)
// limited to the top 200 goals to prevent performance issues.
func (gm *GoalManager) ListActiveGoals() ([]PolicyGoal, error) {
	// Only fetch the top 200 goals
	ids, err := gm.redis.ZRevRange(gm.ctx, gm.keyPriorityZSet(), 0, 199).Result()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []PolicyGoal{}, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = gm.keyGoal(id)
	}

	dataList, err := gm.redis.MGet(gm.ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	goals := make([]PolicyGoal, 0, len(ids))
	for _, data := range dataList {
		if data == nil {
			continue
		}
		var g PolicyGoal
		if str, ok := data.(string); ok {
			if err := json.Unmarshal([]byte(str), &g); err == nil && g.Status == "active" {
				goals = append(goals, g)
			}
		}
	}
	return goals, nil
}

// GetGoal fetches a specific goal by ID
func (gm *GoalManager) GetGoal(id string) (*PolicyGoal, error) {
	return gm.loadGoal(id)
}

// CreateGoal inserts a new goal and emits created event
func (gm *GoalManager) CreateGoal(g PolicyGoal) (*PolicyGoal, error) {
	// Deduplicate by normalized description among ACTIVE goals
	normDesc := strings.ToLower(strings.TrimSpace(g.Description))
	if normDesc != "" {
		if existing, _ := gm.findActiveGoalByDescription(normDesc); existing != nil {
			// Refresh updated_at and return existing goal
			existing.UpdatedAt = time.Now().UTC()
			if err := gm.saveGoal(existing); err == nil {
				_ = gm.publishEvent("agi.goal.updated", *existing)
			}
			return existing, nil
		}
	}

	if g.ID == "" {
		g.ID = fmt.Sprintf("g_%d", time.Now().UTC().UnixNano())
	}
	g.AgentID = gm.agentID
	if g.Status == "" {
		g.Status = "active"
	}
	now := time.Now().UTC()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
	}
	g.UpdatedAt = now

	// Debug: Log context preservation
	if len(g.Context) > 0 {
		fmt.Printf("🐛 DEBUG: CreateGoal preserving context: %+v\n", g.Context)
	}

	if err := gm.saveGoal(&g); err != nil {
		return nil, err
	}
	_ = gm.publishEvent("agi.goal.created", g)
	return &g, nil
}

// findActiveGoalByDescription returns the first ACTIVE goal whose description matches normDesc (lowercased, trimmed)
func (gm *GoalManager) findActiveGoalByDescription(normDesc string) (*PolicyGoal, error) {
	// PERFORMANCE: SMembers can be large, but we need to check active goals for deduplication.
	// Cap at 1000 to prevent OOM if the system is unhealthy.
	ids, err := gm.redis.SMembers(gm.ctx, gm.keyActiveSet()).Result()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 1000 {
		ids = ids[:1000]
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = gm.keyGoal(id)
	}

	dataList, err := gm.redis.MGet(gm.ctx, keys...).Result()
	if err != nil {
		return nil, nil // Silently fail on data mismatch
	}

	for _, data := range dataList {
		if data == nil {
			continue
		}
		var g PolicyGoal
		if str, ok := data.(string); ok {
			if err := json.Unmarshal([]byte(str), &g); err == nil {
				if g.Status == "active" && strings.ToLower(strings.TrimSpace(g.Description)) == normDesc {
					return &g, nil
				}
			}
		}
	}
	return nil, nil
}

// UpdateGoalStatus updates status and archives if terminal; emits events
func (gm *GoalManager) UpdateGoalStatus(id string, status string) (*PolicyGoal, error) {
	g, err := gm.loadGoal(id)
	if err != nil {
		return nil, err
	}
	g.Status = status
	g.UpdatedAt = time.Now().UTC()
	if err := gm.saveGoal(g); err != nil {
		return nil, err
	}
	switch status {
	case "suspended":
		_ = gm.publishEvent("agi.goal.updated", *g)
	case "active":
		_ = gm.publishEvent("agi.goal.updated", *g)
	case "achieved":
		_ = gm.archiveGoal(g.ID)
		_ = gm.publishEvent("agi.goal.achieved", *g)
	case "failed":
		_ = gm.archiveGoal(g.ID)
		_ = gm.publishEvent("agi.goal.failed", *g)
	default:
		_ = gm.publishEvent("agi.goal.updated", *g)
	}
	return g, nil
}

// DeleteGoal removes a goal completely from Redis and emits deleted event
func (gm *GoalManager) DeleteGoal(id string) error {
	// Load goal first to get its data for the event
	g, err := gm.loadGoal(id)
	if err != nil {
		return fmt.Errorf("goal not found: %w", err)
	}

	// Remove from active set
	_ = gm.redis.SRem(gm.ctx, gm.keyActiveSet(), id).Err()

	// Remove from priority ZSet
	_ = gm.redis.ZRem(gm.ctx, gm.keyPriorityZSet(), id).Err()

	// Delete the goal key
	if err := gm.redis.Del(gm.ctx, gm.keyGoal(id)).Err(); err != nil {
		return fmt.Errorf("failed to delete goal: %w", err)
	}

	// Publish deleted event
	_ = gm.publishEvent("agi.goal.deleted", *g)

	return nil
}
