package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"
)

// TriggerFunc is called when a "run" request arrives.
type TriggerFunc func(jobName, reason string) error

// StatusFunc is called when a "status" request arrives.
type StatusFunc func() ([]JobStatus, error)

// StopFunc is called when a "stop" request arrives.
type StopFunc func()

// DagFunc is called when a "dag" request arrives.
// It returns a JSON-encoded byte slice and an error.
type DagFunc func(asJSON bool) ([]byte, error)

// CancelFunc is called when a "cancel" request arrives.
type CancelFunc func(jobName string) error

// SkipFunc is called when a "skip" request arrives.
type SkipFunc func(jobName string) error

// ReloadFunc is called when a "reload" request arrives.
type ReloadFunc func() error

// Callbacks bundles all daemon-side callback functions the server may call.
type Callbacks struct {
	OnStatus  StatusFunc
	OnTrigger TriggerFunc
	OnStop    StopFunc
	OnDag     DagFunc
	OnCancel  CancelFunc
	OnSkip    SkipFunc
	OnReload  ReloadFunc
}

// Server listens on a Unix domain socket and handles IPC requests from the
// husky CLI.
type Server struct {
	socketPath string
	cb         Callbacks
	logger     *slog.Logger
}

// NewServer creates a Server that listens on socketPath.
func NewServer(socketPath string, cb Callbacks, logger *slog.Logger) *Server {
	return &Server{socketPath: socketPath, cb: cb, logger: logger}
}

// ListenAndServe starts the listener and blocks until ctx is cancelled.
// It removes any stale socket file before binding.
func (s *Server) ListenAndServe(ctx context.Context) error {
	_ = os.Remove(s.socketPath)

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("ipc: listen %q: %w", s.socketPath, err)
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	if s.logger != nil {
		s.logger.Info("ipc server listening", "socket", s.socketPath)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if s.logger != nil {
				s.logger.Warn("ipc accept error", "error", err)
			}
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	// Short deadline for reading the request only — prevents hung clients from
	// holding a goroutine forever.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "invalid request: " + err.Error()})
		return
	}

	// Request received — remove the deadline so the handler can take as long
	// as it needs (e.g. dag ASCII render, slow mutex acquisition at startup),
	// then give the write a generous deadline.
	_ = conn.SetDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

	enc := json.NewEncoder(conn)
	reply := func(ok bool, errMsg string, data []byte) {
		r := Response{OK: ok, Error: errMsg}
		if data != nil {
			r.Data = data
		}
		_ = enc.Encode(r)
	}

	// Recover from panics in handler callbacks so the client always receives
	// a response instead of a silent EOF.
	defer func() {
		if r := recover(); r != nil {
			if s.logger != nil {
				s.logger.Error("ipc handler panic", "request", req.Type, "panic", r)
			}
			reply(false, fmt.Sprintf("internal error: %v", r), nil)
		}
	}()

	switch req.Type {
	case ReqPing:
		reply(true, "", nil)

	case ReqStatus:
		statuses, err := s.cb.OnStatus()
		if err != nil {
			reply(false, err.Error(), nil)
			return
		}
		data, _ := json.Marshal(statuses)
		reply(true, "", data)

	case ReqRun:
		if req.Job == "" {
			reply(false, "job name required", nil)
			return
		}
		if err := s.cb.OnTrigger(req.Job, req.Reason); err != nil {
			reply(false, err.Error(), nil)
			return
		}
		reply(true, "", nil)

	case ReqStop:
		reply(true, "", nil)
		go s.cb.OnStop()

	case ReqDag:
		if s.cb.OnDag == nil {
			reply(false, "dag not available", nil)
			return
		}
		data, err := s.cb.OnDag(req.JSON)
		if err != nil {
			reply(false, err.Error(), nil)
			return
		}
		reply(true, "", data)

	case ReqCancel:
		if req.Job == "" {
			reply(false, "job name required", nil)
			return
		}
		if s.cb.OnCancel == nil {
			reply(false, "cancel not available", nil)
			return
		}
		if err := s.cb.OnCancel(req.Job); err != nil {
			reply(false, err.Error(), nil)
			return
		}
		reply(true, "", nil)

	case ReqSkip:
		if req.Job == "" {
			reply(false, "job name required", nil)
			return
		}
		if s.cb.OnSkip == nil {
			reply(false, "skip not available", nil)
			return
		}
		if err := s.cb.OnSkip(req.Job); err != nil {
			reply(false, err.Error(), nil)
			return
		}
		reply(true, "", nil)

	case ReqReload:
		if s.cb.OnReload == nil {
			reply(false, "reload not available", nil)
			return
		}
		if err := s.cb.OnReload(); err != nil {
			reply(false, err.Error(), nil)
			return
		}
		reply(true, "", nil)

	case ReqRetry:
		if req.Job == "" {
			reply(false, "job name required", nil)
			return
		}
		// Retry is implemented as a manual trigger with "retry" reason.
		if err := s.cb.OnTrigger(req.Job, "retry"); err != nil {
			reply(false, err.Error(), nil)
			return
		}
		reply(true, "", nil)

	default:
		reply(false, fmt.Sprintf("unknown request type: %q", req.Type), nil)
	}
}
