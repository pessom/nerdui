#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 The nerdui Authors
# SPDX-License-Identifier: AGPL-3.0-only
#
# Measures the performance NFRs of a running nerdui (#129, docs/nfr.md) and prints a Markdown
# report. Everything is created in a separate namespace and removed at the end, so it is safe
# on a host with other workloads; it does put load on the host while it runs (pull, creates).
#
#   NERDUI_KEY=nerdui_... sudo -E scripts/nfr-bench.sh [options]
#
# NERDUI_KEY is an API key of an administrator (read-only is enough: the script changes the
# host with nerdctl, not through nerdui). Needs root (nerdctl), curl, jq, gzip.
#
# Options:
#   --url URL          nerdui address (default https://127.0.0.1:9443; TLS is not verified
#                      for 127.0.0.1/localhost)
#   --containers N     containers to create (default 500)
#   --images N         image tags to create (default 300)
#   --requests N       requests per list endpoint (default 50)
#   --idle SECONDS     idle measurement window (default 60)
#   --image REF        small image to use (default docker.io/library/busybox:1.37)
#   --keep             keep the namespace for inspection
set -euo pipefail

url=https://127.0.0.1:9443
containers=500
images=300
requests=50
idle=60
image=docker.io/library/busybox:1.37
keep=0
while [ $# -gt 0 ]; do
	case "$1" in
	--url) url=$2; shift 2 ;;
	--containers) containers=$2; shift 2 ;;
	--images) images=$2; shift 2 ;;
	--requests) requests=$2; shift 2 ;;
	--idle) idle=$2; shift 2 ;;
	--image) image=$2; shift 2 ;;
	--keep) keep=1; shift ;;
	-h | --help) sed -n '2,25p' "$0"; exit 0 ;;
	*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
done
: "${NERDUI_KEY:?set NERDUI_KEY to an API key}"
for tool in nerdctl curl jq gzip; do
	command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 2; }
done

ns=nfr-bench-$$
curl_opts=(-sS -H "Authorization: Bearer $NERDUI_KEY")
case "$url" in https://127.0.0.1* | https://localhost*) curl_opts+=(-k) ;; esac
api() { curl "${curl_opts[@]}" -f "$url$1"; }
log() { echo "• $*" >&2; }

cleanup() {
	[ "$keep" = 1 ] && { log "kept namespace $ns"; return; }
	log "cleaning up namespace $ns"
	nerdctl -n "$ns" ps -aq | xargs -r nerdctl -n "$ns" rm -f >/dev/null 2>&1 || true
	nerdctl -n "$ns" images -q | sort -u | xargs -r nerdctl -n "$ns" rmi -f >/dev/null 2>&1 || true
	nerdctl namespace rm "$ns" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# p50/p95/max in milliseconds of the times (seconds, one per line) on stdin.
percentiles() {
	sort -n | awk '{ v[NR] = $1 * 1000 } END {
		if (NR == 0) { print "- - -"; exit }
		i50 = int(NR * 0.50 + 0.5); i95 = int(NR * 0.95 + 0.5)
		if (i50 < 1) i50 = 1; if (i95 < 1) i95 = 1
		printf "%.0f %.0f %.0f\n", v[i50], v[i95], v[NR] }'
}

# Times of n GET requests over one kept-alive connection (no TLS handshake per request).
timings() {
	local path=$1 n=$2 urls=()
	for _ in $(seq 1 "$n"); do urls+=("$url$path"); done
	curl "${curl_opts[@]}" -o /dev/null -w '%{http_code} %{time_total}\n' "${urls[@]}" |
		awk '$1 != 200 { bad = 1 } { print $2 } END { if (bad) exit 1 }'
}

pid_of_nerdui() {
	local pid
	pid=$(systemctl show -p MainPID --value nerdui 2>/dev/null || true)
	[ -n "$pid" ] && [ "$pid" != 0 ] || pid=$(pgrep -xo nerdui || true)
	echo "$pid"
}

# CPU ticks (utime + stime) of a process.
cpu_ticks() { awk '{ print $14 + $15 }' "/proc/$1/stat"; }
rss_mb() { awk '/^VmRSS:/ { printf "%.0f", $2 / 1024 }' "/proc/$1/status"; }

version=$(api /api/v1/version | jq -r '.version')
host="$(uname -m), $(nproc) CPU, $(awk '/MemTotal/ { printf "%.1f GiB", $2 / 1048576 }' /proc/meminfo)"
log "nerdui $version on $host"

pid=$(pid_of_nerdui)
[ -n "$pid" ] || { echo "nerdui process not found (run on the nerdui host as root)" >&2; exit 2; }

# NFR-005: idle resources, measured first, before the benchmark warms caches.
log "measuring idle usage for ${idle}s"
hz=$(getconf CLK_TCK)
t0=$(cpu_ticks "$pid")
sleep "$idle"
t1=$(cpu_ticks "$pid")
idle_cpu=$(awk -v d="$((t1 - t0))" -v hz="$hz" -v s="$idle" 'BEGIN { printf "%.2f", d / hz / s * 100 }')
idle_rss=$(rss_mb "$pid")

