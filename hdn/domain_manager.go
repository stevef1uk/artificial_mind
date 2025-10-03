package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// DomainManager handles multiple domains and their management
type DomainManager struct {
	client        *redis.Client
	actionManager *ActionManager
	ttl           time.Duration
}

// DomainData represents a complete domain with methods and actions
type DomainData struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Methods     []*MethodDef      `json:"methods"`
	Actions     []*ActionDef      `json:"actions"`
	Config      DomainConfig      `json:"config"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Tags        []string          `json:"tags"`
	Metadata    map[string]string `json:"metadata"`
}

// DomainSummary represents a summary of a domain
type DomainSummary struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	MethodCount int       `json:"method_count"`
	ActionCount int       `json:"action_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Tags        []string  `json:"tags"`
}

func NewDomainManager(redisAddr string, ttlHours int) *DomainManager {
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	actionManager := NewActionManager(redisAddr, ttlHours)

	return &DomainManager{
		client:        rdb,
		actionManager: actionManager,
		ttl:           time.Duration(ttlHours) * time.Hour,
	}
}

// CreateDomain creates a new domain
func (dm *DomainManager) CreateDomain(name, description string, config DomainConfig, tags []string) error {
	ctx := context.Background()

	// Check if domain already exists
	exists, err := dm.DomainExists(name)
	if err != nil {
		return fmt.Errorf("failed to check domain existence: %v", err)
	}
	if exists {
		return fmt.Errorf("domain %s already exists", name)
	}

	// Create the domain
	domain := &DomainData{
		Name:        name,
		Description: description,
		Methods:     []*MethodDef{},
		Actions:     []*ActionDef{},
		Config:      config,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Tags:        tags,
		Metadata:    make(map[string]string),
	}

	// Store the domain
	domainKey := fmt.Sprintf("domain:%s:full", name)
	domainData, err := json.Marshal(domain)
	if err != nil {
		return fmt.Errorf("failed to marshal domain: %v", err)
	}

	err = dm.client.Set(ctx, domainKey, domainData, dm.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store domain in Redis: %v", err)
	}

	// Create domain info
	err = dm.actionManager.CreateDomain(name, description)
	if err != nil {
		log.Printf("Warning: failed to create domain info: %v", err)
	}

	log.Printf("✅ [DOMAIN] Created domain: %s", name)
	return nil
}

// GetDomain retrieves a complete domain by name
func (dm *DomainManager) GetDomain(name string) (*DomainData, error) {
	ctx := context.Background()
	domainKey := fmt.Sprintf("domain:%s:full", name)

	data, err := dm.client.Get(ctx, domainKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("domain not found: %s", name)
		}
		return nil, fmt.Errorf("failed to get domain from Redis: %v", err)
	}

	var domain DomainData
	err = json.Unmarshal([]byte(data), &domain)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal domain: %v", err)
	}

	return &domain, nil
}

// GetDomainSummary retrieves a summary of a domain
func (dm *DomainManager) GetDomainSummary(name string) (*DomainSummary, error) {
	domain, err := dm.GetDomain(name)
	if err != nil {
		return nil, err
	}

	return &DomainSummary{
		Name:        domain.Name,
		Description: domain.Description,
		MethodCount: len(domain.Methods),
		ActionCount: len(domain.Actions),
		CreatedAt:   domain.CreatedAt,
		UpdatedAt:   domain.UpdatedAt,
		Tags:        domain.Tags,
	}, nil
}

// ListDomains retrieves all available domains
func (dm *DomainManager) ListDomains() ([]*DomainSummary, error) {
	ctx := context.Background()

	keys, err := dm.client.Keys(ctx, "domain:*:full").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get domain keys: %v", err)
	}

	var domains []*DomainSummary
	for _, key := range keys {
		data, err := dm.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var domain DomainData
		if err := json.Unmarshal([]byte(data), &domain); err != nil {
			continue
		}

		domains = append(domains, &DomainSummary{
			Name:        domain.Name,
			Description: domain.Description,
			MethodCount: len(domain.Methods),
			ActionCount: len(domain.Actions),
			CreatedAt:   domain.CreatedAt,
			UpdatedAt:   domain.UpdatedAt,
			Tags:        domain.Tags,
		})
	}

	return domains, nil
}

