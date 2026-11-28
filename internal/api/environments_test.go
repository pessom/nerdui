// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/hostcheck"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
)

type fakeRuntime struct {
	v          runtime.Versions
	ns         []string
	err        error
	nerdctl    runtime.NerdctlStatus
	events     runtime.EventSource
	containers []runtime.Container
	gotNS      string
	nsCalls    []string          // "create x" / "remove x"
	remote     map[string]string // registry digests by reference
	nsErr      error
	restored   []string // "ns/volume:archive" of Volumes().Restore

	mu           sync.Mutex
	actions      []actionCall
	actionErr    map[string]error // by container id
	inspect      map[string]any   // by container ref
	logs         []runtime.LogLine
	logErr       error // returned before any line
	logFollow    bool  // with Follow, block until ctx ends after the lines
	gotLogs      runtime.LogOptions
	gotSize      bool
	info         runtime.Info
	images       []runtime.Image
	pulls        []pullCall
	pullRelease  chan struct{}
	pullErr      error
	tags         [][3]string
	removed      []string
	pushes       []pushCall
	byNS         map[string][]runtime.Container // containers per namespace, when set
	builds       []buildCall
	builder      runtime.BuilderStatus
	imageErr     map[string]error // by reference (and registry server for logins)
	registries   []runtime.Registry
	prunes       []runtime.PruneOptions
	statsSamples []runtime.StatsSample
	statsErr     error
	volumes      []runtime.Volume
	ports        runtime.PortsReport
	summary      runtime.Summary
	statsHold    bool
	gotVolSizes  bool
	services     map[string][]string // compose services by project
	composeRuns  []string            // "op project dir"
	composeOpts  []runtime.ComposeOptions
	composeHold  chan struct{} // when set, Run blocks until it is closed
	networks     []runtime.Network
	netSpecs     []runtime.NetworkSpec
	volumeSize   bool
	gotInterval  time.Duration
	gotExec      runtime.ExecOptions
	gotSizes     []runtime.TermSize
	execErr      error
	created      []runtime.CreateSpec
	createStart  bool
	createID     string
	createErr    error
	spec         runtime.ContainerSpec
	recreated    []runtime.CreateSpec
	procs        []runtime.Process
	history      []runtime.HistoryEntry
	loaded       []byte // the body of the last image load
	saveErr      error
	recreatePull bool
}

type actionCall struct {
	ns, id string
	action runtime.Action
	opts   runtime.ActionOptions
}

func (f *fakeRuntime) System() runtime.System { return f }
func (f *fakeRuntime) Close() error           { return nil }
func (f *fakeRuntime) Events() runtime.EventSource {
	if f.events == nil {
		return &fakeSource{}
	}
	return f.events
}
func (f *fakeRuntime) Versions(context.Context) (runtime.Versions, error) {
	return f.v, f.err
}
func (f *fakeRuntime) Namespaces(context.Context) ([]string, error) { return f.ns, f.err }
func (f *fakeRuntime) CreateNamespace(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nsCalls = append(f.nsCalls, "create "+name)
	return f.nsErr
}
func (f *fakeRuntime) RemoveNamespace(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nsCalls = append(f.nsCalls, "remove "+name)
	return f.nsErr
}
func (f *fakeRuntime) Nerdctl(context.Context) runtime.NerdctlStatus { return f.nerdctl }
func (f *fakeRuntime) Containers() runtime.Containers                { return f }
func (f *fakeRuntime) Info(context.Context) (runtime.Info, error)    { return f.info, f.err }

// Exec echoes input until a line "exit N" and records sizes; execErr fails it up front.
func (f *fakeRuntime) Exec(ctx context.Context, _, _ string, opts runtime.ExecOptions, stdin io.Reader, out io.Writer, resize <-chan runtime.TermSize) (int, error) {
	f.mu.Lock()
	f.gotExec = opts
	f.mu.Unlock()
	if f.execErr != nil {
		return -1, f.execErr
	}
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	for {
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case s := <-resize:
			f.mu.Lock()
			f.gotSizes = append(f.gotSizes, s)
			f.mu.Unlock()
		case l, ok := <-lines:
			// Sizes sent before this input come first, as the server queued them: select
			// picks randomly among ready cases, which made the test flaky.
			for drained := false; !drained; {
				select {
				case s := <-resize:
					f.mu.Lock()
					f.gotSizes = append(f.gotSizes, s)
					f.mu.Unlock()
				default:
					drained = true
				}
			}
			if !ok {
				return -1, io.EOF
			}
			var code int
			if _, err := fmt.Sscanf(l, "exit %d", &code); err == nil {
				return code, nil
			}
			_, _ = io.WriteString(out, "echo: "+l+"\r\n")
		}
	}
}

