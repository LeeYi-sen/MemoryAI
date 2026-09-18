package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type daemonRequest struct {
	Args []string `json:"args"`
}

type daemonResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
	Frame *Frame `json:"frame,omitempty"`
}

var daemonConnections uint64

const defaultDaemonIdleInterval = time.Second

const (
	defaultDaemonConnectionTimeout = 15 * time.Second
	hardDaemonConnectionTimeout    = 60 * time.Second
	defaultDaemonMaxConcurrent     = 32
	hardDaemonMaxConcurrent        = 256
)

func daemonConnectionTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_DAEMON_TIMEOUT_MS"))
	if raw == "" {
		return defaultDaemonConnectionTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 1 {
		return defaultDaemonConnectionTimeout
	}
	d := time.Duration(ms) * time.Millisecond
	if d > hardDaemonConnectionTimeout {
		return hardDaemonConnectionTimeout
	}
	return d
}

func daemonMaxConcurrent() int {
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_DAEMON_MAX_CONCURRENT"))
	if raw == "" {
		return defaultDaemonMaxConcurrent
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultDaemonMaxConcurrent
	}
	if n > hardDaemonMaxConcurrent {
		return hardDaemonMaxConcurrent
	}
	return n
}

func writeDaemonResponseBounded(w io.Writer, res daemonResponse) error {
	maxBytes := daemonTransportMaxBytes()
	payload, err := encodeJSONPhysicalBounded(res, maxBytes, "daemon response")
	if err != nil {
		payload, err = encodeJSONPhysicalBounded(daemonResponse{
			OK:    false,
			Error: fmt.Sprintf("daemon response exceeds physical byte ceiling: max=%d", maxBytes),
		}, maxBytes, "daemon error response")
		if err != nil {
			return err
		}
	}
	_, err = w.Write(payload)
	return err
}

func (e *Engine) serveDaemonConn(conn net.Conn) {
	if conn == nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(daemonConnectionTimeout()))

	maxBytes := daemonTransportMaxBytes()
	limited := &io.LimitedReader{R: conn, N: maxBytes + 1}
	dec := json.NewDecoder(limited)
	var req daemonRequest
	decodeErr := dec.Decode(&req)
	consumed := maxBytes + 1 - limited.N
	if consumed > maxBytes {
		_ = writeDaemonResponseBounded(conn, daemonResponse{
			OK:    false,
			Error: fmt.Sprintf("daemon request exceeds physical byte ceiling: max=%d", maxBytes),
		})
		return
	}
	if decodeErr != nil {
		_ = writeDaemonResponseBounded(conn, daemonResponse{OK: false, Error: decodeErr.Error()})
		return
	}
	_ = writeDaemonResponseBounded(conn, e.handleDaemonRequest(req))
}

func daemonIdleInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_IDLE_INTERVAL_MS"))
	if raw == "" {
		return defaultDaemonIdleInterval
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 1 {
		return defaultDaemonIdleInterval
	}
	return time.Duration(ms) * time.Millisecond
}

func (e *Engine) runPhysicalIdleTicker(stop <-chan struct{}) {
	// Resume any durable response-ready physical I/O before waiting for the
	// first clock tick. This never chooses a cognitive goal or source.
	_ = e.processExternalIORequests()
	ticker := time.NewTicker(daemonIdleInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// The ticker is a physical clock only. What idle means and which
			// cognition runs are entirely selected by executable Memory triggers.
			_ = e.fireEvent("idle", "", newFrame())
			// Memory may have created opaque physical I/O request envelopes while
			// handling idle. Execute only those envelopes; source choice is Memory-owned.
			_ = e.processExternalIORequests()
		case <-stop:
			return
		}
	}
}

func runDaemonClient(socket string, args []string) error {
	if strings.TrimSpace(socket) == "" {
		return errors.New("daemon socket required")
	}
	c, err := net.Dial("unix", socket)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(daemonConnectionTimeout()))

	payload, err := json.Marshal(daemonRequest{Args: args})
	if err != nil {
		return err
	}
	maxBytes := daemonTransportMaxBytes()
	if int64(len(payload)) > maxBytes {
		return fmt.Errorf("daemon request exceeds physical byte ceiling: size=%d max=%d", len(payload), maxBytes)
	}
	payload = append(payload, '\n')
	if _, err := c.Write(payload); err != nil {
		return err
	}
	response, err := readAllPhysicalBounded(c, maxBytes, "daemon response")
	if err != nil {
		return err
	}
	var res daemonResponse
	if err := json.Unmarshal(response, &res); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(b))
	if !res.OK {
		if res.Error == "" {
			res.Error = "daemon request failed"
		}
		return errors.New(res.Error)
	}
	return nil
}

func daemonFrameFromArgs(args []string) *Frame {
	f := newFrame()
	for k, v := range kvArgs(args) {
		f.Vars[k] = v
	}
	return f
}

// resolveMemoryRunTarget gives executable Memory a physical event opportunity
// to translate one requested identity into another exact identity. Kernel does
// not inspect Context, rank branches, score candidates or choose a fallback by
// meaning. No Memory handler means the exact request is executed unchanged.
func (e *Engine) resolveMemoryRunTarget(requested string, f *Frame) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", errors.New("run requires Memory id/tag")
	}
	if f == nil {
		f = newFrame()
	}
	f.Vars["requested_memory_id"] = requested
	if err := e.fireEvent("memory.run.resolve", requested, f); err != nil {
		return "", err
	}
	resolved := strings.TrimSpace(f.Vars["resolved_memory_id"])
	if resolved == "" {
		return requested, nil
	}
	return resolved, nil
}

