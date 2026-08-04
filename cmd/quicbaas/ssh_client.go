// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
)

// XKMSDURLEnv is the environment variable that, when set, switches
// the CLI to the REST transport pointed at the named xkmsd instance.
const XKMSDURLEnv = "XKMSD_URL"

// XKMSDTokenEnv is the environment variable carrying a bearer token
// forwarded with every REST request to xkmsd.
const XKMSDTokenEnv = "XKMSD_TOKEN"

// CLISSHStoreSubdir is the directory under the operator's home
// directory that holds the embedded file-backed SSH store when the
// REST transport is not in use.
const CLISSHStoreSubdir = ".quicbaas/ssh-store"

// CLISSHClientBuildError wraps a failure to construct the xkms SSH
// client for CLI use. The Op field names the stage that failed
// (resolve_home / open_store / rest_client) so operators see a
// self-describing error message.
type CLISSHClientBuildError struct {
	Op  string
	Err error
}

// Error implements the error interface.
func (e *CLISSHClientBuildError) Error() string {
	return "quicbaas/ssh: cli client: " + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the underlying construction failure.
func (e *CLISSHClientBuildError) Unwrap() error { return e.Err }

// buildCLISSHClient returns an xkms SSH SDK client using the
// environment-selected transport. XKMSD_URL → REST; empty →
// file-backed embedded store under $HOME/.quicbaas/ssh-store.
//
// The helper is shared by the `quicbaas ssh` subcommands and by the
// agent deploy path so both CLI surfaces observe the same transport
// selection rules.
func buildCLISSHClient() (xkmsssh.Client, error) {
	url := strings.TrimSpace(os.Getenv(XKMSDURLEnv))
	if url != "" {
		httpClient, err := buildCLISSHHTTPClient()
		if err != nil {
			return nil, &CLISSHClientBuildError{Op: "rest_client_tls", Err: err}
		}
		client, err := xkmsssh.NewREST(xkmsssh.RESTOptions{
			BaseURL:    url,
			AuthToken:  strings.TrimSpace(os.Getenv(XKMSDTokenEnv)),
			HTTPClient: httpClient,
		})
		if err != nil {
			return nil, &CLISSHClientBuildError{Op: "rest_client", Err: err}
		}
		return client, nil
	}
	root, err := cliSSHStoreRoot()
	if err != nil {
		return nil, err
	}
	client, err := xkmsssh.NewEmbeddedFile(xkmsssh.EmbeddedFileOptions{RootDir: root})
	if err != nil {
		return nil, &CLISSHClientBuildError{Op: "open_store", Err: err}
	}
	return client, nil
}

// buildCLISSHHTTPClient returns an HTTP client for the xkms SSH REST
// SDK that honours the global --tls-ca / --tls-cert / --tls-key flags,
// so remote calls against a private-CA quicbaasd succeed without env-var
// gymnastics. QuicBaaS bans insecure TLS — a CA is required.
func buildCLISSHHTTPClient() (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
	if tlsCA != "" {
		pem, err := os.ReadFile(tlsCA)
		if err != nil {
			return nil, &OpError{Op: "read --tls-ca", Cause: err}
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, &MessageError{Message: "--tls-ca: invalid PEM"}
		}
		tlsCfg.RootCAs = pool
	}
	if tlsCert != "" && tlsKey != "" {
		cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
		if err != nil {
			return nil, &OpError{Op: "load --tls-cert/--tls-key", Cause: err}
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}, nil
}

// cliSSHStoreRoot returns the filesystem path of the local SSH store.
// Uses $HOME/.quicbaas/ssh-store; falls back to CWD-relative when
// $HOME cannot be resolved (matches xkey's fallback policy).
func cliSSHStoreRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, CLISSHStoreSubdir), nil
	}
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return "", &CLISSHClientBuildError{Op: "resolve_home", Err: &MessageError{Message: fmt.Sprintf("home: %v; cwd: %v", err, cwdErr)}}
	}
	return filepath.Join(cwd, CLISSHStoreSubdir), nil
}

// cliSSHOwner returns the operator identifier used as the "owner"
// component of SSH keys / pins / authorized entries. Prefers $USER,
// falls back to $LOGNAME, then "default".
func cliSSHOwner() string {
	if v := strings.TrimSpace(os.Getenv("USER")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("LOGNAME")); v != "" {
		return v
	}
	return "default"
}