# Fixtures: N image tags and N created (not started) containers in a fresh namespace.
log "creating $images image tags and $containers containers in $ns"
nerdctl namespace create "$ns" >/dev/null
nerdctl -n "$ns" pull -q "$image" >/dev/null
for i in $(seq 2 "$images"); do nerdctl -n "$ns" tag "$image" "nfr-bench/img:$i"; done
for i in $(seq 1 "$containers"); do
	nerdctl -n "$ns" create --net none --name "c$i" "$image" true >/dev/null
done

# NFR-001: list latency.
log "timing list endpoints ($requests requests each)"
timings "/api/v1/environments/local/containers?namespace=$ns" 3 >/dev/null # warm up
read -r c50 c95 cmax < <(timings "/api/v1/environments/local/containers?namespace=$ns" "$requests" | percentiles)
timings "/api/v1/environments/local/images?namespace=$ns" 3 >/dev/null
read -r i50 i95 imax < <(timings "/api/v1/environments/local/images?namespace=$ns" "$requests" | percentiles)
listed=$(api "/api/v1/environments/local/containers?namespace=$ns" | jq '.containers | length')
listed_images=$(api "/api/v1/environments/local/images?namespace=$ns" | jq '.images | length')
peak_rss=$(rss_mb "$pid")

# NFR-003: a container created with the CLI shows up in nerdui's event history.
log "measuring CLI-to-nerdui event latency"
start=$(date +%s%N)
nerdctl -n "$ns" create --net none --name fresh "$image" true >/dev/null
fresh_ms=-
for _ in $(seq 1 100); do
	if api "/api/v1/environments/local/events?namespace=$ns&limit=20" |
		jq -e '.events[] | select(.topic == "/containers/create")' >/dev/null; then
		fresh_ms=$((($(date +%s%N) - start) / 1000000))
		break
	fi
	sleep 0.05
done

# NFR-004: log download throughput and time to the first byte.
log "measuring log throughput"
lines=100000
nerdctl -n "$ns" run --net none --name logs "$image" seq 1 "$lines" >/dev/null
id=$(nerdctl -n "$ns" inspect -f '{{.ID}}' logs)
read -r log_ttfb log_total log_bytes < <(curl "${curl_opts[@]}" -o /dev/null \
	-w '%{time_starttransfer} %{time_total} %{size_download}\n' \
	"$url/api/v1/environments/local/containers/$id/logs?namespace=$ns")
log_rate=$(awk -v n="$lines" -v t="$log_total" 'BEGIN { printf "%.0f", n / t }')
log_ttfb_ms=$(awk -v t="$log_ttfb" 'BEGIN { printf "%.0f", t * 1000 }')

# NFR-002: initial page and JavaScript (index and the chunks it preloads), gzip-compressed.
log "measuring the initial page"
page=$(curl "${curl_opts[@]}" "$url/")
page_time=$(curl "${curl_opts[@]}" -o /dev/null -w '%{time_total}' "$url/")
js_bytes=0
for asset in $(grep -oE '/assets/[^"]+\.js' <<<"$page" | sort -u); do
	n=$(curl "${curl_opts[@]}" -f "$url$asset" | gzip -9 | wc -c)
	js_bytes=$((js_bytes + n))
done
js_kb=$((js_bytes / 1024))

verdict() { awk -v v="$1" -v l="$2" 'BEGIN { if (v == "-") print "❔"; else print (v + 0 <= l + 0) ? "✅" : "❌" }'; }

cat <<EOF
## nerdui NFR measurements

nerdui \`$version\` · $host · $(date -u +%Y-%m-%dT%H:%MZ)

| NFR | Measure | Result | Target | |
| --- | --- | --- | --- | --- |
| NFR-001 | \`GET /containers\`, $listed containers (p50 / p95 / max) | $c50 / $c95 / $cmax ms | p95 ≤ 300 ms | $(verdict "$c95" 300) |
| NFR-001 | \`GET /images\`, $listed_images images (p50 / p95 / max) | $i50 / $i95 / $imax ms | p95 ≤ 300 ms | $(verdict "$i95" 300) |
| NFR-002 | Initial JavaScript, gzip | $js_kb KB | ≤ 500 KB | $(verdict "$js_kb" 500) |
| NFR-002 | Initial page response | $(awk -v t="$page_time" 'BEGIN { printf "%.0f", t * 1000 }') ms | (load ≤ 1.5 s) | |
| NFR-003 | CLI \`nerdctl create\` → event in nerdui | $fresh_ms ms | ≤ 2000 ms | $(verdict "$fresh_ms" 2000) |
| NFR-004 | Logs: first byte | $log_ttfb_ms ms | ≤ 1000 ms | $(verdict "$log_ttfb_ms" 1000) |
| NFR-004 | Logs: $lines lines ($((log_bytes / 1024)) KB) | $log_rate lines/s | ≥ 10000 lines/s | $(awk -v r="$log_rate" 'BEGIN { print (r >= 10000) ? "✅" : "❌" }') |
| NFR-005 | Idle RSS | $idle_rss MB | ≤ 80 MB | $(verdict "$idle_rss" 80) |
| NFR-005 | Idle CPU (${idle}s) | $idle_cpu % | ≤ 1 % | $(verdict "$idle_cpu" 1) |
| — | RSS after the list benchmark | $peak_rss MB | | |
EOF
