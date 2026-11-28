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

// Package main provides the non-argv input path for the Security Officer and
// User PINs of the verbs that carry them on the request (#226).
//
// A process has one standard input and an invocation may need up to three
// secrets (passphrase, SO PIN, User PIN), so each PIN gets its own file flag
// (--so-pin-file, --user-pin-file) where "-" means standard input, matching
// --shares-file. At most one secret source may claim stdin. The environment
// variables the daemon already reads (QUICBAAS_SO_PIN, QUICBAAS_USER_PIN) are
// the second source, and the argv flag stays last for compatibility only.
// Every source is read into protected memory first.
package main

import (
	"os"

	"github.com/spf13/cobra"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

const (
	// soPINEnv and userPINEnv are the variables quicbaasd init reads too.
	soPINEnv   = "QUICBAAS_SO_PIN"
	userPINEnv = "QUICBAAS_USER_PIN"

	soPINFileFlag   = "so-pin-file"
	userPINFileFlag = "user-pin-file"
	userPINFlag     = "user-pin"

	// pinArgvWarning is appended to the argv PIN flags' help.
	pinArgvWarning = "visible in the process list; use the -file flag or the environment"
)

// pinInput names one PIN's sources, in precedence order: the file flag, the
// environment variables, then the argv flag bound to target.
type pinInput struct {
	label    string
	flag     string
	fileFlag string
	envs     []string
	target   *string
}

// soPINInput is the SO PIN bound to target. QUICBAAS_BARRIER_SO_PIN, the
// name barrier seal reads, is accepted after the daemon's name.
func soPINInput(target *string) pinInput {
	return pinInput{label: "Security Officer PIN", flag: soPINFlag, fileFlag: soPINFileFlag,
		envs: []string{soPINEnv, barrierSOPINEnv}, target: target}
}

// userPINInput is the User PIN bound to target.
func userPINInput(target *string) pinInput {
	return pinInput{label: "User PIN", flag: userPINFlag, fileFlag: userPINFileFlag,
		envs: []string{userPINEnv}, target: target}
}

// addPINFlags registers, for each input, the compatibility argv flag (with
// note appended to its help) and the file flag.
func addPINFlags(cmd *cobra.Command, note string, inputs ...pinInput) {
	for _, in := range inputs {
		cmd.Flags().StringVar(in.target, in.flag, "",
			in.label+" ("+note+pinArgvWarning+")")
		cmd.Flags().String(in.fileFlag, "",
			"Read the "+in.label+" from this file; - reads standard input")
	}
}

// resolvedPINs holds each input's PIN in protected memory, in the order
// the inputs were given; an input with no source has a nil entry.
type resolvedPINs []*secure.Buffer

// has reports whether input i has a PIN.
func (p resolvedPINs) has(i int) bool { return i < len(p) && p[i] != nil }

// field returns input i's PIN as the string a go-qrdb transport request
// field takes, or "" when it has none. It is the CLI's one bytes-to-string
// conversion of a PIN, because the go-qrdb sdk/go transport request types
// declare SOPin and UserPin as string; call it only inside the request
// literal, so the PIN stays in protected memory until the client call.
func (p resolvedPINs) field(i int) string {
	if !p.has(i) {
		return ""
	}
	var s string
	//secure:unwiped injected-interface the go-qrdb sdk/go transport request PIN fields (SOPin, UserPin) are strings, so the copy the request carries is unwipeable.
	_ = p[i].WithBytes(func(b []byte) error { s = string(b); return nil })
	return s
}

// resolvePINs reads every input's PIN from its first source into protected
// memory and returns the buffers in input order. It clears each input's argv
// target, so no package global keeps the PIN. The returned release destroys
// the buffers; callers defer it past the client call.
func resolvePINs(cmd *cobra.Command, op string, inputs ...pinInput) (resolvedPINs, func(), error) {
	pins := make(resolvedPINs, len(inputs))
	// release destroys the protected buffers, on success and on failure alike.
	release := func() {
		for _, b := range pins {
			if b != nil {
				b.Destroy()
			}
		}
	}
	stdinClaim := ""
	if v, err := cmd.Flags().GetBool(passphraseStdinFlag); err == nil && v {
		stdinClaim = "--" + passphraseStdinFlag
	}
	for i, in := range inputs {
		buf, err := resolvePIN(cmd, op, in, &stdinClaim)
		// resolvePIN has copied any argv PIN into protected memory; drop
		// the string the flag variable holds.
		*in.target = ""
		if err != nil {
			for _, rest := range inputs[i:] {
				*rest.target = ""
			}
			release()
			return nil, func() {}, err
		}
		pins[i] = buf
	}
	return pins, release, nil
}

func resolvePIN(cmd *cobra.Command, op string, in pinInput, stdinClaim *string) (*secure.Buffer, error) {
	path, _ := cmd.Flags().GetString(in.fileFlag)
	if path == "-" {
		if *stdinClaim != "" {
			return nil, errors.Newf(errors.ErrInvalidInput, op,
				"--%s - and %s both read standard input; give one of them a file",
				in.fileFlag, *stdinClaim)
		}
		*stdinClaim = "--" + in.fileFlag + " -"
		buf, err := readPassphrase(cmd.InOrStdin(), op, passphraseReadTimeout())
		return requirePIN(buf, err, op, in)
	}
	if path != "" {
		f, err := os.Open(path) // #nosec G304 -- operator-named secret file
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrInvalidInput, op,
				"failed to open --%s", in.fileFlag)
		}
		defer f.Close()
		buf, err := readPassphraseLimited(f, op)
		return requirePIN(buf, err, op, in)
	}
	for _, env := range in.envs {
		if v := os.Getenv(env); v != "" {
			return protectExposedPassphrase(v, op)
		}
	}
	if *in.target != "" {
		return protectExposedPassphrase(*in.target, op)
	}
	return nil, nil
}

// requirePIN turns an empty file read into an error naming the flag: an
// operator who points a file flag at a PIN meant to supply one.
func requirePIN(buf *secure.Buffer, err error, op string, in pinInput) (*secure.Buffer, error) {
	if err != nil {
		return nil, err
	}
	if buf.Size() == 0 {
		return nil, errors.Newf(errors.ErrInvalidInput, op, "--%s is empty", in.fileFlag)
	}
	return buf, nil
}

// pinFlagsRequested reports whether the operator typed any PIN flag on cmd,
// argv or file; the environment is deliberately not consulted.
func pinFlagsRequested(cmd *cobra.Command) bool {
	for _, name := range []string{soPINFlag, userPINFlag, soPINFileFlag, userPINFileFlag} {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			return true
		}
	}
	return false
}
