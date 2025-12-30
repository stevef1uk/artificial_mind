package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// HostInputFilesDir is the host path that will be mounted read-only into the container at /app/input_files
// Can be overridden via environment variable INPUT_FILES_DIR
var HostInputFilesDir = func() string {
	if v := os.Getenv("INPUT_FILES_DIR"); v != "" {
		return v
	}
	// Derive from AGI_PROJECT_ROOT if set, otherwise fall back to relative path
	if root := os.Getenv("AGI_PROJECT_ROOT"); strings.TrimSpace(root) != "" {
		return filepath.Join(root, "input_files")
	}
	// Ensure absolute path for Docker volume mount
	absPath, err := filepath.Abs("./input_files")
	if err != nil {
		return filepath.Clean("./input_files")
	}
	return absPath
}()

// HostToolsBinDir is the host path to compiled tool binaries (mounted read-only at /app/tools)
// Override with TOOL_BIN_DIR
var HostToolsBinDir = func() string {
	if v := os.Getenv("TOOL_BIN_DIR"); v != "" {
		return v
	}
	if root := os.Getenv("AGI_PROJECT_ROOT"); strings.TrimSpace(root) != "" {
		return filepath.Join(root, "bin", "tools")
	}
	// Ensure absolute path for Docker volume mount
	absPath, err := filepath.Abs("./bin/tools")
	if err != nil {
		return filepath.Clean("./bin/tools")
	}
	return absPath
}()

// Docker resource limits - configurable via environment variables
var DockerMemoryLimit = func() string {
	if v := os.Getenv("DOCKER_MEMORY_LIMIT"); v != "" {
		return v
	}
	return "512m" // Default to 512MB for Raspberry Pi
}()

var DockerCPULimit = func() string {
	if v := os.Getenv("DOCKER_CPU_LIMIT"); v != "" {
		return v
	}
	return "1.0" // Default to 1 CPU for Raspberry Pi
}()

var DockerPidsLimit = func() string {
	if v := os.Getenv("DOCKER_PIDS_LIMIT"); v != "" {
		return v
	}
	return "256" // Default to 256 processes for Raspberry Pi
}()

var DockerTmpfsSize = func() string {
	if v := os.Getenv("DOCKER_TMPFS_SIZE"); v != "" {
		return v
	}
	return "128m" // Default to 128MB for Raspberry Pi
}()

// SimpleDockerExecutor handles code execution using simple Docker commands
type SimpleDockerExecutor struct {
	fileStorage *FileStorage
}

// NewSimpleDockerExecutor creates a new simple Docker executor
func NewSimpleDockerExecutor() *SimpleDockerExecutor {
	return &SimpleDockerExecutor{}
}

// NewSimpleDockerExecutorWithStorage creates a new simple Docker executor with file storage
func NewSimpleDockerExecutorWithStorage(fileStorage *FileStorage) *SimpleDockerExecutor {
	return &SimpleDockerExecutor{
		fileStorage: fileStorage,
	}
}

