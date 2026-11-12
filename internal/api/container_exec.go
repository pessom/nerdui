// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/coder/websocket"
	"github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/api/ws"
	"github.com/pessom/nerdui/internal/auth"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
)

// ExecExit is the last data message of a terminal session.
type ExecExit struct {
	ExitCode int `json:"exitCode"`
}

// execControl is a text message from the client: a terminal size change.
type execControl struct {
	Type string `json:"type"` // "resize"
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// containerExec opens a terminal in a running container (#15, ADR-0022):
//
//	GET /api/v1/environments/{env}/containers/{id}/exec?namespace=&cmd=&user=&workdir=&cols=&rows=
//
// Binary frames carry keyboard input (client → server) and terminal output (server →
// client); text frames from the client are {"type":"resize","cols":…,"rows":…}. The session
// ends with a data message {"exitCode": n} and "end". Closing the WebSocket kills the
// process. cmd may repeat (argv); without it bash or sh runs.
func (h *Handler) containerExec(w http.ResponseWriter, r *http.Request) {
	if h.ExecDisabled {
		httpjson.WriteError(w, http.StatusForbidden, httpjson.CodeForbidden, "the web terminal is disabled on this server (--disable-exec)")
		return
	}
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	q := r.URL.Query()
	opts := runtime.ExecOptions{Cmd: q["cmd"], User: q.Get("user"), Workdir: q.Get("workdir")}
	for _, p := range []struct {
		name string
		dst  *uint16
	}{{"cols", &opts.Cols}, {"rows", &opts.Rows}} {
		if v := q.Get(p.name); v != "" {
			n, err := strconv.ParseUint(v, 10, 16)
			if err != nil {
				httpjson.WriteErr(w, r, h.Logger, fmt.Errorf("%w: %s must be a number", errdefs.ErrInvalidArgument, p.name))
				return
			}
			*p.dst = uint16(n)
		}
	}
	conn, err := ws.Accept(w, r, ws.Options{Bidirectional: true})
	if err != nil {
		return
	}
	id := chi.URLParam(r, "id")
	user := ""
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		user = p.User.Username
	}
	logger := h.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("user", user, "environment", EnvFrom(r.Context()).ID, "namespace", ns,
		"container", id, "cmd", opts.Cmd, "exec_user", opts.User)
	logger.Info("terminal opened")

	stdin, stdinW := io.Pipe()
	resize := make(chan runtime.TermSize, 4)
	go func() {
		defer stdinW.Close()
		for {
			typ, data, err := conn.Read()
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				if _, err := stdinW.Write(data); err != nil {
					return
				}
				continue
			}
			var c execControl
			if json.Unmarshal(data, &c) == nil && c.Type == "resize" {
				select {
				case resize <- runtime.TermSize{Cols: c.Cols, Rows: c.Rows}:
				default: // a newer size follows
				}
			}
		}
	}()

	code, err := EnvFrom(r.Context()).Runtime.Containers().Exec(conn.Context(), ns, id, opts, stdin,
		writerFunc(func(p []byte) (int, error) {
			if !conn.SendBinaryWait(p) {
				return 0, errClientGone
			}
			return len(p), nil
		}), resize)
	_ = stdin.Close()
	switch {
	case conn.Context().Err() != nil:
		logger.Info("terminal closed by the client")
		return
	case err != nil:
		logger.Warn("terminal failed", "error", err)
		_, errCode := httpjson.Status(err)
		conn.SendError(errCode, err.Error())
	default:
		logger.Info("terminal ended", "exit_code", code)
		conn.SendDataWait(ExecExit{ExitCode: code})
	}
	conn.End()
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
