package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const storageWireVersion = 1

const (
	defaultStorageConnectionTimeout = 30 * time.Second
	hardStorageConnectionTimeout    = 120 * time.Second
	defaultStorageRequestTimeout    = 5 * time.Second
	defaultStorageMaxConcurrent     = 32
	hardStorageMaxConcurrent        = 256
)

func storageConnectionTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_STORAGE_TIMEOUT_MS"))
	if raw == "" {
		return defaultStorageConnectionTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 1 {
		return defaultStorageConnectionTimeout
	}
	d := time.Duration(ms) * time.Millisecond
	if d > hardStorageConnectionTimeout {
		return hardStorageConnectionTimeout
	}
	return d
}

func storageRequestTimeout(requested time.Duration) time.Duration {
	return clampPhysicalTimeout(requested, defaultStorageRequestTimeout, hardStorageConnectionTimeout)
}

func storageMaxConcurrent() int {
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_STORAGE_MAX_CONCURRENT"))
	if raw == "" {
		return defaultStorageMaxConcurrent
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultStorageMaxConcurrent
	}
	if n > hardStorageMaxConcurrent {
		return hardStorageMaxConcurrent
	}
	return n
}

type storageWireEnvelope struct {
	Version   int             `json:"version"`
	Body      json.RawMessage `json:"body"`
	Signature string          `json:"signature"`
}

func storageTransportKey() []byte {
	if raw := strings.TrimSpace(os.Getenv("MEMORYAI_STORAGE_CAPABILITY_KEY")); raw != "" {
		return []byte(raw)
	}
	return meshTransportKey()
}
func storageTLSFiles() (certFile, keyFile string) {
	return strings.TrimSpace(os.Getenv("MEMORYAI_STORAGE_TLS_CERT")),
		strings.TrimSpace(os.Getenv("MEMORYAI_STORAGE_TLS_KEY"))
}

func storageTLSCAFile() string {
	return strings.TrimSpace(os.Getenv("MEMORYAI_STORAGE_TLS_CA"))
}

func storageTLSServerName(host string) string {
	if name := strings.TrimSpace(os.Getenv("MEMORYAI_STORAGE_TLS_SERVER_NAME")); name != "" {
		return name
	}
	return strings.Trim(strings.TrimSpace(host), "[]")
}

func storageListenUsesTLS(addr string) (bool, error) {
	certFile, keyFile := storageTLSFiles()
	if (certFile == "") != (keyFile == "") {
		return false, errors.New("MEMORYAI_STORAGE_TLS_CERT and MEMORYAI_STORAGE_TLS_KEY must be configured together")
	}
	if certFile != "" {
		return true, nil
	}
	host := meshListenHost(addr)
	if meshHostIsLoopback(host) {
		return false, nil
	}
	return false, errors.New("non-loopback Memory storage listener requires MEMORYAI_STORAGE_TLS_CERT and MEMORYAI_STORAGE_TLS_KEY")
}
func storageListen(addr string) (net.Listener, error) {
	useTLS, err := storageListenUsesTLS(addr)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if !useTLS {
		return ln, nil
	}
	certFile, keyFile := storageTLSFiles()
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	return tls.NewListener(ln, cfg), nil
}

func storageTLSConfig(host string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: storageTLSServerName(host)}
	if caFile := storageTLSCAFile(); caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("MEMORYAI_STORAGE_TLS_CA contains no certificates")
		}
		cfg.RootCAs = roots
	}
	return cfg, nil
}

func storageDial(host, port string, timeout time.Duration) (net.Conn, error) {
	address := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: timeout}
	if meshHostIsLoopback(host) {
		return dialer.Dial("tcp", address)
	}
	cfg, err := storageTLSConfig(host)
	if err != nil {
		return nil, err
	}
	return tls.DialWithDialer(dialer, "tcp", address, cfg)
}

func storageEnvelopeFor(v any) (storageWireEnvelope, error) {
	key := storageTransportKey()
	if len(key) == 0 {
		return storageWireEnvelope{}, errors.New("MEMORYAI_STORAGE_CAPABILITY_KEY or MEMORYAI_MESH_CAPABILITY_KEY required for remote storage transport")
	}
	maxBytes := storageTransportMaxBytes()
	body, err := encodeJSONPhysicalBounded(v, maxBytes, "remote storage body")
	if err != nil {
		return storageWireEnvelope{}, err
	}
	// Encoder.Encode appends one framing newline. RawMessage is normalized by
	// the outer JSON encoder, so sign the stable raw JSON bytes without that
	// transport delimiter or the receiver would verify different bytes.
	body = bytes.TrimSuffix(body, []byte{'\n'})
	return storageWireEnvelope{
		Version:   storageWireVersion,
		Body:      json.RawMessage(body),
		Signature: meshSignBytes(key, body),
	}, nil
}

func storageEnvelopeBody(env storageWireEnvelope) ([]byte, error) {
	if env.Version != storageWireVersion {
		return nil, fmt.Errorf("remote storage wire version %d unsupported", env.Version)
	}
	key := storageTransportKey()
	if len(key) == 0 {
		return nil, errors.New("remote storage transport key unavailable")
	}
	if !meshVerifyBytes(key, env.Body, env.Signature) {
		return nil, errors.New("remote storage transport signature denied")
	}
	return append([]byte(nil), env.Body...), nil
}

func readStorageEnvelope(r io.Reader, dst any) error {
	maxBytes := storageTransportMaxBytes()
	limited := &io.LimitedReader{R: r, N: maxBytes + 1}
	var env storageWireEnvelope
	decodeErr := json.NewDecoder(limited).Decode(&env)
	consumed := maxBytes + 1 - limited.N
	if consumed > maxBytes {
		return fmt.Errorf("remote storage envelope exceeds physical byte ceiling: max=%d", maxBytes)
	}
	if decodeErr != nil {
		return decodeErr
	}
	body, err := storageEnvelopeBody(env)
	if err != nil {
		return err
	}
	if int64(len(body)) > maxBytes {
		return fmt.Errorf("remote storage body exceeds physical byte ceiling: size=%d max=%d", len(body), maxBytes)
	}
	return json.Unmarshal(body, dst)
}

func writeStorageEnvelope(w io.Writer, v any) error {
	env, err := storageEnvelopeFor(v)
	if err != nil {
		return err
	}
	payload, err := encodeJSONPhysicalBounded(env, storageTransportMaxBytes(), "remote storage envelope")
	if err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

func writeStorageReplyBounded(w io.Writer, v any) error {
	err := writeStorageEnvelope(w, v)
	if err == nil {
		return nil
	}
	if !strings.Contains(strings.ToLower(err.Error()), "physical byte ceiling") {
		return err
	}
	return writeStorageEnvelope(w, map[string]any{
		"ok":    false,
		"error": fmt.Sprintf("remote storage response exceeds physical byte ceiling: max=%d", storageTransportMaxBytes()),
	})
}
