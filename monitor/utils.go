package main

import (
	"net/url"
	"os"
	"strings"
)

func urlQueryEscape(s string) string { return url.QueryEscape(s) }

// loadDotEnv reads simple KEY=VALUE pairs from a file and sets them into the process env.
// Lines starting with # and blank lines are ignored. Quotes around values are trimmed.
func loadDotEnv(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if i := strings.IndexByte(t, '='); i > 0 {
			key := strings.TrimSpace(t[:i])
			val := strings.TrimSpace(t[i+1:])
			val = strings.Trim(val, "\"'")
			if key != "" && os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
	}
}
