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

const (
	sourceAdapterEnabledTag         = "source.adapter.enabled"
	sourceAdapterActionTag          = "source.adapter.action"
	externalRequestPendingTag       = "io.external.request.pending"
	externalRequestExecutingTag     = "io.external.request.executing"
	externalRequestResponseReadyTag = "io.external.request.response-ready"
	externalRequestDoneTag          = "io.external.request.done"
	externalRequestAmbiguousTag     = "io.external.request.ambiguous"
	externalRequestFailedTag        = "io.external.request.failed"
)

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

func sourceConfigEnabled(cfg map[string]string) bool {
	raw, ok := cfg["enabled"]
	if !ok || strings.TrimSpace(raw) == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func sourceConfigAction(cfg map[string]string) bool {
	role := strings.ToLower(strings.TrimSpace(cfg["role"]))
	kind := strings.ToLower(strings.TrimSpace(cfg["kind"]))
	return role == "action" || kind == "action"
}

func sourceAdapterMemory(a sourceAdapter, revision uint64) *Memory {
	state := map[string]any{}
	for k, v := range a.Config {
		state[k] = v
	}
	if _, ok := state["enabled"]; !ok {
		state["enabled"] = "1"
	}
	tags := []string{"memory", sourceAdapterTag}
	if sourceConfigEnabled(a.Config) {
		tags = append(tags, sourceAdapterEnabledTag)
	}
	if sourceConfigAction(a.Config) {
		tags = append(tags, sourceAdapterActionTag)
	}
	return &Memory{
		ID:         a.ID,
		Layer:      "inherited",
		Generation: 0,
		Tags:       tags,
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
func (e *Engine) executeSourceAdapterInput(id, input string, timeout time.Duration) (map[string]any, error) {
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
			sourceAdapterTemplate(adapterStateString(m, "request"), input),
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
	body := sourceAdapterTemplate(adapterStateString(m, "body"), input)
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

func sourceAdapterTemplate(raw, input string) string {
	encoded, _ := json.Marshal(input)
	raw = strings.ReplaceAll(raw, "{{input_json}}", string(encoded))
	return strings.ReplaceAll(raw, "{{input}}", input)
}

func (e *Engine) executeSourceAdapter(id string, timeout time.Duration) (map[string]any, error) {
	return e.executeSourceAdapterInput(id, "", timeout)
}

func externalRequestScalarVars(m *Memory) map[string]string {
	out := map[string]string{}
	if m == nil {
		return out
	}
	for k, value := range m.State {
		switch v := value.(type) {
		case string:
			out[k] = v
		case bool:
			out[k] = strconv.FormatBool(v)
		case float64:
			out[k] = strconv.FormatFloat(v, 'f', -1, 64)
		case int:
			out[k] = strconv.Itoa(v)
		case int64:
			out[k] = strconv.FormatInt(v, 10)
		case uint64:
			out[k] = strconv.FormatUint(v, 10)
		}
	}
	return out
}

func replaceExternalRequestLifecycle(m *Memory, status, tag string, updates map[string]any) *Memory {
	q := copyMemory(m)
	if q.State == nil {
		q.State = map[string]any{}
	}
	q.State["io.status"] = status
	for k, v := range updates {
		q.State[k] = v
	}
	lifecycle := map[string]bool{
		externalRequestPendingTag: true, externalRequestExecutingTag: true,
		externalRequestResponseReadyTag: true, externalRequestAmbiguousTag: true,
		externalRequestFailedTag: true, externalRequestDoneTag: true,
	}
	out := q.Tags[:0]
	for _, existing := range q.Tags {
		if !lifecycle[existing] {
			out = append(out, existing)
		}
	}
	q.Tags = out
	if tag != "" && !contains(q.Tags, tag) {
		q.Tags = append(q.Tags, tag)
	}
	if (status == "response-ready" || status == "done") && !contains(q.Tags, externalRequestDoneTag) {
		q.Tags = append(q.Tags, externalRequestDoneTag)
	}
	q.CapabilitySig = ""
	q.Revision++
	return q
}

func (e *Engine) persistExternalRequest(q *Memory) error {
	if err := e.upsertExplicitMemoryBounded(q); err != nil {
		return err
	}
	return fabricRootFor(e).persistAll()
}

func (e *Engine) deliverExternalResponse(request *Memory) error {
	if request == nil {
		return errors.New("external response request missing")
	}
	eventName := strings.TrimSpace(adapterStateString(request, "response_event"))
	if eventName == "" {
		return fmt.Errorf("external request %s missing response_event", request.ID)
	}
	f := newFrame()
	for k, v := range externalRequestScalarVars(request) {
		f.Vars[k] = v
	}
	return e.fireEvent(eventName, request.ID, f)
}

func (e *Engine) processResponseReadyExternalRequests() error {
	ids, err := e.listTagFabric(externalRequestResponseReadyTag)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	sort.Strings(ids)
	var errs []error
	for _, id := range ids {
		_, request, er := e.resolveLocalFabricMemory(id)
		if er != nil || request == nil {
			if er != nil {
				errs = append(errs, er)
			}
			continue
		}
		if er = e.deliverExternalResponse(request); er != nil {
			errs = append(errs, fmt.Errorf("deliver external response %s: %w", id, er))
			continue
		}
		done := replaceExternalRequestLifecycle(request, "done", "", nil)
		if er = e.persistExternalRequest(done); er != nil {
			errs = append(errs, fmt.Errorf("persist external done %s: %w", id, er))
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) quarantineAmbiguousExternalRequests() error {
	ids, err := e.listTagFabric(externalRequestExecutingTag)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	sort.Strings(ids)
	var errs []error
	for _, id := range ids {
		_, request, er := e.resolveLocalFabricMemory(id)
		if er != nil || request == nil {
			if er != nil {
				errs = append(errs, er)
			}
			continue
		}
		q := replaceExternalRequestLifecycle(request, "ambiguous", externalRequestAmbiguousTag, map[string]any{
			"io.error": "external execution state ambiguous after restart; automatic replay refused",
		})
		if er = e.persistExternalRequest(q); er != nil {
			errs = append(errs, er)
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) executePendingExternalRequest(request *Memory) error {
	adapterID := strings.TrimSpace(adapterStateString(request, "source_adapter_id"))
	if adapterID == "" {
		failed := replaceExternalRequestLifecycle(request, "failed", externalRequestFailedTag, map[string]any{"io.error": "source_adapter_id required"})
		return e.persistExternalRequest(failed)
	}
	if strings.TrimSpace(adapterStateString(request, "response_event")) == "" {
		failed := replaceExternalRequestLifecycle(request, "failed", externalRequestFailedTag, map[string]any{"io.error": "response_event required"})
		return e.persistExternalRequest(failed)
	}

	executing := replaceExternalRequestLifecycle(request, "executing", externalRequestExecutingTag, nil)
	if err := e.persistExternalRequest(executing); err != nil {
		return err
	}
	started := time.Now()
	result, runErr := e.executeSourceAdapterInput(adapterID, adapterStateString(executing, "stimulus_surface"), sourceParseTimeout(adapterStateString(executing, "timeout")))
	elapsedUS := time.Since(started).Microseconds()
	if runErr != nil {
		failed := replaceExternalRequestLifecycle(executing, "failed", externalRequestFailedTag, map[string]any{
			"io.error": runErr.Error(), "elapsed_us": strconv.FormatInt(elapsedUS, 10),
		})
		if result != nil {
			if raw, err := json.Marshal(result); err == nil {
				failed.State["adapter_result"] = string(raw)
			}
		}
		if err := e.persistExternalRequest(failed); err != nil {
			return errors.Join(runErr, err)
		}
		return runErr
	}
	normalized := fmt.Sprint(result["response"])
	rawResult, _ := json.Marshal(result)
	ready := replaceExternalRequestLifecycle(executing, "response-ready", externalRequestResponseReadyTag, map[string]any{
		"normalized_payload": normalized,
		"elapsed_us":         strconv.FormatInt(elapsedUS, 10),
		"adapter_result":     string(rawResult),
	})
	if adapterStateString(ready, "source_id") == "" {
		ready.State["source_id"] = adapterID
	}
	if err := e.persistExternalRequest(ready); err != nil {
		return err
	}
	if err := e.deliverExternalResponse(ready); err != nil {
		return err
	}
	done := replaceExternalRequestLifecycle(ready, "done", "", nil)
	return e.persistExternalRequest(done)
}

func (e *Engine) processExternalIORequests() error {
	root := fabricRootFor(e)
	if root == nil {
		root = e
	}
	root.externalIOMu.Lock()
	defer root.externalIOMu.Unlock()

	var errs []error
	if err := root.quarantineAmbiguousExternalRequests(); err != nil {
		errs = append(errs, err)
	}
	if err := root.processResponseReadyExternalRequests(); err != nil {
		errs = append(errs, err)
	}
	ids, err := root.listTagFabric(externalRequestPendingTag)
	if err != nil && !errors.Is(err, io.EOF) {
		errs = append(errs, err)
		return errors.Join(errs...)
	}
	sort.Strings(ids)
	for _, id := range ids {
		_, request, er := root.resolveLocalFabricMemory(id)
		if er != nil || request == nil {
			if er != nil {
				errs = append(errs, er)
			}
			continue
		}
		if er = root.executePendingExternalRequest(request); er != nil {
			errs = append(errs, fmt.Errorf("external request %s: %w", id, er))
		}
	}
	return errors.Join(errs...)
}

// testSourceAdapter 保留既有管理/诊断入口；真实执行与诊断现在共享同一个物理 I/O 原语。
func (e *Engine) testSourceAdapter(id string, timeout time.Duration) (map[string]any, error) {
	return e.executeSourceAdapter(id, timeout)
}