// ExecuteCode executes code using Docker command line
func (sde *SimpleDockerExecutor) ExecuteCode(ctx context.Context, req *DockerExecutionRequest) (*DockerExecutionResponse, error) {
	start := time.Now()

	// Create a unique container name
	containerName := fmt.Sprintf("code-executor-%d", time.Now().UnixNano())

	// Determine the base image and command based on language
	image, cmd, err := sde.getLanguageConfig(req.Language)
	if err != nil {
		return &DockerExecutionResponse{
			Success: false,
			Error:   fmt.Sprintf("Unsupported language: %s", req.Language),
		}, nil
	}

	// Create temporary file for code
	tempFile := fmt.Sprintf("/tmp/code-%d.%s", time.Now().UnixNano(), sde.getFileExtension(req.Language))
	if err := sde.writeCodeToFile(tempFile, req.Code); err != nil {
		return &DockerExecutionResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to write code file: %v", err),
		}, nil
	}
	defer sde.cleanupFile(tempFile)

	// Create output directory for files
	outputDir := fmt.Sprintf("/tmp/output-%d", time.Now().UnixNano())
	os.MkdirAll(outputDir, 0755)
	// Ensure the host output directory is writable by the container
	_ = os.Chmod(outputDir, 0777)
	defer os.RemoveAll(outputDir)

	// Build Docker command with output volume (pass hasInput flag)
	dockerCmd := sde.buildDockerCommand(image, cmd, tempFile, containerName, outputDir, req.Timeout, req.Environment, req.Input != "")

	// Start with a clean output directory
	_ = os.RemoveAll(outputDir)
	_ = os.MkdirAll(outputDir, 0o755)

	// Execute Docker command (capture stdout and stderr separately)
	log.Printf("🐳 [DOCKER] Executing: %s", strings.Join(dockerCmd, " "))
	execCmd := exec.CommandContext(ctx, dockerCmd[0], dockerCmd[1:]...)
	var stdoutBuf, stderrBuf bytes.Buffer
	execCmd.Stdout = &stdoutBuf
	execCmd.Stderr = &stderrBuf

	// If input is provided, pipe it to stdin
	// Note: We only set stdin if input is non-empty
	// The -i flag is already added in buildDockerCommand when hasInput is true
	if req.Input != "" && strings.TrimSpace(req.Input) != "" {
		inputLen := len(req.Input)
		previewLen := 50
		if inputLen < previewLen {
			previewLen = inputLen
		}
		execCmd.Stdin = strings.NewReader(req.Input)
		log.Printf("📥 [DOCKER] Providing input to program (%d bytes): %s", inputLen, req.Input[:previewLen])
	} else if req.Input != "" {
		// Empty or whitespace-only input - don't set stdin
		log.Printf("⚠️ [DOCKER] Input provided but is empty/whitespace, not setting stdin")
	}

	runErr := execCmd.Run()

	executionTime := time.Since(start).Milliseconds()

	// Extract generated files
	files := sde.extractGeneratedFiles(outputDir)

	// Store files in Redis if file storage is available and execution was successful
	// We'll determine success after checking the output for errors
	if sde.fileStorage != nil {
		log.Printf("🔍 [DOCKER] File storage available, will store files after success check")
	} else {
		log.Printf("⚠️ [DOCKER] File storage is nil, skipping file storage")
	}

	// Clean up container
	go sde.cleanupContainer(containerName)

	// Prepare stdout string only (keep stderr for logs, not artifacts)
	outputStr := stdoutBuf.String()
	errStr := stderrBuf.String()

	// Always log stderr if present, even if execution succeeded (might contain warnings)
	if strings.TrimSpace(errStr) != "" {
		log.Printf("📋 [DOCKER] stderr output: %s", errStr)
	}

	if trimmed := strings.TrimSpace(outputStr); trimmed != "" {
		files["output.txt"] = []byte(trimmed)
		log.Printf("📄 [DOCKER] Captured stdout as output.txt (%d bytes)", len(trimmed))
	} else {
		log.Printf("⚠️ [DOCKER] No stdout output captured (empty)")
	}

	// Check if command execution failed
	if runErr != nil {
		if strings.TrimSpace(errStr) != "" {
			log.Printf("⚠️ [DOCKER] stderr: %s", errStr)
		}
		if strings.TrimSpace(outputStr) != "" {
			log.Printf("⚠️ [DOCKER] stdout (may contain errors): %s", outputStr)
		}
		log.Printf("❌ [DOCKER] Command execution failed: %v", runErr)

		// For compilation errors (especially Go), include stderr AND stdout in the error message
		// Go compilation errors often go to stdout, not stderr
		// This gives the LLM the actual compilation error to fix
		errorMsg := runErr.Error()
		errorDetails := ""

		// For Go, check both stdout and stderr for compilation errors
		if req.Language == "go" {
			// Go compilation errors typically appear in stdout
			if strings.TrimSpace(outputStr) != "" {
				// Check if stdout contains Go compilation errors
				if strings.Contains(outputStr, "imported and not used") ||
					strings.Contains(outputStr, "declared but not used") ||
					strings.Contains(outputStr, "undefined:") ||
					strings.Contains(outputStr, "cannot use") ||
					strings.Contains(outputStr, "syntax error") ||
					strings.Contains(outputStr, "./code.go:") {
					errorDetails = outputStr
				}
			}
			// Also check stderr
			if strings.TrimSpace(errStr) != "" {
				if errorDetails != "" {
					errorDetails += "\n" + errStr
				} else {
					errorDetails = errStr
				}
			}
		} else {
			// For other languages, prefer stderr
			if strings.TrimSpace(errStr) != "" {
				errorDetails = errStr
			} else if strings.TrimSpace(outputStr) != "" {
				errorDetails = outputStr
			}
		}

		if errorDetails != "" {
			errorMsg = fmt.Sprintf("%s\n\nCompilation/Execution Error Details:\n%s", errorMsg, errorDetails)
		}

		return &DockerExecutionResponse{
			Success:       false,
			Output:        outputStr,
			Error:         errorMsg,
			ExitCode:      1,
			ExecutionTime: executionTime,
			ContainerID:   containerName,
			Files:         files,
		}, nil
	}

	// Check if the Docker container exited with non-zero code
	// We need to parse the output to determine if the Python code failed
	success := true
	exitCode := 0
	errorMsg := ""

	// Check for common Python error patterns in both stdout and stderr
	// Python errors can appear in either stream
	combinedOutput := outputStr
	if strings.TrimSpace(errStr) != "" {
		combinedOutput = outputStr + "\n" + errStr
	}

	if strings.Contains(combinedOutput, "Traceback") ||
		strings.Contains(combinedOutput, "Error:") ||
		strings.Contains(combinedOutput, "Exception:") ||
		strings.Contains(combinedOutput, "SyntaxError") ||
		strings.Contains(combinedOutput, "NameError") ||
		strings.Contains(combinedOutput, "ImportError") ||
		strings.Contains(combinedOutput, "ModuleNotFoundError") {
		success = false
		exitCode = 1
		errorMsg = "Python execution failed: " + combinedOutput
		log.Printf("❌ [DOCKER] Python execution failed: %s", combinedOutput)
	} else {
		log.Printf("✅ [DOCKER] Python execution successful")
		log.Printf("📊 [DOCKER] Output: %s", outputStr)
		if strings.TrimSpace(errStr) != "" {
			log.Printf("📊 [DOCKER] stderr (warnings): %s", errStr)
		}
	}

	// Store files in Redis if not a validation attempt (store even on failure to capture errors)
	if sde.fileStorage != nil && !req.IsValidation {
		if success {
			log.Printf("🔍 [DOCKER] Execution successful, storing %d files", len(files))
		} else {
			log.Printf("⚠️ [DOCKER] Execution failed, but storing %d files for debugging", len(files))
		}
		// Also persist stdout as a file artifact so the UI can display results
		if trimmed := strings.TrimSpace(outputStr); trimmed != "" {
			if _, exists := files["output.txt"]; !exists {
				files["output.txt"] = []byte(trimmed)
				log.Printf("📄 [DOCKER] Captured stdout as output.txt (%d bytes)", len(trimmed))
			}
		}
		if strings.TrimSpace(errStr) != "" {
			// Store stderr separately for debugging (especially important on failure)
			files["stderr.txt"] = []byte(errStr)
			log.Printf("📄 [DOCKER] Captured stderr as stderr.txt (%d bytes)", len(errStr))
		}
		sde.storeFilesInRedis(files, req.WorkflowID, req.StepID)
	} else if sde.fileStorage != nil && req.IsValidation {
		log.Printf("🔍 [DOCKER] Validation attempt, skipping file storage")
	}

	return &DockerExecutionResponse{
		Success:       success,
		Output:        outputStr,
		Error:         errorMsg,
		ExitCode:      exitCode,
		ExecutionTime: executionTime,
		ContainerID:   containerName,
		Files:         files,
	}, nil
}

