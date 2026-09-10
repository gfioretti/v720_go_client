package main

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSelfSignedCert_SANsAndHandshake(t *testing.T) {
	cert, err := selfSignedCert("v720.naxclow.com", "*.naxclow.com", "192.168.178.50")
	if err != nil {
		t.Fatalf("selfSignedCert: %v", err)
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if err := leaf.VerifyHostname("v720.naxclow.com"); err != nil {
		t.Errorf("cert does not cover v720.naxclow.com: %v", err)
	}
	if err := leaf.VerifyHostname("api.naxclow.com"); err != nil {
		t.Errorf("wildcard does not cover api.naxclow.com: %v", err)
	}
	foundIP := false
	for _, ip := range leaf.IPAddresses {
		if ip.String() == "192.168.178.50" {
			foundIP = true
		}
	}
	if !foundIP {
		t.Errorf("cert missing IP SAN 192.168.178.50: %v", leaf.IPAddresses)
	}

	// The cert actually serves TLS.
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("TLS GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}
