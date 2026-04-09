/*
Copyright 2026 The Butler Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestCertHealthStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		notAfter time.Time
		want     string
	}{
		{"expired yesterday", now.Add(-24 * time.Hour), "Expired"},
		{"expired 1 second ago", now.Add(-1 * time.Second), "Expired"},
		{"expires in 1 day", now.Add(24 * time.Hour), "Critical"},
		{"expires in 6 days", now.Add(6 * 24 * time.Hour), "Critical"},
		{"expires in exactly 7 days", now.Add(7 * 24 * time.Hour), "Critical"},
		{"expires in 8 days", now.Add(8 * 24 * time.Hour), "Warning"},
		{"expires in 15 days", now.Add(15 * 24 * time.Hour), "Warning"},
		{"expires in 29 days", now.Add(29 * 24 * time.Hour), "Warning"},
		{"expires in exactly 30 days", now.Add(30 * 24 * time.Hour), "Warning"},
		{"expires in 31 days", now.Add(31 * 24 * time.Hour), "Healthy"},
		{"expires in 365 days", now.Add(365 * 24 * time.Hour), "Healthy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := certHealthStatus(now, tt.notAfter)
			if got != tt.want {
				t.Errorf("certHealthStatus(%v) = %q, want %q", tt.notAfter.Sub(now), got, tt.want)
			}
		})
	}
}

func TestParseCertificates(t *testing.T) {
	// Generate a self-signed cert for testing
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-cert"},
		Issuer:       pkix.Name{CommonName: "test-issuer"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	t.Run("valid PEM", func(t *testing.T) {
		certs := parseCertificates(certPEM)
		if len(certs) != 1 {
			t.Fatalf("got %d certs, want 1", len(certs))
		}
		if certs[0].Subject.CommonName != "test-cert" {
			t.Errorf("subject CN = %q, want %q", certs[0].Subject.CommonName, "test-cert")
		}
	})

	t.Run("invalid data", func(t *testing.T) {
		certs := parseCertificates([]byte("not a cert"))
		if len(certs) != 0 {
			t.Errorf("got %d certs from invalid data, want 0", len(certs))
		}
	})

	t.Run("empty data", func(t *testing.T) {
		certs := parseCertificates(nil)
		if len(certs) != 0 {
			t.Errorf("got %d certs from nil, want 0", len(certs))
		}
	})

	t.Run("multiple certs in bundle", func(t *testing.T) {
		bundle := append(certPEM, certPEM...)
		certs := parseCertificates(bundle)
		if len(certs) != 2 {
			t.Errorf("got %d certs from bundle, want 2", len(certs))
		}
	})
}
