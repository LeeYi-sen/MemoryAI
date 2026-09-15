package main

import (
	"os"
	"testing"
)

func withMeshTLSEnv(t *testing.T, cert, key string) {
	t.Helper()
	oldCert := os.Getenv("MEMORYAI_MESH_TLS_CERT")
	oldKey := os.Getenv("MEMORYAI_MESH_TLS_KEY")
	if err := os.Setenv("MEMORYAI_MESH_TLS_CERT", cert); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("MEMORYAI_MESH_TLS_KEY", key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("MEMORYAI_MESH_TLS_CERT", oldCert)
		_ = os.Setenv("MEMORYAI_MESH_TLS_KEY", oldKey)
	})
}

func TestMeshEndpointSecurityBoundary(t *testing.T) {
	if err := validateMeshEndpoint("http://127.0.0.1:8787"); err != nil {
		t.Fatalf("loopback HTTP should be allowed: %v", err)
	}
	if err := validateMeshEndpoint("https://node.example:8787"); err != nil {
		t.Fatalf("HTTPS should be allowed: %v", err)
	}
	if err := validateMeshEndpoint("http://192.0.2.10:8787"); err == nil {
		t.Fatal("cross-host plaintext HTTP was accepted")
	}
	if err := validateMeshEndpoint("https://0.0.0.0:8787"); err == nil {
		t.Fatal("unspecified IPv4 address was accepted as advertised endpoint")
	}
	if err := validateMeshEndpoint("https://[::]:8787"); err == nil {
		t.Fatal("unspecified IPv6 address was accepted as advertised endpoint")
	}
}

func TestMeshWildcardListenRequiresAdvertisedEndpoint(t *testing.T) {
	withMeshTLSEnv(t, "/tmp/fake-cert.pem", "/tmp/fake-key.pem")
	if _, err := defaultMeshEndpoint("0.0.0.0:8787"); err == nil {
		t.Fatal("wildcard listener incorrectly produced an advertised endpoint")
	}
	if _, err := defaultMeshEndpoint("[::]:8787"); err == nil {
		t.Fatal("IPv6 wildcard listener incorrectly produced an advertised endpoint")
	}
}

func TestMeshNonLoopbackListenRequiresTLS(t *testing.T) {
	withMeshTLSEnv(t, "", "")
	if _, err := meshListenUsesTLS("192.0.2.10:8787"); err == nil {
		t.Fatal("non-loopback listener was allowed without TLS")
	}
	if useTLS, err := meshListenUsesTLS("127.0.0.1:8787"); err != nil || useTLS {
		t.Fatalf("loopback listener should allow plaintext: useTLS=%v err=%v", useTLS, err)
	}
}
