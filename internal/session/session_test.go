package session

import (
	"os"
	"testing"
)

func seed(t *testing.T, s Session) {
	t.Helper()
	if err := Save(s); err != nil {
		t.Fatalf("Save(%s): %v", s.SessionID, err)
	}
}

func TestSaveGetListRemove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvVar, "")

	if len(List()) != 0 {
		t.Fatal("expected empty registry")
	}
	want := Session{SessionID: "sess_1", PhoneID: "phone_9", PhoneType: "android", ControlURL: "wss://x/ws?token=t"}
	seed(t, want)

	got, ok := Get("sess_1")
	if !ok || got.SessionID != want.SessionID || got.ControlURL != want.ControlURL {
		t.Fatalf("Get round-trip mismatch: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be stamped")
	}
	if len(List()) != 1 {
		t.Fatalf("expected 1 lease, got %d", len(List()))
	}

	// Remove by phone id also works.
	if err := Remove("phone_9"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(List()) != 0 {
		t.Fatal("expected empty after Remove")
	}
	if err := Remove("nope"); err != nil {
		t.Fatalf("Remove(absent): %v", err)
	}
}

func TestResolvePrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvVar, "")

	// none active
	if _, err := Resolve(""); err == nil {
		t.Fatal("expected error with no sessions")
	}

	seed(t, Session{SessionID: "a", PhoneID: "pa", ControlURL: "wss://a"})
	// sole active -> that one
	if s, err := Resolve(""); err != nil || s.SessionID != "a" {
		t.Fatalf("sole-active resolve: %+v err=%v", s, err)
	}

	seed(t, Session{SessionID: "b", PhoneID: "pb", ControlURL: "wss://b"})
	// two active, no selector -> falls to current pointer (b, the last saved)
	if s, err := Resolve(""); err != nil || s.SessionID != "b" {
		t.Fatalf("current-pointer resolve: %+v err=%v", s, err)
	}

	// explicit --session wins
	if s, err := Resolve("a"); err != nil || s.SessionID != "a" {
		t.Fatalf("explicit resolve: %+v err=%v", s, err)
	}
	// explicit by phone id
	if s, err := Resolve("pa"); err != nil || s.SessionID != "a" {
		t.Fatalf("explicit-by-phone resolve: %+v err=%v", s, err)
	}
	// unknown explicit -> error
	if _, err := Resolve("zzz"); err == nil {
		t.Fatal("expected error for unknown --session")
	}

	// AXILIO_SESSION env overrides the current pointer
	t.Setenv(EnvVar, "a")
	if s, err := Resolve(""); err != nil || s.SessionID != "a" {
		t.Fatalf("env resolve: %+v err=%v", s, err)
	}
	// bad env -> error
	t.Setenv(EnvVar, "zzz")
	if _, err := Resolve(""); err == nil {
		t.Fatal("expected error for bad AXILIO_SESSION")
	}
}

func TestAmbiguousWithoutPointer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvVar, "")

	seed(t, Session{SessionID: "a", ControlURL: "wss://a"})
	seed(t, Session{SessionID: "b", ControlURL: "wss://b"})
	// Drop the current-session pointer that Save set: now two leases, no way to
	// pick, so a bare resolve must fail rather than guess.
	if err := os.Remove(currentPath()); err != nil {
		t.Fatalf("remove pointer: %v", err)
	}
	if _, err := Resolve(""); err == nil {
		t.Fatal("expected an ambiguity error with two leases and no pointer")
	}
}

func TestMatches(t *testing.T) {
	s := Session{SessionID: "sess_1", PhoneID: "phone_9"}
	for _, id := range []string{"sess_1", "phone_9"} {
		if !s.Matches(id) {
			t.Fatalf("expected %q to match", id)
		}
	}
	for _, id := range []string{"", "sess_2", "phone_1"} {
		if s.Matches(id) {
			t.Fatalf("did not expect %q to match", id)
		}
	}
}
