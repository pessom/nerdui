// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package runtime

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"golang.org/x/crypto/bcrypt"
)

// TestIntegrationRegistries logs in to a local registry with basic auth, pushes with the stored
// credentials and logs out (#51). It uses the docker config of the test user (docker/cli caches
// its location per process, so DOCKER_CONFIG cannot be swapped here); the registry address is
// unique to this test and logged out on cleanup.
func TestIntegrationRegistries(t *testing.T) {
	ncfg, _, err := LoadNerdctlConfig()
	if err != nil {
		t.Fatal(err)
	}
	ncfg.InsecureRegistry = true // the registry below speaks plain HTTP
	c := New(DefaultAddress, DefaultNamespace, WithNerdctlConfig(ncfg))
	t.Cleanup(func() { _ = c.Close() })

	auth := t.TempDir()
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret-pass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(auth, "htpasswd"), append([]byte("bob:"), hash...), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	reg := "it-authreg-" + suffix
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", reg).Run() })
	if out, err := exec.Command("nerdctl", "run", "-d", "--name", reg, "-p", "127.0.0.1:5001:5000",
		"-v", auth+":/auth:ro",
		"-e", "REGISTRY_AUTH=htpasswd", "-e", "REGISTRY_AUTH_HTPASSWD_REALM=it",
		"-e", "REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd",
		"docker.io/library/registry:2").CombinedOutput(); err != nil {
		t.Fatalf("start registry: %v: %s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	const server = "localhost:5001"
	regs := c.Registries()
	t.Cleanup(func() { _ = regs.Logout(context.Background(), server) })

	// Wrong password: rejected, nothing stored. Retry while the registry starts.
	var loginErr error
	for range 20 {
		if _, loginErr = regs.Login(ctx, server, "bob", "wrong"); errdefs.IsInvalidArgument(loginErr) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !errdefs.IsInvalidArgument(loginErr) {
		t.Fatalf("wrong password: %v", loginErr)
	}
	if res, err := regs.Login(ctx, server, "bob", "s3cret-pass"); err != nil || res.Username != "bob" {
		t.Fatalf("login: %+v %v", res, err)
	}
	list, err := regs.List(ctx)
	if err != nil || !slices.ContainsFunc(list, func(r Registry) bool { return r.Username == "bob" }) {
		t.Fatalf("list after login: %+v %v", list, err)
	}

	// Push uses the stored credentials.
	ref := server + "/it/alpine:" + suffix
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rmi", "-f", ref).Run() })
	if out, err := exec.Command("nerdctl", "pull", "-q", "docker.io/library/alpine:3").CombinedOutput(); err != nil {
		t.Fatalf("pull: %v: %s", err, out)
	}
	if err := c.Images().Tag(ctx, DefaultNamespace, "docker.io/library/alpine:3", ref); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := c.Images().Push(ctx, DefaultNamespace, ref, PushOptions{}, &out); err != nil {
		t.Fatalf("push with stored credentials: %v\n%s", err, out.String())
	}

	if err := regs.Logout(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := regs.Logout(ctx, server); !errdefs.IsNotFound(err) {
		t.Errorf("logout twice: %v", err)
	}
	out.Reset()
	if err := c.Images().Push(ctx, DefaultNamespace, ref, PushOptions{}, &out); err == nil {
		t.Error("push without credentials succeeded")
	}
}
