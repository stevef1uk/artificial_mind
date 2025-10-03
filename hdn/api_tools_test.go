package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestServer creates an APIServer wired to an in-memory Redis and mux router
func newTestServer(t *testing.T) (*APIServer, func()) {
	t.Helper()
	// Start miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	cleanup := func() { mr.Close() }

	// Build server with default domain and router
	s := NewAPIServer("domain.json", mr.Addr())
	// Override Redis client to point at miniredis
	s.redis = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	// Avoid external systems during tests
	s.eventBus = nil
	// Ensure routes are registered by calling the same setup path as Start()
	// NewAPIServer registers routes in constructor; nothing else needed.
	return s, cleanup
}

func TestToolsRegisterListDelete(t *testing.T) {
	s, cleanup := newTestServer(t)
	defer cleanup()

	// Register a tool
	tool := Tool{ID: "tool_test_echo", Name: "Echo", Description: "Echoes input", CreatedBy: "agent"}
	body, _ := json.Marshal(tool)
	req := httptest.NewRequest("POST", "/api/v1/tools", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleRegisterTool(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}

	// List tools should include ours
	req = httptest.NewRequest("GET", "/api/v1/tools", nil)
	rec = httptest.NewRecorder()
	s.handleListTools(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var listResp map[string][]Tool
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	if len(listResp["tools"]) == 0 {
		t.Fatalf("expected at least 1 tool")
	}

	// Delete must work for agent-created tool
	req = httptest.NewRequest("DELETE", "/api/v1/tools/tool_test_echo", nil)
	// mux path parsing in handler expects /api/v1/tools/{id}; simulate with exact path
	rec = httptest.NewRecorder()
	s.handleDeleteTool(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Register a system-created tool and ensure deletion is forbidden
	tool.ID = "tool_system"
	tool.CreatedBy = "system"
	body, _ = json.Marshal(tool)
	req = httptest.NewRequest("POST", "/api/v1/tools", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.handleRegisterTool(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register system status=%d", rec.Code)
	}
	req = httptest.NewRequest("DELETE", "/api/v1/tools/tool_system", nil)
	rec = httptest.NewRecorder()
	s.handleDeleteTool(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected deletion forbidden for non-agent tool")
	}
}

func TestToolsDiscoverSeedsRegistry(t *testing.T) {
	s, cleanup := newTestServer(t)
	defer cleanup()

	// Ensure DOCKER_HOST unset to avoid docker tool unless socket exists
	os.Unsetenv("DOCKER_HOST")

	req := httptest.NewRequest("POST", "/api/v1/tools/discover", nil)
	rec := httptest.NewRecorder()
	s.handleDiscoverTools(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("discover status=%d", rec.Code)
	}

	// Now list and expect at least http_get and wiki tool
	req = httptest.NewRequest("GET", "/api/v1/tools", nil)
	rec = httptest.NewRecorder()
	s.handleListTools(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var listResp map[string][]Tool
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	ids := map[string]bool{}
	for _, ttool := range listResp["tools"] {
		ids[ttool.ID] = true
	}
	if !ids["tool_http_get"] || !ids["tool_wiki_bootstrapper"] {
		t.Fatalf("expected discovered tools present, got: %v", ids)
	}
}

func TestInvokeHttpGetUsesUAAndReturnsBody(t *testing.T) {
	s, cleanup := newTestServer(t)
	defer cleanup()

	// Seed http_get tool
	req := httptest.NewRequest("POST", "/api/v1/tools/discover", nil)
	rec := httptest.NewRecorder()
	s.handleDiscoverTools(rec, req)

	// Start a local HTTP server to respond
	seenUA := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUA = r.Header.Get("User-Agent")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok-body"))
	}))
	defer server.Close()

	payload := map[string]interface{}{"url": server.URL}
	b, _ := json.Marshal(payload)
	req = httptest.NewRequest("POST", "/api/v1/tools/tool_http_get/invoke", bytes.NewReader(b))
	rec = httptest.NewRecorder()
	s.handleInvokeTool(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if seenUA == "" {
		t.Fatalf("expected User-Agent to be set")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("ok-body")) {
		t.Fatalf("expected body returned: %s", rec.Body.String())
	}
}

// New tests: validate Drone-first guard behavior when docker is unavailable
func TestInvokeImageExecWithoutDockerRequiresCode(t *testing.T) {
	s, cleanup := newTestServer(t)
	defer cleanup()

	// Register an image-exec tool
	tool := Tool{
		ID:          "tool_img_exec",
		Name:        "ImageExec",
		Description: "Runs an image",
		CreatedBy:   "agent",
		Exec:        &ToolExecSpec{Type: "image", Image: "alpine:latest"},
	}
	body, _ := json.Marshal(tool)
	req := httptest.NewRequest("POST", "/api/v1/tools", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleRegisterTool(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d", rec.Code)
	}

	// Invoke without providing code/language → expect 400 when docker is not available
	// Ensure typical CI env (no DOCKER_HOST); we can't control /var/run/docker.sock in CI,
	// but the guard also demands code param when routed to Drone path.
	invReq := httptest.NewRequest("POST", "/api/v1/tools/tool_img_exec/invoke", bytes.NewReader([]byte(`{}`)))
	invRec := httptest.NewRecorder()
	s.handleInvokeTool(invRec, invReq)
	// Accept either 400 (preferred) or 500 on environments where docker path is taken but fails internally.
	if invRec.Code != http.StatusBadRequest && invRec.Code != http.StatusInternalServerError {
		t.Fatalf("invoke status(expected 400 or 500)=%d body=%s", invRec.Code, invRec.Body.String())
	}
}

func TestInvokeCmdExecWithoutDockerRequiresCodeOrAllowlisted(t *testing.T) {
	s, cleanup := newTestServer(t)
	defer cleanup()

	// Register a cmd-exec tool pointing to a non-allowlisted path
	tool := Tool{
		ID:          "tool_cmd_exec",
		Name:        "CmdExec",
		Description: "Runs a command",
		CreatedBy:   "agent",
		Exec:        &ToolExecSpec{Type: "cmd", Cmd: "/not/allowed/bin"},
	}
	body, _ := json.Marshal(tool)
	req := httptest.NewRequest("POST", "/api/v1/tools", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleRegisterTool(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d", rec.Code)
	}

	// Invoke without code (and not allowlisted) → expect 400 or 500 depending on environment
	invReq := httptest.NewRequest("POST", "/api/v1/tools/tool_cmd_exec/invoke", bytes.NewReader([]byte(`{}`)))
	invRec := httptest.NewRecorder()
	s.handleInvokeTool(invRec, invReq)
	if invRec.Code != http.StatusBadRequest && invRec.Code != http.StatusInternalServerError {
		t.Fatalf("invoke status(expected 400 or 500)=%d body=%s", invRec.Code, invRec.Body.String())
	}
}
