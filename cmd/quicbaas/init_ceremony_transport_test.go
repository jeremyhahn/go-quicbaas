// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

const (
	ceremonyTransportSOPIN   = "env-so-pin-4711"
	ceremonyTransportUserPIN = "env-user-pin-4712"
)

// pinCounter counts the ceremony requests a test server receives, and every
// request that carries the SO PIN in a header or the body. Requests outside
// the ceremony routes (the SDK's connection probe) carry no PIN.
type pinCounter struct {
	ceremony atomic.Int64
	withPIN  atomic.Int64
}

func (c *pinCounter) observe(r *http.Request) []byte {
	if strings.HasPrefix(r.URL.Path, "/v1/ceremony/") {
		c.ceremony.Add(1)
	}
	body, _ := io.ReadAll(r.Body)
	if bytes.Contains(body, []byte(ceremonyTransportSOPIN)) {
		c.withPIN.Add(1)
		return body
	}
	for _, vals := range r.Header {
		for _, v := range vals {
			if strings.Contains(v, ceremonyTransportSOPIN) {
				c.withPIN.Add(1)
				return body
			}
		}
	}
	return body
}

// useRESTCeremonyClient points ceremonyClientFactory at a real SDK REST
// ceremony client for addr, trusting the given servers' certificates.
func useRESTCeremonyClient(t *testing.T, addr string, trusted ...*httptest.Server) {
	t.Helper()
	opts := []quicbaas.Option{quicbaas.WithREST()}
	if len(trusted) > 0 {
		pool := x509.NewCertPool()
		for _, srv := range trusted {
			pool.AddCert(srv.Certificate())
		}
		opts = append(opts, quicbaas.WithTLSConfig(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}))
	}
	orig := ceremonyClientFactory
	ceremonyClientFactory = func() (CeremonyClient, error) {
		sdk, err := quicbaas.NewClient(addr, opts...)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = sdk.Close() })
		return sdk.Security().Ceremony()
	}
	t.Cleanup(func() { ceremonyClientFactory = orig })
}

// setCeremonyPINEnv supplies both PINs from the environment, with no PIN flag.
func setCeremonyPINEnv(t *testing.T) {
	t.Helper()
	t.Setenv(soPINEnv, ceremonyTransportSOPIN)
	t.Setenv(userPINEnv, ceremonyTransportUserPIN)
}

// ceremonyCommands returns the two ceremony commands that carry the SO PIN,
// each ready to run with PINs from the environment.
func ceremonyCommands(t *testing.T) map[string]func() *cobra.Command {
	t.Helper()
	csrPath := filepath.Join(t.TempDir(), "alice.csr")
	require.NoError(t, os.WriteFile(csrPath,
		[]byte("-----BEGIN CERTIFICATE REQUEST-----\nx\n-----END CERTIFICATE REQUEST-----\n"), 0o600))
	return map[string]func() *cobra.Command{
		"initialize": func() *cobra.Command {
			resetInitCeremonyFlags()
			cmd := newInitInitializeCommand()
			cmd.SetArgs([]string{})
			return cmd
		},
		"sign-csr": func() *cobra.Command {
			resetInitFlags()
			cmd := newInitSignCSRCommand()
			cmd.SetArgs([]string{"--username", "alice", "--role", "officer", "--csr", csrPath})
			return cmd
		},
	}
}

func runCeremonyCommand(t *testing.T, build func() *cobra.Command) error {
	t.Helper()
	t.Cleanup(resetInitCeremonyFlags)
	t.Cleanup(resetInitFlags)
	cmd := build()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

// TestCeremonyTransport_PlaintextGetsNoPIN: with PINs from the environment,
// neither ceremony command sends anything to a plain http:// server.
func TestCeremonyTransport_PlaintextGetsNoPIN(t *testing.T) {
	for name, build := range ceremonyCommands(t) {
		t.Run(name, func(t *testing.T) {
			setCeremonyPINEnv(t)
			var hits pinCounter
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.observe(r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)
			useRESTCeremonyClient(t, srv.URL)

			err := runCeremonyCommand(t, build)
			require.Error(t, err)
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			assert.True(t, errors.As(err, &tlsErr), "want SOPINRequiresTLSError, got %v", err)
			assert.Zero(t, hits.withPIN.Load(), "the plaintext server received the SO PIN")
			assert.Zero(t, hits.ceremony.Load(), "the plaintext server received a ceremony request")
		})
	}
}

// TestCeremonyTransport_CrossHostRedirectGetsNoPIN: a 307 from the
// configured server to another origin is refused before the PIN body is
// replayed there.
func TestCeremonyTransport_CrossHostRedirectGetsNoPIN(t *testing.T) {
	for name, build := range ceremonyCommands(t) {
		t.Run(name, func(t *testing.T) {
			setCeremonyPINEnv(t)
			var bHits pinCounter
			b := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				bHits.observe(r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(b.Close)
			a := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, b.URL+r.URL.Path, http.StatusTemporaryRedirect)
			}))
			t.Cleanup(a.Close)
			useRESTCeremonyClient(t, strings.TrimPrefix(a.URL, "https://"), a, b)

			err := runCeremonyCommand(t, build)
			require.Error(t, err)
			var hostErr *qrdbtransport.SOPINForeignHostError
			assert.True(t, errors.As(err, &hostErr), "want SOPINForeignHostError, got %v", err)
			assert.Zero(t, bHits.withPIN.Load(), "the redirect target received the SO PIN")
			assert.Zero(t, bHits.ceremony.Load(), "the redirect target received a ceremony request")
		})
	}
}

// TestCeremonyTransport_TLSServerGetsThePIN is the positive control: over
// TLS to the configured host the SO PIN is sent, once.
func TestCeremonyTransport_TLSServerGetsThePIN(t *testing.T) {
	setCeremonyPINEnv(t)
	var hits pinCounter
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.observe(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"enrolling"}`))
	}))
	t.Cleanup(srv.Close)
	useRESTCeremonyClient(t, strings.TrimPrefix(srv.URL, "https://"), srv)

	err := runCeremonyCommand(t, ceremonyCommands(t)["initialize"])
	require.NoError(t, err)
	assert.Equal(t, int64(1), hits.ceremony.Load(), "the TLS server must receive one ceremony request")
	assert.Equal(t, int64(1), hits.withPIN.Load(), "the TLS server must receive the SO PIN once")
}
