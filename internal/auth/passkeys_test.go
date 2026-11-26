// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

var b64 = base64.RawURLEncoding

// fakeAuthenticator is a platform authenticator in software: an ES256 key per credential,
// "none" attestation, user verification.
type fakeAuthenticator struct {
	t      *testing.T
	origin string
	creds  map[string]*fakeCredential // by credential ID
}

type fakeCredential struct {
	id     []byte
	key    *ecdsa.PrivateKey
	rpID   string
	handle []byte
	count  uint32
}

func newFakeAuthenticator(t *testing.T, origin string) *fakeAuthenticator {
	return &fakeAuthenticator{t: t, origin: origin, creds: map[string]*fakeCredential{}}
}

func (a *fakeAuthenticator) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": a.origin, "crossOrigin": false})
	return b
}

func authData(rpID string, flags byte, count uint32, attested []byte) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append(h[:], flags)
	out = binary.BigEndian.AppendUint32(out, count)
	return append(out, attested...)
}

// create answers the options of navigator.credentials.create().
func (a *fakeAuthenticator) create(options json.RawMessage) string {
	a.t.Helper()
	var o struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			Exclude []struct {
				ID string `json:"id"`
			} `json:"excludeCredentials"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &o); err != nil {
		a.t.Fatal(err)
	}
	for _, ex := range o.PublicKey.Exclude {
		if _, ok := a.creds[ex.ID]; ok {
			a.t.Fatal("the authenticator holds an excluded credential")
		}
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	handle, _ := b64.DecodeString(o.PublicKey.User.ID)
	c := &fakeCredential{id: randomBytes(16), key: key, rpID: o.PublicKey.RP.ID, handle: handle}
	a.creds[b64.EncodeToString(c.id)] = c

	pub, _ := key.PublicKey.Bytes() // 0x04 || X || Y
	cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: pub[1:33], -3: pub[33:]})
	attested := make([]byte, 16) // AAGUID
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(c.id)))
	attested = append(append(attested, c.id...), cose...)
	// User present, user verified, backup eligible and backed up (a synced passkey), attested data.
	ad := authData(c.rpID, 0x01|0x04|0x08|0x10|0x40, 0, attested)
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": ad})
	resp, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(c.id), "rawId": b64.EncodeToString(c.id), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(a.clientData("webauthn.create", o.PublicKey.Challenge)),
			"attestationObject": b64.EncodeToString(att),
			"transports":        []string{"internal", "hybrid"},
		},
		"clientExtensionResults":  map[string]any{"credProps": map[string]any{"rk": true}},
		"authenticatorAttachment": "platform",
	})
	return string(resp)
}

// get answers the options of navigator.credentials.get() with the credential cred, or any of
// this host when it is nil.
func (a *fakeAuthenticator) get(options json.RawMessage, cred *fakeCredential) string {
	a.t.Helper()
	var o struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &o); err != nil {
		a.t.Fatal(err)
	}
	if cred == nil {
		for _, c := range a.creds {
			if c.rpID == o.PublicKey.RPID {
				cred = c
			}
		}
	}
	if cred == nil {
		a.t.Fatal("no credential for " + o.PublicKey.RPID)
	}
	cred.count++
	ad := authData(cred.rpID, 0x01|0x04|0x08|0x10, cred.count, nil)
	cd := a.clientData("webauthn.get", o.PublicKey.Challenge)
	sum := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, cred.key, digest[:])
	if err != nil {
		a.t.Fatal(err)
	}
	resp, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(cred.id), "rawId": b64.EncodeToString(cred.id), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(cd),
			"authenticatorData": b64.EncodeToString(ad),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(cred.handle),
		},
		"clientExtensionResults": map[string]any{},
	})
	return string(resp)
}

func (a *fakeAuthenticator) only() *fakeCredential {
	for _, c := range a.creds {
		return c
	}
	return nil
}

func TestRelyingParty(t *testing.T) {
	for host, want := range map[string]string{
		"nerdui.example.com":      "https://nerdui.example.com",
		"Nerdui.Example.com:9443": "https://nerdui.example.com:9443",
		"10.0.0.1:9443":           "",
		"[::1]:9443":              "",
	} {
		rp, err := newRelyingParty(host, true)
		if rp.Origin != want || (want == "") != errors.Is(err, ErrPasskeyHost) {
			t.Errorf("%s: %+v %v", host, rp, err)
		}
	}
	if _, err := newRelyingParty("nerdui.example.com", false); !errors.Is(err, ErrPasskeyHost) {
		t.Error("plain HTTP accepted")
	}
	if rp, err := newRelyingParty("localhost:8080", false); err != nil || rp.ID != "localhost" || rp.Origin != "http://localhost:8080" {
		t.Errorf("localhost: %+v %v", rp, err)
	}
}

func TestPasskeys(t *testing.T) {
	e := newEnv(t, false)
	c := newServer(t, e)
	c.base = strings.Replace(c.base, "127.0.0.1", "localhost", 1)
	ctx := context.Background()
	resp, body := c.do(http.MethodPost, "/api/v1/setup",
		`{"token":"`+e.setupToken(t)+`","username":"admin","password":"`+goodPassword+`"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	c.csrf = decode[SessionResponse](t, body).CSRFToken
	auth := newFakeAuthenticator(t, strings.TrimSuffix(c.base, "/"))

	// Not on an IP address.
	ip := newServer(t, e)
	ip.base = strings.Replace(c.base, "localhost", "127.0.0.1", 1)
	if resp, body := ip.do(http.MethodPost, "/api/v1/auth/passkey/options", `{}`, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("IP address: %d %s", resp.StatusCode, body)
	}

	// Registration checks the password; the first passkey brings backup codes.
	if resp, _ := c.do(http.MethodPost, "/api/v1/auth/passkeys/options", `{"password":"wrong-password"}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong password: %d", resp.StatusCode)
	}
	add := func(name string, a *fakeAuthenticator) (*http.Response, string) {
		t.Helper()
		resp, body := c.do(http.MethodPost, "/api/v1/auth/passkeys/options", `{"password":"`+goodPassword+`"}`, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("options: %d %s", resp.StatusCode, body)
		}
		opts := decode[struct {
			Token   string          `json:"token"`
			Options json.RawMessage `json:"options"`
		}](t, body)
		return c.do(http.MethodPost, "/api/v1/auth/passkeys",
			`{"token":"`+opts.Token+`","name":"`+name+`","credential":`+a.create(opts.Options)+`}`, nil)
	}
	resp, body = add("Laptop", auth)
	added := decode[PasskeyAdded](t, body)
	if resp.StatusCode != http.StatusCreated || added.Passkey.Name != "Laptop" || added.Passkey.Host != "localhost" || len(added.BackupCodes) != backupCodeCount {
		t.Fatalf("add: %d %s", resp.StatusCode, body)
	}
	phone := newFakeAuthenticator(t, auth.origin)
	if resp, body = add("Phone", phone); resp.StatusCode != http.StatusCreated || strings.Contains(body, "backupCodes") {
		t.Fatalf("second: %d %s", resp.StatusCode, body)
	}
	_, body = c.do(http.MethodGet, "/api/v1/auth/mfa", "", nil)
	if st := decode[MFAStatus](t, body); !st.Enabled || st.TOTP || st.Passkeys != 2 || st.BackupCodesLeft != backupCodeCount {
		t.Errorf("status: %s", body)
	}

	// The password now asks for the second factor, and a passkey answers it.
	c2 := newServer(t, e)
	c2.base = c.base
	_, body = c2.do(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+goodPassword+`"}`, nil)
	ch := decode[MFAChallengeResponse](t, body)
	if !ch.MFARequired || !ch.Passkey || ch.TOTP {
		t.Fatalf("login: %s", body)
	}
	resp, body = c2.do(http.MethodPost, "/api/v1/auth/login/mfa/passkey", `{"mfaToken":"`+ch.MFAToken+`"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mfa options: %d %s", resp.StatusCode, body)
	}
	opts := decode[PasskeyOptions](t, body)
	raw, _ := json.Marshal(opts.Options)
	answer := auth.get(raw, nil)
	resp, body = c2.do(http.MethodPost, "/api/v1/auth/login/mfa", `{"mfaToken":"`+ch.MFAToken+`","passkey":`+answer+`}`, nil)
	if resp.StatusCode != http.StatusOK || !decode[SessionResponse](t, body).User.MFAEnabled {
		t.Fatalf("mfa: %d %s", resp.StatusCode, body)
	}

	// Without a password: the browser picks the passkey; a replayed answer is refused.
	login := func(cl *client, cred *fakeCredential) (*http.Response, string, string) {
		t.Helper()
		resp, body := cl.do(http.MethodPost, "/api/v1/auth/passkey/options", "{}", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login options: %d %s", resp.StatusCode, body)
		}
		o := decode[struct {
			Token   string          `json:"token"`
			Options json.RawMessage `json:"options"`
		}](t, body)
		answer := auth.get(o.Options, cred)
		resp, body = cl.do(http.MethodPost, "/api/v1/auth/passkey/login", `{"token":"`+o.Token+`","credential":`+answer+`}`, nil)
		return resp, body, answer
	}
	c3 := newServer(t, e)
	c3.base = c.base
	resp, body, answer = login(c3, nil)
	if resp.StatusCode != http.StatusOK || decode[SessionResponse](t, body).User.Username != "admin" {
		t.Fatalf("passkey login: %d %s", resp.StatusCode, body)
	}
	_, body = c3.do(http.MethodPost, "/api/v1/auth/passkey/options", "{}", nil)
	token := decode[PasskeyOptions](t, body).Token
	if resp, body := c3.do(http.MethodPost, "/api/v1/auth/passkey/login", `{"token":"`+token+`","credential":`+answer+`}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("replayed: %d %s", resp.StatusCode, body)
	}
	// A cloned authenticator: its counter goes back.
	cred := auth.only()
	cred.count = 0
	if resp, body, _ := login(c3, cred); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("counter went back: %d %s", resp.StatusCode, body)
	}

	// Removing passkeys needs the password; without a second factor left, the backup codes go.
	keys, _ := e.svc.Passkeys(ctx, 1)
	if len(keys) != 2 || keys[0].LastUsedAt == nil && keys[1].LastUsedAt == nil {
		t.Fatalf("keys %+v", keys)
	}
	if resp, _ := c.do(http.MethodDelete, "/api/v1/auth/passkeys/"+strconv.FormatInt(keys[0].ID, 10), `{"password":"wrong-password"}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("delete with a wrong password: %d", resp.StatusCode)
	}
	for _, k := range keys {
		if resp, body := c.do(http.MethodDelete, "/api/v1/auth/passkeys/"+strconv.FormatInt(k.ID, 10), `{"password":"`+goodPassword+`"}`, nil); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete: %d %s", resp.StatusCode, body)
		}
	}
	if st, _ := e.svc.MFAStatusOf(ctx, 1); st.Enabled || st.BackupCodesLeft != 0 {
		t.Errorf("after delete: %+v", st)
	}
	if resp, _ := c.do(http.MethodDelete, "/api/v1/auth/passkeys/"+strconv.FormatInt(keys[0].ID, 10), `{"password":"`+goodPassword+`"}`, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("delete twice: %d", resp.StatusCode)
	}
	// The authenticator still has the passkey, nerdui does not.
	if resp, body, _ := login(c3, cred); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("removed passkey: %d %s", resp.StatusCode, body)
	}
}

func TestResetMFARemovesPasskeys(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	u, _, err := e.svc.CreateUser(ctx, "bob", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	rp, _ := newRelyingParty("localhost:1", false)
	auth := newFakeAuthenticator(t, rp.Origin)
	token, opts, err := e.svc.BeginPasskeyRegistration(ctx, u.ID, goodPassword, rp)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(opts)
	if _, _, err := e.svc.FinishPasskeyRegistration(ctx, u.ID, token, "", []byte(auth.create(raw))); err != nil {
		t.Fatal(err)
	}
	if keys, _ := e.svc.Passkeys(ctx, u.ID); len(keys) != 1 || keys[0].Name != defaultPassName {
		t.Fatalf("keys %+v", keys)
	}
	if err := e.svc.ResetMFA(ctx, u.ID+100, u.ID); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.svc.MFAStatusOf(ctx, u.ID); st.Enabled || st.Passkeys != 0 {
		t.Errorf("after reset: %+v", st)
	}
}
