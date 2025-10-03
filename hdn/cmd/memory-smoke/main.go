package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Minimal vector database REST smoke: create collection, upsert one point, search it.

func main() {
	vectorDBURL := getenv("WEAVIATE_URL", "http://localhost:8080")
	collection := getenv("RAG_COLLECTION", "agi-episodes")

	// Test vector database (Weaviate or Qdrant)
	if err := testVectorDatabase(vectorDBURL, collection); err != nil {
		fmt.Println("❌ vector database test:", err)
		os.Exit(1)
	}
	fmt.Println("✅ vector database test passed")

	// Optional extras (Neo4j) behind build tags
	runExtra()
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func toyEmbed(s string, dim int) []float32 {
	v := make([]float32, dim)
	for i, b := range []byte(s) {
		v[i%dim] += float32(b%13) / 13.0
	}
	return v
}

func qdrantCreateCollection(base, name string, dim int) error {
	// First check if exists: GET /collections/{name}
	chkReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/collections/%s", base, name), nil)
	chkResp, err := http.DefaultClient.Do(chkReq)
	if err == nil && chkResp.StatusCode == 200 {
		chkResp.Body.Close()
		return nil
	}
	if chkResp != nil {
		chkResp.Body.Close()
	}

	// Create if not exists: PUT /collections/{name}
	body := map[string]any{
		"vectors": map[string]any{
			"size":     dim,
			"distance": "Cosine",
		},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/collections/%s", base, name), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != 409 { // 409 may occur if exists
		return fmt.Errorf("create collection status %s", resp.Status)
	}
	return nil
}

func qdrantUpsert(base, name string, vec []float32, payload map[string]any) error {
	// PUT /collections/{name}/points
	point := map[string]any{
		"id":      time.Now().UnixNano(),
		"vector":  vec,
		"payload": payload,
	}
	body := map[string]any{"points": []any{point}}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/collections/%s/points", base, name), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upsert status %s", resp.Status)
	}
	return nil
}

func qdrantSearch(base, name string, vec []float32, limit int, filters map[string]any) ([]map[string]any, error) {
	// POST /collections/{name}/points/search
	body := map[string]any{
		"vector": vec,
		"limit":  limit,
	}
	if len(filters) > 0 {
		must := []any{}
		for k, v := range filters {
			must = append(must, map[string]any{
				"key":   k,
				"match": map[string]any{"value": v},
			})
		}
		body["filter"] = map[string]any{"must": must}
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/collections/%s/points/search", base, name), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search status %s", resp.Status)
	}
	var out struct {
		Result []map[string]any `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

// testVectorDatabase tests either Weaviate or Qdrant based on URL
func testVectorDatabase(url, collection string) error {
	// Determine if this is Weaviate or Qdrant based on URL
	if isWeaviateURL(url) {
		return testWeaviate(url, collection)
	} else {
		return testQdrant(url, collection)
	}
}

// isWeaviateURL checks if the URL is for Weaviate
func isWeaviateURL(url string) bool {
	return strings.Contains(url, "weaviate") || strings.Contains(url, ":8080")
}

// testWeaviate tests Weaviate functionality
func testWeaviate(url, collection string) error {
	// For now, just test basic connectivity
	resp, err := http.Get(url + "/v1/meta")
	if err != nil {
		return fmt.Errorf("weaviate connectivity: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("weaviate status: %s", resp.Status)
	}

	fmt.Println("✅ Weaviate connectivity test passed")
	return nil
}

// testQdrant tests Qdrant functionality
func testQdrant(url, collection string) error {
	// Ensure collection exists (vector size 8 for toy embedding)
	if err := qdrantCreateCollection(url, collection, 8); err != nil {
		return fmt.Errorf("qdrant create collection: %v", err)
	}

	// Build a toy embedding
	text := "First step: validated episodic indexing"
	vec := toyEmbed(text, 8)

	// Upsert point with payload
	payload := map[string]any{
		"text":       text,
		"session_id": "smoke_demo",
		"tags":       []string{"smoke", "demo"},
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
	if err := qdrantUpsert(url, collection, vec, payload); err != nil {
		return fmt.Errorf("qdrant upsert: %v", err)
	}
	fmt.Println("✅ episodic index ok (qdrant)")

	// Search back
	qvec := toyEmbed("validated episodic", 8)
	results, err := qdrantSearch(url, collection, qvec, 5, map[string]any{"session_id": "smoke_demo"})
	if err != nil {
		return fmt.Errorf("qdrant search: %v", err)
	}
	fmt.Printf("✅ episodic search ok (qdrant): %d result(s)\n", len(results))

	return nil
}
