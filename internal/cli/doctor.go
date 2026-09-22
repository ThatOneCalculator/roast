package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bluesky-social/roast/internal/buildinfo"
	"github.com/bluesky-social/roast/internal/config"
	"github.com/bluesky-social/roast/internal/gate"
	"github.com/bluesky-social/roast/internal/scope"
	"github.com/bluesky-social/roast/internal/skill"
	"github.com/bluesky-social/roast/internal/updatecheck"
)

type doctorStatus string

const (
	doctorPass doctorStatus = "pass"
	doctorWarn doctorStatus = "warn"
	doctorFail doctorStatus = "fail"
)

type doctorResult struct {
	Name   string       `json:"name"`
	Status doctorStatus `json:"status"`
	Detail string       `json:"detail"`
}

func newDoctorCommand(stdout io.Writer) *cobra.Command {
	var configPath string
	var asJSON bool
	cmd := &cobra.Command{
		Use:           "doctor",
		Short:         "Check configuration, providers, and local integration",
		Args:          noArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			results := doctorChecks(cmd.Context(), configPath)
			if asJSON {
				encoder := json.NewEncoder(stdout)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(results); err != nil {
					return fmt.Errorf("write doctor results: %w", err)
				}
			} else {
				for _, result := range results {
					fmt.Fprintf(stdout, "%-5s %-18s %s\n", strings.ToUpper(string(result.Status)), result.Name, result.Detail)
				}
			}
			for _, result := range results {
				if result.Status == doctorFail {
					return fmt.Errorf("one or more doctor checks failed")
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: user config directory/roast/config.yaml)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON array")
	return cmd
}

func doctorChecks(ctx context.Context, configPath string) []doctorResult {
	results := []doctorResult{doctorBuildCheck()}
	path := configPath
	if path == "" {
		var err error
		path, err = config.UserPath()
		if err != nil {
			results = append(results, doctorResult{"config", doctorFail, err.Error()})
			return append(results, doctorLocalChecks(ctx)...)
		}
	}
	if _, err := os.Stat(path); err == nil {
		results = append(results, doctorResult{"config-file", doctorPass, path})
	} else if os.IsNotExist(err) && configPath == "" {
		results = append(results, doctorResult{"config-file", doctorWarn, "not found; using defaults and environment: " + path})
	} else if err != nil {
		results = append(results, doctorResult{"config-file", doctorFail, err.Error()})
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		results = append(results, doctorResult{"config", doctorFail, err.Error()})
		return append(results, doctorLocalChecks(ctx)...)
	}
	results = append(results, doctorResult{"config", doctorPass, fmt.Sprintf("schema version %d", cfg.Version)})

	client := &http.Client{Timeout: 5 * time.Second}
	for _, name := range []string{"finder", "verifier"} {
		role, resolveErr := cfg.ResolveRole(name)
		if resolveErr != nil {
			results = append(results, doctorResult{name + "-profile", doctorFail, resolveErr.Error()})
			continue
		}
		if validateErr := config.ValidateRole(name, role); validateErr != nil {
			results = append(results, doctorResult{name + "-profile", doctorFail, validateErr.Error()})
			continue
		}
		results = append(results, doctorResult{
			Name: name + "-profile", Status: doctorPass,
			Detail: fmt.Sprintf("%s: %s via %s at %s", role.Profile, role.Model, role.Protocol, role.BaseURL),
		})
		results = append(results, doctorEndpointCheck(ctx, client, name, role))
	}

	if !cfg.Upload.Enabled {
		results = append(results, doctorResult{"uploads", doctorPass, "disabled"})
	} else if upload, resolveErr := cfg.ResolveUpload(); resolveErr != nil {
		results = append(results, doctorResult{"uploads", doctorFail, resolveErr.Error()})
	} else {
		results = append(results, doctorResult{"uploads", doctorPass, fmt.Sprintf("%s backend at %s", upload.Backend, upload.Endpoint)})
	}
	return append(results, doctorLocalChecks(ctx)...)
}

func doctorBuildCheck() doctorResult {
	if buildinfo.Version == "dev" {
		return doctorResult{"build", doctorWarn, buildinfo.String() + "; release check disabled"}
	}
	return doctorResult{"build", doctorPass, buildinfo.String()}
}

func doctorEndpointCheck(ctx context.Context, client *http.Client, name string, role config.ResolvedRole) doctorResult {
	endpoint := strings.TrimRight(role.BaseURL, "/") + "/models"
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return doctorResult{name + "-endpoint", doctorFail, "invalid models endpoint: " + err.Error()}
	}
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return doctorResult{name + "-endpoint", doctorFail, err.Error()}
	}
	if role.Protocol == "anthropic" {
		req.Header.Set("x-api-key", role.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+role.APIKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return doctorResult{name + "-endpoint", doctorFail, fmt.Sprintf("GET %s: %v", endpoint, err)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return doctorResult{name + "-endpoint", doctorPass, fmt.Sprintf("GET %s: HTTP %s", endpoint, resp.Status)}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return doctorResult{name + "-endpoint", doctorFail, fmt.Sprintf("credential rejected by %s: HTTP %s", endpoint, resp.Status)}
	case resp.StatusCode >= 500:
		return doctorResult{name + "-endpoint", doctorFail, fmt.Sprintf("%s returned HTTP %s", endpoint, resp.Status)}
	default:
		return doctorResult{name + "-endpoint", doctorWarn, fmt.Sprintf("endpoint is reachable but model listing returned HTTP %s", resp.Status)}
	}
}

func doctorLocalChecks(ctx context.Context) []doctorResult {
	results := append(doctorGitChecks(), doctorSkillCheck())
	if updateCheckDisabled() {
		return append(results, doctorResult{"release", doctorWarn, "disabled by ROAST_NO_UPDATE_CHECK"})
	}
	checker, err := updatecheck.Default()
	if err != nil {
		results = append(results, doctorResult{"release", doctorWarn, err.Error()})
		return results
	}
	status := checker.Check(ctx, buildinfo.Version, true)
	switch {
	case status.Skipped:
		results = append(results, doctorResult{"release", doctorWarn, "development build; release check skipped"})
	case status.Error != "":
		results = append(results, doctorResult{"release", doctorWarn, status.Error})
	case status.UpdateAvailable():
		results = append(results, doctorResult{"release", doctorWarn, fmt.Sprintf("%s available; running %s", status.Latest, status.Current)})
	default:
		results = append(results, doctorResult{"release", doctorPass, fmt.Sprintf("%s is current", status.Current)})
	}
	return results
}

func doctorGitChecks() []doctorResult {
	repo, err := scope.GitRoot(".")
	if err != nil {
		return []doctorResult{{"git", doctorWarn, "not inside a Git repository"}}
	}
	results := []doctorResult{{"git", doctorPass, repo}}
	status, err := gate.Inspect(repo)
	if err != nil {
		return append(results, doctorResult{"git-gate", doctorWarn, err.Error()})
	}
	if status.Stale || (status.Decision == gate.DecisionEnabled && !status.Active()) {
		return append(results, doctorResult{"git-gate", doctorWarn, status.Summary()})
	}
	return append(results, doctorResult{"git-gate", doctorPass, status.Summary()})
}

func doctorSkillCheck() doctorResult {
	target, err := skill.Target("")
	if err != nil {
		return doctorResult{"agent-skill", doctorWarn, err.Error()}
	}
	status, err := skill.Inspect(target)
	if err != nil {
		return doctorResult{"agent-skill", doctorWarn, err.Error()}
	}
	switch status.State {
	case skill.NotInstalled:
		return doctorResult{"agent-skill", doctorWarn, "not installed; run `roast skill install`"}
	case skill.Stale:
		return doctorResult{"agent-skill", doctorWarn, "stale; run `roast skill install --force`"}
	case skill.Symlink:
		return doctorResult{"agent-skill", doctorPass, target + " (development symlink)"}
	default:
		return doctorResult{"agent-skill", doctorPass, target}
	}
}
