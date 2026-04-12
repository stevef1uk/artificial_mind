package main

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

func (ie *IntelligentExecutor) enhanceHypothesisRequest(req *ExecutionRequest, descLower string) *ExecutionRequest {
	hypothesisContent := req.Description
	if strings.HasPrefix(descLower, "test hypothesis:") {
		parts := strings.SplitN(req.Description, ":", 2)
		if len(parts) > 1 {
			hypothesisContent = strings.TrimSpace(parts[1])
		}
	}

	extractedTerms := ie.extractHypothesisTerms(hypothesisContent)
	termsJSON, _ := json.Marshal(extractedTerms)

	rawTemplate := `import requests
import json
import os

hypothesis = %q
input_terms = %s
hdn_url = os.getenv('HDN_URL', 'http://host.docker.internal:8081')

evidence = []
logs = []

for term in input_terms:
    if len(term) < 3: continue
    query = {"query": f"MATCH (c:Concept) WHERE toLower(c.name) CONTAINS toLower('{term}') RETURN c.name AS name, c.description AS description LIMIT 5"}
    try:
        resp = requests.post(f"{hdn_url}/api/v1/knowledge/query", json=query, timeout=5)
        if resp.status_code == 200:
            results = resp.json().get('results', [])
            for r in results:
                evidence.append({'term': term, 'name': r.get('name'), 'desc': r.get('description')})
    except Exception as e:
        logs.append(f"Failed {term}: {str(e)}")

# Write Markdown report for UI
try:
    with open('hypothesis_test_report.md', 'w') as f:
        f.write(f"# Hypothesis Test Report\n\n")
        f.write(f"## Hypothesis\n{hypothesis}\n\n")
        f.write(f"## Evidence\n")
        if evidence:
            evidence_by_term = {}
            for e in evidence:
                term = e.get('term', 'Unknown')
                if term not in evidence_by_term:
                    evidence_by_term[term] = []
                evidence_by_term[term].append(e)
            
            for term, items in evidence_by_term.items():
                f.write(f"\n### Search term: {term}\n")
                for e in items:
                    name = e.get('name', 'N/A')
                    desc = e.get('desc', 'No description')
                    f.write(f"- **{name}**: {desc}\n")
        else:
            f.write("\nNo evidence found in knowledge base.\n")
        
        f.write(f"\n## Conclusion\n")
        if evidence:
            unique_concepts = set(e.get('name') for e in evidence if e.get('name'))
            f.write(f"Found {len(evidence)} evidence items across {len(unique_concepts)} unique concepts.\n")
            if unique_concepts:
                f.write(f"\nConcepts discovered: {', '.join(sorted(unique_concepts))}\n")
        else:
            searched_terms = [t for t in input_terms if len(t) >= 3]
            f.write(f"No evidence found for terms: {searched_terms}\n")
    
    print(f"Report generated with {len(evidence)} evidence items")
except Exception as e:
    # Force write a failure report so the UI shows SOMETHING
    with open('hypothesis_test_report.md', 'w') as f:
        f.write(f"# Hypothesis Test Report\n\n## Error\n{str(e)}")
`
	req.Description = fmt.Sprintf(rawTemplate, hypothesisContent, string(termsJSON))

	if req.Context == nil {
		req.Context = make(map[string]string)
	}
	req.Context["hypothesis_enhanced"] = "true"
	req.Context["artifact_names"] = "hypothesis_test_report.md"

	return req
}

