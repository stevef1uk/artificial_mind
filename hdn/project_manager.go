package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Project represents a long-lived multi-step goal tracked across sessions
type Project struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Status      string            `json:"status"` // active|paused|archived|completed
	Owner       string            `json:"owner,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	NextAction  string            `json:"next_action,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type ProjectManager struct {
	client *redis.Client
	ttl    time.Duration
}

func NewProjectManager(redisAddr string, ttlHours int) *ProjectManager {
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	return &ProjectManager{client: rdb, ttl: time.Duration(ttlHours) * time.Hour}
}

func (pm *ProjectManager) projectKey(id string) string {
	return fmt.Sprintf("project:%s", id)
}

func (pm *ProjectManager) indexStatusKey(status string) string {
	return fmt.Sprintf("projects:by_status:%s", status)
}

func (pm *ProjectManager) allProjectsKey() string {
	return "projects:all"
}

func (pm *ProjectManager) projectWorkflowsKey(projectID string) string {
	return fmt.Sprintf("project:%s:workflows", projectID)
}

func (pm *ProjectManager) checkpointsKey(projectID string) string {
	return fmt.Sprintf("project:%s:checkpoints", projectID)
}

func (pm *ProjectManager) CreateProject(p *Project) (*Project, error) {
	ctx := context.Background()
	if p.ID == "" {
		p.ID = fmt.Sprintf("proj_%d", time.Now().UnixNano())
	}
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.Status == "" {
		p.Status = "active"
	}

	data, _ := json.Marshal(p)
	if err := pm.client.Set(ctx, pm.projectKey(p.ID), data, pm.ttl).Err(); err != nil {
		return nil, err
	}
	// indexes
	_ = pm.client.SAdd(ctx, pm.allProjectsKey(), p.ID).Err()
	_ = pm.client.SAdd(ctx, pm.indexStatusKey(p.Status), p.ID).Err()
	return p, nil
}

func (pm *ProjectManager) GetProject(id string) (*Project, error) {
	ctx := context.Background()
	val, err := pm.client.Get(ctx, pm.projectKey(id)).Result()
	if err != nil {
		return nil, err
	}
	var p Project
	if err := json.Unmarshal([]byte(val), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (pm *ProjectManager) ListProjects() ([]*Project, error) {
	ctx := context.Background()
	ids, err := pm.client.SMembers(ctx, pm.allProjectsKey()).Result()
	if err != nil {
		return nil, err
	}
	var out []*Project
	for _, id := range ids {
		p, err := pm.GetProject(id)
		if err == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func (pm *ProjectManager) UpdateProject(id string, update func(*Project) error) (*Project, error) {
	p, err := pm.GetProject(id)
	if err != nil {
		return nil, err
	}
	oldStatus := p.Status
	if err := update(p); err != nil {
		return nil, err
	}
	p.UpdatedAt = time.Now()
	data, _ := json.Marshal(p)
	ctx := context.Background()
	if err := pm.client.Set(ctx, pm.projectKey(id), data, pm.ttl).Err(); err != nil {
		return nil, err
	}
	if p.Status != oldStatus {
		_ = pm.client.SRem(ctx, pm.indexStatusKey(oldStatus), id).Err()
		_ = pm.client.SAdd(ctx, pm.indexStatusKey(p.Status), id).Err()
	}
	return p, nil
}

// ---- Workflow Associations ----

func (pm *ProjectManager) LinkWorkflow(projectID, workflowID string) error {
	ctx := context.Background()
	if err := pm.client.SAdd(ctx, pm.projectWorkflowsKey(projectID), workflowID).Err(); err != nil {
		return err
	}
	// reverse mapping for quick lookup
	if err := pm.client.Set(ctx, fmt.Sprintf("workflow_project:%s", workflowID), projectID, pm.ttl).Err(); err != nil {
		return err
	}
	return nil
}

// ---- Checkpoints ----

type ProjectCheckpoint struct {
	ID         string                 `json:"id"`
	Time       time.Time              `json:"time"`
	Summary    string                 `json:"summary"`
	NextAction string                 `json:"next_action,omitempty"`
	Context    map[string]interface{} `json:"context,omitempty"`
}

func (pm *ProjectManager) AddCheckpoint(projectID string, cp *ProjectCheckpoint) (*ProjectCheckpoint, error) {
	ctx := context.Background()
	if cp.ID == "" {
		cp.ID = fmt.Sprintf("cp_%d", time.Now().UnixNano())
	}
	if cp.Time.IsZero() {
		cp.Time = time.Now()
	}
	data, _ := json.Marshal(cp)
	if err := pm.client.RPush(ctx, pm.checkpointsKey(projectID), string(data)).Err(); err != nil {
		return nil, err
	}
	// bump project TTL by touching main key
	_ = pm.client.Expire(ctx, pm.projectKey(projectID), pm.ttl).Err()
	return cp, nil
}

func (pm *ProjectManager) ListCheckpoints(projectID string, limit int) ([]*ProjectCheckpoint, error) {
	ctx := context.Background()
	total, err := pm.client.LLen(ctx, pm.checkpointsKey(projectID)).Result()
	if err != nil {
		return nil, err
	}
	start := int64(0)
	end := total - 1
	if limit > 0 && int64(limit) < total {
		start = total - int64(limit)
		end = total - 1
	}
	if total == 0 {
		return []*ProjectCheckpoint{}, nil
	}
	items, err := pm.client.LRange(ctx, pm.checkpointsKey(projectID), start, end).Result()
	if err != nil {
		return nil, err
	}
	out := make([]*ProjectCheckpoint, 0, len(items))
	for _, s := range items {
		var cp ProjectCheckpoint
		if err := json.Unmarshal([]byte(s), &cp); err == nil {
			out = append(out, &cp)
		}
	}
	return out, nil
}

// ListWorkflowIDs returns workflow IDs linked to a project
func (pm *ProjectManager) ListWorkflowIDs(projectID string) ([]string, error) {
	ctx := context.Background()
	ids, err := pm.client.SMembers(ctx, pm.projectWorkflowsKey(projectID)).Result()
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// DeleteProject removes a project and associated indexes/artifacts
func (pm *ProjectManager) DeleteProject(id string) error {
	ctx := context.Background()
	// Load project to know status for index cleanup
	p, _ := pm.GetProject(id)

	// Remove main key
	_ = pm.client.Del(ctx, pm.projectKey(id)).Err()
	// Remove from global index
	_ = pm.client.SRem(ctx, pm.allProjectsKey(), id).Err()
	// Remove from status index
	if p != nil && p.Status != "" {
		_ = pm.client.SRem(ctx, pm.indexStatusKey(p.Status), id).Err()
	}
	// Remove workflow associations and reverse mappings
	wids, _ := pm.ListWorkflowIDs(id)
	for _, wid := range wids {
		_ = pm.client.Del(ctx, fmt.Sprintf("workflow_project:%s", wid)).Err()
	}
	_ = pm.client.Del(ctx, pm.projectWorkflowsKey(id)).Err()
	// Remove checkpoints list
	_ = pm.client.Del(ctx, pm.checkpointsKey(id)).Err()
	return nil
}
