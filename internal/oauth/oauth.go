package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/browser"
)

const (
	consentPath   = "/cli/authorize"           // dashboard consent page
	tokenPath     = "/api/v1/auth/oauth/token" // backend token endpoint
	flowTimeout   = 5 * time.Minute            // whole browser round trip
	refreshWindow = 60 * time.Second           // refresh this long before expiry
)

// ErrNoSession means no usable OAuth session is stored for the target host.
var ErrNoSession = errors.New("oauth: no session; run `axilio login`")

// tokenResponse is the token endpoint's success body.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func randB64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func challengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Login runs the PKCE browser flow: open the consent page, wait for the
// loopback callback, and exchange the code for tokens. dashboardBase hosts the
// consent page; apiBase hosts the token endpoint. notify (optional) is called
// with the consent URL so the caller can print it as a fallback.
func Login(ctx context.Context, apiBase, dashboardBase string, notify func(url string)) (Tokens, error) {
	verifier, err := randB64(32)
	if err != nil {
		return Tokens{}, err
	}
	state, err := randB64(16)
	if err != nil {
		return Tokens{}, err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Tokens{}, fmt.Errorf("start loopback server: %w", err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	type result struct{ code, errStr string }
	resCh := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state { // reject a callback we didn't initiate
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(callbackHTML))
		resCh <- result{code: q.Get("code"), errStr: q.Get("error")}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	consent := dashboardBase + consentPath + "?" + url.Values{
		"code_challenge":        {challengeS256(verifier)},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"redirect_uri":          {redirectURI},
	}.Encode()
	if notify != nil {
		notify(consent)
	}
	_ = browser.OpenURL(consent)

	select {
	case res := <-resCh:
		if res.errStr != "" {
			return Tokens{}, fmt.Errorf("authorization denied (%s)", res.errStr)
		}
		if res.code == "" {
			return Tokens{}, errors.New("no authorization code returned")
		}
		return exchange(ctx, apiBase, map[string]string{
			"grant_type":    "authorization_code",
			"code":          res.code,
			"code_verifier": verifier,
		})
	case <-time.After(flowTimeout):
		return Tokens{}, errors.New("timed out waiting for browser authorization")
	case <-ctx.Done():
		return Tokens{}, ctx.Err()
	}
}

// ValidAccessToken returns a currently-valid access token for apiBase,
// refreshing proactively when the stored one is within refreshWindow of expiry.
// A failed refresh clears the session and returns an error.
func ValidAccessToken(ctx context.Context, apiBase string) (string, error) {
	t, ok := Load()
	if !ok || t.Host != apiBase {
		return "", ErrNoSession
	}
	if time.Now().Before(t.Expiry.Add(-refreshWindow)) {
		return t.AccessToken, nil
	}
	nt, err := exchange(ctx, apiBase, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": t.RefreshToken,
	})
	if err != nil {
		Clear()
		return "", fmt.Errorf("session expired; run `axilio login`: %w", err)
	}
	_ = Save(nt)
	return nt.AccessToken, nil
}

// exchange POSTs a grant to the token endpoint and returns stored-shape tokens.
func exchange(ctx context.Context, apiBase string, form map[string]string) (Tokens, error) {
	body, err := json.Marshal(form)
	if err != nil {
		return Tokens{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+tokenPath, bytes.NewReader(body))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Tokens{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var oe oauthError
		_ = json.NewDecoder(resp.Body).Decode(&oe)
		if oe.ErrorDescription != "" {
			return Tokens{}, fmt.Errorf("token exchange failed: %s", oe.ErrorDescription)
		}
		return Tokens{}, fmt.Errorf("token exchange failed (HTTP %d)", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return Tokens{}, err
	}
	if tr.AccessToken == "" || tr.RefreshToken == "" {
		return Tokens{}, errors.New("token endpoint returned an incomplete response")
	}
	return Tokens{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		Host:         apiBase,
	}, nil
}

const callbackHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Axilio CLI</title>
<style>body{background:#0a0a0a;color:#e6e6e8;font-family:ui-monospace,monospace;display:flex;
min-height:100vh;align-items:center;justify-content:center;margin:0}
.c{text-align:center}.c h1{color:#10b981;font-size:16px;letter-spacing:.05em;text-transform:uppercase}
.c p{color:#737373;font-size:13px}</style></head>
<body><div class="c"><h1>Authorized</h1><p>You can close this tab and return to your terminal.</p></div></body></html>`
