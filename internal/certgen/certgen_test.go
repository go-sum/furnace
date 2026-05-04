package certgen

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateCA(t *testing.T) {
	ca, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	if !ca.Cert.IsCA {
		t.Fatal("expected IsCA to be true")
	}
	if ca.Cert.Subject.CommonName != "Furnace Local CA" {
		t.Fatalf("unexpected CN: %q", ca.Cert.Subject.CommonName)
	}
	if ca.Cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatal("expected KeyUsageCertSign")
	}
	if len(ca.CertPEM) == 0 {
		t.Fatal("expected non-empty CertPEM")
	}
	if len(ca.KeyPEM) == 0 {
		t.Fatal("expected non-empty KeyPEM")
	}
	validity := ca.Cert.NotAfter.Sub(ca.Cert.NotBefore)
	expected := 10 * 365 * 24 * time.Hour
	if validity < expected-24*time.Hour || validity > expected+24*time.Hour {
		t.Fatalf("unexpected validity: %v", validity)
	}
}

func TestWriteAndLoadCA(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca-key.pem")

	ca, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	if err := WriteCA(ca, certPath, keyPath); err != nil {
		t.Fatalf("WriteCA: %v", err)
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected key mode 0600, got %04o", info.Mode().Perm())
	}

	loaded, err := LoadCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadCA: %v", err)
	}

	if !loaded.Cert.Equal(ca.Cert) {
		t.Fatal("loaded cert does not match original")
	}
	if loaded.Key.D.Cmp(ca.Key.D) != 0 {
		t.Fatal("loaded key does not match original")
	}
}

func TestGenerateServerCert(t *testing.T) {
	ca, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	domains := []string{"app.example.com", "api.example.com"}
	certPEM, keyPEM, err := GenerateServerCert(ca, domains)
	if err != nil {
		t.Fatalf("GenerateServerCert: %v", err)
	}

	if len(certPEM) == 0 {
		t.Fatal("expected non-empty certPEM")
	}
	if len(keyPEM) == 0 {
		t.Fatal("expected non-empty keyPEM")
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("expected PEM block in certPEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	if cert.IsCA {
		t.Fatal("server cert should not be CA")
	}
	if len(cert.DNSNames) != len(domains) {
		t.Fatalf("expected %d DNS names, got %d", len(domains), len(cert.DNSNames))
	}
	for i, d := range domains {
		if cert.DNSNames[i] != d {
			t.Fatalf("expected DNS name %q, got %q", d, cert.DNSNames[i])
		}
	}

	hasServerAuth := false
	for _, u := range cert.ExtKeyUsage {
		if u == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
	}
	if !hasServerAuth {
		t.Fatal("expected ExtKeyUsageServerAuth")
	}

	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:       pool,
		DNSName:     domains[0],
		CurrentTime: time.Now(),
	}); err != nil {
		t.Fatalf("verify chain: %v", err)
	}

	validity := cert.NotAfter.Sub(cert.NotBefore)
	expected := 2 * 365 * 24 * time.Hour
	if validity < expected-24*time.Hour || validity > expected+24*time.Hour {
		t.Fatalf("unexpected validity: %v", validity)
	}
}

func TestGenerateServerCert_SingleDomain(t *testing.T) {
	ca, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	certPEM, _, err := GenerateServerCert(ca, []string{"single.example.com"})
	if err != nil {
		t.Fatalf("GenerateServerCert: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	cert, _ := x509.ParseCertificate(block.Bytes)
	if cert.Subject.CommonName != "single.example.com" {
		t.Fatalf("expected CN %q, got %q", "single.example.com", cert.Subject.CommonName)
	}
}

func TestLoadCA_NotFound(t *testing.T) {
	_, err := LoadCA("/nonexistent/ca.pem", "/nonexistent/ca-key.pem")
	if err == nil {
		t.Fatal("expected error for missing files")
	}
	if !strings.Contains(err.Error(), "read CA cert") {
		t.Fatalf("expected descriptive error containing 'read CA cert', got: %v", err)
	}
}

func TestGenerateServerCert_EmptyDomains(t *testing.T) {
	ca, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	_, _, err = GenerateServerCert(ca, nil)
	if err == nil {
		t.Fatal("expected error for empty domains")
	}
}
