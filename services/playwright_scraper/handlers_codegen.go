package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type CodegenSession struct {
	ID          string     `json:"id"`
	URL         string     `json:"url"`
	OutputPath  string     `json:"output_path"`
	LogPath     string     `json:"log_path"`
	NoVNCURL    string     `json:"novnc_url,omitempty"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	cmd         *exec.Cmd
}

var (
	codegenMu       sync.Mutex
	codegenSessions = make(map[string]*CodegenSession)
)

type codegenStartRequest struct {
	URL        string `json:"url"`
	OutputPath string `json:"output_path,omitempty"`
}

func handleCodegenStart(w http.ResponseWriter, r *http.Request) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req codegenStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv("CODEGEN_MODE")))

	log.Printf("📥 Codegen start request for URL: %s (Mode: %s)", req.URL, mode)

	id := uuid.New().String()
	outputDir := getenvDefault("CODEGEN_OUTPUT_DIR", filepath.Join(os.TempDir(), "agi_codegen"))
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create output dir: %v", err), http.StatusInternalServerError)
		return
	}

	outputPath := req.OutputPath
	if outputPath == "" {
		outputPath = filepath.Join(outputDir, fmt.Sprintf("codegen_%s.ts", id))
	}
	logPath := filepath.Join(outputDir, fmt.Sprintf("codegen_%s.log", id))

	// Create log file for both modes
	logFile, err := os.Create(logPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create log file: %v", err), http.StatusInternalServerError)
		return
	}

	var cmd *exec.Cmd
	var novncURL string

	if mode == "container" {
		// In container mode, the codegen container should be running (e.g., via docker-compose)
		// Start codegen inside the running container and return session info + noVNC URL
		cmd, err = startCodegenInContainer(req.URL, outputPath, logFile)
		if err != nil {
			_ = logFile.Close()
			http.Error(w, fmt.Sprintf("Failed to start container codegen: %v", err), http.StatusInternalServerError)
			return
		}
		novncURL = getenvDefault("CODEGEN_NOVNC_URL", "http://localhost:7000/vnc.html?autoconnect=1&resize=remote")
	} else {
		if _, err := exec.LookPath("npx"); err != nil {
			_ = logFile.Close()
			http.Error(w, "npx not found on PATH", http.StatusBadRequest)
			return
		}

		cmd = exec.Command("npx", "playwright", "codegen", "--output", outputPath, req.URL)
		cmd.Stdout = logFile
		cmd.Stderr = logFile

		if err := cmd.Start(); err != nil {
			_ = logFile.Close()
			http.Error(w, fmt.Sprintf("Failed to start codegen: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Monitor process in background
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()

		codegenMu.Lock()
		defer codegenMu.Unlock()
		finished := time.Now()
		session := codegenSessions[id]
		if session == nil {
			return
		}
		session.CompletedAt = &finished
		if err != nil {
			session.Status = "failed"
			session.Error = err.Error()
			return
		}
		session.Status = "completed"
	}()

	session := &CodegenSession{
		ID:         id,
		URL:        req.URL,
		OutputPath: outputPath,
		LogPath:    logPath,
		NoVNCURL:   novncURL,
		Status:     "running",
		StartedAt:  time.Now(),
		cmd:        cmd,
	}

	codegenMu.Lock()
	codegenSessions[id] = session
	codegenMu.Unlock()

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

func startCodegenInContainer(url, outputHostPath string, logFile *os.File) (*exec.Cmd, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found on PATH")
	}

	containerName, err := resolveCodegenContainerName()
	if err != nil {
		return nil, err
	}
	log.Printf("🐳 Using codegen container: %s", containerName)

	// Clean up any existing codegen processes first
	cleanupCmd := exec.Command("docker", "exec", containerName, "pkill", "-f", "playwright codegen")
	_ = cleanupCmd.Run() // Ignore error if no process found

	outputContainerPath := filepath.Join("/output", filepath.Base(outputHostPath))

	// Note: We avoid passing complex flags to playwright codegen as it doesn't support
	// arbitrary browser arguments via CLI cleanly. Rely on standard environment or default behavior.
	args := []string{
		"exec", "-e", "DISPLAY=:99", containerName,
		"npx", "playwright", "codegen", "--output", outputContainerPath, url,
		"--browser=chromium",
	}

	cmd := exec.Command("docker", args...)
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func resolveCodegenContainerName() (string, error) {
	if name := strings.TrimSpace(os.Getenv("CODEGEN_CONTAINER_NAME")); name != "" {
		return name, nil
	}

	name, ok, err := firstDockerPsName([]string{"--filter", "ancestor=agi-codegen"})
	if err != nil {
		return "", err
	}
	if ok {
		return name, nil
	}

	name, ok, err = firstDockerPsName([]string{"--filter", "name=codegen"})
	if err != nil {
		return "", err
	}
	if ok {
		return name, nil
	}

	return "", fmt.Errorf("codegen container not running; set CODEGEN_CONTAINER_NAME")
}

func firstDockerPsName(filters []string) (string, bool, error) {
	args := append([]string{"ps", "--format", "{{.Names}}"}, filters...)
	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", false, err
	}
	names := strings.Fields(string(output))
	if len(names) == 0 {
		return "", false, nil
	}
	return names[0], true, nil
}

func handleCodegenStatus(w http.ResponseWriter, r *http.Request) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id", http.StatusBadRequest)
		return
	}

	codegenMu.Lock()
	session, ok := codegenSessions[id]
	if !ok {
		// Attempt to reconstruct from filesystem (if restart happened)
		outputDir := getenvDefault("CODEGEN_OUTPUT_DIR", filepath.Join(os.TempDir(), "agi_codegen"))
		outputPath := filepath.Join(outputDir, fmt.Sprintf("codegen_%s.ts", id))
		if _, err := os.Stat(outputPath); err == nil {
			session = &CodegenSession{
				ID:         id,
				OutputPath: outputPath,
				Status:     "completed",
				StartedAt:  time.Now(), // approximate
			}
			codegenSessions[id] = session
			ok = true
		}
	}
	codegenMu.Unlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

func handleCodegenResult(w http.ResponseWriter, r *http.Request) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id", http.StatusBadRequest)
		return
	}

	codegenMu.Lock()
	session, ok := codegenSessions[id]
	if !ok {
		// Attempt to reconstruct from filesystem (if restart happened)
		outputDir := getenvDefault("CODEGEN_OUTPUT_DIR", filepath.Join(os.TempDir(), "agi_codegen"))
		outputPath := filepath.Join(outputDir, fmt.Sprintf("codegen_%s.ts", id))
		if _, err := os.Stat(outputPath); err == nil {
			session = &CodegenSession{
				ID:         id,
				OutputPath: outputPath,
				Status:     "completed",
				StartedAt:  time.Now(),
			}
			codegenSessions[id] = session
			ok = true
		}
	}
	codegenMu.Unlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	// Allow reading output even if not completed (peek)
	if _, err := os.Stat(session.OutputPath); os.IsNotExist(err) {
		http.Error(w, "Output not ready yet", http.StatusAccepted) // 202
		return
	}

	data, err := os.ReadFile(session.OutputPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read output: %v", err), http.StatusInternalServerError)
		return
	}

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func resolveComposeCommand() (string, []string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", nil, fmt.Errorf("docker not found on PATH")
	}
	if _, err := exec.LookPath("docker-compose"); err == nil {
		return "docker-compose", []string{}, nil
	}
	return "docker", []string{"compose"}, nil
}

func getenvDefault(key, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}

// handleCodegenLatest returns the most recently modified .ts file in the output directory
func handleCodegenLatest(w http.ResponseWriter, r *http.Request) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	outputDir := getenvDefault("CODEGEN_OUTPUT_DIR", filepath.Join(os.TempDir(), "agi_codegen"))
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("Cannot read output dir: %v", err), http.StatusInternalServerError)
		return
	}

	var latestPath string
	var latestMod time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestMod) {
			latestMod = info.ModTime()
			latestPath = filepath.Join(outputDir, e.Name())
		}
	}

	if latestPath == "" {
		http.Error(w, "No recorded scripts found", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(latestPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read script: %v", err), http.StatusInternalServerError)
		return
	}

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Script-File", filepath.Base(latestPath))
	w.Header().Set("X-Script-Modified", latestMod.Format(time.RFC3339))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