// ExecutePrimeCalculation demonstrates executing a prime calculation
func (sde *SimpleDockerExecutor) ExecutePrimeCalculation(ctx context.Context) (*DockerExecutionResponse, error) {
	pythonCode := `#!/usr/bin/env python3
import sys
import time

def is_prime(n):
    if n < 2:
        return False
    for i in range(2, int(n**0.5) + 1):
        if n % i == 0:
            return False
    return True

def find_first_n_primes(n):
    primes = []
    num = 2
    while len(primes) < n:
        if is_prime(num):
            primes.append(num)
        num += 1
    return primes

if __name__ == "__main__":
    try:
        n = 10
        if len(sys.argv) > 1:
            n = int(sys.argv[1])
        
        start_time = time.time()
        primes = find_first_n_primes(n)
        end_time = time.time()
        
        print(f"First {n} prime numbers:")
        print(", ".join(map(str, primes)))
        print(f"Calculation time: {end_time - start_time:.4f} seconds")
        
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)
`

	req := &DockerExecutionRequest{
		Language: "python",
		Code:     pythonCode,
		Input:    "",
		Timeout:  600,
	}

	return sde.ExecuteCode(ctx, req)
}

// getLanguageConfig returns Docker configuration for a programming language
func (sde *SimpleDockerExecutor) getLanguageConfig(language string) (image, cmd string, err error) {
	switch strings.ToLower(language) {
	case "python", "py":
		return "python:3.11-slim", "python", nil
	case "javascript", "js", "node":
		return "node:18-slim", "node", nil
	case "go":
		return "golang:1.21-alpine", "go run", nil
	case "java":
		return "eclipse-temurin:17-jdk", "java", nil
	case "cpp", "c++":
		return "gcc:latest", "g++ -o main && ./main", nil
	case "c":
		return "gcc:latest", "gcc -o main && ./main", nil
	case "rust":
		return "rust:1.70-slim", "cargo run", nil
	default:
		return "", "", fmt.Errorf("unsupported language: %s", language)
	}
}

// getFileExtension returns the file extension for a language
func (sde *SimpleDockerExecutor) getFileExtension(language string) string {
	switch strings.ToLower(language) {
	case "python", "py":
		return "py"
	case "javascript", "js", "node":
		return "js"
	case "go":
		return "go"
	case "java":
		return "java"
	case "cpp", "c++":
		return "cpp"
	case "c":
		return "c"
	case "rust":
		return "rs"
	default:
		return "txt"
	}
}

// writeCodeToFile writes code to a temporary file
func (sde *SimpleDockerExecutor) writeCodeToFile(filename, code string) error {
	return os.WriteFile(filename, []byte(code), 0644)
}

// cleanupFile removes a temporary file
func (sde *SimpleDockerExecutor) cleanupFile(filename string) {
	exec.Command("rm", "-f", filename).Run()
}

// cleanupContainer removes a Docker container
func (sde *SimpleDockerExecutor) cleanupContainer(containerName string) {
	exec.Command("docker", "rm", "-f", containerName).Run()
}

// addDataFileMounts dynamically adds data file mounts based on context
func (sde *SimpleDockerExecutor) addDataFileMounts(args []string, context map[string]string) []string {
	// Look for data file references in the context
	for key, value := range context {
		if strings.Contains(strings.ToLower(key), "data") ||
			strings.Contains(strings.ToLower(key), "file") ||
			strings.Contains(strings.ToLower(key), "source") ||
			strings.Contains(strings.ToLower(key), "input") {
			// Check if the value looks like a file path
			if strings.Contains(value, ".") && !strings.Contains(value, " ") {
				// Try to find the file in common locations
				// Resolve project root from env if available
				projectRoot := strings.TrimSpace(os.Getenv("AGI_PROJECT_ROOT"))
				if projectRoot == "" {
					projectRoot = "."
				}
				possiblePaths := []string{
					value, // Direct path
					filepath.Clean(filepath.Join(".", value)),                 // Current directory
					filepath.Clean(filepath.Join(projectRoot, value)),         // Project root
					filepath.Clean(filepath.Join(projectRoot, "data", value)), // Data directory
					filepath.Base(value),                                      // Just the filename
				}

				for _, path := range possiblePaths {
					if _, err := os.Stat(path); err == nil {
						// File exists, mount it
						containerPath := "/app/data/" + filepath.Base(value)
						args = append(args, "-v", fmt.Sprintf("%s:%s", path, containerPath))
						log.Printf("📁 [DOCKER] Mounting data file: %s -> %s", path, containerPath)
						break
					}
				}
			}
		}
	}
	return args
}

