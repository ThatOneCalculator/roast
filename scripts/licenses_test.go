package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLicenseGenerationGuards(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is unavailable")
	}
	script, err := os.ReadFile("licenses.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"invalid target", "unowned directory", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			scriptDir := filepath.Join(root, "scripts")
			if err := os.Mkdir(scriptDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(scriptDir, "licenses.sh"), script, 0o600); err != nil {
				t.Fatal(err)
			}
			protected := filepath.Join(root, "THIRD_PARTY_LICENSES")
			target := "linux/amd64"
			if scenario == "invalid target" {
				target = "../invalid"
			} else if scenario == "symlink" {
				protected = t.TempDir()
				if err := os.Symlink(protected, filepath.Join(root, "THIRD_PARTY_LICENSES")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			} else if err := os.Mkdir(protected, 0o700); err != nil {
				t.Fatal(err)
			}
			if scenario != "invalid target" {
				if err := os.WriteFile(filepath.Join(protected, "keep.txt"), []byte("user data"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(scriptDir, "licenses.sh"), target)
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatal("unsafe license generation was accepted")
			}
			want := "refusing to replace"
			if scenario == "invalid target" {
				want = "unsupported license target"
			}
			if !strings.Contains(string(output), want) {
				t.Fatalf("wrong rejection: %s", output)
			}
			if scenario != "invalid target" {
				body, err := os.ReadFile(filepath.Join(protected, "keep.txt"))
				if err != nil || string(body) != "user data" {
					t.Fatal("license generation modified an unowned directory")
				}
			}
		})
	}
}
