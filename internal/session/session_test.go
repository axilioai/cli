package session

import "testing"

func TestSaveLoadClear(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, ok := Load(); ok {
		t.Fatal("expected no session initially")
	}

	want := Session{SessionID: "sess_1", PhoneID: "phone_9", PhoneType: "android", ControlURL: "wss://x/ws?token=t"}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok := Load()
	if !ok {
		t.Fatal("expected a session after Save")
	}
	if got != want {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}

	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, ok := Load(); ok {
		t.Fatal("expected no session after Clear")
	}
	// Clear is idempotent.
	if err := Clear(); err != nil {
		t.Fatalf("second Clear: %v", err)
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
