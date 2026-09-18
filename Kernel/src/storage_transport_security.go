package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

const storageWireVersion = 1

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
	body, err := json.Marshal(v)
	if err != nil {
		return storageWireEnvelope{}, err
	}
	return storageWireEnvelope{
		Version:   storageWireVersion,
		Body:      body,
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
	var env storageWireEnvelope
	if err := json.NewDecoder(io.LimitReader(r, 4<<20)).Decode(&env); err != nil {
		return err
	}
	body, err := storageEnvelopeBody(env)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

func writeStorageEnvelope(w io.Writer, v any) error {
	env, err := storageEnvelopeFor(v)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(env)
}
