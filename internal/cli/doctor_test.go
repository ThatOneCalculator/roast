package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluesky-social/roast/internal/config"
)

func TestDoctorEndpointCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("request path=%q auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	result := doctorEndpointCheck(t.Context(), server.Client(), "finder", config.ResolvedRole{
		BaseURL: server.URL + "/v1", APIKey: "secret", Protocol: "responses",
	})
	if result.Status != doctorPass || strings.Contains(result.Detail, "secret") {
		t.Fatalf("doctorEndpointCheck() = %#v", result)
	}
}

func TestDoctorJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	t.Setenv("DOCTOR_KEY", "secret")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	body := "version: 1\ninference_profiles:\n  finder:\n    endpoint: " + server.URL + "/v1\n    api_key_env: DOCTOR_KEY\n    model: finder-model\n    protocol: responses\n  verifier:\n    endpoint: " + server.URL + "/v1\n    api_key_env: DOCTOR_KEY\n    model: verifier-model\n    protocol: responses\nroles:\n  finder:\n    profile: finder\n  verifier:\n    profile: verifier\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code, err := ExecuteContext(t.Context(), []string{"doctor", "--json", "--config", configPath}, &stdout, &stderr)
	if err != nil || code != ExitClean {
		t.Fatalf("doctor code=%d err=%v stdout=%s stderr=%s", code, err, stdout.String(), stderr.String())
	}
	var results []doctorResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("decode results: %v: %s", err, stdout.String())
	}
	if len(results) < 8 {
		t.Fatalf("doctor returned only %d results: %#v", len(results), results)
	}
	if strings.Contains(stdout.String(), "secret") {
		t.Fatal("doctor output exposed a credential")
	}
}
