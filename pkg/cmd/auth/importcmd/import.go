// Package importcmd implements `aeroflare auth import`. It's named importcmd
// because "import" is a Go keyword; the cobra Use string stays "import".
package importcmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/itzemoji/aeroflare/internal/auth"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"

	"github.com/spf13/cobra"
)

// Options holds the dependencies importRun needs, so it can be exercised in
// tests without going through cobra.
type Options struct {
	F *cmdutil.Factory
}

func NewCmdImport(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{F: f}

	return &cobra.Command{
		Use:   "import",
		Short: "Import credentials from other CLIs (gh, glab, docker)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return importRun(opts)
		},
	}
}

func importRun(opts *Options) error {
	f := opts.F
	manager := f.Secrets()
	imported := 0

	// 1. GitHub CLI
	if ghPath, err := exec.LookPath("gh"); err == nil {
		out, err := exec.Command(ghPath, "auth", "token").Output()
		if err == nil {
			token := strings.TrimSpace(string(out))
			if token != "" {
				if err := manager.Set("github-token", token); err == nil {
					msg := "✅ Imported GitHub token from gh CLI" + githubScopeWarning(token)
					_, _ = fmt.Fprintln(f.IOStreams.Out, msg)
					imported++
				}
			}
		}
	}

	// 2. GitLab CLI
	if glabPath, err := exec.LookPath("glab"); err == nil {
		out, err := exec.Command(glabPath, "auth", "token").Output()
		if err == nil {
			token := strings.TrimSpace(string(out))
			if token != "" {
				if err := manager.Set("gitlab-token", token); err == nil {
					_, _ = fmt.Fprintln(f.IOStreams.Out, "✅ Imported GitLab token from glab CLI")
					imported++
				}
			}
		}
	}

	// 3. Docker CLI. `docker login` stores one base64("user:pass") blob per
	// registry host in ~/.docker/config.json; decode each and import it as
	// an OCI username/token pair for that registry.
	homeDir, err := os.UserHomeDir()
	if err == nil {
		dockerConfigPath := filepath.Join(homeDir, ".docker", "config.json")
		if data, err := os.ReadFile(dockerConfigPath); err == nil {
			var config struct {
				Auths map[string]struct {
					Auth string `json:"auth"`
				} `json:"auths"`
			}
			if err := json.Unmarshal(data, &config); err == nil {
				for registry, authData := range config.Auths {
					if authData.Auth == "" {
						continue
					}
					decoded, err := base64.StdEncoding.DecodeString(authData.Auth)
					if err != nil {
						continue
					}
					parts := strings.SplitN(string(decoded), ":", 2)
					if len(parts) == 2 {
						username := parts[0]
						token := parts[1]

						err1 := manager.Set(fmt.Sprintf("oci-%s-username", registry), username)
						err2 := manager.Set(fmt.Sprintf("oci-%s-token", registry), token)

						if err1 == nil && err2 == nil {
							_, _ = fmt.Fprintf(f.IOStreams.Out, "✅ Imported OCI credentials for %s from Docker config\n", registry)
							imported++
						}
					}
				}
			}
		}
	}

	if imported == 0 {
		_, _ = fmt.Fprintln(f.IOStreams.Out, "No credentials found to import.")
	} else {
		_, _ = fmt.Fprintf(f.IOStreams.Out, "Successfully imported %d credential(s).\n", imported)
	}

	return nil
}

// githubScopeWarning returns a human-readable warning if the given GitHub
// token is missing scopes aeroflare needs (e.g. write:packages for GHCR
// pushes), or "" if the token is fine or could not be checked. It delegates to
// the catalog's live validation so scope knowledge lives in one place.
func githubScopeWarning(token string) string {
	svc, ok := auth.ServiceByID("github")
	if !ok || svc.Validate == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := svc.Validate(ctx, map[string]string{"token": token})
	if err != nil || id == nil || len(id.Warnings) == 0 {
		return ""
	}
	return " (⚠️ Warning: " + strings.Join(id.Warnings, "; ") + ")"
}