// Create records the spec and returns createID (or createErr).
func (f *fakeRuntime) Create(_ context.Context, _ string, spec runtime.CreateSpec, start bool, out io.Writer) (string, error) {
	f.mu.Lock()
	f.created = append(f.created, spec)
	f.createStart = start
	f.mu.Unlock()
	_, _ = io.WriteString(out, "Created\n")
	return f.createID, f.createErr
}

func (f *fakeRuntime) Top(_ context.Context, ns, _ string) ([]runtime.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotNS = ns
	return f.procs, f.err
}

func (f *fakeRuntime) Spec(context.Context, string, string) (runtime.ContainerSpec, error) {
	return f.spec, f.err
}

func (f *fakeRuntime) Recreate(_ context.Context, _, _ string, spec runtime.CreateSpec, opts runtime.RecreateOptions, out io.Writer) (string, error) {
	f.mu.Lock()
	f.recreated = append(f.recreated, spec)
	f.recreatePull = opts.Pull
	f.mu.Unlock()
	_, _ = io.WriteString(out, "Recreated\n")
	return f.createID, f.createErr
}

func (f *fakeRuntime) Images() runtime.Images                             { return fakeImages{f} }
func (f *fakeRuntime) Ports(context.Context) (runtime.PortsReport, error) { return f.ports, f.err }

func (f *fakeRuntime) Summary(_ context.Context, sizes bool) (runtime.Summary, error) {
	f.mu.Lock()
	f.gotVolSizes = sizes
	f.mu.Unlock()
	return f.summary, f.err
}

func (f *fakeRuntime) PrunePreview(_ context.Context, ns string, all bool) (runtime.PrunePreview, error) {
	f.gotNS = ns
	p := runtime.PrunePreview{Containers: []runtime.PrunedItem{{ID: "c1", Name: "old"}}, Images: []runtime.PrunedItem{}}
	if all {
		p.Images = append(p.Images, runtime.PrunedItem{Name: "nginx:1.27", Size: 100})
		p.ImagesSize = 100
	}
	return p, f.err
}
func (f *fakeRuntime) Prune(_ context.Context, _ string, opts runtime.PruneOptions, out io.Writer) error {
	f.mu.Lock()
	f.prunes = append(f.prunes, opts)
	f.mu.Unlock()
	_, _ = io.WriteString(out, "Deleted Containers:\nc1\n")
	return f.err
}

// Stats emits statsSamples, then ends (container stopped) or returns statsErr.
func (f *fakeRuntime) Stats(ctx context.Context, _, _ string, interval time.Duration, emit runtime.StatsEmitter) error {
	f.mu.Lock()
	f.gotInterval = interval
	f.mu.Unlock()
	if f.statsErr != nil {
		return f.statsErr
	}
	for _, s := range f.statsSamples {
		if err := emit(s); err != nil {
			return err
		}
	}
	if f.statsHold {
		<-ctx.Done() // a running container keeps its stream open
	}
	return nil
}
func (f *fakeRuntime) Registries() runtime.Registries { return fakeRegistries{f} }

func (f *fakeRuntime) Volumes() runtime.Volumes { return fakeVolumes{f} }

func (f *fakeRuntime) Networks() runtime.Networks { return fakeNetworks{f} }

func (f *fakeRuntime) Compose() runtime.Compose { return fakeCompose{f} }

type fakeCompose struct{ f *fakeRuntime }

