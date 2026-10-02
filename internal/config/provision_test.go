package config

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const provisionTemplate = `version: 1
provisioning:
  import_legacy_credential: true
inference_profiles:
  finder:
    endpoint: https://models.example/v1
    model: finder-model
    protocol: chat
  verifier:
    endpoint: https://models.example/v1
    model: verifier-model
    protocol: responses
roles:
  finder:
    profile: finder
  verifier:
    profile: verifier
upload:
  enabled: true
  backend: presigned
  endpoint: https://uploads.example
`

func TestProvisionMigratesLegacyCredentialWithoutRemoteSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(provisionTemplate))
	}))
	defer server.Close()
	legacy := filepath.Join(t.TempDir(), "legacy.yaml")
	if err := os.WriteFile(legacy, []byte("gateway:\n  base_url: https://old.example/v1\n  api_key: legacy-secret\nupload:\n  enabled: true\nroles:\n  finder:\n    model: old-finder\n    api: chat\n  verifier:\n    model: old-verifier\n    api: responses\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "roast", "config.yaml")
	if err := Provision(t.Context(), server.URL, destination, legacy); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(destination)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InferenceProfiles["finder"].APIKey != "legacy-secret" || cfg.Upload.Token != "legacy-secret" {
		t.Fatal("legacy credential was not carried into inference and broker upload")
	}
	if cfg.Upload.Backend != "presigned" || cfg.Upload.Endpoint != "https://uploads.example" {
		t.Fatalf("upload = %#v", cfg.Upload)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, err=%v", info.Mode().Perm(), err)
	}
}

func TestProvisionDoesNotImportLegacyCredentialWithoutExplicitInstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Replace(provisionTemplate, "provisioning:\n  import_legacy_credential: true\n", "", 1)))
	}))
	defer server.Close()
	legacy := filepath.Join(t.TempDir(), "legacy.yaml")
	if err := os.WriteFile(legacy, []byte("gateway:\n  api_key: legacy-secret\nroles:\n  finder:\n    model: old\n    api: chat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "config.yaml")
	if err := Provision(t.Context(), server.URL, destination, legacy); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(destination)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InferenceProfiles["finder"].APIKey != "" || cfg.Upload.Token != "" {
		t.Fatal("legacy credential crossed into a template which did not request migration")
	}
}

func TestProvisionRejectsCrossOriginRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(provisionTemplate)) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	if err := Provision(t.Context(), source.URL, filepath.Join(t.TempDir(), "config.yaml"), ""); err == nil || !strings.Contains(err.Error(), "another origin") {
		t.Fatalf("expected cross-origin redirect rejection, got %v", err)
	}
}

func TestProvisionInPlaceBacksUpLegacyConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("configuration fetch must not send the legacy credential")
		}
		_, _ = w.Write([]byte(provisionTemplate))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.yaml")
	legacy := []byte("gateway:\n  api_key: legacy-secret\nroles:\n  finder:\n    model: old\n    api: chat\ngate:\n  auto_install: false\nsourcebot:\n  url: https://retired-search.example\n  token: discarded-retrieval-key\n")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Provision(t.Context(), server.URL, path, path); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".pre-v1")
	if err != nil || string(backup) != string(legacy) {
		t.Fatalf("backup mismatch, err=%v", err)
	}
	needs, err := NeedsProvision(path)
	if err != nil || needs {
		t.Fatalf("installed config still needs provisioning: needs=%v err=%v", needs, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, retired := range []string{"sourcebot", "auto_install", "discarded-retrieval-key", "retired-search.example"} {
		if strings.Contains(string(raw), retired) {
			t.Errorf("retired setting %q was copied into the provisioned config", retired)
		}
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InferenceProfiles["finder"].APIKey != "legacy-secret" || cfg.Upload.Token != "legacy-secret" {
		t.Fatal("legacy gateway credential was not imported into the opted-in template")
	}
}

func TestProvisionInPlaceReusesMatchingLegacyBackup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(provisionTemplate))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.yaml")
	legacy := []byte("gateway:\n  api_key: legacy-secret\nroles:\n  finder:\n    model: old\n    api: chat\n")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".pre-v1", legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Provision(t.Context(), server.URL, path, path); err != nil {
		t.Fatal(err)
	}
	needs, err := NeedsProvision(path)
	if err != nil || needs {
		t.Fatalf("installed config still needs provisioning: needs=%v err=%v", needs, err)
	}
}

func TestProvisionInPlaceRejectsMismatchedLegacyBackup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(provisionTemplate))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.yaml")
	legacy := []byte("gateway:\n  api_key: legacy-secret\nroles:\n  finder:\n    model: old\n    api: chat\n")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".pre-v1", []byte("unrelated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Provision(t.Context(), server.URL, path, path)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatched backup rejection, got %v", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || string(after) != string(legacy) {
		t.Fatalf("legacy config changed after rejected backup: %q, err=%v", after, readErr)
	}
}

func TestCopyExclusiveRemovesPartialDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "backup")
	err := copyExclusive(destination, []byte("legacy config"), 0o600, func(file *os.File, raw []byte) error {
		if _, err := file.Write(raw[:4]); err != nil {
			return err
		}
		return errors.New("injected persistence failure")
	})
	if err == nil || !strings.Contains(err.Error(), "injected persistence failure") {
		t.Fatalf("expected injected failure, got %v", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial destination was not removed: %v", err)
	}
}

func TestProvisionRejectsUnversionedOrSecretRemoteConfig(t *testing.T) {
	for name, body := range map[string]string{
		"unversioned": strings.Replace(provisionTemplate, "version: 1\n", "", 1),
		"secret":      strings.Replace(provisionTemplate, "    protocol: chat\n", "    protocol: chat\n    api_key: leaked\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			if err := Provision(t.Context(), server.URL, filepath.Join(t.TempDir(), "config.yaml"), ""); err == nil {
				t.Fatal("expected provisioning failure")
			}
		})
	}
}
