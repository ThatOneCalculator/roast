package runupload

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/roast/internal/config"
)

func TestS3CompatibleStoreUsesExplicitCredentialsAndContentAddressedKey(t *testing.T) {
	body := []byte("bundle bytes")
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/runs-bucket/team/roast/sha256/abc123.tar.zst" {
			http.Error(w, "unexpected request "+r.Method+" "+r.URL.Path, http.StatusBadRequest)
			return
		}
		if auth := r.Header.Get("Authorization"); !strings.Contains(auth, "Credential=explicit-access/") || !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
			http.Error(w, "request was not signed with the explicit access key", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("X-Amz-Meta-Roast-Bundle-Sha256"); got != "abc123" {
			http.Error(w, "missing bundle digest metadata", http.StatusBadRequest)
			return
		}
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("ETag", `"etag-value"`)
		w.Header().Set("x-amz-version-id", "version-1")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store, err := NewStore(config.ResolvedUpload{
		Endpoint: server.URL, Bucket: "runs-bucket", Prefix: "team/roast", Region: "us-east-1",
		AccessKey: "explicit-access", SecretKey: "explicit-secret", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Put(t.Context(), "run-1", bytes.NewReader(body), int64(len(body)), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBody, body) {
		t.Fatalf("uploaded body = %q", gotBody)
	}
	if result.Key != "team/roast/sha256/abc123.tar.zst" || result.ETag != "etag-value" || result.VersionID != "version-1" {
		t.Fatalf("upload result = %#v", result)
	}
}

func TestPresignedStoreUsesBearerOnlyForControlPlane(t *testing.T) {
	body := []byte("bundle bytes")
	var dataAuthorization string
	data := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dataAuthorization = r.Header.Get("Authorization")
		if r.Header.Get("X-Bound") != "yes" {
			http.Error(w, "missing bound header", http.StatusBadRequest)
			return
		}
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("x-amz-version-id", "v1")
		w.WriteHeader(http.StatusOK)
	}))
	defer data.Close()
	var complete bool
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer broker-secret" {
			http.Error(w, "bad bearer", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/roast/runs":
			_ = json.NewEncoder(w).Encode(uploadGrant{
				RunID: "run-1", AttemptID: "attempt", ObjectKey: "runs/run-1/bundle.tar.zst",
				Method: http.MethodPut, UploadURL: data.URL, RequiredHeaders: map[string]string{"X-Bound": "yes"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/roast/runs/run-1/complete":
			complete = true
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "uploaded"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer control.Close()
	store, err := NewStore(config.ResolvedUpload{Backend: "presigned", Endpoint: control.URL, Token: "broker-secret"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Put(t.Context(), "run-1", bytes.NewReader(body), int64(len(body)), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if !complete || dataAuthorization != "" || result.Key != "runs/run-1/bundle.tar.zst" || result.VersionID != "v1" {
		t.Fatalf("complete=%v data auth=%q result=%#v", complete, dataAuthorization, result)
	}
}
