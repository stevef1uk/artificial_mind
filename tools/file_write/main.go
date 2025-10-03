package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	path := flag.String("path", "", "file path")
	content := flag.String("content", "", "content to write")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "missing -path")
		os.Exit(2)
	}
	if err := os.WriteFile(*path, []byte(*content), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]int{"written": len(*content)})
}
