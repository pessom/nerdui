// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"slices"
	"strings"
	"testing"
)

func TestParseProcNet(t *testing.T) {
	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1
   1: 0100007F:0CEA 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2 1
   2: 0F02000A:0016 0101A8C0:D2F0 01 00000000:00000000 00:00000000 00000000     0        0 3 1
`
	got := parseProcNet(strings.NewReader(tcp), "tcp", tcpListen)
	want := []Listener{{IP: "0.0.0.0", Port: 22, Protocol: "tcp"}, {IP: "127.0.0.1", Port: 3306, Protocol: "tcp"}}
	if !slices.Equal(got, want) {
		t.Errorf("tcp: %+v", got)
	}

	tcp6 := `  sl  local_address                         remote_address                        st
   0: 00000000000000000000000000000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000
   1: 00000000000000000000000001000000:0277 00000000000000000000000000000000:0000 0A 00000000:00000000
`
	got = parseProcNet(strings.NewReader(tcp6), "tcp", tcpListen)
	want = []Listener{{IP: "::", Port: 8080, Protocol: "tcp"}, {IP: "::1", Port: 631, Protocol: "tcp"}}
	if !slices.Equal(got, want) {
		t.Errorf("tcp6: %+v", got)
	}

	udp := `  sl  local_address rem_address   st
  10: 3500007F:0035 00000000:0000 07 00000000:00000000
  11: 00000000:0044 00000000:0000 01 00000000:00000000
`
	got = parseProcNet(strings.NewReader(udp), "udp", udpUnconnected)
	want = []Listener{{IP: "127.0.0.53", Port: 53, Protocol: "udp"}}
	if !slices.Equal(got, want) {
		t.Errorf("udp: %+v", got)
	}

	if _, ok := hostListeners(t.TempDir()); ok {
		t.Error("an empty directory has no tables")
	}
}