// buildDockerCommand builds the Docker command to execute
func (sde *SimpleDockerExecutor) buildDockerCommand(image, cmd, codeFile, containerName, outputDir string, timeout int, context map[string]string, hasInput bool) []string {
	// For simple execution, we'll use docker run with volume mount
	args := []string{
		"docker", "run",
		"--rm",
		"--name", containerName,
		// Harden container: no new privileges, drop all caps, read-only fs, limited memory/CPU, no network by default
		"--security-opt", "no-new-privileges:true",
		"--cap-drop", "ALL",
		"--pids-limit", DockerPidsLimit,
		"--memory", DockerMemoryLimit,
		"--cpus", DockerCPULimit,
		// Provide minimal writable tmpfs for Python/matplotlib if needed
		"--tmpfs", fmt.Sprintf("/tmp:rw,nosuid,size=%s", DockerTmpfsSize),
		// Enable default network for dependency installs (pip/go mod) used in demos
		"--network", "bridge",
		// Add host.docker.internal for container-to-host communication (works on Mac/Windows, Linux 20.10+)
		// Note: This may fail on very old Docker versions, but the error is non-fatal
		"--add-host", "host.docker.internal:host-gateway",
		"-v", fmt.Sprintf("%s:/app/code.%s", codeFile, sde.getFileExtensionFromFile(codeFile)),
		"-v", fmt.Sprintf("%s:/app/output", outputDir),
	}

	// Add -i flag if input is provided to keep stdin open
	if hasInput {
		args = append(args, "-i")
		log.Printf("📥 [DOCKER] Adding -i flag for stdin input")
	}

	// Pass environment variables
	if context != nil {
		for key, value := range context {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
		}
	}

	// Mount local input_files directory into the container at /app/input_files (read-only) if it exists
	if st, err := os.Stat(HostInputFilesDir); err == nil && st.IsDir() {
		args = append(args, "-v", fmt.Sprintf("%s:%s", HostInputFilesDir, "/app/input_files:ro"))
		log.Printf("📁 [DOCKER] Mounted input directory: %s -> %s", HostInputFilesDir, "/app/input_files:ro")
	}

	// Mount compiled tool binaries at /app/tools (read-only) if exists
	if st, err := os.Stat(HostToolsBinDir); err == nil && st.IsDir() {
		args = append(args, "-v", fmt.Sprintf("%s:%s", HostToolsBinDir, "/app/tools:ro"))
		log.Printf("🛠️ [DOCKER] Mounted tools directory: %s -> %s", HostToolsBinDir, "/app/tools:ro")
	}

	// Add data file mounts based on context
	if context != nil {
		args = sde.addDataFileMounts(args, context)
	}

	// Add the image name after all volume mounts
	args = append(args, image)

	quiet := false
	if context != nil {
		if q, ok := context["QUIET"]; ok && strings.TrimSpace(strings.ToLower(q)) == "1" {
			quiet = true
		}
	}

	// For Go, we need to handle modules specially
	if sde.getFileExtensionFromFile(codeFile) == "go" {
		// Prepare writable dirs and build a binary explicitly to avoid /tmp noexec and go run issues
		if quiet {
			args = append(args, "sh", "-c", fmt.Sprintf(`
			set -e
			cd /app && 
			mkdir -p /app/.cache /app/tmp && 
			export GOCACHE=/app/.cache 
			if [ ! -f go.mod ]; then go mod init test-module >/dev/null 2>&1; fi && 
			go mod tidy >/dev/null 2>&1 && 
			go build -o /app/tmp/app code.%s 2>&1 || (go build -o /app/tmp/app code.%s 2>&1; exit 1) && 
			/app/tmp/app
		`, sde.getFileExtensionFromFile(codeFile), sde.getFileExtensionFromFile(codeFile)))
		} else {
			args = append(args, "sh", "-c", fmt.Sprintf(`
			set -e
			cd /app && 
			mkdir -p /app/.cache /app/tmp && 
			export GOCACHE=/app/.cache 
			# Initialize go.mod only if missing
			if [ ! -f go.mod ]; then go mod init test-module >/dev/null 2>&1; fi && 
			go mod tidy >/dev/null 2>&1 && 
			# Build with -e flag to continue on unused variable/import warnings (they're non-fatal)
			go build -o /app/tmp/app code.%s 2>&1 || exit 1 && 
			/app/tmp/app &&
			cp *.go *.mod *.sum *.pdf *.png *.jpg *.jpeg *.csv *.txt *.json *.md /app/output/ 2>/dev/null || true
		`, sde.getFileExtensionFromFile(codeFile)))
		}
	} else if sde.getFileExtensionFromFile(codeFile) == "py" {
		// For Python, analyze code and generate dynamic requirements.txt
		// Even in quiet mode, we need to install packages if code imports them
		// Read code file to check for imports
		codeContent, _ := os.ReadFile(codeFile)
		codeStr := string(codeContent)
		needsPackages := strings.Contains(codeStr, "import requests") || strings.Contains(codeStr, "from requests") ||
			strings.Contains(codeStr, "requests.post") || strings.Contains(codeStr, "requests.get")

		if quiet && !needsPackages {
			// Quiet mode and no packages needed - simple execution
			args = append(args, "sh", "-c", fmt.Sprintf(`
            cd /app && 
            mkdir -p /app/data && 
            python code.%s
        `, sde.getFileExtensionFromFile(codeFile)))
		} else if quiet && needsPackages {
			// Quiet mode but packages needed - install silently then run
			// Use the same package detection logic as non-quiet mode, but redirect output to stderr
			args = append(args, "sh", "-c", fmt.Sprintf(`
			set -eu
			cd /app && 
			mkdir -p /app/data && 
			# Analyze the code to determine what packages are actually needed (silently)
			python3 -c "
import re
import sys

# Read the code file
with open('code.%s', 'r') as f:
    code = f.read()

# Extract import statements
imports = re.findall(r'^import\s+([\w.]+)|^from\s+([\w.]+)\s+import', code, re.MULTILINE)
packages = set()

# Built-in Python modules that don't need to be installed
builtin_modules = {
    'csv', 'json', 'os', 'sys', 'time', 'datetime', 'math', 'random', 'collections',
    'itertools', 'functools', 'operator', 're', 'string', 'io', 'pathlib', 'urllib',
    'http', 'socket', 'threading', 'multiprocessing', 'subprocess', 'shutil',
    'glob', 'tempfile', 'zipfile', 'tarfile', 'gzip', 'bz2', 'lzma', 'base64',
    'hashlib', 'hmac', 'secrets', 'uuid', 'pickle', 'copy', 'pprint', 'traceback',
    'logging', 'warnings', 'contextlib', 'typing', 'dataclasses', 'enum', 'abc',
    'argparse', 'configparser', 'getopt', 'optparse', 'fileinput', 'linecache',
    'cmd', 'shlex', 'readline', 'rlcompleter', 'stat', 'filecmp', 'fnmatch',
    'glob', 'linecache', 'tempfile', 'shutil', 'zipfile', 'tarfile', 'gzip',
    'bz2', 'lzma', 'base64', 'binascii', 'quopri', 'uu', 'codecs', 'unicodedata',
    'stringprep', 'locale', 'gettext', 'calendar', 'collections', 'heapq', 'bisect',
    'array', 'weakref', 'types', 'copy', 'pprint', 'reprlib', 'enum', 'numbers',
    'math', 'cmath', 'decimal', 'fractions', 'random', 'statistics', 'itertools',
    'functools', 'operator', 'pathlib', 'os', 'io', 'time', 'argparse', 'getopt',
    'logging', 'getpass', 'curses', 'platform', 'errno', 'ctypes', 'threading',
    'multiprocessing', 'concurrent', 'subprocess', 'sched', 'queue', 'contextvars',
    '_thread', 'dummy_threading', 'asyncio', 'socket', 'ssl', 'select', 'selectors',
    'signal', 'mmap', 'readline', 'rlcompleter', 'cmd', 'shlex', 'tkinter',
    'turtle', 'pdb', 'profile', 'pstats', 'timeit', 'trace', 'tracemalloc',
    'distutils', 'ensurepip', 'venv', 'zipapp', 'runpy', 'importlib', 'pkgutil',
    'modulefinder', 'sys', 'builtins', 'warnings', 'contextlib', 'abc', 'atexit',
    'traceback', 'future_builtins', 'gc', 'inspect', 'site', 'sysconfig', 'ast',
    'symtable', 'keyword', 'token', 'tokenize', 'tabnanny', 'pyclbr', 'py_compile',
    'compileall', 'dis', 'pickletools', 'formatter', 'codeop', 'code', 'codecs',
    'unicodedata', 'stringprep', 'locale', 'gettext', 'calendar', 'collections',
    'heapq', 'bisect', 'array', 'weakref', 'types', 'copy', 'pprint', 'reprlib',
    'enum', 'numbers', 'math', 'cmath', 'decimal', 'fractions', 'random',
    'statistics', 'itertools', 'functools', 'operator', 'pathlib', 'os', 'io',
    'time', 'argparse', 'getopt', 'logging', 'getpass', 'curses', 'platform',
    'errno', 'ctypes', 'threading', 'multiprocessing', 'concurrent', 'subprocess',
    'sched', 'queue', 'contextvars', '_thread', 'dummy_threading', 'asyncio',
    'socket', 'ssl', 'select', 'selectors', 'signal', 'mmap', 'readline',
    'rlcompleter', 'cmd', 'shlex', 'tkinter', 'turtle', 'pdb', 'profile', 'pstats',
    'timeit', 'trace', 'tracemalloc', 'distutils', 'ensurepip', 'venv', 'zipapp',
    'runpy', 'importlib', 'pkgutil', 'modulefinder', 'sys', 'builtins', 'warnings',
    'contextlib', 'abc', 'atexit', 'traceback', 'future_builtins', 'gc', 'inspect',
    'site', 'sysconfig', 'ast', 'symtable', 'keyword', 'token', 'tokenize',
    'tabnanny', 'pyclbr', 'py_compile', 'compileall', 'dis', 'pickletools',
    'formatter', 'codeop', 'code'
}

for imp in imports:
    package = imp[0] or imp[1]  # Get the package name
    if package and not package.startswith('_'):
        # For dotted imports like 'reportlab.pdfgen', use only the first part
        base_package = package.split('.')[0]
        # Skip tool_* patterns (these are internal tool identifiers, not real packages)
        if base_package.startswith('tool_'):
            continue
        # Only add if it's not a built-in module
        if base_package not in builtin_modules:
            packages.add(base_package)

# Map common packages to their pip names
package_map = {
    'numpy': 'numpy>=1.21.0',
    'pandas': 'pandas>=1.3.0', 
    'matplotlib': 'matplotlib>=3.5.0',
    'reportlab': 'reportlab>=3.6.0',
    'requests': 'requests>=2.25.0',
    'beautifulsoup4': 'beautifulsoup4>=4.9.0',
    'scipy': 'scipy>=1.7.0',
    'sklearn': 'scikit-learn>=1.0.0',
    'seaborn': 'seaborn>=0.11.0',
    'plotly': 'plotly>=5.0.0',
    'openpyxl': 'openpyxl>=3.0.0',
    'xlrd': 'xlrd>=2.0.0',
    'pillow': 'pillow>=8.0.0',
    'cv2': 'opencv-python>=4.5.0',
    'tensorflow': 'tensorflow>=2.6.0',
    'torch': 'torch>=1.9.0',
    'transformers': 'transformers>=4.0.0'
}

# Generate requirements.txt only if there are packages
if packages:
    with open('requirements.txt', 'w') as f:
        for package in sorted(packages):
            if package in package_map:
                f.write(package_map[package] + '\n')
            else:
                # For unknown packages, try the package name as-is
                f.write(package + '\n')
" 1>&2 &&
			# Install only the packages that are actually needed (silently)
            if [ -f requirements.txt ] && [ -s requirements.txt ]; then
                pip install -r requirements.txt 1>&2
			fi &&
            python code.%s
        `, sde.getFileExtensionFromFile(codeFile), sde.getFileExtensionFromFile(codeFile)))
		} else {
			args = append(args, "sh", "-c", fmt.Sprintf(`
			set -eu
			cd /app && 
			# Ensure a data directory exists so user code can write to it
			mkdir -p /app/data && 
			# Analyze the code to determine what packages are actually needed
			python3 -c "
import re
import sys

# Read the code file
with open('code.%s', 'r') as f:
    code = f.read()

# Extract import statements
imports = re.findall(r'^import\s+([\w.]+)|^from\s+([\w.]+)\s+import', code, re.MULTILINE)
packages = set()

# Built-in Python modules that don't need to be installed
builtin_modules = {
    'csv', 'json', 'os', 'sys', 'time', 'datetime', 'math', 'random', 'collections',
    'itertools', 'functools', 'operator', 're', 'string', 'io', 'pathlib', 'urllib',
    'http', 'socket', 'threading', 'multiprocessing', 'subprocess', 'shutil',
    'glob', 'tempfile', 'zipfile', 'tarfile', 'gzip', 'bz2', 'lzma', 'base64',
    'hashlib', 'hmac', 'secrets', 'uuid', 'pickle', 'copy', 'pprint', 'traceback',
    'logging', 'warnings', 'contextlib', 'typing', 'dataclasses', 'enum', 'abc',
    'argparse', 'configparser', 'getopt', 'optparse', 'fileinput', 'linecache',
    'cmd', 'shlex', 'readline', 'rlcompleter', 'stat', 'filecmp', 'fnmatch',
    'glob', 'linecache', 'tempfile', 'shutil', 'zipfile', 'tarfile', 'gzip',
    'bz2', 'lzma', 'base64', 'binascii', 'quopri', 'uu', 'codecs', 'unicodedata',
    'stringprep', 'locale', 'gettext', 'calendar', 'collections', 'heapq', 'bisect',
    'array', 'weakref', 'types', 'copy', 'pprint', 'reprlib', 'enum', 'numbers',
    'math', 'cmath', 'decimal', 'fractions', 'random', 'statistics', 'itertools',
    'functools', 'operator', 'pathlib', 'os', 'io', 'time', 'argparse', 'getopt',
    'logging', 'getpass', 'curses', 'platform', 'errno', 'ctypes', 'threading',
    'multiprocessing', 'concurrent', 'subprocess', 'sched', 'queue', 'contextvars',
    '_thread', 'dummy_threading', 'asyncio', 'socket', 'ssl', 'select', 'selectors',
    'signal', 'mmap', 'readline', 'rlcompleter', 'cmd', 'shlex', 'tkinter',
    'turtle', 'pdb', 'profile', 'pstats', 'timeit', 'trace', 'tracemalloc',
    'distutils', 'ensurepip', 'venv', 'zipapp', 'runpy', 'importlib', 'pkgutil',
    'modulefinder', 'sys', 'builtins', 'warnings', 'contextlib', 'abc', 'atexit',
    'traceback', 'future_builtins', 'gc', 'inspect', 'site', 'sysconfig', 'ast',
    'symtable', 'keyword', 'token', 'tokenize', 'tabnanny', 'pyclbr', 'py_compile',
    'compileall', 'dis', 'pickletools', 'formatter', 'codeop', 'code', 'codecs',
    'unicodedata', 'stringprep', 'locale', 'gettext', 'calendar', 'collections',
    'heapq', 'bisect', 'array', 'weakref', 'types', 'copy', 'pprint', 'reprlib',
    'enum', 'numbers', 'math', 'cmath', 'decimal', 'fractions', 'random',
    'statistics', 'itertools', 'functools', 'operator', 'pathlib', 'os', 'io',
    'time', 'argparse', 'getopt', 'logging', 'getpass', 'curses', 'platform',
    'errno', 'ctypes', 'threading', 'multiprocessing', 'concurrent', 'subprocess',
    'sched', 'queue', 'contextvars', '_thread', 'dummy_threading', 'asyncio',
    'socket', 'ssl', 'select', 'selectors', 'signal', 'mmap', 'readline',
    'rlcompleter', 'cmd', 'shlex', 'tkinter', 'turtle', 'pdb', 'profile', 'pstats',
    'timeit', 'trace', 'tracemalloc', 'distutils', 'ensurepip', 'venv', 'zipapp',
    'runpy', 'importlib', 'pkgutil', 'modulefinder', 'sys', 'builtins', 'warnings',
    'contextlib', 'abc', 'atexit', 'traceback', 'future_builtins', 'gc', 'inspect',
    'site', 'sysconfig', 'ast', 'symtable', 'keyword', 'token', 'tokenize',
    'tabnanny', 'pyclbr', 'py_compile', 'compileall', 'dis', 'pickletools',
    'formatter', 'codeop', 'code'
}

for imp in imports:
    package = imp[0] or imp[1]  # Get the package name
    if package and not package.startswith('_'):
        # For dotted imports like 'reportlab.pdfgen', use only the first part
        base_package = package.split('.')[0]
        # Skip tool_* patterns (these are internal tool identifiers, not real packages)
        if base_package.startswith('tool_'):
            continue
        # Only add if it's not a built-in module
        if base_package not in builtin_modules:
            packages.add(base_package)

# Map common packages to their pip names
package_map = {
    'numpy': 'numpy>=1.21.0',
    'pandas': 'pandas>=1.3.0', 
    'matplotlib': 'matplotlib>=3.5.0',
    'reportlab': 'reportlab>=3.6.0',
    'requests': 'requests>=2.25.0',
    'beautifulsoup4': 'beautifulsoup4>=4.9.0',
    'scipy': 'scipy>=1.7.0',
    'sklearn': 'scikit-learn>=1.0.0',
    'seaborn': 'seaborn>=0.11.0',
    'plotly': 'plotly>=5.0.0',
    'openpyxl': 'openpyxl>=3.0.0',
    'xlrd': 'xlrd>=2.0.0',
    'pillow': 'pillow>=8.0.0',
    'cv2': 'opencv-python>=4.5.0',
    'tensorflow': 'tensorflow>=2.6.0',
    'torch': 'torch>=1.9.0',
    'transformers': 'transformers>=4.0.0'
}

# Generate requirements.txt only if there are packages
if packages:
    with open('requirements.txt', 'w') as f:
        for package in sorted(packages):
            if package in package_map:
                f.write(package_map[package] + '\n')
            else:
                # For unknown packages, try the package name as-is
                f.write(package + '\n')
    # Avoid printing to stdout to keep user output clean
else:
    # No packages needed; avoid printing to stdout
    pass
" &&
			# Install only the packages that are actually needed
            if [ -f requirements.txt ] && [ -s requirements.txt ]; then
                echo "Installing required packages..." 1>&2
                # Send pip install output to stderr to keep program stdout clean
                pip install -r requirements.txt 1>&2
			else
				# No packages needed; avoid printing to stdout
				:
			fi &&
            python code.%s &&
            # Ensure output dir is writable inside container
            chmod 777 /app/output 2>/dev/null || true
            # Diagnostics: list PDFs before copying
            echo "Scanning for PDFs..." 1>&2
            pdf_count=$(find . -type f -name '*.pdf' | wc -l || echo 0)
            echo "Found ${pdf_count} PDF(s) in working dir" 1>&2
            if [ "$pdf_count" != "0" ]; then find . -type f -name '*.pdf' -maxdepth 5 -print 1>&2; fi
            # Recursively collect artifacts from working dir, pruning heavy or irrelevant dirs
            find . \
              -path './output' -prune -o \
              -path './.git' -prune -o \
              -path './venv' -prune -o \
              -path './.venv' -prune -o \
              -path './node_modules' -prune -o \
              -type f \
              \( -name '*.pdf' -o -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.csv' -o -name '*.txt' -o -name '*.json' -o -name '*.py' -o -name '*.go' -o -name '*.js' -o -name '*.java' -o -name '*.cpp' -o -name '*.rs' -o -name '*.md' \) \
              -exec cp {} /app/output/ \; 2>/dev/null || \
            # Fallback copy using xargs (in case -exec failures due to spaces)
            find . \
              -path './output' -prune -o \
              -path './.git' -prune -o \
              -path './venv' -prune -o \
              -path './.venv' -prune -o \
              -path './node_modules' -prune -o \
              -type f \
              \( -name '*.pdf' -o -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.csv' -o -name '*.txt' -o -name '*.json' -o -name '*.py' -o -name '*.go' -o -name '*.js' -o -name '*.java' -o -name '*.cpp' -o -name '*.rs' -o -name '*.md' \) \
              -print0 | xargs -0 -I {} cp {} /app/output/ 2>/dev/null || \
            find . \
              -path './output' -prune -o \
              -path './.git' -prune -o \
              -path './venv' -prune -o \
              -path './.venv' -prune -o \
              -path './node_modules' -prune -o \
              -type f \
              \( -name '*.pdf' -o -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.csv' -o -name '*.txt' -o -name '*.json' -o -name '*.py' -o -name '*.go' -o -name '*.js' -o -name '*.java' -o -name '*.cpp' -o -name '*.rs' -o -name '*.md' \) \
              -exec cp {} /app/output/ \; 2>/dev/null || true
            if [ -d /app/data ]; then \
              echo "Scanning /app/data for PDFs..." 1>&2; \
              find /app/data -type f -name '*.pdf' -maxdepth 5 -print 1>&2 || true; \
              find /app/data -type f -exec cp {} /app/output/ \; 2>/dev/null || true; \
            fi
            if [ -d /app/output_files ]; then \
              echo "Scanning /app/output_files for PDFs..." 1>&2; \
              find /app/output_files -type f -name '*.pdf' -maxdepth 5 -print 1>&2 || true; \
              find /app/output_files -type f -exec cp {} /app/output/ \; 2>/dev/null || true; \
            fi
            # Post-copy diagnostics
            echo "--- /app contents ---" 1>&2
            ls -l /app 1>&2 || true
            echo "--- /app/output contents ---" 1>&2
            ls -l /app/output 1>&2 || true
		`, sde.getFileExtensionFromFile(codeFile), sde.getFileExtensionFromFile(codeFile)))
		}
	} else if sde.getFileExtensionFromFile(codeFile) == "rs" {
		// For Rust, we need to set up a Cargo project or use rustc directly
		// Using rustc is simpler for single-file scripts
		if quiet {
			args = append(args, "sh", "-c", fmt.Sprintf(`
			set -e
			cd /app && 
			mkdir -p /app/data /app/tmp && 
			rustc code.%s -o /app/tmp/app 2>&1 && 
			/app/tmp/app
		`, sde.getFileExtensionFromFile(codeFile)))
		} else {
			args = append(args, "sh", "-c", fmt.Sprintf(`
			set -e
			cd /app && 
			mkdir -p /app/data /app/tmp && 
			rustc code.%s -o /app/tmp/app 2>&1 && 
			/app/tmp/app &&
			# Ensure output dir is writable inside container
			chmod 777 /app/output 2>/dev/null || true
			# Copy generated artifacts
			find . \
			  -path './output' -prune -o \
			  -path './.git' -prune -o \
			  -path './tmp' -prune -o \
			  -type f \
			  \( -name '*.pdf' -o -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.csv' -o -name '*.txt' -o -name '*.json' -o -name '*.rs' -o -name '*.md' \) \
			  -exec cp {} /app/output/ \; 2>/dev/null || true
		`, sde.getFileExtensionFromFile(codeFile)))
		}
	} else if sde.getFileExtensionFromFile(codeFile) == "java" {
		// For Java, we need to compile first with javac, then run with java
		// Java requires public classes to be in files matching the class name
		// Try to extract class name from code file
		className := "Main" // Default class name
		if codeContent, err := os.ReadFile(codeFile); err == nil {
			codeStr := string(codeContent)
			// Look for "public class X" pattern
			re := regexp.MustCompile(`public\s+class\s+(\w+)`)
			if matches := re.FindStringSubmatch(codeStr); len(matches) > 1 {
				className = matches[1]
			} else {
				// Try "class X" without public
				re2 := regexp.MustCompile(`class\s+(\w+)`)
				if matches := re2.FindStringSubmatch(codeStr); len(matches) > 1 {
					className = matches[1]
				}
			}
		}
		// Copy code.java to ClassName.java before compilation (Java requirement for public classes)
		if quiet {
			args = append(args, "sh", "-c", fmt.Sprintf(`
			set -e
			cd /app && 
			mkdir -p /app/data /app/tmp && 
			cp code.%s %s.java && 
			javac -d /app/tmp %s.java 2>&1 && 
			java -cp /app/tmp %s
		`, sde.getFileExtensionFromFile(codeFile), className, className, className))
		} else {
			args = append(args, "sh", "-c", fmt.Sprintf(`
			set -e
			cd /app && 
			mkdir -p /app/data /app/tmp && 
			cp code.%s %s.java && 
			javac -d /app/tmp %s.java 2>&1 && 
			java -cp /app/tmp %s &&
			# Ensure output dir is writable inside container
			chmod 777 /app/output 2>/dev/null || true
			# Copy generated artifacts
			find . \
			  -path './output' -prune -o \
			  -path './.git' -prune -o \
			  -path './tmp' -prune -o \
			  -type f \
			  \( -name '*.pdf' -o -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.csv' -o -name '*.txt' -o -name '*.json' -o -name '*.java' -o -name '*.class' -o -name '*.md' \) \
			  -exec cp {} /app/output/ \; 2>/dev/null || true
		`, sde.getFileExtensionFromFile(codeFile), className, className, className))
		}
	} else {
		// For other languages (JavaScript, etc.), run the code and copy generated files
		// Use bash for non-quiet mode to support set -o pipefail if needed
		shellCmd := "sh"
		if !quiet {
			shellCmd = "bash"
		}
		if quiet {
			args = append(args, shellCmd, "-c", fmt.Sprintf(`
            set -eu
            cd /app && 
            mkdir -p /app/data && 
            %s /app/code.%s
        `, cmd, sde.getFileExtensionFromFile(codeFile)))
		} else {
			args = append(args, shellCmd, "-c", fmt.Sprintf(`
			set -eu
			cd /app && 
			mkdir -p /app/data && 
			%s /app/code.%s &&
			# Ensure output dir is writable inside container
			chmod 777 /app/output 2>/dev/null || true
			# Diagnostics: list PDFs before copying
			echo "Scanning for PDFs..." 1>&2
			pdf_count=$(find . -type f -name '*.pdf' | wc -l || echo 0)
			echo "Found ${pdf_count} PDF(s) in working dir" 1>&2
			if [ "$pdf_count" != "0" ]; then find . -type f -name '*.pdf' -maxdepth 5 -print 1>&2; fi
			# Recursively collect artifacts from working dir, pruning heavy or irrelevant dirs
			find . \
			  -path './output' -prune -o \
			  -path './.git' -prune -o \
			  -path './venv' -prune -o \
			  -path './.venv' -prune -o \
			  -path './node_modules' -prune -o \
			  -type f \
			  \( -name '*.pdf' -o -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.csv' -o -name '*.txt' -o -name '*.json' -o -name '*.py' -o -name '*.go' -o -name '*.js' -o -name '*.java' -o -name '*.cpp' -o -name '*.rs' -o -name '*.md' \) \
			  -exec cp {} /app/output/ \; 2>/dev/null || \
			# Fallback copy using xargs (in case -exec failures due to spaces)
			find . \
			  -path './output' -prune -o \
			  -path './.git' -prune -o \
			  -path './venv' -prune -o \
			  -path './.venv' -prune -o \
			  -path './node_modules' -prune -o \
			  -type f \
			  \( -name '*.pdf' -o -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.csv' -o -name '*.txt' -o -name '*.json' -o -name '*.py' -o -name '*.go' -o -name '*.js' -o -name '*.java' -o -name '*.cpp' -o -name '*.rs' -o -name '*.md' \) \
			  -print0 | xargs -0 -I {} cp {} /app/output/ 2>/dev/null || \
			find . \
			  -path './output' -prune -o \
			  -path './.git' -prune -o \
			  -path './venv' -prune -o \
			  -path './.venv' -prune -o \
			  -path './node_modules' -prune -o \
			  -type f \
			  \( -name '*.pdf' -o -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.csv' -o -name '*.txt' -o -name '*.json' -o -name '*.py' -o -name '*.go' -o -name '*.js' -o -name '*.java' -o -name '*.cpp' -o -name '*.rs' -o -name '*.md' \) \
			  -exec cp {} /app/output/ \; 2>/dev/null || true
			if [ -d /app/data ]; then \
			  echo "Scanning /app/data for PDFs..." 1>&2; \
			  find /app/data -type f -name '*.pdf' -maxdepth 5 -print 1>&2 || true; \
			  find /app/data -type f -exec cp {} /app/output/ \; 2>/dev/null || true; \
			fi
			if [ -d /app/output_files ]; then \
			  echo "Scanning /app/output_files for PDFs..." 1>&2; \
			  find /app/output_files -type f -name '*.pdf' -maxdepth 5 -print 1>&2 || true; \
			  find /app/output_files -type f -exec cp {} /app/output/ \; 2>/dev/null || true; \
			fi
			# Post-copy diagnostics
			echo "--- /app contents ---" 1>&2
			ls -l /app 1>&2 || true
			echo "--- /app/output contents ---" 1>&2
			ls -l /app/output 1>&2 || true
		`, cmd, sde.getFileExtensionFromFile(codeFile)))
		}
	}

	// Add timeout if specified
	if timeout > 0 {
		args = append([]string{"timeout", fmt.Sprintf("%ds", timeout)}, args...)
	}

	return args
}

