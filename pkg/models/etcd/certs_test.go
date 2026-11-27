// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

type testPKI struct {
	dir string

	caFile string

	serverCertFile string
	serverKeyFile  string

	clientCertFile string
	clientKeyFile  string

	rogueCAFile         string
	rogueClientCertFile string
	rogueClientKeyFile  string
}

func newSerial(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(1).Lsh(big.NewInt(1), 62))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	return n
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func newCA(t *testing.T, cn string) *testCA {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          newSerial(t),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	return &testCA{cert: cert, key: key, certPEM: pemCert(der)}
}

func (ca *testCA) newLeaf(t *testing.T, cn string, usage x509.ExtKeyUsage, ips []net.IP) (certPEM, keyPEM []byte) {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: newSerial(t),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return pemCert(der), keyPEM
}

func pemCert(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// newTestPKI creates a CA, a server certificate (SAN 127.0.0.1), a client
// certificate and a rogue CA with its own client certificate.
func newTestPKI(t *testing.T) *testPKI {
	t.Helper()
	dir := t.TempDir()
	ca := newCA(t, "codis-test-ca")
	rogue := newCA(t, "codis-test-rogue-ca")

	serverCert, serverKey := ca.newLeaf(t, "etcd-server", x509.ExtKeyUsageServerAuth, []net.IP{net.ParseIP("127.0.0.1")})
	clientCert, clientKey := ca.newLeaf(t, "etcd-client", x509.ExtKeyUsageClientAuth, nil)
	rogueClientCert, rogueClientKey := rogue.newLeaf(t, "etcd-rogue-client", x509.ExtKeyUsageClientAuth, nil)

	return &testPKI{
		dir:                 dir,
		caFile:              writeFile(t, dir, "ca.pem", ca.certPEM),
		serverCertFile:      writeFile(t, dir, "server.pem", serverCert),
		serverKeyFile:       writeFile(t, dir, "server-key.pem", serverKey),
		clientCertFile:      writeFile(t, dir, "client.pem", clientCert),
		clientKeyFile:       writeFile(t, dir, "client-key.pem", clientKey),
		rogueCAFile:         writeFile(t, dir, "rogue-ca.pem", rogue.certPEM),
		rogueClientCertFile: writeFile(t, dir, "rogue-client.pem", rogueClientCert),
		rogueClientKeyFile:  writeFile(t, dir, "rogue-client-key.pem", rogueClientKey),
	}
}
