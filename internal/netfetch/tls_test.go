package netfetch

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Real TLS/HTTP over in-memory net.Pipe (no DNS, listeners or sockets).
// Only tests add a temporary root; production retains normal system trust.
// TLS 1.2 avoids unbuffered net.Pipe TLS 1.3 simultaneous alert/flight writes
// deadlocking on a rejected certificate; production still supports TLS 1.3.
func TestNativeTLSOriginalHostname(t *testing.T) {
	for _, certHost := range []string{"www.recipetineats.com", "evil.example"} {
		t.Run(certHost, func(t *testing.T) {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: certHost}, DNSNames: []string{certHost}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				t.Fatal(err)
			}
			roots := x509.NewCertPool()
			roots.AddCert(cert)
			u, _ := url.Parse(recipeURL)
			serverResult := make(chan error, 1)
			sni := make(chan string, 1)
			transport := pinnedTransport(u, public, func(_ context.Context, network, address string) (net.Conn, error) {
				if network != "tcp" || address != "93.184.216.34:443" {
					t.Errorf("unpinned dial %s %s", network, address)
				}
				client, server := net.Pipe()
				go func() {
					conn := tls.Server(server, &tls.Config{MaxVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) { sni <- hello.ServerName; return nil, nil }})
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(3 * time.Second))
					if err := conn.Handshake(); err != nil {
						serverResult <- err
						return
					}
					req, err := http.ReadRequest(bufio.NewReader(conn))
					if err != nil {
						serverResult <- err
						return
					}
					req.Body.Close()
					if req.Host != "www.recipetineats.com" {
						serverResult <- fmt.Errorf("wrong Host %s", req.Host)
						return
					}
					_, err = fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
					serverResult <- err
				}()
				return client, nil
			})
			transport.TLSClientConfig.RootCAs = roots // test-only trust, never InsecureSkipVerify
			defer transport.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", recipeURL, nil)
			res, err := transport.RoundTrip(req)
			if certHost == "evil.example" {
				if err == nil {
					if res != nil {
						res.Body.Close()
					}
					t.Fatal("trusted wrong-host certificate accepted")
				}
				if !strings.Contains(err.Error(), "certificate") {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				b, err := io.ReadAll(res.Body)
				res.Body.Close()
				if err != nil || string(b) != "ok" {
					t.Fatal(string(b), err)
				}
			}
			select {
			case name := <-sni:
				if name != "www.recipetineats.com" {
					t.Fatal("lost original TLS hostname", name)
				}
			case <-ctx.Done():
				t.Fatal("missing TLS ClientHello")
			}
			select {
			case serverErr := <-serverResult:
				if certHost != "evil.example" && serverErr != nil {
					t.Fatal(serverErr)
				}
			case <-ctx.Done():
				t.Fatal("server not closed")
			}
		})
	}
}
