package webhook

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// Generates test/development certificates onto temporary location on disk
func GenerateLocalCertificates() (CertInfo, error) {
	caPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return CertInfo{}, fmt.Errorf("failed to generate ca private key: %w", err)
	}
	serverPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return CertInfo{}, fmt.Errorf("failed to generate server private key: %w", err)
	}

	caRandBytes := make([]byte, 16)
	_, err = rand.Read(caRandBytes)
	if err != nil {
		return CertInfo{}, fmt.Errorf("failed to generate ca random serial: %w", err)
	}

	serverRandBytes := make([]byte, 16)
	_, err = rand.Read(serverRandBytes)
	if err != nil {
		return CertInfo{}, fmt.Errorf("failed to generate server random serial: %w", err)
	}

	caTemplate := x509.Certificate{
		IsCA: true,
		Subject: pkix.Name{
			Organization: []string{"Company"},
			CommonName:   "root",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		SerialNumber:          new(big.Int).SetBytes(caRandBytes),
	}

	serverTemplate := x509.Certificate{
		Subject: pkix.Name{
			Organization: []string{"Company"},
			CommonName:   "server",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses: []net.IP{
			net.ParseIP("0.0.0.0"),
			net.ParseIP("127.0.0.1"),
		},
		DNSNames: []string{
			"localhost",
		},
		SerialNumber: new(big.Int).SetBytes(serverRandBytes),
	}

	caDer, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		return CertInfo{}, fmt.Errorf("failed to create ca certificate: %w", err)
	}

	serverDer, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		return CertInfo{}, fmt.Errorf("failed to generate ca random serial: %w", err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: caDer}); err != nil {
		return CertInfo{}, fmt.Errorf("failed to encode ca pem: %w", err)
	}
	caPEM := buf.Bytes()
	buf.Reset()

	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: serverDer}); err != nil {
		return CertInfo{}, fmt.Errorf("failed to encode server pem: %w", err)
	}
	serverPEM := buf.Bytes()

	// Don't care about preserving root CA key
	// caEC, err := x509.MarshalECPrivateKey(caPrivateKey)
	// if err != nil {
	// 	return CertInfo{}, fmt.Errorf("failed to marshal ca private key %v", err)
	// }
	// caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caEC})
	// if caKeyPEM == nil {
	// 	return CertInfo{}, fmt.Errorf("failed to PEM-encode ca private key")
	// }

	serverEC, err := x509.MarshalECPrivateKey(serverPrivateKey)
	if err != nil {
		return CertInfo{}, fmt.Errorf("failed to marshal server private key %v", err)
	}
	if err := pem.Encode(&buf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: serverEC}); err != nil {
		return CertInfo{}, fmt.Errorf("failed to encode server pem: %w", err)
	}
	serverKeyPEM := buf.Bytes()

	// Verify that the certificate is signed by the CA.
	caCert, err := x509.ParseCertificate(caDer)
	if err != nil {
		panic(err)
	}
	cert, err := x509.ParseCertificate(serverDer)
	if err != nil {
		panic(err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	opts := x509.VerifyOptions{
		Roots: roots,
	}

	if _, err := cert.Verify(opts); err != nil {
		return CertInfo{}, fmt.Errorf("failed to verify generated cert: %w", err)
	}

	return CertInfo{
		Root: caPEM,
		Cert: serverPEM,
		Key:  serverKeyPEM,
	}, nil
}
