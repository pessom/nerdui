// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
)

type redeployFake struct {
	Runtime
	images     [][]Image // List answers, in order
	containers []Container
	removed    []string
	runs       []string
}

type redeployContainers struct {
	Containers
	f *redeployFake
}

type redeployImages struct {
	Images
	f *redeployFake
}

type redeployCompose struct {
	Compose
	f *redeployFake
}

func (f *redeployFake) Containers() Containers { return redeployContainers{f: f} }
func (f *redeployFake) Images() Images         { return redeployImages{f: f} }
func (f *redeployFake) Compose() Compose       { return redeployCompose{f: f} }

func (c redeployContainers) List(context.Context, string) ([]Container, error) {
	return c.f.containers, nil
}

func (c redeployContainers) Do(_ context.Context, _, id string, a Action, _ ActionOptions) error {
	if a == ActionRemove {
		c.f.removed = append(c.f.removed, id)
	}
	return nil
}

func (i redeployImages) List(context.Context, string) ([]Image, error) {
	next := i.f.images[0]
	if len(i.f.images) > 1 {
		i.f.images = i.f.images[1:]
	}
	return next, nil
}

func (c redeployCompose) Run(_ context.Context, _, project, _, op string, opts ComposeOptions, _ io.Writer) error {
	c.f.runs = append(c.f.runs, strings.TrimSpace(op+" "+project+" "+strings.Join(opts.Services, ",")))
	return nil
}

func TestRedeploy(t *testing.T) {
	svc := func(id, service, image string, labels map[string]string) Container {
		return Container{ID: id, Name: id, Image: image, Labels: labels, Compose: &ComposeRef{Project: "shop", Service: service}}
	}
	containers := []Container{
		svc("web1", "web", "docker.io/acme/web:latest", nil),
		svc("web2", "web", "docker.io/acme/web:latest", nil),
		svc("db1", "db", "docker.io/library/postgres:17", nil),
		// Created by nerdctl 2.4 from an image pulled before without recreating.
		svc("cache1", "cache", "docker.io/library/redis:8", map[string]string{imageDigestLabel: "sha256:old-redis"}),
		{ID: "other", Image: "docker.io/acme/web:latest", Compose: &ComposeRef{Project: "blog", Service: "web"}},
	}
	before := []Image{
		{Name: "docker.io/acme/web:latest", Digest: "sha256:web1"},
		{Name: "docker.io/library/postgres:17", Digest: "sha256:pg"},
		{Name: "docker.io/library/redis:8", Digest: "sha256:redis"},
	}
	after := []Image{
		{Name: "docker.io/acme/web:latest", Digest: "sha256:web2"},
		{Name: "docker.io/library/postgres:17", Digest: "sha256:pg"},
		{Name: "docker.io/library/redis:8", Digest: "sha256:redis"},
	}

	f := &redeployFake{images: [][]Image{before, after}, containers: containers}
	var out strings.Builder
	if err := Redeploy(context.Background(), f, "default", "shop", "/srv/shop", RedeployOptions{RemoveOrphans: true}, &out); err != nil {
		t.Fatal(err)
	}
	slices.Sort(f.removed)
	if !slices.Equal(f.removed, []string{"cache1", "web1", "web2"}) || !slices.Equal(f.runs, []string{"pull shop", "up shop"}) {
		t.Errorf("removed %v runs %v", f.removed, f.runs)
	}
	if !strings.Contains(out.String(), "[cache web]") {
		t.Errorf("output %q", out.String())
	}

	// One service: only its containers are considered; nothing changed, nothing is removed.
	f = &redeployFake{images: [][]Image{after}, containers: containers}
	out.Reset()
	if err := Redeploy(context.Background(), f, "default", "shop", "/srv/shop", RedeployOptions{Services: []string{"db"}}, &out); err != nil {
		t.Fatal(err)
	}
	if len(f.removed) != 0 || !slices.Equal(f.runs, []string{"pull shop db", "up shop db"}) || !strings.Contains(out.String(), "No new images") {
		t.Errorf("removed %v runs %v out %q", f.removed, f.runs, out.String())
	}
}
