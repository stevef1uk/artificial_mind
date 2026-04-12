package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ragSearch performs a simple semantic search over Qdrant using a toy embedder (8-dim)
func (m *MonitorService) ragSearch(c *gin.Context) {

	log.Printf("🔍 [RAG-SEARCH] ===== ENTRY POINT REACHED =====")
	log.Printf("🔍 [RAG-SEARCH] Request URL: %s", c.Request.URL.String())
	log.Printf("🔍 [RAG-SEARCH] Request Method: %s", c.Request.Method)
	log.Printf("🔍 [RAG-SEARCH] Query params: %v", c.Request.URL.Query())

	q := strings.TrimSpace(c.Query("q"))
	limit := c.DefaultQuery("limit", "10")
	collection := c.DefaultQuery("collection", "WikipediaArticle")

	log.Printf("🔍 [RAG-SEARCH] Parsed - Query: '%s', Collection: '%s', Limit: %s", q, collection, limit)

	if q == "" {
		log.Printf("❌ [RAG-SEARCH] Missing query parameter")
		c.JSON(http.StatusBadRequest, gin.H{"error": "q required"})
		return
	}

	log.Printf("🔍 [RAG-SEARCH] Starting search for: '%s'", q)

	vec := func(text string) []float32 {
		sum := sha256.Sum256([]byte(text))
		out := make([]float32, 8)
		for i := 0; i < 8; i++ {
			off := i * 4
			v := binary.LittleEndian.Uint32(sum[off : off+4])
			out[i] = (float32(v%20000)/10000.0 - 1.0)
		}
		// normalize
		var s float32
		for i := 0; i < 8; i++ {
			s += out[i] * out[i]
		}
		if s > 0 {
			z := s
			for j := 0; j < 6; j++ {
				z = 0.5 * (z + s/z)
			}
			inv := 1.0 / float32(z)
			for i := 0; i < 8; i++ {
				out[i] *= inv
			}
		}
		return out
	}(q)

	limitInt, _ := strconv.Atoi(limit)
	if limitInt <= 0 {
		limitInt = 10
	}

	vectorStr := "["
	for i, v := range vec {
		if i > 0 {
			vectorStr += ","
		}
		vectorStr += fmt.Sprintf("%.6f", v)
	}
	vectorStr += "]"

	// GraphQL query for Weaviate
	// Note: Different collections have different schemas:
	// - AgiWiki: only has text, timestamp, metadata (JSON string) - Wikipedia articles
	// - AgiEpisodes: only has text, timestamp, metadata (JSON string) - Episodes
	// - WikipediaArticle: has title, text, source, url, timestamp, metadata
	// We'll query all possible fields and handle missing ones gracefully
	var queryStr string
	if collection == "AgiEpisodes" || collection == "AgiWiki" {
		queryStr = fmt.Sprintf(`{
			Get {
				%s(nearVector: {vector: %s}, limit: %d) {
					_additional {
						id
						distance
					}
					text
					timestamp
					metadata
				}
			}
		}`, collection, vectorStr, limitInt)
	} else {

		queryStr = fmt.Sprintf(`{
			Get {
				%s(nearVector: {vector: %s}, limit: %d) {
					_additional {
						id
						distance
					}
					title
					text
					source
					timestamp
					url
					metadata
				}
			}
		}`, collection, vectorStr, limitInt)
	}

	queryData := map[string]interface{}{
		"query": queryStr,
	}

	queryBytes, _ := json.Marshal(queryData)
	url := strings.TrimRight(m.weaviateURL, "/") + "/v1/graphql"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(queryBytes))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to create request: " + err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "weaviate search failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("weaviate returned status %d: %s", resp.StatusCode, string(bodyBytes))})
		return
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to decode weaviate response: " + err.Error()})
		return
	}

	if errors, ok := res["errors"].([]interface{}); ok && len(errors) > 0 {
		if errMap, ok := errors[0].(map[string]interface{}); ok {
			if msg, ok := errMap["message"].(string); ok {

				if strings.Contains(msg, "Cannot query field") || strings.Contains(msg, "does not exist") {
					log.Printf("⚠️ Weaviate class %s does not exist yet, returning empty results", collection)
					c.JSON(http.StatusOK, map[string]interface{}{
						"result": map[string]interface{}{"points": []interface{}{}},
					})
					return
				}
				c.JSON(http.StatusBadGateway, gin.H{"error": "Weaviate query error: " + msg})
				return
			}
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Weaviate query error"})
		return
	}

	if data, ok := res["data"].(map[string]interface{}); ok {
		if get, ok := data["Get"].(map[string]interface{}); ok {
			if results, ok := get[collection].([]interface{}); ok {
				log.Printf("🔍 [RAG-SEARCH] Weaviate returned %d raw results for collection '%s'", len(results), collection)

				queryLower := strings.ToLower(strings.TrimSpace(q))
				queryWords := strings.Fields(queryLower)
				stopWords := map[string]bool{
					"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
					"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
					"with": true, "by": true, "is": true, "are": true, "was": true, "were": true,
					"who": true, "what": true, "where": true, "when": true, "why": true, "how": true,
					"tell": true, "me": true, "about": true, "search": true, "find": true,
				}
				keywords := make([]string, 0)
				for _, word := range queryWords {
					word = strings.Trim(word, ".,!?;:()[]{}'\"")
					if !stopWords[word] && len(word) > 2 {
						keywords = append(keywords, word)
					}
				}

				if len(keywords) == 0 {
					cleaned := strings.Trim(queryLower, ".,!?;:()[]{}'\"")
					if len(cleaned) > 2 {
						keywords = []string{cleaned}
					}
				}
				log.Printf("🔍 [RAG-SEARCH] Extracted keywords: %v", keywords)

				if len(results) > 0 {
					if firstItem, ok := results[0].(map[string]interface{}); ok {
						title := ""
						text := ""
						if t, ok := firstItem["title"].(string); ok {
							title = t
						}
						if t, ok := firstItem["text"].(string); ok {
							if len(t) > 200 {
								text = t[:200] + "..."
							} else {
								text = t
							}
						}
						log.Printf("🔍 [RAG-SEARCH] First result - Title: '%s', Text preview: '%s'", title, text)
					}
				}

				maxDistance := 2.0
				points := make([]map[string]interface{}, 0, len(results))
				filteredCount := 0
				for _, result := range results {
					if item, ok := result.(map[string]interface{}); ok {
						// Step 1: Check distance threshold
						var distance float64
						hasDistance := false
						if additional, ok := item["_additional"].(map[string]interface{}); ok {
							if d, ok := additional["distance"].(float64); ok {
								distance = d
								hasDistance = true
							}
						}

						if hasDistance && distance > maxDistance {
							filteredCount++
							continue
						}

						title, hasTitle := item["title"].(string)
						text, hasText := item["text"].(string)

						if len(keywords) == 0 {

							if hasDistance && distance > maxDistance {
								continue
							}

						} else {

							titleLower := ""
							if hasTitle {
								titleLower = strings.ToLower(title)
							}
							textLower := ""
							if hasText {
								textLower = strings.ToLower(text)
							}

							if !hasTitle {

								if !hasText || len(strings.TrimSpace(text)) < 10 {
									filteredCount++
									continue
								}

							} else {

								primaryKeyword := keywords[0]
								primaryInTitle := hasTitle && strings.Contains(titleLower, primaryKeyword)
								primaryInText := hasText && strings.Contains(textLower, primaryKeyword)

								if !primaryInTitle && !primaryInText {
									filteredCount++
									continue
								}

								if len(keywords) > 1 {
									titleMatches := 0
									textMatches := 0
									for _, keyword := range keywords {
										if strings.Contains(titleLower, keyword) {
											titleMatches++
										}
										if hasText && strings.Contains(textLower, keyword) {
											textMatches++
										}
									}

									if titleMatches == 0 && textMatches < 2 {
										filteredCount++
										continue
									}
								}
							}

							if hasTitle && hasText && len(text) > 0 {
								primaryKeyword := keywords[0]
								primaryInTitle := strings.Contains(titleLower, primaryKeyword)
								if !primaryInTitle {
									textPreview := textLower
									if len(textPreview) > 1000 {
										textPreview = textPreview[:1000]
									}
									if !strings.Contains(textPreview, primaryKeyword) {
										filteredCount++
										continue
									}
								}
							}
						}

						metadata, _ := item["metadata"].(string)

						isWikipedia := strings.Contains(strings.ToLower(metadata), "\"source\":\"wikipedia\"")

						if collection == "AgiWiki" {

						} else if !isWikipedia {

							combinedText := text + " " + metadata

							textLower := strings.ToLower(strings.TrimSpace(text))
							if (strings.HasPrefix(textLower, "analyze_") ||
								strings.HasPrefix(textLower, "execute_") ||
								strings.HasPrefix(textLower, "extract_") ||
								strings.HasPrefix(textLower, "fetch_") ||
								strings.HasPrefix(textLower, "generate_") ||
								strings.Contains(textLower, ": analyze") ||
								strings.Contains(textLower, ": extract") ||
								strings.Contains(textLower, ": fetch")) &&
								len(strings.TrimSpace(text)) < 200 {

								continue
							}

							if (strings.Contains(strings.ToLower(combinedText), "\"execution_plan\"") ||
								strings.Contains(strings.ToLower(combinedText), "\"interpretation\"") ||
								strings.Contains(strings.ToLower(combinedText), "\"tasks\":[") ||
								strings.Contains(strings.ToLower(combinedText), "\"interpreted_at\"") ||
								strings.Contains(strings.ToLower(combinedText), "\"session_id\"") ||
								strings.Contains(strings.ToLower(combinedText), "execute_goal_plan")) &&
								strings.Contains(combinedText, "{") && strings.Contains(combinedText, "}") {

								continue
							}

							if strings.Contains(strings.ToLower(metadata), "\"source\":\"api:") ||
								strings.Contains(strings.ToLower(metadata), "\"event_id\":\"evt_") {
								continue
							}

							if strings.Contains(strings.ToLower(metadata), "\"project_id\":\"fsm") &&
								strings.Contains(strings.ToLower(metadata), "\"outcome\":\"success\"") &&
								strings.Contains(strings.ToLower(text), "{") {
								continue
							}

							if len(strings.TrimSpace(text)) < 50 {
								continue
							}

							if strings.Count(text, "{") > 2 && strings.Count(text, "}") > 2 &&
								strings.Count(text, "\"") > 10 {
								continue
							}
						}

						p := map[string]interface{}{"payload": item}
						if addl, ok := item["_additional"].(map[string]interface{}); ok {
							if d, ok := addl["distance"].(float64); ok {
								p["score"] = 1.0 - d
							}
							if idv, ok := addl["id"]; ok {
								p["id"] = idv
							}
						}
						points = append(points, p)
					}
				}
				log.Printf("🔍 [RAG-SEARCH] Filtered %d results, returning %d results after filtering", filteredCount, len(points))

				shouldUseFallback := false
				if len(keywords) > 0 {
					if collection == "AgiWiki" || collection == "AgiEpisodes" {

						hasKeywordMatch := false
						for _, result := range results {
							if item, ok := result.(map[string]interface{}); ok {
								text, hasText := item["text"].(string)
								if hasText {
									textLower := strings.ToLower(text)
									for _, keyword := range keywords {
										if strings.Contains(textLower, keyword) {
											hasKeywordMatch = true
											break
										}
									}
									if hasKeywordMatch {
										break
									}
								}
							}
						}

						if !hasKeywordMatch && len(results) > 0 {
							shouldUseFallback = true
							log.Printf("🔍 [RAG-SEARCH] AgiWiki: No keyword matches in vector results, using fallback text search")
						}
					} else if len(results) > 0 && len(points) == 0 {

						shouldUseFallback = true
						log.Printf("🔍 [RAG-SEARCH] All vector results filtered, trying fallback text search")
					}
				}

				if shouldUseFallback {
					log.Printf("🔍 [RAG-SEARCH] Fallback text search with keywords: %v", keywords)
					fallbackResults, fallbackErr := m.searchWeaviateByText(c.Request.Context(), collection, keywords, limitInt)
					if fallbackErr != nil {
						log.Printf("❌ [RAG-SEARCH] Fallback text search error: %v", fallbackErr)
					} else if len(fallbackResults) > 0 {
						log.Printf("🔍 [RAG-SEARCH] Fallback text search found %d results", len(fallbackResults))
						points = fallbackResults
					} else {
						log.Printf("🔍 [RAG-SEARCH] Fallback text search returned 0 results")
					}
				}

				res = map[string]interface{}{
					"result": map[string]interface{}{"points": points},
				}
			} else {

				res = map[string]interface{}{
					"result": map[string]interface{}{"points": []interface{}{}},
				}
			}
		} else {

			res = map[string]interface{}{
				"result": map[string]interface{}{"points": []interface{}{}},
			}
		}
	} else {

		res = map[string]interface{}{
			"result": map[string]interface{}{"points": []interface{}{}},
		}
	}

	c.JSON(http.StatusOK, res)
}

