package settings

import (
	"strings"
	"testing"

	"github.com/itzemoji/aeroflare/pkg/iostreams"
)

// The wording differs for a first-time save and an update, and it is driven by
// the Factory's IsNewConfig. Getting the branch backwards would tell a
// returning user their config was created from scratch.
func TestReportSaved(t *testing.T) {
	tests := []struct {
		name        string
		isNewConfig bool
		want        string
	}{
		{
			name:        "a fresh config reports an initial save",
			isNewConfig: true,
			want:        "Initial config has been saved to /home/u/.config/aeroflare/aeroflare.yaml",
		},
		{
			name:        "an existing config reports an update",
			isNewConfig: false,
			want:        "Config has been updated in /home/u/.config/aeroflare/aeroflare.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, _, out, _ := iostreams.Test()
			opts := &Options{IO: io}

			opts.reportSaved(tt.isNewConfig, "/home/u/.config/aeroflare/aeroflare.yaml")

			if got := out.String(); !strings.Contains(got, tt.want) {
				t.Errorf("stdout = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}
