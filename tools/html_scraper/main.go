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
	node, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return []Item{}
	}
	var items []Item
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "title" || n.Data == "h1" || n.Data == "h2" || n.Data == "h3" || n.Data == "p" || n.Data == "a" {
				text := strings.TrimSpace(textContent(n))
				attrs := map[string]string{}
				for _, a := range n.Attr {
					attrs[a.Key] = a.Val
				}
				items = append(items, Item{Tag: n.Data, Text: text, Attributes: attrs})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(node)
	return items
}

func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textContent(c))
	}
	return b.String()
}
