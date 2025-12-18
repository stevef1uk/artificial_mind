package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// CodeStorage handles storing and retrieving generated code in Redis
type CodeStorage struct {
	client *redis.Client
	ttl    time.Duration
}

// GeneratedCode represents a piece of generated code with metadata
type GeneratedCode struct {
	ID          string            `json:"id"`
	TaskName    string            `json:"task_name"`
	Description string            `json:"description"`
	Language    string            `json:"language"`
	Code        string            `json:"code"`
	Context     map[string]string `json:"context"`
	CreatedAt   time.Time         `json:"created_at"`
	Tags        []string          `json:"tags"`
	Executable  bool              `json:"executable"`
}

// CodeSearchResult represents a search result for code
type CodeSearchResult struct {
	Code    *GeneratedCode `json:"code"`
	Score   float64        `json:"score"`
	Matched string         `json:"matched"`
}

func NewCodeStorage(redisAddr string, ttlHours int) *CodeStorage {
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	return &CodeStorage{
		client: rdb,
		ttl:    time.Duration(ttlHours) * time.Hour,
	}
}

// StoreCode stores generated code in Redis
func (cs *CodeStorage) StoreCode(code *GeneratedCode) error {
	ctx := context.Background()

	// Check for duplicate capabilities before storing
	// Normalize task name and description for comparison
	taskNameNorm := strings.ToLower(strings.TrimSpace(code.TaskName))
	descNorm := strings.ToLower(strings.TrimSpace(code.Description))

	// Skip trivial repetitive tasks
	trivialPatterns := []string{
		"create example.txt",
		"create example",
		"list directory and create",
		"list current directory",
	}
	for _, pattern := range trivialPatterns {
		if strings.Contains(descNorm, pattern) || strings.Contains(taskNameNorm, pattern) {
			log.Printf("🚫 [CODE-STORAGE] Skipping storage of trivial task: %s", code.TaskName)
			return nil // Return nil to not error, just skip
		}
	}

	// Check for existing similar code (same task name + language)
	existingKey := fmt.Sprintf("index:task:%s", code.TaskName)
	existingIDs, err := cs.client.SMembers(ctx, existingKey).Result()
	if err == nil && len(existingIDs) > 0 {
		// Check if any existing code has similar description (normalized)
		for _, existingID := range existingIDs {
			existingCode, err := cs.GetCode(existingID)
			if err != nil {
				continue
			}
			// If same task name, language, and similar description, skip storing duplicate
			if existingCode.Language == code.Language {
				existingDescNorm := strings.ToLower(strings.TrimSpace(existingCode.Description))
				// If descriptions are very similar (80% overlap), skip
				if cs.similarity(existingDescNorm, descNorm) > 0.8 {
					log.Printf("🚫 [CODE-STORAGE] Skipping duplicate capability: %s (similar to existing %s)", code.TaskName, existingID)
					return nil // Skip duplicate
				}
			}
		}
	}

	// Generate unique ID if not provided
	if code.ID == "" {
		code.ID = fmt.Sprintf("code_%d", time.Now().UnixNano())
	}

	// Set creation time if not set
	if code.CreatedAt.IsZero() {
		code.CreatedAt = time.Now()
	}

	// Store the code object
	codeKey := fmt.Sprintf("code:%s", code.ID)
	codeData, err := json.Marshal(code)
	if err != nil {
		return fmt.Errorf("failed to marshal code: %v", err)
	}

	err = cs.client.Set(ctx, codeKey, codeData, cs.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store code in Redis: %v", err)
	}

	// Create searchable indexes
	err = cs.createIndexes(ctx, code)
	if err != nil {
		return fmt.Errorf("failed to create indexes: %v", err)
	}

	return nil
}

// similarity calculates a simple similarity score between two strings (0.0 to 1.0)
func (cs *CodeStorage) similarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Simple word overlap similarity
	words1 := strings.Fields(s1)
	words2 := strings.Fields(s2)
	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	// Count common words
	common := 0
	for _, w1 := range words1 {
		for _, w2 := range words2 {
			if w1 == w2 {
				common++
				break
			}
		}
	}

	// Return ratio of common words to total unique words
	maxLen := len(words1)
	if len(words2) > maxLen {
		maxLen = len(words2)
	}
	if maxLen == 0 {
		return 0.0
	}
	return float64(common) / float64(maxLen)
}

