package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
)

type Result struct {
	Matches []string `json:"matches"`
}

func main() {
	pattern := flag.String("pattern", "", "regex pattern")
	flag.Parse()
	if *pattern == "" {
		fmt.Fprintln(os.Stderr, "missing -pattern")
		os.Exit(2)
	}
	re, err := regexp.Compile(*pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	scanner := bufio.NewScanner(os.Stdin)
	matches := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, line)
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(Result{Matches: matches})
}
