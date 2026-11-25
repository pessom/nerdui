// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/pessom/nerdui/internal/secretbox"
	"github.com/pessom/nerdui/internal/store/db"
)

// fakeIDP is a minimal OpenID Connect provider: its authorization endpoint signs in the
// current user at once and sends the browser back with a code.
type fakeIDP struct {
	t      *testing.T
	srv    *httptest.Server
	key    *rsa.PrivateKey
	now    func() time.Time
	secret string

	mu    sync.Mutex
	user  map[string]any // claims of the user signing in
	codes map[string]issuedCode
}

type issuedCode struct {
	nonce, challenge, redirect string
	claims                     map[string]any
}

func newFakeIDP(t *testing.T, now func() time.Time) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeIDP{t: t, key: key, now: now, secret: "s3cret", codes: map[string]issuedCode{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/auth", "token_endpoint": p.srv.URL + "/token",
			"jwks_uri": p.srv.URL + "/keys", "userinfo_endpoint": p.srv.URL + "/userinfo",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || !strings.Contains(q.Get("scope"), "openid") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		code := randomToken(8)
		p.codes[code] = issuedCode{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), claims: p.user}
		p.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+url.Values{"code": {code}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, _ := r.BasicAuth()
		p.mu.Lock()
		c, ok := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code"))
		p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || id != "nerdui" || secret != p.secret || base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge ||
			r.Form.Get("redirect_uri") != c.redirect {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		claims := map[string]any{"iss": p.srv.URL, "aud": "nerdui", "nonce": c.nonce,
			"iat": p.now().Unix(), "exp": p.now().Add(5 * time.Minute).Unix()}
		for k, v := range c.claims {
			claims[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": p.sign(claims)})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeIDP) sign(claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: p.key, KeyID: "k1"}}, nil)
	if err != nil {
		p.t.Fatal(err)
	}
	b, _ := json.Marshal(claims)
	jws, err := signer.Sign(b)
	if err != nil {
		p.t.Fatal(err)
	}
	s, _ := jws.CompactSerialize()
	return s
}

func (p *fakeIDP) signIn(claims map[string]any) {
	p.mu.Lock()
	p.user = claims
	p.mu.Unlock()
}

// ssoLogin runs the browser's part: start, the provider, the callback. It returns the target
// of the page the callback answers with.
func ssoLogin(t *testing.T, c *client) string {
	t.Helper()
	resp, body := c.do(http.MethodGet, "/api/v1/auth/oidc/login?redirect=/stacks", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `http-equiv="refresh"`) {
		t.Fatalf("sign-in: %d %s", resp.StatusCode, body)
	}
	_, rest, _ := strings.Cut(body, "url=")
	target, _, _ := strings.Cut(rest, `"`)
	return strings.ReplaceAll(target, "&amp;", "&")
}

func TestOIDC(t *testing.T) {
	e := newEnv(t, false)
	box, err := secretbox.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	e.svc.opts.Box = box
	idp := newFakeIDP(t, e.clock.Now)
	c := newServer(t, e)
	ctx := context.Background()
	// alice has a temporary password: it does not block her single sign-on sessions.
	alice, _, err := e.svc.CreateUser(ctx, "alice", "")
	if err != nil {
		t.Fatal(err)
	}

	// Not enabled: back to the sign-in page with the reason.
	if target := ssoLogin(t, c); !strings.HasPrefix(target, "/login?") || !strings.Contains(target, "not+enabled") {
		t.Errorf("disabled: %s", target)
	}
	if _, err := e.svc.SetOIDCSettings(ctx, OIDCSettings{Enabled: true, Issuer: "ftp://x"}); err == nil {
		t.Error("bad issuer accepted")
	}
	secret := idp.secret
	saved, err := e.svc.SetOIDCSettings(ctx, OIDCSettings{Enabled: true, Issuer: idp.srv.URL + "/", ClientID: "nerdui", ClientSecret: &secret})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.HasClientSecret || saved.ClientSecret != nil || saved.Issuer != idp.srv.URL {
		t.Errorf("saved %+v", saved)
	}
	if raw, _ := e.st.Read().GetSetting(ctx, oidcSettingKey); strings.Contains(raw, secret) {
		t.Error("client secret stored in clear")
	}

	// An existing account is linked by its username; the session skips the local second factor.
	if err := e.svc.SetRequireMFA(ctx, true); err != nil {
		t.Fatal(err)
	}
	idp.signIn(map[string]any{"sub": "a1", "preferred_username": "alice"})
	if target := ssoLogin(t, c); target != "/stacks" {
		t.Fatalf("alice: %s", target)
	}
	resp, body := c.do(http.MethodGet, "/api/v1/auth/me", "", nil)
	var me SessionResponse
	_ = json.Unmarshal([]byte(body), &me)
	if resp.StatusCode != http.StatusOK || me.User.ID != alice.ID || !me.User.SSO || me.MFASetupRequired || me.User.MustChangePassword {
		t.Fatalf("me: %d %s", resp.StatusCode, body)
	}
	c.csrf = me.CSRFToken
	if resp, body := c.do(http.MethodPost, "/api/v1/protected", "", nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("protected: %d %s", resp.StatusCode, body)
	}
	u, _ := e.st.Read().GetUserByID(ctx, alice.ID)
	if u.OidcIdentity.String != idp.srv.URL+" a1" {
		t.Errorf("identity %q", u.OidcIdentity.String)
	}

	fails := func(claims map[string]any, want string) {
		t.Helper()
		jar := newServer(t, e) // a fresh browser
		jar.base = c.base
		idp.signIn(claims)
		if target := ssoLogin(t, jar); !strings.Contains(target, "sso_error") || !strings.Contains(target, want) {
			t.Errorf("%v: %s", claims, target)
		}
	}
	// No account, and no group to create one; another account at the provider for alice.
	fails(map[string]any{"sub": "b1", "preferred_username": "bob"}, "no+nerdui+account")
	fails(map[string]any{"sub": "a2", "preferred_username": "alice"}, "linked+to+another")

	// With a group: its members get an account, others cannot sign in, linked or not.
	if _, err := e.svc.SetOIDCSettings(ctx, OIDCSettings{Enabled: true, Issuer: idp.srv.URL, ClientID: "nerdui", Group: "nerdui-admins"}); err != nil {
		t.Fatal(err)
	}
	fails(map[string]any{"sub": "a1", "preferred_username": "alice", "groups": []string{"other"}}, "not+allowed")
	b := newServer(t, e)
	b.base = c.base
	idp.signIn(map[string]any{"sub": "b1", "preferred_username": "bob", "groups": []string{"nerdui-admins"}})
	if target := ssoLogin(t, b); target != "/stacks" {
		t.Fatalf("bob: %s", target)
	}
	bob, err := e.st.Read().GetUserByUsername(ctx, "bob")
	if err != nil || !bob.OidcIdentity.Valid || bob.Role != RoleAdmin {
		t.Fatalf("bob %+v %v", bob, err)
	}
	// A disabled account cannot sign in.
	if _, err := e.st.Write().SetUserDisabled(ctx, db.SetUserDisabledParams{ID: bob.ID, DisabledAt: nullInt(1), Now: 1}); err != nil {
		t.Fatal(err)
	}
	fails(map[string]any{"sub": "b1", "preferred_username": "bob", "groups": []string{"nerdui-admins"}}, "disabled")

	// A callback that did not start here is refused.
	resp, body = c.do(http.MethodGet, "/api/v1/auth/oidc/callback?code=x&state=forged", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "sso_error") {
		t.Errorf("forged state: %s", body)
	}

	// Unlinking.
	if d, err := e.svc.UnlinkOIDC(ctx, alice.ID); err != nil || d.SSO {
		t.Errorf("unlink %+v %v", d, err)
	}
}

func TestSafeRedirect(t *testing.T) {
	for in, want := range map[string]string{"/stacks": "/stacks", "//evil.com": "/", "https://evil.com": "/", "/\\evil": "/", "": "/"} {
		if got := safeRedirect(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