// searchWeaviateByText performs a text-based keyword search as fallback when vector search fails
func (m *MonitorService) searchWeaviateByText(ctx context.Context, collection string, keywords []string, limit int) ([]map[string]interface{}, error) {

	var whereClause string
	if collection == "AgiWiki" || collection == "AgiEpisodes" {

		textConditions := []string{}
		for _, keyword := range keywords {
			textConditions = append(textConditions, fmt.Sprintf(`{path: ["text"], operator: Like, valueText: "*%s*"}`, keyword))
		}
		if len(textConditions) == 1 {
			whereClause = fmt.Sprintf(`where: %s`, textConditions[0])
		} else {
			whereClause = fmt.Sprintf(`where: {operator: Or, operands: [%s]}`, strings.Join(textConditions, ", "))
		}
	} else {

		allConditions := []string{}
		for _, keyword := range keywords {

			keywordCondition := fmt.Sprintf(`{operator: Or, operands: [{path: ["title"], operator: Like, valueText: "*%s*"}, {path: ["text"], operator: Like, valueText: "*%s*"}]}`, keyword, keyword)
			allConditions = append(allConditions, keywordCondition)
		}
		if len(allConditions) == 1 {
			whereClause = fmt.Sprintf(`where: %s`, allConditions[0])
		} else {

			whereClause = fmt.Sprintf(`where: {operator: Or, operands: [%s]}`, strings.Join(allConditions, ", "))
		}
	}

	fields := m.getCollectionFields(collection)
	queryStr := fmt.Sprintf(`{Get {%s(%s limit: %d) {_additional {id} %s}}}`, collection, whereClause, limit, fields)

	log.Printf("🔍 [RAG-SEARCH] Fallback query: %s", queryStr)

	queryData := map[string]interface{}{"query": queryStr}
	queryBytes, _ := json.Marshal(queryData)
	url := strings.TrimRight(m.weaviateURL, "/") + "/v1/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(queryBytes))
	if err != nil {
		log.Printf("❌ [RAG-SEARCH] Fallback request creation failed: %v", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ [RAG-SEARCH] Fallback request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("❌ [RAG-SEARCH] Fallback search failed with status %d: %s", resp.StatusCode, string(bodyBytes))
		return nil, fmt.Errorf("weaviate text search failed: %s: %s", resp.Status, string(bodyBytes))
	}

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		log.Printf("❌ [RAG-SEARCH] Fallback response decode failed: %v", err)
		return nil, err
	}

	if errors, ok := res["errors"].([]interface{}); ok && len(errors) > 0 {
		log.Printf("❌ [RAG-SEARCH] Fallback search GraphQL errors: %v", errors)
		return nil, fmt.Errorf("weaviate text search errors: %v", errors)
	}

	// Extract results and convert to points format
	var points []map[string]interface{}
	if data, ok := res["data"].(map[string]interface{}); ok {
		if get, ok := data["Get"].(map[string]interface{}); ok {
			if results, ok := get[collection].([]interface{}); ok {
				log.Printf("🔍 [RAG-SEARCH] Fallback search returned %d raw results from Weaviate", len(results))
				for _, result := range results {
					if item, ok := result.(map[string]interface{}); ok {
						p := map[string]interface{}{"payload": item}
						if addl, ok := item["_additional"].(map[string]interface{}); ok {
							if idv, ok := addl["id"]; ok {
								p["id"] = idv
							}
						}
						points = append(points, p)
					}
				}
			} else {
				log.Printf("⚠️ [RAG-SEARCH] Fallback search: collection '%s' not found in response", collection)
			}
		} else {
			log.Printf("⚠️ [RAG-SEARCH] Fallback search: 'Get' field not found in response")
		}
	} else {
		log.Printf("⚠️ [RAG-SEARCH] Fallback search: 'data' field not found in response")
	}

	log.Printf("🔍 [RAG-SEARCH] Fallback search converted to %d points", len(points))
	return points, nil
}

