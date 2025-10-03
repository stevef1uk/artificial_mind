package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	var seeds string
	var maxDepth int
	var maxNodes int
	var rpm int
	var burst int
	var jitter int
	var minConf float64
	var domain string
	var jobID string
	var resume bool
	var pause bool

	flag.StringVar(&seeds, "seeds", "", "Comma-separated Wikipedia titles")
	flag.IntVar(&maxDepth, "max-depth", 1, "Max crawl depth")
	flag.IntVar(&maxNodes, "max-nodes", 50, "Max nodes to ingest")
	flag.IntVar(&rpm, "rpm", 30, "Requests per minute")
	flag.IntVar(&burst, "burst", 5, "Burst size")
	flag.IntVar(&jitter, "jitter-ms", 250, "Jitter ms")
	flag.Float64Var(&minConf, "min-confidence", 0.6, "Min relation confidence")
	flag.StringVar(&domain, "domain", "General", "Domain tag")
	flag.StringVar(&jobID, "job-id", "", "Job ID for pause/resume")
	flag.BoolVar(&resume, "resume", false, "Resume job")
	flag.BoolVar(&pause, "pause", false, "Pause job")
	flag.Parse()

	params := map[string]any{}
	if strings.TrimSpace(seeds) != "" {
		params["seeds"] = seeds
	}
	params["max_depth"] = maxDepth
	params["max_nodes"] = maxNodes
	params["rpm"] = rpm
	params["burst"] = burst
	params["jitter_ms"] = jitter
	params["min_confidence"] = minConf
	if strings.TrimSpace(domain) != "" {
		params["domain"] = domain
	}
	if strings.TrimSpace(jobID) != "" {
		params["job_id"] = jobID
	}
	if resume {
		params["resume"] = true
	}
	if pause {
		params["pause"] = true
	}

	base := os.Getenv("HDN_URL")
	if strings.TrimSpace(base) == "" {
		base = "http://host.docker.internal:8080"
	}
	url := strings.TrimRight(base, "/") + "/api/v1/tools/tool_wiki_bootstrapper/invoke"

	b, _ := json.Marshal(params)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Println(string(out))
}
