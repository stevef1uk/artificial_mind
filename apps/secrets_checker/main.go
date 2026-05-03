package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config represents the application configuration
type Config struct {
	GitHubUsers         []string `json:"github_users"`
	MonitorDir          string   `json:"monitor_dir"`
	MCPServerURL        string   `json:"mcp_server_url"` // Kept for compatibility if needed later
	StateFile           string   `json:"state_file"`
	Concurrency         int      `json:"concurrency"`
	StaleThresholdDays  int      `json:"stale_threshold_days"`
	MaxReposPerUser     int      `json:"max_repos_per_user"`
}

// State stores the last scanned commit for each repository
type State struct {
	LastCommits map[string]string `json:"last_commits"`
}

// FoundKey matches the structure in secret_scanner
type FoundKey struct {
	Type   string `json:"type"`
	Last4  string `json:"last4"`
	LineNo int    `json:"line_no"`
	Offset int64  `json:"offset"`
}

// ScanResult matches the structure in secret_scanner
type ScanResult struct {
	ExposedKeys []FoundKey `json:"exposed_keys"`
	Status      string     `json:"status"`
}

var (
	config  Config
	state   State
	stateMu sync.Mutex
	scanner *InternalScanner
)

func main() {
	configPath := flag.String("config", "config.json", "Path to config file")
	force := flag.Bool("force", false, "Force scan all files regardless of state")
	flag.Parse()

	loadConfig(*configPath)
	loadState()

	scanner = NewInternalScanner()

	if config.MonitorDir == "" {
		cwd, _ := os.Getwd()
		config.MonitorDir = filepath.Join(cwd, "repos")
	}
	os.MkdirAll(config.MonitorDir, 0755)

	// Discover and clone repos for GitHub users
	for _, user := range config.GitHubUsers {
		log.Printf("Fetching repositories for user: %s", user)
		repoURLs, err := fetchGitHubRepos(user)
		if err != nil {
			log.Printf("Error fetching repos for %s: %v", user, err)
			continue
		}
		for _, repoURL := range repoURLs {
			ensureRepo(user, repoURL)
		}
	}

	repos := findGitRepos(config.MonitorDir)
	log.Printf("Found %d repositories to scan", len(repos))

	if config.Concurrency <= 0 {
		config.Concurrency = 10
	}

	repoChan := make(chan string, len(repos))
	for _, repo := range repos {
		repoChan <- repo
	}
	close(repoChan)

	var wg sync.WaitGroup
	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repoPath := range repoChan {
				if *force {
					stateMu.Lock()
					delete(state.LastCommits, repoPath)
					stateMu.Unlock()
				}
				scanRepo(repoPath)
			}
		}()
	}
	wg.Wait()

	saveState()
	log.Println("Scan completed.")
}

func loadConfig(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: Config file not found, using defaults: %v", err)
		config = Config{
			MCPServerURL:       "http://localhost:8080/mcp",
			StateFile:          "state.json",
			Concurrency:        10,
			StaleThresholdDays: 30,
			MaxReposPerUser:    20,
		}
		return
	}
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatalf("Error parsing config: %v", err)
	}
	if config.StaleThresholdDays <= 0 {
		config.StaleThresholdDays = 30
	}
}

func loadState() {
	state.LastCommits = make(map[string]string)
	data, err := os.ReadFile(config.StateFile)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &state)
}

func saveState() {
	stateMu.Lock()
	defer stateMu.Unlock()
	data, _ := json.MarshalIndent(state, "", "  ")
	_ = os.WriteFile(config.StateFile, data, 0644)
}

func findGitRepos(root string) []string {
	var repos []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && info.Name() == ".git" {
			repos = append(repos, filepath.Dir(path))
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		log.Printf("Error walking directory: %v", err)
	}
	return repos
}

