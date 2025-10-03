package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// ActionManager handles dynamic action creation and storage in Redis
type ActionManager struct {
	client *redis.Client
	ttl    time.Duration
}

// DynamicAction represents an action that can be created and stored dynamically
type DynamicAction struct {
	ID            string            `json:"id"`
	Task          string            `json:"task"`
	Preconditions []string          `json:"preconditions"`
	Effects       []string          `json:"effects"`
	TaskType      string            `json:"task_type"`
	Description   string            `json:"description"`
	Code          string            `json:"code,omitempty"`
	Language      string            `json:"language,omitempty"`
	Context       map[string]string `json:"context"`
	CreatedAt     time.Time         `json:"created_at"`
	Domain        string            `json:"domain"`
	Tags          []string          `json:"tags"`
}

// DomainInfo represents information about a domain
type DomainInfo struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	ActionCount int       `json:"action_count"`
	MethodCount int       `json:"method_count"`
}

func NewActionManager(redisAddr string, ttlHours int) *ActionManager {
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	return &ActionManager{
		client: rdb,
		ttl:    time.Duration(ttlHours) * time.Hour,
	}
}

// CreateAction creates a new dynamic action and stores it in Redis
func (am *ActionManager) CreateAction(action *DynamicAction) error {
	ctx := context.Background()

	// Generate ID if not provided
	if action.ID == "" {
		action.ID = fmt.Sprintf("action_%d_%s", time.Now().UnixNano(), action.Task)
	}

	// Set creation time
	if action.CreatedAt.IsZero() {
		action.CreatedAt = time.Now()
	}

	// Set default domain if not provided
	if action.Domain == "" {
		action.Domain = "default"
	}

	// Store the action
	actionKey := fmt.Sprintf("action:%s:%s", action.Domain, action.ID)
	actionData, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("failed to marshal action: %v", err)
	}

	err = am.client.Set(ctx, actionKey, actionData, am.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store action in Redis: %v", err)
	}

	// Update domain info
	err = am.updateDomainInfo(ctx, action.Domain, "action")
	if err != nil {
		log.Printf("Warning: failed to update domain info: %v", err)
	}

	// Create searchable indexes
	err = am.createActionIndexes(ctx, action)
	if err != nil {
		log.Printf("Warning: failed to create action indexes: %v", err)
	}

	log.Printf("✅ [ACTION] Created action %s in domain %s", action.Task, action.Domain)
	return nil
}

// GetAction retrieves an action by ID and domain
func (am *ActionManager) GetAction(domain, id string) (*DynamicAction, error) {
	ctx := context.Background()
	actionKey := fmt.Sprintf("action:%s:%s", domain, id)

	data, err := am.client.Get(ctx, actionKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("action not found: %s in domain %s", id, domain)
		}
		return nil, fmt.Errorf("failed to get action from Redis: %v", err)
	}

	var action DynamicAction
	err = json.Unmarshal([]byte(data), &action)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal action: %v", err)
	}

	return &action, nil
}

// GetActionsByDomain retrieves all actions for a specific domain
func (am *ActionManager) GetActionsByDomain(domain string) ([]*DynamicAction, error) {
	ctx := context.Background()
	pattern := fmt.Sprintf("action:%s:*", domain)

	keys, err := am.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get action keys: %v", err)
	}

	var actions []*DynamicAction
	for _, key := range keys {
		data, err := am.client.Get(ctx, key).Result()
		if err != nil {
			continue // Skip errors, continue with other actions
		}

		var action DynamicAction
		if err := json.Unmarshal([]byte(data), &action); err != nil {
			continue // Skip invalid JSON
		}

		actions = append(actions, &action)
	}

	return actions, nil
}

// SearchActions searches for actions by various criteria
func (am *ActionManager) SearchActions(domain, query string, taskType string, tags []string) ([]*DynamicAction, error) {
	var actions []*DynamicAction

	// Get all actions for the domain
	domainActions, err := am.GetActionsByDomain(domain)
	if err != nil {
		return nil, err
	}

	// Filter by criteria
	for _, action := range domainActions {
		matches := true

		// Filter by query
		if query != "" {
			if !containsSubstring(action.Task, query) &&
				!containsSubstring(action.Description, query) {
				matches = false
			}
		}

		// Filter by task type
		if taskType != "" && action.TaskType != taskType {
			matches = false
		}

		// Filter by tags
		if len(tags) > 0 {
			tagMatch := false
			for _, tag := range tags {
				for _, actionTag := range action.Tags {
					if actionTag == tag {
						tagMatch = true
						break
					}
				}
			}
			if !tagMatch {
				matches = false
			}
		}

		if matches {
			actions = append(actions, action)
		}
	}

	return actions, nil
}

// DeleteAction removes an action by ID and domain
func (am *ActionManager) DeleteAction(domain, id string) error {
	ctx := context.Background()

	// Get the action first to clean up indexes
	action, err := am.GetAction(domain, id)
	if err != nil {
		return err
	}

	// Delete the action
	actionKey := fmt.Sprintf("action:%s:%s", domain, id)
	err = am.client.Del(ctx, actionKey).Err()
	if err != nil {
		return fmt.Errorf("failed to delete action: %v", err)
	}

	// Clean up indexes
	err = am.cleanupActionIndexes(ctx, action)
	if err != nil {
		log.Printf("Warning: failed to cleanup action indexes: %v", err)
	}

	// Update domain info
	err = am.updateDomainInfo(ctx, domain, "action_deleted")
	if err != nil {
		log.Printf("Warning: failed to update domain info: %v", err)
	}

	log.Printf("✅ [ACTION] Deleted action %s from domain %s", action.Task, domain)
	return nil
}