func (fc fakeCompose) Run(ctx context.Context, _, project, dir, op string, opts runtime.ComposeOptions, out io.Writer) error {
	fc.f.mu.Lock()
	fc.f.composeRuns = append(fc.f.composeRuns, op+" "+project+" "+dir)
	fc.f.composeOpts = append(fc.f.composeOpts, opts)
	hold := fc.f.composeHold
	fc.f.mu.Unlock()
	_, _ = fmt.Fprintf(out, "compose %s %s\r\n", op, project)
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (fc fakeCompose) Validate(_ context.Context, _, dir, name string, compose, _ []byte) ([]string, error) {
	if compose == nil {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		compose = b
	}
	if strings.Contains(string(compose), "broken") {
		return nil, fmt.Errorf("%w: yaml: line 2: did not find expected key", errdefs.ErrInvalidArgument)
	}
	return []string{"web"}, nil
}

func (fc fakeCompose) Describe(_ context.Context, dir, file string) (runtime.ComposeProject, error) {
	return runtime.ComposeProject{Name: filepath.Base(dir), Services: []string{"web"}, Files: []string{file}}, nil
}

func (fc fakeCompose) Services(_ context.Context, _, project, _, _ string) ([]string, error) {
	svcs, ok := fc.f.services[project]
	if !ok {
		return nil, fmt.Errorf("%w: services.web: invalid", errdefs.ErrInvalidArgument)
	}
	return svcs, nil
}

type fakeNetworks struct{ f *fakeRuntime }

func (fn fakeNetworks) List(_ context.Context, ns string) ([]runtime.Network, error) {
	fn.f.gotNS = ns
	return fn.f.networks, fn.f.err
}

func (fn fakeNetworks) Create(_ context.Context, _ string, spec runtime.NetworkSpec) (runtime.Network, error) {
	if _, err := runtime.NetworkCreateArgs(spec); err != nil {
		return runtime.Network{}, err
	}
	fn.f.netSpecs = append(fn.f.netSpecs, spec)
	n := runtime.Network{Name: spec.Name, Driver: spec.Driver, Removable: true, Containers: []runtime.NetworkContainer{}}
	fn.f.networks = append(fn.f.networks, n)
	return n, nil
}

func (fn fakeNetworks) Remove(_ context.Context, _, name string) error {
	for i, n := range fn.f.networks {
		if n.Name != name {
			continue
		}
		if !n.Removable {
			return fmt.Errorf("%w: pre-defined", errdefs.ErrFailedPrecondition)
		}
		fn.f.networks = append(fn.f.networks[:i], fn.f.networks[i+1:]...)
		return nil
	}
	return fmt.Errorf("%w: network %q not found", errdefs.ErrNotFound, name)
}

type fakeVolumes struct{ f *fakeRuntime }

func (fv fakeVolumes) List(_ context.Context, ns string, size bool) ([]runtime.Volume, error) {
	fv.f.gotNS, fv.f.volumeSize = ns, size
	return fv.f.volumes, fv.f.err
}

func (fv fakeVolumes) Create(_ context.Context, _, name string, lbls map[string]string) (runtime.Volume, error) {
	for _, v := range fv.f.volumes {
		if v.Name == name {
			return runtime.Volume{}, fmt.Errorf("%w: volume %q already exists", errdefs.ErrAlreadyExists, name)
		}
	}
	v := runtime.Volume{Name: name, Labels: lbls, Containers: []runtime.VolumeMounter{}}
	fv.f.volumes = append(fv.f.volumes, v)
	return v, nil
}

func (fv fakeVolumes) ListFiles(ns, name, dir string) ([]runtime.VolumeEntry, bool, error) {
	fv.f.gotNS = ns
	if name != "data" {
		return nil, false, fmt.Errorf("volume %s: %w", name, errdefs.ErrNotFound)
	}
	if dir != "/" {
		return nil, false, fmt.Errorf("%w: %s is not a directory", errdefs.ErrInvalidArgument, dir)
	}
	return []runtime.VolumeEntry{{Name: "app.conf", Type: "file", Size: 10}}, false, nil
}

func (fv fakeVolumes) PreviewFile(ns, _, _ string) (runtime.VolumeFile, error) {
	fv.f.gotNS = ns
	return runtime.VolumeFile{Size: 10, Content: "listen 80\n"}, nil
}

func (fv fakeVolumes) Download(ns, _, target string) (runtime.VolumeDownload, error) {
	fv.f.gotNS = ns
	if target == "/" {
		return runtime.VolumeDownload{
			Name: "data.tar.gz", Size: -1, Close: func() error { return nil },
			Write: func(w io.Writer) error { _, err := io.WriteString(w, "GZ"); return err },
		}, nil
	}
	return runtime.VolumeDownload{
		Name: "app conf\".txt", Size: 10, Close: func() error { return nil },
		Write: func(w io.Writer) error { _, err := io.WriteString(w, "listen 80\n"); return err },
	}, nil
}

func (fv fakeVolumes) Restore(ns, name string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	fv.f.restored = append(fv.f.restored, ns+"/"+name+":"+string(b))
	return nil
}

func (fv fakeVolumes) Remove(_ context.Context, _, name string) error {
	for i, v := range fv.f.volumes {
		if v.Name != name {
			continue
		}
		if len(v.Containers) > 0 {
			return fmt.Errorf("%w: volume %q is in use", errdefs.ErrFailedPrecondition, name)
		}
		fv.f.volumes = append(fv.f.volumes[:i], fv.f.volumes[i+1:]...)
		return nil
	}
	return fmt.Errorf("%w: volume %q not found", errdefs.ErrNotFound, name)
}

type fakeRegistries struct{ f *fakeRuntime }

func (fr fakeRegistries) List(context.Context) ([]runtime.Registry, error) {
	return fr.f.registries, fr.f.err
}

func (fr fakeRegistries) Login(_ context.Context, server, user, _ string) (runtime.LoginResult, error) {
	if err := fr.f.imageErr[server]; err != nil {
		return runtime.LoginResult{}, err
	}
	fr.f.registries = append(fr.f.registries, runtime.Registry{Server: server, Username: user})
	return runtime.LoginResult{Registry: runtime.Registry{Server: server, Username: user}, Warning: "stored unencrypted"}, nil
}

func (fr fakeRegistries) Logout(_ context.Context, server string) error {
	for i, r := range fr.f.registries {
		if r.Server == server {
			fr.f.registries = append(fr.f.registries[:i], fr.f.registries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: not logged in to %s", errdefs.ErrNotFound, server)
}

type fakeImages struct{ f *fakeRuntime }

func (fi fakeImages) History(_ context.Context, ns, name string) ([]runtime.HistoryEntry, error) {
	fi.f.mu.Lock()
	defer fi.f.mu.Unlock()
	fi.f.gotNS = ns
	if name == "" {
		return nil, fmt.Errorf("%w: image reference is required", errdefs.ErrInvalidArgument)
	}
	return fi.f.history, fi.f.err
}

func (fi fakeImages) List(_ context.Context, ns string) ([]runtime.Image, error) {
	fi.f.gotNS = ns
	return fi.f.images, fi.f.err
}

// Pull writes pullOutput, then waits for pullRelease (when set) or cancellation.
func (fi fakeImages) Pull(ctx context.Context, ns, ref string, opts runtime.PullOptions, out io.Writer) error {
	fi.f.mu.Lock()
	fi.f.pulls = append(fi.f.pulls, pullCall{ns, ref, opts})
	release := fi.f.pullRelease
	fi.f.mu.Unlock()
	_, _ = io.WriteString(out, "pulling "+ref+"\n")
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fi.f.pullErr
}

func (fi fakeImages) Tag(_ context.Context, ns, source, target string) error {
	fi.f.mu.Lock()
	defer fi.f.mu.Unlock()
	fi.f.tags = append(fi.f.tags, [3]string{ns, source, target})
	return fi.f.imageErr[source]
}

func (fi fakeImages) Remove(_ context.Context, ns, ref string, force bool) error {
	fi.f.mu.Lock()
	defer fi.f.mu.Unlock()
	fi.f.removed = append(fi.f.removed, fmt.Sprintf("%s/%s/%v", ns, ref, force))
	return fi.f.imageErr[ref]
}

func (fi fakeImages) RemoteDigest(_ context.Context, ref string) (string, error) {
	fi.f.mu.Lock()
	defer fi.f.mu.Unlock()
	if d, ok := fi.f.remote[ref]; ok {
		return d, nil
	}
	return "", fmt.Errorf("%w: %s not in registry", errdefs.ErrNotFound, ref)
}

func (fi fakeImages) Push(_ context.Context, ns, ref string, opts runtime.PushOptions, out io.Writer) error {
	fi.f.mu.Lock()
	fi.f.pushes = append(fi.f.pushes, pushCall{ns, ref, opts})
	fi.f.mu.Unlock()
	_, _ = io.WriteString(out, "pushing "+ref+"\n")
	return fi.f.imageErr[ref]
}

// buildCall records a build with the Dockerfile it would read: the context is removed after.
type buildCall struct {
	ns, dir    string
	opts       runtime.BuildOptions
	dockerfile string
}

func (fi fakeImages) Build(_ context.Context, ns, dir string, opts runtime.BuildOptions, out io.Writer) error {
	file := opts.Dockerfile
	if file == "" {
		file = "Dockerfile"
	}
	b, _ := os.ReadFile(filepath.Join(dir, file))
	fi.f.mu.Lock()
	fi.f.builds = append(fi.f.builds, buildCall{ns, dir, opts, string(b)})
	fi.f.mu.Unlock()
	_, _ = io.WriteString(out, "building\n")
	return fi.f.imageErr["build"]
}

func (fi fakeImages) BuilderStatus(context.Context, string) runtime.BuilderStatus {
	return fi.f.builder
}

func (fi fakeImages) CheckSave(_ context.Context, ns string, names []string) error {
	fi.f.mu.Lock()
	defer fi.f.mu.Unlock()
	fi.f.gotNS = ns
	if len(names) == 0 {
		return fmt.Errorf("%w: 1 to 50 images are required", errdefs.ErrInvalidArgument)
	}
	for _, n := range names {
		if !slices.ContainsFunc(fi.f.images, func(img runtime.Image) bool { return img.Name == n }) {
			return fmt.Errorf("image %s: %w", n, errdefs.ErrNotFound)
		}
	}
	return nil
}

func (fi fakeImages) Save(ctx context.Context, ns string, names []string, w io.Writer) error {
	if err := fi.CheckSave(ctx, ns, names); err != nil {
		return err
	}
	_, _ = io.WriteString(w, "TAR:"+strings.Join(names, ","))
	return fi.f.saveErr
}

func (fi fakeImages) Load(_ context.Context, ns string, r io.Reader) ([]string, error) {
	b, err := io.ReadAll(r)
	fi.f.mu.Lock()
	defer fi.f.mu.Unlock()
	fi.f.gotNS, fi.f.loaded = ns, b
	if err != nil || string(b) == "bad" {
		return nil, fmt.Errorf("%w: not an image archive", errdefs.ErrInvalidArgument)
	}
	return []string{"docker.io/library/app:1"}, nil
}

type pushCall struct {
	ns, ref string
	opts    runtime.PushOptions
}

type pullCall struct {
	ns, ref string
	opts    runtime.PullOptions
}

func (f *fakeRuntime) List(_ context.Context, ns string) ([]runtime.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotNS = ns
	if f.byNS != nil {
		return f.byNS[ns], f.err
	}
	return f.containers, f.err
}
func (f *fakeRuntime) Do(_ context.Context, ns, id string, a runtime.Action, o runtime.ActionOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, actionCall{ns, id, a, o})
	return f.actionErr[id]
}

func (f *fakeRuntime) Inspect(_ context.Context, ns, ref string, size bool) (any, error) {
	f.gotNS, f.gotSize = ns, size
	if f.err != nil {
		return nil, f.err
	}
	v, ok := f.inspect[ref]
	if !ok {
		return nil, fmt.Errorf("%w: no such container: %s", errdefs.ErrNotFound, ref)
	}
	return v, nil
}

func (f *fakeRuntime) Logs(ctx context.Context, ns, _ string, o runtime.LogOptions, emit runtime.LogEmitter) error {
	f.mu.Lock()
	f.gotNS, f.gotLogs = ns, o
	f.mu.Unlock()
	if f.logErr != nil {
		return f.logErr
	}
	for _, l := range f.logs {
		if err := emit(l); err != nil {
			return err
		}
	}
	if o.Follow && f.logFollow {
		<-ctx.Done()
	}
	return nil
}

func newRouter(rt runtime.Runtime) http.Handler {
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: jobs.NewManager()}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	return r
}

func do(t *testing.T, h http.Handler, path string, out any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", rec.Body, err)
		}
	}
	return rec
}