// AddMethod adds a method to a domain
func (dm *DomainManager) AddMethod(domainName string, method *MethodDef) error {
	domain, err := dm.GetDomain(domainName)
	if err != nil {
		return err
	}

	// Check if method already exists
	for i, existingMethod := range domain.Methods {
		if existingMethod.Task == method.Task {
			// Update existing method
			domain.Methods[i] = method
			log.Printf("✅ [DOMAIN] Updated method %s in domain %s", method.Task, domainName)
		} else {
			// Add new method
			domain.Methods = append(domain.Methods, method)
			log.Printf("✅ [DOMAIN] Added method %s to domain %s", method.Task, domainName)
		}
	}

	// Update timestamp
	domain.UpdatedAt = time.Now()

	// Save domain
	return dm.saveDomain(domain)
}

// AddAction adds an action to a domain
func (dm *DomainManager) AddAction(domainName string, action *ActionDef) error {
	domain, err := dm.GetDomain(domainName)
	if err != nil {
		return err
	}

	// Check if action already exists
	for i, existingAction := range domain.Actions {
		if existingAction.Task == action.Task {
			// Update existing action
			domain.Actions[i] = action
			log.Printf("✅ [DOMAIN] Updated action %s in domain %s", action.Task, domainName)
		} else {
			// Add new action
			domain.Actions = append(domain.Actions, action)
			log.Printf("✅ [DOMAIN] Added action %s to domain %s", action.Task, domainName)
		}
	}

	// Update timestamp
	domain.UpdatedAt = time.Now()

	// Save domain
	return dm.saveDomain(domain)
}

// RemoveMethod removes a method from a domain
func (dm *DomainManager) RemoveMethod(domainName, taskName string) error {
	domain, err := dm.GetDomain(domainName)
	if err != nil {
		return err
	}

	// Find and remove method
	for i, method := range domain.Methods {
		if method.Task == taskName {
			domain.Methods = append(domain.Methods[:i], domain.Methods[i+1:]...)
			log.Printf("✅ [DOMAIN] Removed method %s from domain %s", taskName, domainName)
			break
		}
	}

	// Update timestamp
	domain.UpdatedAt = time.Now()

	// Save domain
	return dm.saveDomain(domain)
}

// RemoveAction removes an action from a domain
func (dm *DomainManager) RemoveAction(domainName, taskName string) error {
	domain, err := dm.GetDomain(domainName)
	if err != nil {
		return err
	}

	// Find and remove action
	for i, action := range domain.Actions {
		if action.Task == taskName {
			domain.Actions = append(domain.Actions[:i], domain.Actions[i+1:]...)
			log.Printf("✅ [DOMAIN] Removed action %s from domain %s", taskName, domainName)
			break
		}
	}

	// Update timestamp
	domain.UpdatedAt = time.Now()

	// Save domain
	return dm.saveDomain(domain)
}

// DomainExists checks if a domain exists
func (dm *DomainManager) DomainExists(name string) (bool, error) {
	ctx := context.Background()
	domainKey := fmt.Sprintf("domain:%s:full", name)

	exists, err := dm.client.Exists(ctx, domainKey).Result()
	if err != nil {
		return false, err
	}

	return exists > 0, nil
}

// DeleteDomain deletes a domain and all its data
func (dm *DomainManager) DeleteDomain(name string) error {
	ctx := context.Background()

	// Delete domain data
	domainKey := fmt.Sprintf("domain:%s:full", name)
	err := dm.client.Del(ctx, domainKey).Err()
	if err != nil {
		return fmt.Errorf("failed to delete domain: %v", err)
	}

	// Delete domain info
	domainInfoKey := fmt.Sprintf("domain:%s", name)
	dm.client.Del(ctx, domainInfoKey)

	// Delete all actions in this domain
	actionPattern := fmt.Sprintf("action:%s:*", name)
	keys, err := dm.client.Keys(ctx, actionPattern).Result()
	if err == nil {
		for _, key := range keys {
			dm.client.Del(ctx, key)
		}
	}

	// Delete all indexes for this domain
	indexPattern := fmt.Sprintf("index:*:%s:*", name)
	indexKeys, err := dm.client.Keys(ctx, indexPattern).Result()
	if err == nil {
		for _, key := range indexKeys {
			dm.client.Del(ctx, key)
		}
	}

	log.Printf("✅ [DOMAIN] Deleted domain: %s", name)
	return nil
}

// saveDomain saves a domain to Redis
func (dm *DomainManager) saveDomain(domain *DomainData) error {
	ctx := context.Background()
	domainKey := fmt.Sprintf("domain:%s:full", domain.Name)

	data, err := json.Marshal(domain)
	if err != nil {
		return fmt.Errorf("failed to marshal domain: %v", err)
	}

	return dm.client.Set(ctx, domainKey, data, dm.ttl).Err()
}

// GetActionManager returns the action manager
func (dm *DomainManager) GetActionManager() *ActionManager {
	return dm.actionManager
}