// CreateDomain creates a new domain
func (am *ActionManager) CreateDomain(name, description string) error {
	ctx := context.Background()

	domainInfo := DomainInfo{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		ActionCount: 0,
		MethodCount: 0,
	}

	domainKey := fmt.Sprintf("domain:%s", name)
	domainData, err := json.Marshal(domainInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal domain info: %v", err)
	}

	err = am.client.Set(ctx, domainKey, domainData, am.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store domain info: %v", err)
	}

	log.Printf("✅ [DOMAIN] Created domain: %s", name)
	return nil
}

// GetDomains retrieves all available domains
func (am *ActionManager) GetDomains() ([]*DomainInfo, error) {
	ctx := context.Background()

	keys, err := am.client.Keys(ctx, "domain:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get domain keys: %v", err)
	}

	var domains []*DomainInfo
	for _, key := range keys {
		data, err := am.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var domain DomainInfo
		if err := json.Unmarshal([]byte(data), &domain); err != nil {
			continue
		}

		domains = append(domains, &domain)
	}

	return domains, nil
}

// ConvertToLegacyAction converts a DynamicAction to the legacy ActionDef format
func (am *ActionManager) ConvertToLegacyAction(action *DynamicAction) *ActionDef {
	return &ActionDef{
		Task:          action.Task,
		Preconditions: action.Preconditions,
		Effects:       action.Effects,
	}
}

// updateDomainInfo updates the domain information
func (am *ActionManager) updateDomainInfo(ctx context.Context, domain, actionType string) error {
	domainKey := fmt.Sprintf("domain:%s", domain)

	// Get current domain info
	data, err := am.client.Get(ctx, domainKey).Result()
	if err != nil {
		if err == redis.Nil {
			// Domain doesn't exist, create it
			domainInfo := DomainInfo{
				Name:        domain,
				Description: "Auto-created domain",
				CreatedAt:   time.Now(),
				ActionCount: 0,
				MethodCount: 0,
			}

			if actionType == "action" {
				domainInfo.ActionCount = 1
			} else if actionType == "method" {
				domainInfo.MethodCount = 1
			}

			domainData, _ := json.Marshal(domainInfo)
			return am.client.Set(ctx, domainKey, domainData, am.ttl).Err()
		}
		return err
	}

	var domainInfo DomainInfo
	if err := json.Unmarshal([]byte(data), &domainInfo); err != nil {
		return err
	}

	// Update counts
	if actionType == "action" {
		domainInfo.ActionCount++
	} else if actionType == "action_deleted" {
		if domainInfo.ActionCount > 0 {
			domainInfo.ActionCount--
		}
	} else if actionType == "method" {
		domainInfo.MethodCount++
	} else if actionType == "method_deleted" {
		if domainInfo.MethodCount > 0 {
			domainInfo.MethodCount--
		}
	}

	// Save updated domain info
	domainData, _ := json.Marshal(domainInfo)
	return am.client.Set(ctx, domainKey, domainData, am.ttl).Err()
}

// createActionIndexes creates searchable indexes for the action
func (am *ActionManager) createActionIndexes(ctx context.Context, action *DynamicAction) error {
	// Index by task name
	taskKey := fmt.Sprintf("index:task:%s:%s", action.Domain, action.Task)
	err := am.client.SAdd(ctx, taskKey, action.ID).Err()
	if err != nil {
		return err
	}
	am.client.Expire(ctx, taskKey, am.ttl)

	// Index by task type
	typeKey := fmt.Sprintf("index:type:%s:%s", action.Domain, action.TaskType)
	err = am.client.SAdd(ctx, typeKey, action.ID).Err()
	if err != nil {
		return err
	}
	am.client.Expire(ctx, typeKey, am.ttl)

	// Index by tags
	for _, tag := range action.Tags {
		tagKey := fmt.Sprintf("index:tag:%s:%s", action.Domain, tag)
		err = am.client.SAdd(ctx, tagKey, action.ID).Err()
		if err != nil {
			return err
		}
		am.client.Expire(ctx, tagKey, am.ttl)
	}

	return nil
}

// cleanupActionIndexes removes indexes for deleted action
func (am *ActionManager) cleanupActionIndexes(ctx context.Context, action *DynamicAction) error {
	// Remove from task index
	taskKey := fmt.Sprintf("index:task:%s:%s", action.Domain, action.Task)
	am.client.SRem(ctx, taskKey, action.ID)

	// Remove from type index
	typeKey := fmt.Sprintf("index:type:%s:%s", action.Domain, action.TaskType)
	am.client.SRem(ctx, typeKey, action.ID)

	// Remove from tag indexes
	for _, tag := range action.Tags {
		tagKey := fmt.Sprintf("index:tag:%s:%s", action.Domain, tag)
		am.client.SRem(ctx, tagKey, action.ID)
	}

	return nil
}