// GetCode retrieves code by ID
func (cs *CodeStorage) GetCode(id string) (*GeneratedCode, error) {
	ctx := context.Background()
	codeKey := fmt.Sprintf("code:%s", id)

	data, err := cs.client.Get(ctx, codeKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("code not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get code from Redis: %v", err)
	}

	var code GeneratedCode
	err = json.Unmarshal([]byte(data), &code)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal code: %v", err)
	}

	return &code, nil
}

// SearchCode searches for code by various criteria
func (cs *CodeStorage) SearchCode(query string, language string, tags []string) ([]CodeSearchResult, error) {
	ctx := context.Background()
	var results []CodeSearchResult

	// Search by task name
	if query != "" {
		taskResults, err := cs.searchByTaskName(ctx, query)
		if err != nil {
			return nil, err
		}
		results = append(results, taskResults...)
	}

	// Search by language
	if language != "" {
		langResults, err := cs.searchByLanguage(ctx, language)
		if err != nil {
			return nil, err
		}
		results = append(results, langResults...)
	}

	// Search by tags
	if len(tags) > 0 {
		tagResults, err := cs.searchByTags(ctx, tags)
		if err != nil {
			return nil, err
		}
		results = append(results, tagResults...)
	}

	// Remove duplicates and sort by score
	results = cs.deduplicateAndSort(results)

	return results, nil
}

// ListAllCode returns all stored code
func (cs *CodeStorage) ListAllCode() ([]*GeneratedCode, error) {
	ctx := context.Background()

	// Get all code keys
	keys, err := cs.client.Keys(ctx, "code:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get code keys: %v", err)
	}

	var codes []*GeneratedCode
	for _, key := range keys {
		data, err := cs.client.Get(ctx, key).Result()
		if err != nil {
			continue // Skip errors, continue with other codes
		}

		var code GeneratedCode
		if err := json.Unmarshal([]byte(data), &code); err != nil {
			continue // Skip invalid JSON
		}

		codes = append(codes, &code)
	}

	return codes, nil
}

// DeleteCode removes code by ID
func (cs *CodeStorage) DeleteCode(id string) error {
	ctx := context.Background()

	// Get the code first to clean up indexes
	code, err := cs.GetCode(id)
	if err != nil {
		return err
	}

	// Delete the code
	codeKey := fmt.Sprintf("code:%s", id)
	err = cs.client.Del(ctx, codeKey).Err()
	if err != nil {
		return fmt.Errorf("failed to delete code: %v", err)
	}

	// Clean up indexes
	err = cs.cleanupIndexes(ctx, code)
	if err != nil {
		return fmt.Errorf("failed to cleanup indexes: %v", err)
	}

	return nil
}

// createIndexes creates searchable indexes for the code
func (cs *CodeStorage) createIndexes(ctx context.Context, code *GeneratedCode) error {
	// Index by task name
	taskKey := fmt.Sprintf("index:task:%s", code.TaskName)
	err := cs.client.SAdd(ctx, taskKey, code.ID).Err()
	if err != nil {
		return err
	}
	cs.client.Expire(ctx, taskKey, cs.ttl)

	// Index by language
	langKey := fmt.Sprintf("index:language:%s", code.Language)
	err = cs.client.SAdd(ctx, langKey, code.ID).Err()
	if err != nil {
		return err
	}
	cs.client.Expire(ctx, langKey, cs.ttl)

	// Index by tags
	for _, tag := range code.Tags {
		tagKey := fmt.Sprintf("index:tag:%s", tag)
		err = cs.client.SAdd(ctx, tagKey, code.ID).Err()
		if err != nil {
			return err
		}
		cs.client.Expire(ctx, tagKey, cs.ttl)
	}

	// Index by creation date (for time-based queries)
	dateKey := fmt.Sprintf("index:date:%s", code.CreatedAt.Format("2006-01-02"))
	err = cs.client.SAdd(ctx, dateKey, code.ID).Err()
	if err != nil {
		return err
	}
	cs.client.Expire(ctx, dateKey, cs.ttl)

	return nil
}

