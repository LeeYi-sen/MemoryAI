package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func daemonTestRoundTrip(t *testing.T, socket string, args ...string) daemonResponse {
	t.Helper()
	c, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := json.NewEncoder(c).Encode(daemonRequest{Args: args}); err != nil {
		t.Fatal(err)
	}
	var res daemonResponse
	if err := json.NewDecoder(c).Decode(&res); err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDaemonIdleHelperProcess(t *testing.T) {
	if os.Getenv("MEMORYAI_TEST_DAEMON_IDLE_HELPER") != "1" {
		return
	}
	e, err := loadEngineWithMutationJournal(os.Getenv("MEMORYAI_TEST_DAEMON_BODY"))
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	if err := e.runDaemon(os.Getenv("MEMORYAI_TEST_DAEMON_SOCKET")); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonAutonomouslyFiresPhysicalIdle(t *testing.T) {
	dir := t.TempDir()
	body := filepath.Join(dir, "Memory.mem")
	socket := filepath.Join(dir, "memoryai.sock")
	root := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	idle := &Memory{
		ID: "idle.handler", Layer: "emergent", Tags: []string{"memory"}, Trigger: []string{"event:idle"},
		Capabilities: []string{"memory.write"}, State: map[string]any{"ticks": "0"}, Revision: 1,
		Program: []Op{{Code: "state_num_add", A: "idle.handler", B: "ticks", C: "1"}, {Code: "halt"}},
	}
	probe := &Memory{
		ID: "idle.probe", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1,
		Program: []Op{{Code: "state_get", A: "idle.handler", B: "ticks", C: "ticks"}, {Code: "halt"}},
	}
	writeBodyForPersistenceTest(t, body, "core", []*Memory{root, idle, probe})

	cmd := exec.Command(os.Args[0], "-test.run=^TestDaemonIdleHelperProcess$")
	cmd.Env = append(os.Environ(),
		"MEMORYAI_TEST_DAEMON_IDLE_HELPER=1",
		"MEMORYAI_TEST_DAEMON_BODY="+body,
		"MEMORYAI_TEST_DAEMON_SOCKET="+socket,
		"MEMORYAI_IDLE_INTERVAL_MS=20",
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	deadline := time.Now().Add(900 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		res := daemonTestRoundTrip(t, socket, "run", "idle.probe")
		if res.OK && res.Frame != nil {
			ticks, _ := strconv.Atoi(res.Frame.Vars["ticks"])
			if ticks > 0 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("daemon produced no autonomous physical idle event")
}
