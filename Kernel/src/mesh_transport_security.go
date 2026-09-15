package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

func meshTLSFiles() (certFile, keyFile string) {
	return strings.TrimSpace(os.Getenv("MEMORYAI_MESH_TLS_CERT")), strings.TrimSpace(os.Getenv("MEMORYAI_MESH_TLS_KEY"))
}

func meshHostIsLoopback(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func meshHostIsUnspecified(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

func meshListenHost(addr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(strings.TrimSpace(addr), "[]")
}

// validateMeshEndpoint prevents Sovereign grants and channel MACs from crossing
// an unauthenticated plaintext network. HTTP is only accepted on loopback. An
// unspecified bind address (0.0.0.0/::) is never a valid advertised endpoint.
func validateMeshEndpoint(endpoint string) error {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return fmt.Errorf("invalid mesh endpoint: %w", err)
	}
	if u.Host == "" {
		return errors.New("mesh endpoint host required")
	}
	host := u.Hostname()
	if meshHostIsUnspecified(host) {
		return errors.New("mesh endpoint cannot advertise an unspecified bind address; configure MEMORYAI_MESH_ENDPOINT with a routable host")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if meshHostIsLoopback(host) {
			return nil
		}
		return errors.New("plaintext mesh HTTP is restricted to loopback; configure HTTPS for cross-host mesh")
	default:
		return fmt.Errorf("mesh endpoint scheme %q denied; use https (or loopback http)", u.Scheme)
	}
}

func meshListenUsesTLS(addr string) (bool, error) {
	certFile, keyFile := meshTLSFiles()
	if (certFile == "") != (keyFile == "") {
		return false, errors.New("MEMORYAI_MESH_TLS_CERT and MEMORYAI_MESH_TLS_KEY must be configured together")
	}
	if certFile != "" {
		return true, nil
	}
	host := meshListenHost(addr)
	if meshHostIsLoopback(host) {
		return false, nil
	}
	return false, errors.New("non-loopback mesh listener requires MEMORYAI_MESH_TLS_CERT and MEMORYAI_MESH_TLS_KEY")
}

func defaultMeshEndpoint(listen string) (string, error) {
	if strings.TrimSpace(listen) == "" {
		return "", nil
	}
	host := meshListenHost(listen)
	if meshHostIsUnspecified(host) {
		return "", errors.New("wildcard mesh listener requires explicit MEMORYAI_MESH_ENDPOINT with a routable host")
	}
	useTLS, err := meshListenUsesTLS(listen)
	if err != nil {
		return "", err
	}
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	return scheme + "://" + strings.TrimSpace(listen), nil
}
