package main

import (
	"encoding/json"
	"fmt"
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

	id := uuid.New().String()
	outputDir := filepath.Join(os.TempDir(), "agi_codegen")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create output dir: %v", err), http.StatusInternalServerError)
		return
	}

	outputPath := req.OutputPath
	if outputPath == "" {
		outputPath = filepath.Join(outputDir, fmt.Sprintf("codegen_%s.ts", id))
	}
	logPath := filepath.Join(outputDir, fmt.Sprintf("codegen_%s.log", id))

	var cmd *exec.Cmd
	var novncURL string
	if mode == "container" {
		// In container mode, the codegen container should be running (e.g., via docker-compose)
		// Start codegen inside the running container and return session info + noVNC URL
		if err := startCodegenInContainer(req.URL, outputPath); err != nil {
			http.Error(w, fmt.Sprintf("Failed to start container codegen: %v", err), http.StatusInternalServerError)
			return
		}
		novncURL = getenvDefault("CODEGEN_NOVNC_URL", "http://localhost:7000/vnc.html?autoconnect=1&resize=remote")
		logPath = ""
	} else {
		if _, err := exec.LookPath("npx"); err != nil {
			http.Error(w, "npx not found on PATH", http.StatusBadRequest)
			return
		}
		logFile, err := os.Create(logPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create log file: %v", err), http.StatusInternalServerError)
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
	}

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

func startCodegenInContainer(url, outputHostPath string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found on PATH")
	}

	containerName, err := resolveCodegenContainerName()
	if err != nil {
		return err
	}

	outputContainerPath := filepath.Join("/output", filepath.Base(outputHostPath))
	chromiumFlags := []string{
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--disable-software-rasterizer",
		"--disable-setuid-sandbox",
		"--disable-accelerated-2d-canvas",
		"--disable-accelerated-video-decode",
	}
	args := []string{
		"exec", "-e", "DISPLAY=:99", containerName,
		"npx", "playwright", "codegen", "--output", outputContainerPath, url,
		"--browser=chromium",
		"--",
	}
	args = append(args, chromiumFlags...)
	cmd := exec.Command("docker", args...)

	if err := cmd.Start(); err != nil {
		return err
	}
	return nil
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
	codegenMu.Unlock()
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	if strings.ToLower(strings.TrimSpace(os.Getenv("CODEGEN_MODE"))) == "container" {
		if _, err := os.Stat(session.OutputPath); err == nil {
			if session.Status != "completed" {
				finished := time.Now()
				session.CompletedAt = &finished
				session.Status = "completed"
			}
		}
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
	codegenMu.Unlock()
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	if session.Status != "completed" {
		http.Error(w, "Codegen not completed", http.StatusConflict)
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

// handleCodegenLatest returns the most recently completed codegen script
func handleCodegenLatest(w http.ResponseWriter, r *http.Request) {
	if handleCORSPreflight(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	codegenMu.Lock()
	var latest *CodegenSession
	for _, s := range codegenSessions {
		if s.Status != "completed" || s.OutputPath == "" {
			continue
		}
		if latest == nil || (s.CompletedAt != nil && latest.CompletedAt != nil && s.CompletedAt.After(*latest.CompletedAt)) {
			latest = s
		}
	}
	codegenMu.Unlock()

	if latest == nil {
		http.Error(w, "No completed codegen sessions", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(latest.OutputPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read output: %v", err), http.StatusInternalServerError)
		return
	}

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Script-File", filepath.Base(latest.OutputPath))
	if latest.CompletedAt != nil {
		w.Header().Set("X-Script-Modified", latest.CompletedAt.Format(time.RFC3339))
	}
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
