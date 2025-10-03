package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
)

type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func main() {
	cmdStr := flag.String("cmd", "", "command to run")
	flag.Parse()
	if *cmdStr == "" {
		fmt.Fprintln(os.Stderr, "missing -cmd")
		os.Exit(2)
	}
	cmd := exec.Command("/bin/sh", "-lc", *cmdStr)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(Result{Stdout: string(out), Stderr: "", ExitCode: code})
}