// getFileExtensionFromFile extracts extension from filename
func (sde *SimpleDockerExecutor) getFileExtensionFromFile(filename string) string {
	parts := strings.Split(filename, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return "txt"
}

// extractGeneratedFiles extracts files from the output directory
func (sde *SimpleDockerExecutor) extractGeneratedFiles(outputDir string) map[string][]byte {
	files := make(map[string][]byte)

	// Read all files from the output directory
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		log.Printf("❌ [DOCKER] Failed to read output directory: %v", err)
		return files
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			filePath := fmt.Sprintf("%s/%s", outputDir, entry.Name())
			content, err := os.ReadFile(filePath)
			if err != nil {
				log.Printf("❌ [DOCKER] Failed to read file %s: %v", entry.Name(), err)
				continue
			}
			files[entry.Name()] = content
			log.Printf("📁 [DOCKER] Extracted file: %s (%d bytes)", entry.Name(), len(content))
		}
	}

	return files
}

// storeFilesInRedis stores generated files in Redis
func (sde *SimpleDockerExecutor) storeFilesInRedis(files map[string][]byte, workflowID, stepID string) {
	log.Printf("🔍 [DOCKER] storeFilesInRedis called with %d files, workflowID: %s, stepID: %s", len(files), workflowID, stepID)
	if sde.fileStorage == nil {
		log.Printf("⚠️ [DOCKER] File storage is nil in storeFilesInRedis")
		return
	}

	for filename, content := range files {
		// Determine content type
		contentType := "application/octet-stream"
		if strings.HasSuffix(filename, ".pdf") {
			contentType = "application/pdf"
		} else if strings.HasSuffix(filename, ".png") {
			contentType = "image/png"
		} else if strings.HasSuffix(filename, ".jpg") || strings.HasSuffix(filename, ".jpeg") {
			contentType = "image/jpeg"
		} else if strings.HasSuffix(filename, ".csv") {
			contentType = "text/csv"
		} else if strings.HasSuffix(filename, ".txt") {
			contentType = "text/plain"
		} else if strings.HasSuffix(filename, ".json") {
			contentType = "application/json"
		} else if strings.HasSuffix(filename, ".md") {
			contentType = "text/markdown"
		}

		// Create stored file
		storedFile := &StoredFile{
			Filename:    filename,
			Content:     content,
			ContentType: contentType,
			Size:        int64(len(content)),
			WorkflowID:  workflowID,
			StepID:      stepID,
		}

		// Store in Redis
		err := sde.fileStorage.StoreFile(storedFile)
		if err != nil {
			log.Printf("❌ [DOCKER] Failed to store file %s in Redis: %v", filename, err)
		} else {
			log.Printf("✅ [DOCKER] Stored file %s in Redis (%d bytes)", filename, len(content))
		}
	}
}
