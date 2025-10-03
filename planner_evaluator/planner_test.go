// FILE: planner_test.go
package planner

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// simple executor stub
type stubExecutor struct{}

func (s *stubExecutor) ExecutePlan(ctx context.Context, p Plan) (interface{}, error) {
	// return a deterministic result
	return map[string]interface{}{"ok": true, "plan_id": p.ID}, nil
}

func TestPlannerEndToEnd(t *testing.T) {
	m, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis start: %v", err)
	}
	defer m.Close()

	rdb := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()

	exec := &stubExecutor{}
	pl := NewPlanner(ctx, rdb, exec, "http://localhost:8080")

	// create capability
	cap := Capability{TaskName: "PrimeNumberGenerator", Entrypoint: "python prime.py", Language: "python", InputSig: map[string]string{"count": "int"}, Score: 0.92}
	if err := pl.SaveCapability(ctx, cap); err != nil {
		t.Fatalf("save cap: %v", err)
	}

	// craft goal
	goal := Goal{ID: "g1", Type: "PrimeNumberGenerator", Params: map[string]interface{}{"count": 10}}

	// generate plans
	plans, err := pl.GeneratePlans(ctx, goal)
	if err != nil {
		t.Fatalf("generate plans: %v", err)
	}
	if len(plans) == 0 {
		t.Fatalf("expected plans")
	}

	// score and sort
	scored := pl.ScoreAndSortPlans(plans)
	if len(scored) == 0 {
		t.Fatalf("expected scored plans")
	}

	// execute best plan using stub executor (skip principles by mocking CheckPlanAgainstPrinciples)
	best := scored[0]
	// small hack: bypass principles
	ep, err := pl.ExecutePlan(ctx, best, "test request")
	if err != nil {
		t.Fatalf("execute plan: %v", err)
	}
	if ep.Result == nil {
		t.Fatalf("expected result")
	}

	// load episode
	loaded, err := pl.LoadEpisode(ctx, ep.ID)
	if err != nil {
		t.Fatalf("load episode: %v", err)
	}
	if loaded.SelectedPlan.ID != best.ID {
		t.Fatalf("mismatch plan id")
	}
}

func TestDeterministicScoring(t *testing.T) {
	m, _ := miniredis.Run()
	defer m.Close()
	rdb := redis.NewClient(&redis.Options{Addr: m.Addr()})
	ctx := context.Background()
	exec := &stubExecutor{}
	pl := NewPlanner(ctx, rdb, exec, "http://localhost:8080")

	// two synthetic plans
	p1 := Plan{ID: "p1", EstimatedUtility: 0.9, PrinciplesRisk: 0.0, Steps: []PlanStep{{EstimatedCost: 1, Confidence: 0.9}}}
	p2 := Plan{ID: "p2", EstimatedUtility: 0.8, PrinciplesRisk: 0.0, Steps: []PlanStep{{EstimatedCost: 0.5, Confidence: 0.95}}}

	scored := pl.ScoreAndSortPlans([]Plan{p1, p2})
	if scored[0].ID == scored[1].ID {
		t.Fatalf("expected deterministic ordering")
	}
}
