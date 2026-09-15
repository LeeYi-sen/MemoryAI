package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const sourceAdapterTag = "kernel.physical.source-adapter"

type sourceAdapter struct {
	ID     string
	Config map[string]string
}

func kvArgs(args []string) map[string]string {
	out := map[string]string{}
	for _, arg := range args {
		k, v, ok := strings.Cut(arg, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

func sourceAdapterFromVars(v map[string]string) sourceAdapter {
	cfg := map[string]string{}
	for k, value := range v {
		if k != "id" {
			cfg[k] = value
		}
	}
	return sourceAdapter{ID: strings.TrimSpace(v["id"]), Config: cfg}
}

func sourceParseTimeout(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 15 * time.Second
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 15 * time.Second
}

func sourceAdapterMemory(a sourceAdapter, revision uint64) *Memory {
	state := map[string]any{}
	for k, v := range a.Config {
		state[k] = v
	}
	return &Memory{
		ID:         a.ID,
		Layer:      "inherited",
		Generation: 0,
		Tags:       []string{"memory", sourceAdapterTag},
		Content:    "Physical external source adapter configuration.",
		Revision:   revision,
		State:      state,
	}
}

func (e *Engine) upsertSourceAdapter(a sourceAdapter) (*Memory, error) {
	if a.ID == "" {
		return nil, errors.New("source adapter id required")
	}
	revision := uint64(1)
	_, old, err := e.resolveLocalFabricMemory(a.ID)
	if err == nil && old != nil {
		if !memoryHasTag(old, sourceAdapterTag) {
			return nil, fmt.Errorf("Memory id %s is not a source adapter", a.ID)
		}
		revision = old.Revision + 1
	} else if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	m := sourceAdapterMemory(a, revision)
	if err := e.upsertExplicitMemoryBounded(m); err != nil {
		return nil, err
	}
	return copyMemory(m), nil
}

func (e *Engine) deleteSourceAdapter(id string) error {
	_, m, err := e.resolveLocalFabricMemory(strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if !memoryHasTag(m, sourceAdapterTag) {
		return fmt.Errorf("Memory id %s is not a source adapter", id)
	}
	return e.deleteExplicitMemoryBounded(m.ID)
}

func (e *Engine) listSourceAdapters() []map[string]any {
	ids, err := e.listTagFabric(sourceAdapterTag)
	if err != nil {
		return []map[string]any{{"error": err.Error()}}
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		_, m, err := e.resolveLocalFabricMemory(id)
		if err != nil || m == nil {
			continue
		}
		state := map[string]any{}
		for k, v := range m.State {
			state[k] = v
		}
		out = append(out, map[string]any{
			"id":       m.ID,
			"revision": m.Revision,
			"config":   state,
		})
	}
	return out
}

func adapterStateString(m *Memory, key string) string {
	if m == nil || m.State == nil {
		return ""
	}
	if v, ok := m.State[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// executeSourceAdapter 是 source adapter 的物理 I/O 原语。
// Memory Growth / Grounding 复用这里已有的 HTTP/TCP/TLS 边界，不创建第二套网络执行器。
func (e *Engine) executeSourceAdapter(id string, timeout time.Duration) (map[string]any, error) {
	_, m, err := e.resolveLocalFabricMemory(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if !memoryHasTag(m, sourceAdapterTag) {
		return nil, fmt.Errorf("Memory id %s is not a source adapter", id)
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	transport := strings.ToLower(strings.TrimSpace(adapterStateString(m, "transport")))
	if transport == "tcp" || transport == "tls" {
		resp, err := physicalExchange(
			transport,
			adapterStateString(m, "host"),
			adapterStateString(m, "port"),
			adapterStateString(m, "request"),
			timeout,
		)
		result := map[string]any{"id": m.ID, "transport": transport, "ok": err == nil}
		if err != nil {
			result["error"] = err.Error()
			return result, err
		}
		result["response"] = resp
		return result, nil
	}

	u := strings.TrimSpace(adapterStateString(m, "url"))
	if u == "" {
		return nil, errors.New("source adapter requires url or tcp/tls host+port")
	}
	method := strings.ToUpper(strings.TrimSpace(adapterStateString(m, "method")))
	if method == "" {
		method = http.MethodGet
	}
	body := adapterStateString(m, "body")
	req, err := http.NewRequest(method, u, bytes.NewBufferString(body))
	if err != nil {
		return nil, err
	}
	if rawHeaders := strings.TrimSpace(adapterStateString(m, "headers")); rawHeaders != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(rawHeaders), &headers); err != nil {
			return nil, fmt.Errorf("source adapter headers must be JSON object: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}
	client := &http.Client{Timeout: timeout}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"id": m.ID, "transport": "http", "ok": false, "error": err.Error()}, err
	}
	defer resp.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, readErr
	}
	result := map[string]any{
		"id":          m.ID,
		"transport":   "http",
		"ok":          resp.StatusCode >= 200 && resp.StatusCode < 400,
		"status":      resp.Status,
		"status_code": resp.StatusCode,
		"elapsed_ms":  time.Since(started).Milliseconds(),
		"bytes":       len(payload),
		"response":    string(payload),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return result, fmt.Errorf("source adapter returned HTTP %d", resp.StatusCode)
	}
	return result, nil
}

// testSourceAdapter 保留既有管理/诊断入口；真实执行与诊断现在共享同一个物理 I/O 原语。
func (e *Engine) testSourceAdapter(id string, timeout time.Duration) (map[string]any, error) {
	return e.executeSourceAdapter(id, timeout)
}