// runMemoryActivityAfterLiveActivity exposes one completed physical stimulus to
// executable Memory. This replaces the historical fixed Go pipeline
// (remote-evidence -> grounded-action -> context-branch -> growth). The Kernel
// now emits only one exact physical event; Memory structures decide what should
// activate next and can emit further events themselves.
func (e *Engine) runMemoryActivityAfterLiveActivity(kind, subject string, f *Frame) {
	if e == nil || f == nil {
		return
	}
	if f.Vars == nil {
		f.Vars = map[string]string{}
	}
	f.Vars["activity_kind"] = strings.TrimSpace(kind)
	if err := e.fireEvent("memory.activity", strings.TrimSpace(subject), f); err != nil {
		// A post-activity cognitive failure cannot roll back the already completed
		// physical user operation. It remains observable to Memory/the caller.
		f.Vars["__memory_activity_error"] = err.Error()
	}
}

func (e *Engine) handleDaemonRequest(req daemonRequest) daemonResponse {
	if len(req.Args) == 0 {
		return daemonResponse{OK: false, Error: "daemon command required"}
	}
	cmd := strings.ToLower(strings.TrimSpace(req.Args[0]))
	args := req.Args[1:]
	fail := func(err error) daemonResponse {
		return daemonResponse{OK: false, Error: err.Error()}
	}

	switch cmd {
	case "health":
		return daemonResponse{OK: true, Data: map[string]any{
			"version":       imageVersion,
			"role":          e.manifest.Role,
			"memory_count":  fabricMemoryCountFast(e),
			"connections":   atomic.LoadUint64(&daemonConnections),
			"runtime":       globalParallelRuntime.Info(),
			"scheduler":     globalTxnScheduler.Info(),
			"activation":    globalActivationRuntime.Info(),
			"kernel_policy": "physical-only",
		}}
	case "persist":
		if err := e.persistAll(); err != nil {
			return fail(err)
		}
		return daemonResponse{OK: true, Data: map[string]any{"persisted": true}}
	case "run":
		if len(args) < 1 {
			return fail(errors.New("run requires Memory id/tag"))
		}
		f := daemonFrameFromArgs(args[1:])
		targetID, err := e.resolveMemoryRunTarget(args[0], f)
		if err != nil {
			return fail(err)
		}
		if err := globalTxnScheduler.run(e, targetID, f); err != nil {
			return fail(err)
		}
		f.Vars["executed_memory_id"] = targetID
		e.runMemoryActivityAfterLiveActivity("run", targetID, f)
		return daemonResponse{OK: true, Frame: f}
	case "input":
		if len(args) < 1 {
			return fail(errors.New("input requires text"))
		}
		f := newFrame()
		f.Vars["raw_input"] = args[0]
		f.Vars["surface"] = args[0]
		f.Vars["source_id"] = "source.human.input"
		if err := e.fireEvent("input", "", f); err != nil {
			return fail(err)
		}
		e.runMemoryActivityAfterLiveActivity("input", "", f)
		return daemonResponse{OK: true, Frame: f}
	case "event":
		if len(args) < 1 {
			return fail(errors.New("event requires name"))
		}
		subject := ""
		varStart := 1
		if len(args) > 1 && !strings.Contains(args[1], "=") {
			subject = args[1]
			varStart = 2
		}
		f := daemonFrameFromArgs(args[varStart:])
		if err := e.fireEvent(args[0], subject, f); err != nil {
			return fail(err)
		}
		e.runMemoryActivityAfterLiveActivity("event", subject, f)
		return daemonResponse{OK: true, Frame: f}
	case "activate":
		if len(args) < 1 {
			return fail(errors.New("activate requires exact stimulus"))
		}
		pageCap := 0
		if len(args) > 1 {
			pageCap, _ = strconv.Atoi(args[1])
		}
		if err := globalActivationRuntime.Build(e); err != nil {
			return fail(err)
		}
		res, err := globalActivationRuntime.Activate(e, args[0], pageCap)
		if err != nil {
			return fail(err)
		}
		return daemonResponse{OK: true, Data: res}
	case "runtime-info":
		return daemonResponse{OK: true, Data: map[string]any{
			"parallel":    globalParallelRuntime.Info(),
			"scheduler":   globalTxnScheduler.Info(),
			"activation":  globalActivationRuntime.Info(),
			"speculative": speculativeInfo(),
		}}
	default:
		return fail(fmt.Errorf("unsupported daemon command %q", cmd))
	}
}

func (e *Engine) runDaemon(socket string) error {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return errors.New("daemon socket required")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace non-socket path %s", socket)
		}
		_ = os.Remove(socket)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(socket)
	}()
	if err := os.Chmod(socket, 0600); err != nil {
		return err
	}

	idleStop := make(chan struct{})
	go e.runPhysicalIdleTicker(idleStop)
	defer close(idleStop)

	concurrency := make(chan struct{}, daemonMaxConcurrent())
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		atomic.AddUint64(&daemonConnections, 1)
		select {
		case concurrency <- struct{}{}:
			go func(conn net.Conn) {
				defer func() { <-concurrency }()
				e.serveDaemonConn(conn)
			}(c)
		default:
			_ = c.SetDeadline(time.Now().Add(time.Second))
			_ = writeDaemonResponseBounded(c, daemonResponse{
				OK: false, Error: "daemon physical concurrency limit reached",
			})
			_ = c.Close()
		}
	}
}
