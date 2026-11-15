// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Listener is a socket of the host bound to a port: a listening TCP socket or a bound UDP
// socket (#62).
type Listener struct {
	IP       string
	Port     int32
	Protocol string // tcp or udp
}

// TCP state LISTEN and UDP state "unconnected" (TCP_CLOSE) in /proc/net.
const (
	tcpListen      = "0A"
	udpUnconnected = "07"
)

// hostListeners reads the sockets of nerdui's network namespace from procNet (/proc/net):
// the host's when nerdui runs as a binary on the host. ok is false when nothing could be read
// (other operating systems, or /proc unavailable).
func hostListeners(procNet string) (out []Listener, ok bool) {
	for _, f := range []struct{ file, proto, state string }{
		{"tcp", "tcp", tcpListen}, {"tcp6", "tcp", tcpListen},
		{"udp", "udp", udpUnconnected}, {"udp6", "udp", udpUnconnected},
	} {
		fh, err := os.Open(filepath.Join(procNet, f.file)) //nolint:gosec // G304: fixed /proc/net tables (tests pass a temp dir)
		if err != nil {
			continue
		}
		ok = true
		out = append(out, parseProcNet(fh, f.proto, f.state)...)
		_ = fh.Close()
	}
	return out, ok
}

// parseProcNet parses a /proc/net/{tcp,tcp6,udp,udp6} table, keeping sockets in state.
func parseProcNet(r io.Reader, proto, state string) []Listener {
	var out []Listener
	seen := map[Listener]bool{}
	sc := bufio.NewScanner(r)
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[3] != state {
			continue
		}
		addr, portHex, found := strings.Cut(f[1], ":")
		if !found {
			continue
		}
		port, err := strconv.ParseUint(portHex, 16, 16)
		if err != nil || port == 0 {
			continue
		}
		ip := procNetIP(addr)
		if ip == nil {
			continue
		}
		l := Listener{IP: ip.String(), Port: int32(port), Protocol: proto}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

// procNetIP decodes an address of /proc/net: 32-bit words in host (little-endian) byte order.
func procNetIP(s string) net.IP {
	b, err := hex.DecodeString(s)
	if err != nil || (len(b) != net.IPv4len && len(b) != net.IPv6len) {
		return nil
	}
	ip := make(net.IP, len(b))
	for i := 0; i < len(b); i += 4 {
		binary.BigEndian.PutUint32(ip[i:], binary.LittleEndian.Uint32(b[i:]))
	}
	return ip
}