func scanRepo(repoPath string) {
	repoName := filepath.Base(repoPath)
	log.Printf("[%s] Scanning repository...", repoName)

	stateMu.Lock()
	lastCommit := state.LastCommits[repoPath]
	stateMu.Unlock()

	// Get current HEAD
	headCmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	headOutput, err := headCmd.Output()
	if err != nil {
		log.Printf("[%s] Error getting HEAD: %v", repoName, err)
		return
	}
	currentHead := strings.TrimSpace(string(headOutput))

	if currentHead == lastCommit {
		log.Printf("[%s] No new commits since last scan (%s)", repoName, currentHead[:8])
		return
	}

	// Get changed files
	var diffRange string
	if lastCommit == "" {
		// First scan, scan all files in current HEAD
		diffRange = "HEAD"
	} else {
		diffRange = fmt.Sprintf("%s..%s", lastCommit, currentHead)
	}

	var files []string
	if lastCommit == "" {
		filesCmd := exec.Command("git", "-C", repoPath, "ls-tree", "-r", "--name-only", "HEAD")
		filesOutput, err := filesCmd.Output()
		if err != nil {
			log.Printf("[%s] Error getting files: %v", repoName, err)
			return
		}
		files = strings.Split(strings.TrimSpace(string(filesOutput)), "\n")
	} else {
		filesCmd := exec.Command("git", "-C", repoPath, "diff", "--name-only", diffRange)
		filesOutput, err := filesCmd.Output()
		if err != nil {
			log.Printf("[%s] Error getting changed files: %v", repoName, err)
			return
		}
		files = strings.Split(strings.TrimSpace(string(filesOutput)), "\n")
	}

	for _, file := range files {
		if file == "" {
			continue
		}
		fullPath := filepath.Join(repoPath, file)
		
		// Check if it's a file (not deleted)
		if info, err := os.Stat(fullPath); err != nil || info.IsDir() {
			continue
		}

		checkFile(repoName, fullPath)
	}

	stateMu.Lock()
	state.LastCommits[repoPath] = currentHead
	stateMu.Unlock()
}

func checkFile(repoName, filePath string) bool {
	// Skip binary files and large files for efficiency
	if isBinaryOrLarge(filePath) {
		return false
	}

	log.Printf("[%s] Checking file: %s", repoName, filepath.Base(filePath))

	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("[%s] Error opening file %s: %v", repoName, filePath, err)
		return false
	}
	defer f.Close()

	scanResult, err := scanner.Scan(f)
	if err != nil {
		log.Printf("[%s] Scan error for %s: %v", repoName, filePath, err)
		return false
	}

	if len(scanResult.ExposedKeys) > 0 {
		msg := fmt.Sprintf("⚠️ [%s] FOUND SECRETS in %s!\n", repoName, filepath.Base(filePath))
		log.Print(msg)
		for _, key := range scanResult.ExposedKeys {
			detail := fmt.Sprintf("   - Type: %s, Line: %d, Last4: %s\n", key.Type, key.LineNo, key.Last4)
			log.Print(detail)
			msg += detail
		}
		sendTelegramAlert(msg)
		return true
	}
	return false
}

func sendTelegramAlert(message string) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chatID == "" {
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]string{
		"chat_id": chatID,
		"text":    message,
	}
	body, _ := json.Marshal(payload)
	
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("Error sending Telegram alert: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram API returned status %d", resp.StatusCode)
	}
}

func isBinaryOrLarge(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	binaryExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
		".exe": true, ".bin": true, ".zip": true, ".tar": true, ".gz": true,
		".pdf": true, ".docx": true, ".xlsx": true, ".pptx": true,
		".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	}
	if binaryExts[ext] {
		return true
	}

	info, err := os.Stat(path)
	if err == nil && info.Size() > 5*1024*1024 { // Skip files larger than 5MB
		return true
	}
	return false
}

func fetchGitHubRepos(user string) ([]string, error) {
	url := fmt.Sprintf("https://api.github.com/users/%s/repos?sort=pushed&direction=desc&per_page=100", user)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var repos []struct {
		CloneURL string    `json:"clone_url"`
		PushedAt time.Time `json:"pushed_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, err
	}

	var urls []string
	staleThreshold := time.Duration(config.StaleThresholdDays) * 24 * time.Hour
	for _, r := range repos {
		if time.Since(r.PushedAt) > staleThreshold {
			continue
		}
		urls = append(urls, r.CloneURL)
		if config.MaxReposPerUser > 0 && len(urls) >= config.MaxReposPerUser {
			break
		}
	}
	return urls, nil
}

func ensureRepo(user, cloneURL string) {
	repoName := filepath.Base(cloneURL)
	repoName = strings.TrimSuffix(repoName, ".git")
	targetDir := filepath.Join(config.MonitorDir, user, repoName)

	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		log.Printf("Cloning %s...", cloneURL)
		os.MkdirAll(filepath.Dir(targetDir), 0755)
		cmd := exec.Command("git", "clone", "--depth", "1", cloneURL, targetDir)
		if err := cmd.Run(); err != nil {
			log.Printf("Error cloning %s: %v", cloneURL, err)
		}
	} else {
		log.Printf("Updating %s...", repoName)
		cmd := exec.Command("git", "-C", targetDir, "pull")
		_ = cmd.Run() // Best effort update
	}
}
