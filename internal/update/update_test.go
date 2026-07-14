package update

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestNotify(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// A fresh cache lets Notify decide without hitting the network.
	if err := writeCache(cacheState{LastCheck: time.Now(), Latest: "1.2.0"}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	cases := []struct {
		name      string
		current   string
		wantNudge bool
	}{
		{"behind -> nudge", "1.0.0", true},
		{"equal -> silent", "1.2.0", false},
		{"ahead -> silent", "1.3.0", false},
		{"dev build -> silent", "dev", false},
		{"v-prefixed behind -> nudge", "v1.1.0", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			Notify(context.Background(), &buf, tc.current)
			got := strings.Contains(buf.String(), "new release")
			if got != tc.wantNudge {
				t.Fatalf("Notify(%q) nudge=%v, want %v (out=%q)", tc.current, got, tc.wantNudge, buf.String())
			}
		})
	}
}

func TestEnsureV(t *testing.T) {
	for in, want := range map[string]string{"1.2.3": "v1.2.3", "v1.2.3": "v1.2.3", "": "", "dev": "vdev"} {
		if got := ensureV(in); got != want {
			t.Fatalf("ensureV(%q) = %q, want %q", in, got, want)
		}
	}
}