// getCollectionFields returns the fields to query for a given collection
func (m *MonitorService) getCollectionFields(collection string) string {
	if collection == "AgiEpisodes" || collection == "AgiWiki" {
		return "text\ntimestamp\nmetadata"
	} else {
		return "title\ntext\nsource\ntimestamp\nurl\nmetadata"
	}
}

// getDailySummaryLatest returns the latest daily summary from Redis
func (m *MonitorService) getDailySummaryLatest(c *gin.Context) {
	ctx := context.Background()
	val, err := m.redisClient.Get(ctx, "daily_summary:latest").Result()
	if err != nil || strings.TrimSpace(val) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no daily summary"})
		return
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(val), &obj); err != nil {
		c.JSON(http.StatusOK, gin.H{"raw": val})
		return
	}
	c.JSON(http.StatusOK, obj)
}

// getDailySummaryHistory returns recent daily summaries (limit 30)
func (m *MonitorService) getDailySummaryHistory(c *gin.Context) {
	ctx := context.Background()
	items, err := m.redisClient.LRange(ctx, "daily_summary:history", 0, 29).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redis error"})
		return
	}
	var list []map[string]interface{}
	for _, it := range items {
		var obj map[string]interface{}
		if json.Unmarshal([]byte(it), &obj) == nil {
			list = append(list, obj)
		}
	}
	c.JSON(http.StatusOK, gin.H{"history": list})
}

