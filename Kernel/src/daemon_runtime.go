package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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

func runDaemonClient(socket string, args []string) error {
	if strings.TrimSpace(socket) == "" {
		return errors.New("daemon socket required")
	}
	c, err := net.Dial("unix", socket)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := json.NewEncoder(c).Encode(daemonRequest{Args: args}); err != nil {
		return err
	}
	var res daemonResponse
	if err := json.NewDecoder(c).Decode(&res); err != nil {
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

// runMemoryGrowthAfterLiveActivity 把真实 live 活动与 Grounding + Context Split + Memory Growth 闭环连接起来。
// 每个完成的 run/input/event 最多机会式推进一个 Memory 明确授权的外部动作、一个上下文裂分步骤
// 和一个既有内部生长步骤；这里仍然没有独立后台 cognitive scheduler。
func (e *Engine) runMemoryGrowthAfterLiveActivity(f *Frame) {
	if e == nil {
		return
	}
	_, groundedErr := RunAutonomousGroundedActionCycle(e)
	_, branchErr := RunAutonomousContextBranchingCycle(e)
	_, growthErr := RunAutonomousMemoryGrowthCycle(e)
	if f == nil || (groundedErr == nil && branchErr == nil && growthErr == nil) {
		return
	}
	if f.Vars == nil {
		f.Vars = map[string]string{}
	}
	// 外部动作/裂分/生长失败不回滚已经完成的用户事件；把物理故障暴露给 Memory/调用方观察。
	if groundedErr != nil {
		f.Vars["__memory_grounded_error"] = groundedErr.Error()
	}
	if branchErr != nil {
		f.Vars["__memory_branching_error"] = branchErr.Error()
	}
	if growthErr != nil {
		f.Vars["__memory_growth_error"] = growthErr.Error()
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
		if err := globalTxnScheduler.run(e, args[0], f); err != nil {
			return fail(err)
		}
		e.runMemoryGrowthAfterLiveActivity(f)
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
		e.runMemoryGrowthAfterLiveActivity(f)
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
		e.runMemoryGrowthAfterLiveActivity(f)
		return daemonResponse{OK: true, Frame: f}
	case "activate":
		if len(args) < 1 {
			return fail(errors.New("activate requires exact stimulus"))
		}
		topK := 0
		if len(args) > 1 {
			topK, _ = strconv.Atoi(args[1])
		}
		if err := globalActivationRuntime.Build(e); err != nil {
			return fail(err)
		}
		res, err := globalActivationRuntime.Activate(e, args[0], topK)
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

	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		atomic.AddUint64(&daemonConnections, 1)
		go func(conn net.Conn) {
			defer conn.Close()
			dec := json.NewDecoder(bufio.NewReader(conn))
			enc := json.NewEncoder(conn)
			var req daemonRequest
			if err := dec.Decode(&req); err != nil {
				_ = enc.Encode(daemonResponse{OK: false, Error: err.Error()})
				return
			}
			_ = enc.Encode(e.handleDaemonRequest(req))
		}(c)
	}
}
