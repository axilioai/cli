package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestChallengeS256(t *testing.T) {
	// RFC 7636 Appendix B worked example.
	got := challengeS256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challengeS256 = %q", got)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	Clear()

	if HasSession() {
		t.Fatal("expected no session initially")
	}
	want := Tokens{AccessToken: "a", RefreshToken: "r", Host: "https://h", Expiry: time.Now().Add(time.Hour), OrgID: "org_1", OrgSlug: "acme", OrgName: "Acme"}
	if err := Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok := Load()
	if !ok || got.AccessToken != "a" || got.RefreshToken != "r" || got.Host != "https://h" {
		t.Fatalf("load mismatch: %+v ok=%v", got, ok)
	}
	if got.OrgID != "org_1" || got.OrgSlug != "acme" || got.OrgName != "Acme" {
		t.Fatalf("org fields did not round-trip: %+v", got)
	}
	if !HasSession() {
		t.Fatal("expected a session after save")
	}
	Clear()
	if HasSession() {
		t.Fatal("expected no session after clear")
	}
}

// tokenServer returns an httptest server that emits a fresh token pair for the
// expected grant type, and 400s anything else.
func tokenServer(t *testing.T, wantGrant string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body["grant_type"] != wantGrant {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"unsupported_grant_type"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"ACCESS","refresh_token":"REFRESH2","token_type":"Bearer","expires_in":3600,"organization":{"id":"org_1","slug":"acme","name":"Acme"}}`)
	}))
}

func TestExchange(t *testing.T) {
	srv := tokenServer(t, "authorization_code")
	defer srv.Close()

	tok, err := exchange(context.Background(), srv.URL, map[string]string{
		"grant_type": "authorization_code", "code": "c", "code_verifier": "v",
	})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if tok.AccessToken != "ACCESS" || tok.RefreshToken != "REFRESH2" || tok.Host != srv.URL {
		t.Fatalf("unexpected tokens: %+v", tok)
	}
	if tok.OrgID != "org_1" || tok.OrgSlug != "acme" || tok.OrgName != "Acme" {
		t.Fatalf("organization not mapped from the token response: %+v", tok)
	}
	if !tok.Expiry.After(time.Now().Add(59 * time.Minute)) {
		t.Fatalf("expiry not set from expires_in: %v", tok.Expiry)
	}
}

func TestValidAccessTokenRefreshesWhenExpired(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := tokenServer(t, "refresh_token")
	defer srv.Close()

	// A stored token that is already expired forces a refresh.
	if err := Save(Tokens{AccessToken: "old", RefreshToken: "r1", Host: srv.URL, Expiry: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, err := ValidAccessToken(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("ValidAccessToken: %v", err)
	}
	if tok != "ACCESS" {
		t.Fatalf("expected refreshed access token, got %q", tok)
	}
	got, _ := Load()
	if got.RefreshToken != "REFRESH2" {
		t.Fatalf("refresh token was not rotated in the store: %q", got.RefreshToken)
	}
	if got.OrgSlug != "acme" {
		t.Fatalf("refreshed session lost the org from the token response: %+v", got)
	}
}

// A refresh against a backend that omits the organization (pre-AXI-1348) must
// not erase the org identity recorded at login.
func TestValidAccessTokenRefreshPreservesOrg(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"ACCESS","refresh_token":"REFRESH2","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	seed := Tokens{AccessToken: "old", RefreshToken: "r1", Host: srv.URL, Expiry: time.Now().Add(-time.Minute), OrgID: "org_1", OrgSlug: "acme", OrgName: "Acme"}
	if err := Save(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := ValidAccessToken(context.Background(), srv.URL); err != nil {
		t.Fatalf("ValidAccessToken: %v", err)
	}
	got, _ := Load()
	if got.OrgID != "org_1" || got.OrgSlug != "acme" || got.OrgName != "Acme" {
		t.Fatalf("org identity was erased by the refresh: %+v", got)
	}
}

func TestValidAccessTokenReturnsFresh(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Tokens{AccessToken: "fresh", RefreshToken: "r", Host: "https://h", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, err := ValidAccessToken(context.Background(), "https://h")
	if err != nil || tok != "fresh" {
		t.Fatalf("expected the fresh token, got %q err %v", tok, err)
	}
}

func TestValidAccessTokenNoSession(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	Clear()

	// No session, and a session for a different host, both error.
	if _, err := ValidAccessToken(context.Background(), "https://h"); err == nil {
		t.Fatal("expected an error with no session")
	}
	_ = Save(Tokens{AccessToken: "a", RefreshToken: "r", Host: "https://one", Expiry: time.Now().Add(time.Hour)})
	if _, err := ValidAccessToken(context.Background(), "https://two"); err == nil {
		t.Fatal("expected an error for a session bound to a different host")
	}
}
