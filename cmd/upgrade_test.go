package cmd

import "testing"

// In tests Version is "dev" (no release ldflags), so `upgrade` must recognize a
// development build and print a hint without touching the network, exiting 0.
func TestUpgradeDevBuildNoNetwork(t *testing.T) {
	srv := fakeAPI(t)
	if _, err := run(t, srv, "upgrade"); err != nil {
		t.Fatalf("upgrade on a dev build should succeed with a hint: %v", err)
	}
	if _, err := run(t, srv, "upgrade", "--check"); err != nil {
		t.Fatalf("upgrade --check on a dev build should succeed with a hint: %v", err)
	}
}