// getDailySummaryByDate returns the daily summary for a specific date (YYYY-MM-DD, UTC)
func (m *MonitorService) getDailySummaryByDate(c *gin.Context) {
	date := strings.TrimSpace(c.Param("date"))
	if date == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date required"})
		return
	}
	ctx := context.Background()
	val, err := m.redisClient.Get(ctx, "daily_summary:"+date).Result()
	if err != nil || strings.TrimSpace(val) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(val), &obj); err != nil {
		c.JSON(http.StatusOK, gin.H{"raw": val})
		return
	}
	c.JSON(http.StatusOK, obj)
}

// getMemorySummaryForSession fetches memory summary from HDN (for auto-executor)
func (m *MonitorService) getMemorySummaryForSession(sessionID string) (map[string]interface{}, error) {
	url := m.hdnURL + "/api/v1/memory/summary?session_id=" + urlQueryEscape(sessionID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var summary map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, err
	}
	return summary, nil
}

// clearSafetyData clears only safety-related data from Redis and log files
func (m *MonitorService) clearSafetyData(c *gin.Context) {
	ctx := context.Background()

	patterns := []string{
		"tool_metrics:*",
		"tool_calls:*",
	}

	clearedKeys := 0
	for _, pattern := range patterns {
		keys, err := m.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			log.Printf("Error getting keys for pattern %s: %v", pattern, err)
			continue
		}

		if len(keys) > 0 {
			err = m.redisClient.Del(ctx, keys...).Err()
			if err != nil {
				log.Printf("Error deleting keys for pattern %s: %v", pattern, err)
				continue
			}
			clearedKeys += len(keys)
		}
	}

	logFiles, err := filepath.Glob("/tmp/tool_calls_*.log")
	if err != nil {
		log.Printf("Error finding log files: %v", err)
	} else {
		for _, logFile := range logFiles {
			err := os.Remove(logFile)
			if err != nil {
				log.Printf("Error removing log file %s: %v", logFile, err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "Safety data cleared successfully",
		"cleared_keys": clearedKeys,
		"cleared_logs": len(logFiles),
	})
}
