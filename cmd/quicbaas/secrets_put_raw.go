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
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// =============================================================================
// `secrets put` value sources.
//
// --data KEY=VALUE stores a JSON object, which is what a multi-field secret
// wants and what `secrets get` renders. A secret that is ONE opaque value — a
// provider OAuth token a pooled agent account references, a forge token, a
// webhook HMAC key — has to be stored as exactly its bytes, because every
// consumer of a secret reference reads the value verbatim. --from-stdin and
// --from-file store those bytes with no envelope.
//
// Neither takes the value on the command line: argv lands in shell history and
// the process table. From an interactive terminal --from-stdin reads one line
// with echo disabled; from a pipe it reads the whole stream verbatim.
// =============================================================================

var (
	// secretsFromStdin stores the raw bytes read from stdin.
	secretsFromStdin bool

	// secretsFromFile stores the raw bytes of this file.
	secretsFromFile string
)

// secretValuePrompt is shown on stderr before a hidden terminal read.
const secretValuePrompt = "Secret value (input hidden): "

// SecretsPutSourceError reports a `secrets put` invocation whose value source
// is missing, ambiguous, or empty.
type SecretsPutSourceError struct {
	Reason string
}

// Error implements the error interface.
func (e *SecretsPutSourceError) Error() string {
	return "secrets.put: " + e.Reason
}

// SecretsPutReadError reports a value source that could not be read.
type SecretsPutReadError struct {
	Source string
	Err    error
}

// Error implements the error interface.
func (e *SecretsPutReadError) Error() string {
	return "secrets.put: failed to read the secret value from " + e.Source + ": " + e.Err.Error()
}

// Unwrap exposes the underlying I/O error.
func (e *SecretsPutReadError) Unwrap() error { return e.Err }

// secretsPutValue returns the bytes `secrets put` stores, from whichever single
// source the flags name.
func secretsPutValue(cmd *cobra.Command) ([]byte, error) {
	sources := 0
	for _, set := range []bool{len(secretsData) > 0, secretsFromStdin, secretsFromFile != ""} {
		if set {
			sources++
		}
	}
	switch {
	case sources == 0:
		return nil, &SecretsPutSourceError{Reason: "a value is required: --data KEY=VALUE, --from-stdin, or --from-file PATH"}
	case sources > 1:
		return nil, &SecretsPutSourceError{Reason: "--data, --from-stdin and --from-file are mutually exclusive"}
	}

	var (
		value []byte
		err   error
	)
	switch {
	case secretsFromFile != "":
		value, err = readSecretValueFile(secretsFromFile)
	case secretsFromStdin:
		value, err = readSecretValueStdin(cmd)
	default:
		value, err = secretsDataJSON(secretsData)
	}
	if err != nil {
		return nil, err
	}
	if len(value) == 0 {
		return nil, &SecretsPutSourceError{Reason: "the secret value is empty"}
	}
	return value, nil
}

// readSecretValueFile reads the file's bytes verbatim.
func readSecretValueFile(path string) ([]byte, error) {
	value, err := os.ReadFile(path) //nolint:gosec // G304: the operator names the file to store
	if err != nil {
		return nil, &SecretsPutReadError{Source: path, Err: err}
	}
	return value, nil
}

// readSecretValueStdin reads the value from stdin: one hidden line from an
// interactive terminal, otherwise the whole stream verbatim — so
// `--from-stdin </dev/null` is an empty value, not a prompt.
func readSecretValueStdin(cmd *cobra.Command) ([]byte, error) {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && isTerminal(f) {
		return readHiddenSecretValue(cmd.Context())
	}
	value, err := io.ReadAll(in)
	if err != nil {
		return nil, &SecretsPutReadError{Source: "stdin", Err: err}
	}
	return value, nil
}

// readHiddenSecretValue reads one line from the terminal with echo disabled,
// through the platform's protected-memory terminal reader, and copies it out
// for the request.
func readHiddenSecretValue(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	buf, err := secure.NewTerminalProvider().Passphrase(ctx, secretValuePrompt)
	if err != nil {
		return nil, &SecretsPutReadError{Source: "terminal", Err: err}
	}
	defer buf.Destroy()

	var value []byte
	if err := buf.WithBytes(func(b []byte) error {
		value = make([]byte, len(b))
		copy(value, b)
		return nil
	}); err != nil {
		return nil, &SecretsPutReadError{Source: "terminal", Err: err}
	}
	return value, nil
}

// secretsDataJSON encodes --data KEY=VALUE pairs as the JSON object
// `secrets get` renders.
func secretsDataJSON(pairs []string) ([]byte, error) {
	dataMap := make(map[string]string, len(pairs))
	for _, kv := range pairs {
		key, value, found := strings.Cut(kv, "=")
		if !found {
			return nil, &SecretsPutSourceError{Reason: "invalid data format: " + kv + " (expected KEY=VALUE)"}
		}
		dataMap[key] = value
	}
	encoded, err := json.Marshal(dataMap)
	if err != nil {
		return nil, &SecretsPutSourceError{Reason: "failed to marshal secret data: " + err.Error()}
	}
	return encoded, nil
}
