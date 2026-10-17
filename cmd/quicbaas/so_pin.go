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

// Package main provides the Security Officer PIN input path for barrier
// verbs whose SDK call carries the PIN on the context (#946).
//
// The input surface mirrors the passphrase one in passphrase.go — stdin,
// then the environment, then the compatibility argv flag — and reuses its
// bounded, protected-memory reader, so the PIN never forms a Go string on
// the preferred path. The resolved PIN is bound to the call with the SDK's
// WithSOPIN, which seals its own copy and sends it only over TLS.
package main

import (
	"context"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

const (
	// barrierSOPINEnv names the environment variable that carries the SO
	// PIN when --so-pin-stdin is not used.
	barrierSOPINEnv = "QUICBAAS_BARRIER_SO_PIN"

	// soPINFlag is the compatibility flag that takes the PIN on argv.
	soPINFlag = "so-pin"

	// soPINStdinFlag selects the preferred standard-input path.
	soPINStdinFlag = "so-pin-stdin"

	// soPINExposureWarning is appended to the --so-pin help string.
	soPINExposureWarning = "visible in the process list; use --so-pin-stdin"

	// soPINStdinUsage is the help text for --so-pin-stdin.
	soPINStdinUsage = "Read the Security Officer PIN from standard input " +
		"(preferred: keeps it out of the process list and the shell history)"

	// soPINInputHelp documents the input paths in the long help of every
	// verb that takes an SO PIN on the context.
	soPINInputHelp = `The SO PIN is read from, in precedence order:

  --so-pin-stdin       standard input (preferred: it keeps the PIN out of
                       the process list and shell history)
  ` + barrierSOPINEnv + `
                       environment variable
  --so-pin             accepted for compatibility only; it is visible in
                       'ps', so do not use it`
)

// addSOPINFlags registers the SO PIN input surface on cmd: --so-pin-stdin
// plus the compatibility --so-pin bound to target.
func addSOPINFlags(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, soPINFlag, "",
		"Security Officer PIN ("+soPINExposureWarning+")")
	cmd.Flags().Bool(soPINStdinFlag, false, soPINStdinUsage)
}

// resolveSOPIN returns the SO PIN from the first source that supplies one:
// --so-pin-stdin, then QUICBAAS_BARRIER_SO_PIN, then flagValue. It returns
// a nil buffer when no source does; whether a PIN is needed is the server's
// call, made against its PIN backend.
func resolveSOPIN(cmd *cobra.Command, op, flagValue string) (*secure.Buffer, error) {
	// A command without the flag registered has no stdin path; that falls
	// through to the env and the flag, as resolvePassphrase does.
	fromStdin, err := cmd.Flags().GetBool(soPINStdinFlag)
	if err != nil {
		fromStdin = false
	}
	if fromStdin {
		return readSOPIN(cmd.InOrStdin(), op, passphraseReadTimeout())
	}
	if fromEnv := os.Getenv(barrierSOPINEnv); fromEnv != "" {
		return protectExposedPassphrase(fromEnv, op)
	}
	if flagValue != "" {
		return protectExposedPassphrase(flagValue, op)
	}
	return nil, nil
}

// readSOPIN reads one SO PIN line from r into protected memory, bounded by
// limit. It is readPassphrase with an error that names the SO PIN sources:
// where stdin cannot be bounded it points at the environment, never at argv.
func readSOPIN(r io.Reader, op string, limit time.Duration) (*secure.Buffer, error) {
	br, cleanup, err := boundedReader(r, time.Now().Add(limit))
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInvalidInput, op,
			"--%s cannot be bounded by a timeout on %s; supply the SO PIN in %s",
			soPINStdinFlag, runtime.GOOS, barrierSOPINEnv)
	}
	defer cleanup()
	return readPassphraseLimited(br, op)
}

// bindSOPIN returns ctx carrying pin as the SO PIN for SDK calls made with
// it, and a release that destroys the SDK's sealed copy. With no PIN, ctx
// is returned unchanged and release does nothing. The caller still owns
// and destroys pin.
func bindSOPIN(ctx context.Context, op string, pin *secure.Buffer) (context.Context, func(), error) {
	if pin.Size() == 0 {
		return ctx, func() {}, nil
	}
	bound, release := ctx, func() {}
	err := pin.WithBytes(func(b []byte) error {
		var werr error
		bound, release, werr = qrdbtransport.WithSOPIN(ctx, b)
		return werr
	})
	if err != nil {
		return ctx, func() {}, errors.Wrapf(err, errors.ErrInternal, op,
			"failed to bind the SO PIN to the call")
	}
	return bound, release, nil
}
