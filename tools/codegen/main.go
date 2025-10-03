package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type Result struct {
	Code string `json:"code"`
}

func main() {
	spec := flag.String("spec", "", "specification prompt")
	flag.Parse()
	if *spec == "" {
		fmt.Fprintln(os.Stderr, "missing -spec")
		os.Exit(2)
	}
	// Stub: echo a trivial program
	code := "print('hello from generated code')\n"
	_ = json.NewEncoder(os.Stdout).Encode(Result{Code: code})
}