// searchByTaskName searches for code by task name
func (cs *CodeStorage) searchByTaskName(ctx context.Context, query string) ([]CodeSearchResult, error) {
	// Simple pattern matching - in production, you might want to use Redis Search
	keys, err := cs.client.Keys(ctx, "index:task:*").Result()
	if err != nil {
		return nil, err
	}

	var results []CodeSearchResult
	for _, key := range keys {
		if containsSubstring(key, query) {
			ids, err := cs.client.SMembers(ctx, key).Result()
			if err != nil {
				continue
			}

			for _, id := range ids {
				code, err := cs.GetCode(id)
				if err != nil {
					continue
				}

				results = append(results, CodeSearchResult{
					Code:    code,
					Score:   1.0,
					Matched: "task_name",
				})
			}
		}
	}

	return results, nil
}

// searchByLanguage searches for code by language
func (cs *CodeStorage) searchByLanguage(ctx context.Context, language string) ([]CodeSearchResult, error) {
	langKey := fmt.Sprintf("index:language:%s", language)
	ids, err := cs.client.SMembers(ctx, langKey).Result()
	if err != nil {
		return nil, err
	}

	var results []CodeSearchResult
	for _, id := range ids {
		code, err := cs.GetCode(id)
		if err != nil {
			continue
		}

		results = append(results, CodeSearchResult{
			Code:    code,
			Score:   1.0,
			Matched: "language",
		})
	}

	return results, nil
}

// searchByTags searches for code by tags
func (cs *CodeStorage) searchByTags(ctx context.Context, tags []string) ([]CodeSearchResult, error) {
	var allIds []string

	for _, tag := range tags {
		tagKey := fmt.Sprintf("index:tag:%s", tag)
		ids, err := cs.client.SMembers(ctx, tagKey).Result()
		if err != nil {
			continue
		}
		allIds = append(allIds, ids...)
	}

	// Count occurrences for scoring
	idCounts := make(map[string]int)
	for _, id := range allIds {
		idCounts[id]++
	}

	var results []CodeSearchResult
	for id, count := range idCounts {
		code, err := cs.GetCode(id)
		if err != nil {
			continue
		}

		score := float64(count) / float64(len(tags))
		results = append(results, CodeSearchResult{
			Code:    code,
			Score:   score,
			Matched: "tags",
		})
	}

	return results, nil
}

// cleanupIndexes removes indexes for deleted code
func (cs *CodeStorage) cleanupIndexes(ctx context.Context, code *GeneratedCode) error {
	// Remove from task index
	taskKey := fmt.Sprintf("index:task:%s", code.TaskName)
	cs.client.SRem(ctx, taskKey, code.ID)

	// Remove from language index
	langKey := fmt.Sprintf("index:language:%s", code.Language)
	cs.client.SRem(ctx, langKey, code.ID)

	// Remove from tag indexes
	for _, tag := range code.Tags {
		tagKey := fmt.Sprintf("index:tag:%s", tag)
		cs.client.SRem(ctx, tagKey, code.ID)
	}

	// Remove from date index
	dateKey := fmt.Sprintf("index:date:%s", code.CreatedAt.Format("2006-01-02"))
	cs.client.SRem(ctx, dateKey, code.ID)

	return nil
}

// deduplicateAndSort removes duplicates and sorts by score
func (cs *CodeStorage) deduplicateAndSort(results []CodeSearchResult) []CodeSearchResult {
	seen := make(map[string]bool)
	var unique []CodeSearchResult

	for _, result := range results {
		if !seen[result.Code.ID] {
			seen[result.Code.ID] = true
			unique = append(unique, result)
		}
	}

	// Simple sort by score (descending)
	for i := 0; i < len(unique)-1; i++ {
		for j := i + 1; j < len(unique); j++ {
			if unique[i].Score < unique[j].Score {
				unique[i], unique[j] = unique[j], unique[i]
			}
		}
	}

	return unique
}

// contains checks if a string contains a substring (case-insensitive)
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && containsSubstring(s[1:], substr)
}
