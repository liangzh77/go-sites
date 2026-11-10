package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadedDemoUsesOpaqueOriginSandbox(t *testing.T) {
	dataDir := t.TempDir()
	demoDir := filepath.Join(dataDir, "demos", "sandboxed")
	if err := os.MkdirAll(demoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(demoDir, "index.html"),
		[]byte("<!doctype html><title>sandbox</title><script>console.log('ok')</script>"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	a := &app{
		dataDir:      dataDir,
		demosDir:     filepath.Join(dataDir, "demos"),
		manifestPath: filepath.Join(dataDir, "manifest.json"),
	}
	if err := a.saveManifestLocked(manifest{Demos: []demoItem{{
		Title: "Sandboxed", Slug: "sandboxed", Kind: "html",
	}}}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/demo/sandboxed/", nil)
	response := httptest.NewRecorder()
	a.handleServeDemo(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "sandbox") || !strings.Contains(policy, "allow-scripts") {
		t.Fatalf("CSP = %q, want script-capable sandbox", policy)
	}
	if strings.Contains(policy, "allow-same-origin") {
		t.Fatalf("CSP must not grant allow-same-origin: %q", policy)
	}
}
