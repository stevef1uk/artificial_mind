package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Item struct {
	Tag        string            `json:"tag"`
	Text       string            `json:"text"`
	Attributes map[string]string `json:"attributes"`
}

type Result struct {
	Items []Item `json:"items"`
}

func main() {
	url := flag.String("url", "", "URL to scrape")
	flag.Parse()
	if *url == "" {
		fmt.Fprintln(os.Stderr, "missing -url")
		os.Exit(2)
	}
	items, err := fetchItems(*url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching items: %v\n", err)
		_ = json.NewEncoder(os.Stdout).Encode(Result{Items: []Item{}})
		return
	}
	_ = json.NewEncoder(os.Stdout).Encode(Result{Items: items})
}

// fetchItems fetches the URL with appropriate headers and extracts basic items
func fetchItems(u string) ([]Item, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("GET", u, nil)
	// Some sites (e.g., Wikipedia) require a UA; also accept typical encodings
	req.Header.Set("User-Agent", "agi-html-scraper/1.0 (+https://example.local)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Read body
	b, _ := io.ReadAll(resp.Body)
	items := extractBasic(string(b))
	return items, nil
}

func extractBasic(htmlStr string) []Item {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return []Item{}
	}

	// Try to find a better root node (main content) to avoid sidebars/navs
	root := findContentRoot(doc)

	var items []Item
	lastText := "" // Track last text to avoid duplicates

	// State for section-based pruning
	skipRemaining := false

	var f func(*html.Node)
	f = func(n *html.Node) {
		if skipRemaining {
			return
		}

		if n.Type == html.ElementNode {
			// Skip script, style, navigation data, and noisy structural elements
			if shouldSkipNode(n) {
				return
			}

			// Check for "terminal" sections (References, See also, etc.)
			// Once we hit these, we usually have most of the useful article text
			if n.Data == "h2" || n.Data == "h3" {
				id := strings.ToLower(getAttribute(n, "id"))
				text := strings.ToLower(textContent(n))
				if strings.Contains(id, "references") || strings.Contains(text, "references") ||
					strings.Contains(id, "see_also") || strings.Contains(text, "see also") ||
					strings.Contains(id, "external_links") || strings.Contains(text, "external links") ||
					strings.Contains(id, "notes") || strings.Contains(text, "notes") {
					skipRemaining = true
					return
				}
			}

			// Extract useful content tags
			if n.Data == "title" || n.Data == "h1" || n.Data == "h2" || n.Data == "h3" || n.Data == "p" || n.Data == "li" || n.Data == "blockquote" {
				text := strings.TrimSpace(textContent(n))

				// Skip if:
				// 1. Empty text
				// 2. Too short (likely navigation/menu items)
				// 3. Duplicate of previous item (case-insensitive)
				if text == "" || len(text) < 10 {
					// Skip for child processing but don't add
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						f(c)
					}
					return
				}

				// Check for duplicate (case-insensitive)
				textLower := strings.ToLower(text)
				if textLower == strings.ToLower(lastText) {
					return // Skip duplicate
				}

				// Only add if there is actual text
				attrs := map[string]string{}
				for _, a := range n.Attr {
					attrs[a.Key] = a.Val
				}
				items = append(items, Item{Tag: n.Data, Text: text, Attributes: attrs})
				lastText = text
				return // Don't process children of content nodes
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(root)
	return items
}

// shouldSkipNode determines if a node is likely noise (navigation, sidebars, hidden elements)
func shouldSkipNode(n *html.Node) bool {
	if n.Data == "script" || n.Data == "style" || n.Data == "nav" || n.Data == "footer" || n.Data == "header" {
		return true
	}

	// Filter based on class/id
	class := getAttribute(n, "class")
	id := getAttribute(n, "id")

	// Filter out common noise classes/IDs
	noiseTerms := []string{
		"interlanguage-link", "mw-jump-link", "vector-toc-list-item",
		"mw-editsection", "noprint", "sidebar", "navigation", "menu",
		"reference", "reflist", "portalbox", "catlinks", "citation",
		"mw-list-item", "infobox", "template", "metadata",
	}

	for _, term := range noiseTerms {
		if strings.Contains(class, term) || strings.Contains(id, term) {
			return true
		}
	}

	return false
}

// findContentRoot attempts to find the main content container
func findContentRoot(n *html.Node) *html.Node {
	// Candidate IDs for main content areas (Wikipedia specific + generic)
	candidateIDs := []string{"mw-content-text", "bodyContent", "content", "main", "article"}

	// Queue for BFS
	queue := []*html.Node{n}

	// Limit search depth/nodes to avoid infinite loops or massive perf hits (though BFS is finite)
	visited := 0

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		visited++

		if curr.Type == html.ElementNode {
			// Check IDs
			id := getAttribute(curr, "id")
			for _, cid := range candidateIDs {
				if id == cid {
					return curr
				}
			}

			// Check semantic tags
			if curr.Data == "main" || curr.Data == "article" {
				return curr
			}

			// Check role="main"
			if getAttribute(curr, "role") == "main" {
				return curr
			}
		}

		for c := curr.FirstChild; c != nil; c = c.NextSibling {
			queue = append(queue, c)
		}
	}

	// If no specific content root found, return original root
	return n
}

func getAttribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	if n == nil || n.Type == html.CommentNode {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	// Avoid leaking style or script content into the text output
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
		return ""
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textContent(c))
	}
	return b.String()
}
