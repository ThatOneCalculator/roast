package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfigInitRejectsBlankInteractiveURL(t *testing.T) {
	var stdout bytes.Buffer
	cmd := newConfigInitCommand(&stdout)
	cmd.SetIn(strings.NewReader("   \n"))
	err := cmd.Execute()
	if err == nil || err.Error() != "configuration URL is required" {
		t.Fatalf("expected required URL error, got %v", err)
	}
}