func TestListEnvironments(t *testing.T) {
	var got struct{ Environments []Environment }
	rec := do(t, newRouter(&fakeRuntime{}), "/api/v1/environments", &got)
	if rec.Code != http.StatusOK || len(got.Environments) != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	want := Environment{ID: "local", Name: "Local host", Kind: "local", DefaultNamespace: "default"}
	if got.Environments[0] != want {
		t.Errorf("got %+v, want %+v", got.Environments[0], want)
	}
}

func TestGetEnvironment(t *testing.T) {
	for _, path := range []string{"/api/v1/environments/local", "/api/v1/environments/local/"} {
		var got EnvironmentStatus
		rec := do(t, newRouter(&fakeRuntime{v: runtime.Versions{Containerd: "v2.4.2"}}), path, &got)
		if rec.Code != http.StatusOK || got.Status != StatusConnected || got.Runtime == nil || got.Runtime.Containerd != "v2.4.2" {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}

	var got EnvironmentStatus
	down := &fakeRuntime{err: runtime.ErrNotConnected, v: runtime.Versions{Nerdctl: runtime.NerdctlStatus{Version: "2.4.2", Compatible: true}}}
	do(t, newRouter(down), "/api/v1/environments/local", &got)
	if got.Status != StatusUnavailable || got.Error == "" {
		t.Errorf("unavailable: %+v", got)
	}
	if got.Runtime == nil || got.Runtime.Nerdctl.Version != "2.4.2" {
		t.Errorf("nerdctl status missing while containerd is down: %+v", got.Runtime)
	}

	rec := do(t, newRouter(&fakeRuntime{}), "/api/v1/environments/remote", nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Errorf("unknown env: %d %s", rec.Code, rec.Body)
	}
}

func TestEnvironmentWarnings(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"sys/module/apparmor/parameters/enabled":                    "Y",
		"proc/sys/kernel/apparmor_restrict_unprivileged_unconfined": "1",
		"proc/sys/kernel/osrelease":                                 "7.0.0-1017-gcp",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{
			Envs:      environment.NewRegistry(environment.Local(&fakeRuntime{}, "default")),
			Jobs:      jobs.NewManager(),
			HostPaths: hostcheck.Paths{Proc: filepath.Join(root, "proc"), Sys: filepath.Join(root, "sys")},
		}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	var got EnvironmentStatus
	do(t, r, "/api/v1/environments/local", &got)
	if !slices.Equal(got.Warnings, []hostcheck.Warning{hostcheck.AppArmorSignals}) {
		t.Errorf("warnings = %v", got.Warnings)
	}
}

func TestNamespaces(t *testing.T) {
	var got struct {
		Namespaces []string
		Default    string
	}
	rec := do(t, newRouter(&fakeRuntime{ns: []string{"default", "k8s.io"}}), "/api/v1/environments/local/namespaces", &got)
	if rec.Code != http.StatusOK || len(got.Namespaces) != 2 || got.Default != "default" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	rec = do(t, newRouter(&fakeRuntime{err: runtime.ErrNotConnected}), "/api/v1/environments/local/namespaces", nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"unavailable"`) {
		t.Errorf("containerd down: %d %s", rec.Code, rec.Body)
	}
}

func TestInfo(t *testing.T) {
	want := runtime.Info{
		ID: "uuid", KernelVersion: "6.8.0", CPUs: 4, MemoryBytes: 8 << 30,
		Components: []runtime.Component{{Name: "runc", Version: "1.3.0"}},
		Plugins:    []runtime.Plugin{{Type: "io.containerd.snapshotter.v1", ID: "overlayfs", Status: runtime.PluginOK}},
	}
	var got runtime.Info
	rec := do(t, newRouter(&fakeRuntime{info: want}), "/api/v1/environments/local/info", &got)
	if rec.Code != http.StatusOK || got.ID != "uuid" || got.CPUs != 4 || got.MemoryBytes != 8<<30 ||
		len(got.Components) != 1 || got.Plugins[0].ID != "overlayfs" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, field := range []string{`"kernelVersion":"6.8.0"`, `"memoryBytes":`, `"status":"ok"`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("body lacks %s: %s", field, rec.Body)
		}
	}

	rec = do(t, newRouter(&fakeRuntime{err: runtime.ErrNotConnected}), "/api/v1/environments/local/info", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("containerd down: %d %s", rec.Code, rec.Body)
	}
}

func TestNamespaceParam(t *testing.T) {
	env := environment.Local(&fakeRuntime{}, "default")
	req := func(q string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x"+q, nil)
		return r.WithContext(context.WithValue(r.Context(), envKey{}, env))
	}
	if ns, err := Namespace(req("")); err != nil || ns != "default" {
		t.Errorf("default: %q %v", ns, err)
	}
	if ns, err := Namespace(req("?namespace=k8s.io")); err != nil || ns != "k8s.io" {
		t.Errorf("explicit: %q %v", ns, err)
	}
	for _, bad := range []string{"?namespace=../etc", "?namespace=a%20b", "?namespace=-x"} {
		if _, err := Namespace(req(bad)); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%s: err = %v, want invalid argument", bad, err)
		}
	}
	if _, err := Namespace(httptest.NewRequest(http.MethodGet, "/x", nil)); !errdefs.IsInvalidArgument(err) {
		t.Errorf("no env and no namespace: %v", err)
	}
}

func TestErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("container web: %w", errdefs.ErrNotFound), http.StatusNotFound, httpjson.CodeNotFound},
		{errdefs.ErrAlreadyExists, http.StatusConflict, httpjson.CodeConflict},
		{errdefs.ErrFailedPrecondition, http.StatusConflict, httpjson.CodeConflict},
		{errdefs.ErrInvalidArgument, http.StatusBadRequest, httpjson.CodeInvalidArgument},
		{errdefs.ErrPermissionDenied, http.StatusForbidden, httpjson.CodeForbidden},
		{runtime.ErrNotConnected, http.StatusServiceUnavailable, httpjson.CodeUnavailable},
		{context.DeadlineExceeded, http.StatusServiceUnavailable, httpjson.CodeUnavailable},
		{errdefs.ErrNotImplemented, http.StatusNotImplemented, httpjson.CodeUnavailable},
		{errors.New("boom"), http.StatusInternalServerError, httpjson.CodeInternal},
	} {
		status, code := httpjson.Status(tt.err)
		if status != tt.status || code != tt.code {
			t.Errorf("%v: got %d %s, want %d %s", tt.err, status, code, tt.status, tt.code)
		}
	}

	// Internal errors do not leak details.
	rec := httptest.NewRecorder()
	httpjson.WriteErr(rec, httptest.NewRequest(http.MethodGet, "/", nil), nil, errors.New("secret detail"))
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Errorf("internal error leaked: %s", rec.Body)
	}
}

func TestReadOnlyMode(t *testing.T) {
	newEnvRouter := func(st runtime.NerdctlStatus) http.Handler {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(&fakeRuntime{nerdctl: st}, "default"))}
		r := chi.NewRouter()
		r.Route("/api/v1/environments/{env}", func(r chi.Router) {
			r.Use(h.resolveEnv, h.requireWritable)
			ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
			r.Get("/thing", ok)
			r.Post("/thing", ok)
			r.Delete("/thing", ok)
		})
		return r
	}
	call := func(h http.Handler, method string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/environments/local/thing", nil))
		return rec
	}

	incompatible := newEnvRouter(runtime.NerdctlStatus{Version: "2.3.5", Required: "2.4"})
	if rec := call(incompatible, http.MethodGet); rec.Code != http.StatusNoContent {
		t.Errorf("GET in read-only mode: %d", rec.Code)
	}
	for _, m := range []string{http.MethodPost, http.MethodDelete} {
		rec := call(incompatible, m)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), httpjson.CodeNerdctlIncompatible) ||
			!strings.Contains(rec.Body.String(), "2.3.5") || !strings.Contains(rec.Body.String(), "2.4.x") {
			t.Errorf("%s in read-only mode: %d %s", m, rec.Code, rec.Body)
		}
	}

	missing := newEnvRouter(runtime.NerdctlStatus{Required: "2.4", Error: "nerdctl binary \"nerdctl\" not found"})
	if rec := call(missing, http.MethodPost); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "not found") {
		t.Errorf("missing nerdctl: %d %s", rec.Code, rec.Body)
	}

	for name, st := range map[string]runtime.NerdctlStatus{
		"compatible": {Version: "2.4.0", Required: "2.4", Compatible: true},
		"skipped":    {Version: "2.3.5", Required: "2.4", CheckSkipped: true},
	} {
		if rec := call(newEnvRouter(st), http.MethodPost); rec.Code != http.StatusNoContent {
			t.Errorf("%s: POST = %d", name, rec.Code)
		}
	}
}