func (ie *IntelligentExecutor) extractExplicitToolRequest(req *ExecutionRequest, descLower, taskLower string) string {
	combined := descLower + " " + taskLower

	toolPatterns := []string{
		"use tool_http_get",
		"use tool_html_scraper",
		"use tool_file_read",
		"use tool_file_write",
		"use tool_ls",
		"use tool_exec",
		"use tool_wiki",
		"tool_http_get to",
		"tool_html_scraper to",
	}

	for _, pattern := range toolPatterns {
		if !strings.Contains(combined, pattern) {
			continue
		}

		log.Printf("🔧 [INTELLIGENT] Detected explicit tool usage request: %s", pattern)

		if strings.HasPrefix(pattern, "use ") {
			parts := strings.Fields(pattern)
			if len(parts) >= 2 {
				return parts[1]
			}
		} else if strings.Contains(pattern, "tool_") {
			parts := strings.Fields(pattern)
			for _, part := range parts {
				if strings.HasPrefix(part, "tool_") {
					return part
				}
			}
		}
	}

	return ""
}

func (ie *IntelligentExecutor) extractHypothesisTerms(hypothesis string) []string {
	var rawTerms []string

	eventRegex := regexp.MustCompile(`test_event_\w+_\w+`)
	rawTerms = append(rawTerms, eventRegex.FindAllString(hypothesis, -1)...)

	domainRegex := regexp.MustCompile(`(?i)(\w+)\s+domain`)
	domainMatches := domainRegex.FindAllStringSubmatch(hypothesis, -1)
	for _, m := range domainMatches {
		if len(m) > 1 {
			rawTerms = append(rawTerms, m[1]+" domain")
		}
	}

	words := strings.Fields(hypothesis)
	for i, word := range words {
		cleaned := strings.Trim(word, ".,!?;:")

		if len(cleaned) < 4 {
			continue
		}
		lower := strings.ToLower(cleaned)

		importantTerms := []string{"learning", "memory", "consolidation", "pattern", "cognitive",
			"process", "knowledge", "hypothesis", "evidence", "relationship", "insight",
			"biology", "medicine", "computer", "science", "technology", "system"}
		for _, term := range importantTerms {
			if lower == term || strings.HasPrefix(lower, term) {
				rawTerms = append(rawTerms, cleaned)
				break
			}
		}

		if i < len(words)-1 {
			nextWord := strings.Trim(words[i+1], ".,!?;:")
			if len(nextWord) >= 4 {
				phrase := cleaned + " " + nextWord
				phraseLower := strings.ToLower(phrase)

				conceptPatterns := []string{"learning pattern", "memory consolidation",
					"cognitive process", "knowledge graph", "system state"}
				for _, pattern := range conceptPatterns {
					if phraseLower == pattern {
						rawTerms = append(rawTerms, phrase)
						break
					}
				}
			}
		}
	}

	stopWords := map[string]bool{
		"use": true, "test": true, "task": true, "find": true,
		"get": true, "show": true, "this": true, "using": true,
	}
	seen := make(map[string]bool)
	var unique []string

	log.Printf("🧪 [DEBUG] Raw terms extracted from text: %v", rawTerms)

	for _, t := range rawTerms {
		lower := strings.ToLower(strings.TrimSpace(t))

		isStopWord := false
		for sw := range stopWords {
			if lower == sw || strings.HasPrefix(lower, sw+" ") {
				isStopWord = true
				break
			}
		}

		if isStopWord {
			log.Printf("🧪 [DEBUG] Skipping term '%s': matched stopword list", t)
			continue
		}
		if len(lower) <= 2 {
			log.Printf("🧪 [DEBUG] Skipping term '%s': too short", t)
			continue
		}
		if seen[lower] {
			continue
		}

		log.Printf("🧪 [DEBUG] Keeping term: '%s'", t)
		seen[lower] = true
		unique = append(unique, t)
	}

	return unique
}

// collectURLsFromContext extracts candidate URLs from context map.
func (ie *IntelligentExecutor) collectURLsFromContext(ctxMap map[string]string) []string {
	var urls []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}

		parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				urls = append(urls, p)
			}
		}
	}

	for k, v := range ctxMap {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "url" || lk == "urls" || strings.HasPrefix(lk, "source_url") || strings.HasPrefix(lk, "link_") {
			add(v)
		}
	}
	return urls
}
