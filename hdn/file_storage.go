package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// FileStorage handles storing and retrieving generated files in Redis
type FileStorage struct {
	client *redis.Client
	ttl    time.Duration
}

// StoredFile represents a file stored in Redis with metadata
type StoredFile struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	Content     []byte    `json:"content"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	WorkflowID  string    `json:"workflow_id"`
	StepID      string    `json:"step_id"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// FileMetadata represents just the metadata without content
type FileMetadata struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	WorkflowID  string    `json:"workflow_id"`
	StepID      string    `json:"step_id"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func NewFileStorage(redisAddr string, ttlHours int) *FileStorage {
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	return &FileStorage{
		client: rdb,
		ttl:    time.Duration(ttlHours) * time.Hour,
	}
}

// StoreFile stores a file in Redis with automatic expiration
func (fs *FileStorage) StoreFile(file *StoredFile) error {
	ctx := context.Background()

	// Generate unique ID if not provided
	if file.ID == "" {
		file.ID = fmt.Sprintf("file_%d", time.Now().UnixNano())
	}

	// Set creation time if not set
	if file.CreatedAt.IsZero() {
		file.CreatedAt = time.Now()
	}

	// Set expiration time
	file.ExpiresAt = file.CreatedAt.Add(fs.ttl)

	// Store the file content
	fileKey := fmt.Sprintf("file:content:%s", file.ID)
	err := fs.client.Set(ctx, fileKey, file.Content, fs.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store file content in Redis: %v", err)
	}

	// Store the file metadata
	metadataKey := fmt.Sprintf("file:metadata:%s", file.ID)
	metadata := FileMetadata{
		ID:          file.ID,
		Filename:    file.Filename,
		ContentType: file.ContentType,
		Size:        file.Size,
		WorkflowID:  file.WorkflowID,
		StepID:      file.StepID,
		CreatedAt:   file.CreatedAt,
		ExpiresAt:   file.ExpiresAt,
	}

	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal file metadata: %v", err)
	}

	err = fs.client.Set(ctx, metadataKey, metadataData, fs.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to store file metadata in Redis: %v", err)
	}

	// Create indexes for easy lookup
	err = fs.createIndexes(ctx, &metadata)
	if err != nil {
		log.Printf("Warning: failed to create file indexes: %v", err)
	}

	log.Printf("📁 [FILE] Stored file %s (%d bytes) with TTL %v", file.Filename, file.Size, fs.ttl)
	return nil
}

// GetFile retrieves a file by ID
func (fs *FileStorage) GetFile(id string) (*StoredFile, error) {
	ctx := context.Background()

	// Get metadata first
	metadataKey := fmt.Sprintf("file:metadata:%s", id)
	metadataData, err := fs.client.Get(ctx, metadataKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("file not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get file metadata from Redis: %v", err)
	}

	var metadata FileMetadata
	err = json.Unmarshal([]byte(metadataData), &metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal file metadata: %v", err)
	}

	// Get file content
	fileKey := fmt.Sprintf("file:content:%s", id)
	content, err := fs.client.Get(ctx, fileKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("file content not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get file content from Redis: %v", err)
	}

	return &StoredFile{
		ID:          metadata.ID,
		Filename:    metadata.Filename,
		Content:     content,
		ContentType: metadata.ContentType,
		Size:        metadata.Size,
		WorkflowID:  metadata.WorkflowID,
		StepID:      metadata.StepID,
		CreatedAt:   metadata.CreatedAt,
		ExpiresAt:   metadata.ExpiresAt,
	}, nil
}

// GetFileByFilename retrieves a file by filename (latest if multiple)
func (fs *FileStorage) GetFileByFilename(filename string) (*StoredFile, error) {
	ctx := context.Background()

	// Get the latest file ID for this filename
	indexKey := fmt.Sprintf("file:by_name:%s", filename)
	fileID, err := fs.client.Get(ctx, indexKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("file not found: %s", filename)
		}
		return nil, fmt.Errorf("failed to get file ID from Redis: %v", err)
	}

	return fs.GetFile(fileID)
}

// GetFilesByWorkflow retrieves all files for a specific workflow
func (fs *FileStorage) GetFilesByWorkflow(workflowID string) ([]FileMetadata, error) {
	ctx := context.Background()

	// Get all file IDs for this workflow
	indexKey := fmt.Sprintf("file:by_workflow:%s", workflowID)
	fileIDs, err := fs.client.SMembers(ctx, indexKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get file IDs for workflow: %v", err)
	}

	var files []FileMetadata
	for _, fileID := range fileIDs {
		metadataKey := fmt.Sprintf("file:metadata:%s", fileID)
		metadataData, err := fs.client.Get(ctx, metadataKey).Result()
		if err != nil {
			log.Printf("Warning: failed to get metadata for file %s: %v", fileID, err)
			continue
		}

		var metadata FileMetadata
		err = json.Unmarshal([]byte(metadataData), &metadata)
		if err != nil {
			log.Printf("Warning: failed to unmarshal metadata for file %s: %v", fileID, err)
			continue
		}

		files = append(files, metadata)
	}

	return files, nil
}

// DeleteFile deletes a file by ID
func (fs *FileStorage) DeleteFile(id string) error {
	ctx := context.Background()

	// Get metadata first to clean up indexes
	metadataKey := fmt.Sprintf("file:metadata:%s", id)
	metadataData, err := fs.client.Get(ctx, metadataKey).Result()
	if err == nil {
		var metadata FileMetadata
		if json.Unmarshal([]byte(metadataData), &metadata) == nil {
			fs.cleanupIndexes(ctx, &metadata)
		}
	}

	// Delete file content and metadata
	fileKey := fmt.Sprintf("file:content:%s", id)
	fs.client.Del(ctx, fileKey, metadataKey)

	log.Printf("🗑️ [FILE] Deleted file %s", id)
	return nil
}

// createIndexes creates searchable indexes for the file
func (fs *FileStorage) createIndexes(ctx context.Context, metadata *FileMetadata) error {
	// Index by filename (latest wins)
	filenameIndexKey := fmt.Sprintf("file:by_name:%s", metadata.Filename)
	err := fs.client.Set(ctx, filenameIndexKey, metadata.ID, fs.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to create filename index: %v", err)
	}

	// Index by workflow
	workflowIndexKey := fmt.Sprintf("file:by_workflow:%s", metadata.WorkflowID)
	err = fs.client.SAdd(ctx, workflowIndexKey, metadata.ID).Err()
	if err != nil {
		return fmt.Errorf("failed to create workflow index: %v", err)
	}
	fs.client.Expire(ctx, workflowIndexKey, fs.ttl)

	// Index by step
	if metadata.StepID != "" {
		stepIndexKey := fmt.Sprintf("file:by_step:%s", metadata.StepID)
		err = fs.client.SAdd(ctx, stepIndexKey, metadata.ID).Err()
		if err != nil {
			return fmt.Errorf("failed to create step index: %v", err)
		}
		fs.client.Expire(ctx, stepIndexKey, fs.ttl)
	}

	return nil
}

// cleanupIndexes removes indexes for a file
func (fs *FileStorage) cleanupIndexes(ctx context.Context, metadata *FileMetadata) {
	// Remove from workflow index
	workflowIndexKey := fmt.Sprintf("file:by_workflow:%s", metadata.WorkflowID)
	fs.client.SRem(ctx, workflowIndexKey, metadata.ID)

	// Remove from step index
	if metadata.StepID != "" {
		stepIndexKey := fmt.Sprintf("file:by_step:%s", metadata.StepID)
		fs.client.SRem(ctx, stepIndexKey, metadata.ID)
	}

	// Note: filename index will expire automatically
}

// CleanupExpiredFiles removes expired files (can be called periodically)
func (fs *FileStorage) CleanupExpiredFiles() error {
	ctx := context.Background()
	now := time.Now()

	// Get all metadata keys
	pattern := "file:metadata:*"
	keys, err := fs.client.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get metadata keys: %v", err)
	}

	var expiredCount int
	for _, key := range keys {
		metadataData, err := fs.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var metadata FileMetadata
		if json.Unmarshal([]byte(metadataData), &metadata) != nil {
			continue
		}

		if now.After(metadata.ExpiresAt) {
			fs.DeleteFile(metadata.ID)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		log.Printf("🧹 [FILE] Cleaned up %d expired files", expiredCount)
	}

	return nil
}
