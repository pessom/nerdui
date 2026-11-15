// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package prometheus

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestIntegrationContainerdMetrics checks that containerd's built-in metrics endpoint exports
// every metric the history queries use, with the container_id and namespace labels (#176).
// NERDUI_TEST_CONTAINERD_METRICS is the endpoint, e.g. http://127.0.0.1:1338/v1/metrics.
func TestIntegrationContainerdMetrics(t *testing.T) {
	endpoint := os.Getenv("NERDUI_TEST_CONTAINERD_METRICS")
	if endpoint == "" {
		t.Skip("NERDUI_TEST_CONTAINERD_METRICS is not set")
	}
	name := "it-metrics-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { _ = exec.Command("nerdctl", "rm", "-f", name).Run() })
	out, err := exec.Command("nerdctl", "run", "-d", "--name", name, "--memory", "64m",
		"docker.io/library/alpine:3", "sh", "-c", "while :; do dd if=/dev/zero of=/tmp/f bs=64k count=16 conv=fsync 2>/dev/null; sleep 1; done").CombinedOutput()
	if err != nil {
		t.Fatalf("nerdctl run: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	// Labels are sorted by name in the text format; block I/O adds major and minor between.
	labels := []string{fmt.Sprintf(`container_id=%q`, id), `namespace="default"`}

	want := []string{MetricCPU, MetricMemory, MetricInactiveFile, MetricMemoryLimit, MetricPIDs, MetricIORead, MetricIOWrite}
	var found map[string]string
	deadline := time.Now().Add(30 * time.Second)
	for {
		found = scrape(t, endpoint, labels, want)
		if len(found) == len(want) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	for _, m := range want {
		if _, ok := found[m]; !ok {
			t.Errorf("metric %s with %v is not exported", m, labels)
		}
	}
	if v := found[MetricMemoryLimit]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err != nil || f != 64<<20 {
			t.Errorf("%s = %s, want the 64 MiB limit", MetricMemoryLimit, v)
		}
	}
	t.Logf("containerd metrics of %s: %v", id[:12], found)
}

// scrape returns the value of the first sample of each wanted metric with the labels.
func scrape(t *testing.T, endpoint string, labels, want []string) map[string]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scrape %s: %v", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	found := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 64<<20))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		for _, m := range want {
			if _, ok := found[m]; ok || !strings.HasPrefix(line, m+"{") {
				continue
			}
			if !strings.Contains(line, labels[0]) || !strings.Contains(line, labels[1]) {
				continue
			}
			found[m] = line[strings.LastIndexByte(line, ' ')+1:]
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	return found
}
