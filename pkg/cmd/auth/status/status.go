// Package status implements `aeroflare auth status`.
package status

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/itzemoji/aeroflare/internal/auth"
	"github.com/itzemoji/aeroflare/internal/secrets"
	"github.com/itzemoji/aeroflare/internal/ui"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"

	"github.com/spf13/cobra"
)

// Options holds the flags and dependencies statusRun needs, so it can be
// exercised in tests without going through cobra.
type Options struct {
	F        *cmdutil.Factory
	JSON     bool
	NoVerify bool
}

func NewCmdStatus(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{F: f}

	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"list"},
		Short:   "Show stored credentials and their validity",
		RunE: func(cmd *cobra.Command, args []string) error {
			return statusRun(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&opts.NoVerify, "no-verify", false, "Skip live validation of credentials")

	return cmd
}

// statusFieldJSON / statusEntryJSON are the JSON shapes emitted by
// `auth status --json`.
type statusFieldJSON struct {
	Name   string `json:"name"`
	Set    bool   `json:"set"`
	Secret bool   `json:"secret"`
	Value  string `json:"value,omitempty"`
}

type statusEntryJSON struct {
	Service    string            `json:"service"`
	ID         string            `json:"id"`
	Fields     []statusFieldJSON `json:"fields"`
	Verifiable bool              `json:"verifiable"`
	User       string            `json:"user,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
	Error      string            `json:"error,omitempty"`
}

func statusRun(opts *Options) error {
	f := opts.F
	out := f.IOStreams.Out
	manager := f.Secrets()

	keys, err := manager.List()
	if err != nil {
		return err
	}

	services, custom := storedServices(manager, keys)

	var entries []statusEntryJSON
	for _, svc := range services {
		entry := statusEntryJSON{Service: svc.DisplayName, ID: svc.ID, Verifiable: svc.Validate != nil}
		for _, fld := range svc.Fields {
			val, gerr := manager.Get(fld.SecretKey)
			set := gerr == nil && val != ""
			fj := statusFieldJSON{Name: fld.Name, Set: set, Secret: fld.Secret}
			if set {
				if fld.Secret {
					fj.Value = shared.Redact(val)
				} else {
					fj.Value = val
				}
			}
			entry.Fields = append(entry.Fields, fj)
		}

		if !opts.NoVerify {
			if id, verr := shared.ValidateService(svc, manager); verr != nil {
				entry.Error = verr.Error()
			} else if id != nil {
				entry.User = id.User
				entry.Warnings = id.Warnings
			}
		}
		entries = append(entries, entry)
	}

	if opts.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	if len(entries) == 0 && len(custom) == 0 {
		_, err := fmt.Fprintln(out, "No credentials saved.")
		return err
	}

	// Build one row per field. The Service, ID, and Status columns are
	// filled on the first row of each service and left blank on its
	// remaining field rows, so each credential reads as a visual block.
	var rows [][]string
	var footnotes []string
	for _, e := range entries {
		for i, f := range e.Fields {
			service, id, status := "", "", ""
			if i == 0 {
				service = e.Service
				id = e.ID
				status = statusCell(e, opts.NoVerify)
			}
			value := "not set"
			if f.Set {
				value = f.Value
			}
			rows = append(rows, []string{service, id, f.Name, value, status})
		}
		for _, w := range e.Warnings {
			footnotes = append(footnotes, fmt.Sprintf("%s: %s", e.Service, w))
		}
	}

	for _, k := range custom {
		rows = append(rows, []string{"Custom", "-", k, "(hidden)", "-"})
	}

	if err := ui.PrintTableTo(out, []string{"Service", "ID", "Field", "Value", "Status"}, rows); err != nil {
		return err
	}

	if len(footnotes) > 0 {
		if _, err := fmt.Fprintln(out, "\nWarnings:"); err != nil {
			return err
		}
		for _, note := range footnotes {
			if _, err := fmt.Fprintf(out, "  • %s\n", note); err != nil {
				return err
			}
		}
	}
	return nil
}

// storedServices returns the services that have at least one field stored, in
// stable order (fixed services first, then OCI registries sorted by host),
// along with any leftover keys that belong to no known service.
func storedServices(m secrets.Manager, keys []string) (services []auth.Service, custom []string) {
	stored := make(map[string]bool, len(keys))
	for _, k := range keys {
		stored[k] = true
	}

	seen := make(map[string]bool)
	add := func(svc auth.Service) {
		if seen[svc.ID] {
			return
		}
		for _, f := range svc.Fields {
			if stored[f.SecretKey] {
				seen[svc.ID] = true
				services = append(services, svc)
				return
			}
		}
	}

	for _, svc := range auth.Services() {
		add(svc)
	}

	// Discover OCI registry hosts from the stored keys, then add each once.
	var ociHosts []string
	ociSeen := make(map[string]bool)
	for _, k := range keys {
		svc, ok := auth.ServiceForSecretKey(k)
		if !ok {
			custom = append(custom, k)
			continue
		}
		if _, isFixed := auth.ServiceByID(svc.ID); isFixed {
			continue
		}
		if !ociSeen[svc.ID] {
			ociSeen[svc.ID] = true
			ociHosts = append(ociHosts, k)
		}
	}
	sort.Strings(custom)

	var ociServices []auth.Service
	for _, k := range ociHosts {
		if svc, ok := auth.ServiceForSecretKey(k); ok {
			ociServices = append(ociServices, svc)
		}
	}
	sort.Slice(ociServices, func(i, j int) bool { return ociServices[i].ID < ociServices[j].ID })
	for _, svc := range ociServices {
		add(svc)
	}

	return services, custom
}

// statusCell renders the Status column for a service entry: whether it was
// verified, and if so, who it authenticated as or why it failed.
func statusCell(e statusEntryJSON, noVerify bool) string {
	if noVerify {
		return "-"
	}
	if !e.Verifiable {
		return "n/a"
	}
	if e.Error != "" {
		return "invalid"
	}
	status := "valid"
	if e.User != "" {
		status = "valid (" + e.User + ")"
	}
	if len(e.Warnings) > 0 {
		status += " ⚠"
	}
	return status
}
