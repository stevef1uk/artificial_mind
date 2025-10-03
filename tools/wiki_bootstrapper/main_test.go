package main

import (
	"os"
	"os/exec"
	"testing"
)

// This test exercises the CLI wrapper against a local HDN API if available.
// It is skipped if HDN_URL is not set to a reachable endpoint.
func TestWikiBootstrapperToolWrapper(t *testing.T) {
	base := os.Getenv("HDN_URL")
	if base == "" {
		t.Skip("HDN_URL not set; skipping integration test")
	}
	bin := "./wiki_bootstrapper_test_bin"
	// Build test binary
	if err := exec.Command("go", "build", "-o", bin, ".").Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	defer os.Remove(bin)

	cmd := exec.Command(bin, "--seeds", "Science", "--max-depth", "0", "--max-nodes", "1", "--job-id", "testjob")
	cmd.Env = append(os.Environ(), "HDN_URL="+base)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invoke failed: %v, out=%s", err, string(out))
	}
}
