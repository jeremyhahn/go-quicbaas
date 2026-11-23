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

// Package main provides the input path for the Shamir shares and recovery
// keys of the barrier share verbs (unseal-shares, root-token and recovery
// recover).
//
// The share verbs keep each share or recovery key in protected memory until
// the go-qrdb request literal: --shares-file (or "-" for standard input) is
// read straight into a protected buffer and split into one protected buffer
// per share, and --share argv values are copied into protected buffers and
// the flag's package global cleared. The buffers are destroyed after the
// client call.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// maxSharesFileBytes caps a --shares-file read. It is far above any real
// quorum (255 shares of a few hundred bytes each), so the whole file fits
// one protected allocation.
const maxSharesFileBytes = 64 << 10

// resolvedShares holds each share or recovery key in protected memory, in
// input order.
type resolvedShares []*secure.Buffer

// fields returns the shares as the []string a go-qrdb transport request
// field takes, in order. It is the CLI's one bytes-to-string conversion of a
// share, because the go-qrdb sdk/go transport request types declare Shares
// and Keys as []string; call it only inside the request literal, so the
// shares stay in protected memory until the client call.
func (s resolvedShares) fields() []string {
	out := make([]string, len(s))
	for i, b := range s {
		//secure:unwiped injected-interface the go-qrdb sdk/go transport request share fields (Shares, Keys) are []string, so the copy the request carries is unwipeable.
		_ = b.WithBytes(func(v []byte) error { out[i] = string(v); return nil })
	}
	return out
}

// release destroys every share buffer.
func (s resolvedShares) release() {
	for _, b := range s {
		b.Destroy()
	}
}

// resolveShares enforces the --share / --shares-file invariants and returns
// the shares in protected memory. argv points at the --share flag's package
// global; it is set to nil whatever happens, so no global keeps a share (the
// argv strings themselves cannot be wiped). op names the invoking verb so
// the error points at the command the operator actually ran. The returned
// release destroys the buffers and is never nil; callers defer it past the
// client call.
func resolveShares(argv *[]string, sharesFile, op string) (resolvedShares, func(), error) {
	given := *argv
	*argv = nil
	hasFlag := len(given) > 0
	hasFile := sharesFile != ""
	if hasFlag && hasFile {
		return nil, func() {}, &BarrierShareInputError{
			Op:     op,
			Reason: "exactly one of --share (repeatable) or --shares-file is allowed",
		}
	}
	if !hasFlag && !hasFile {
		return nil, func() {}, &BarrierShareInputError{
			Op:     op,
			Reason: "one of --share (repeatable) or --shares-file is required",
		}
	}
	if hasFile {
		return readSharesFile(sharesFile, op)
	}
	shares := make(resolvedShares, 0, len(given))
	for _, s := range given {
		b := []byte(s)
		buf, err := secure.NewBufferFromBytes(b)
		if err != nil {
			secure.Wipe(b)
			shares.release()
			return nil, func() {}, errors.Wrapf(err, errors.ErrInternal, op,
				"failed to move the shares into protected memory")
		}
		shares = append(shares, buf)
	}
	return shares, shares.release, nil
}

// readSharesFile reads one share per line from path. Blank lines and
// '#' comments are skipped. The literal path "-" reads from stdin. op names
// the invoking verb and rides on the returned errors. The input never
// leaves protected memory: the whole file is read into one protected
// buffer (capped at maxSharesFileBytes) and each share copied into its own.
// The returned release destroys the share buffers and is never nil.
func readSharesFile(path, op string) (resolvedShares, func(), error) {
	noop := func() {}
	var src io.Reader
	if path == "-" {
		src = os.Stdin
	} else {
		f, err := os.Open(path) // #nosec G304 -- operator-named shares file
		if err != nil {
			return nil, noop, &BarrierSharesFileError{Path: path, Op: op, Err: err}
		}
		defer func() { _ = f.Close() }()
		src = f
	}

	// One byte over the cap, so a file of exactly the cap reads to EOF and
	// a larger one is told apart.
	scratch, err := secure.NewBuffer(maxSharesFileBytes + 1)
	if err != nil {
		return nil, noop, errors.Wrapf(err, errors.ErrInternal, op,
			"failed to move the shares into protected memory")
	}
	defer scratch.Destroy()

	//secure:unborrowed allocates-protected the shares file is read straight into the scratch protected buffer.
	dst := scratch.Bytes()
	n := 0
	for n < len(dst) {
		m, rerr := src.Read(dst[n:])
		n += m
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, noop, &BarrierSharesFileError{Path: path, Op: op, Err: rerr}
		}
	}
	if n > maxSharesFileBytes {
		return nil, noop, &BarrierSharesFileError{Path: path, Op: op,
			Err: fmt.Errorf("the file exceeds the %d-byte limit", maxSharesFileBytes)}
	}

	var shares resolvedShares
	rest := dst[:n]
	for len(rest) > 0 {
		line := rest
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			line, rest = rest[:i], rest[i+1:]
		} else {
			rest = nil
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		// NewBufferFromBytes copies into its own locked allocation and
		// wipes the source window; scratch.Destroy wipes the rest.
		buf, err := secure.NewBufferFromBytes(line)
		if err != nil {
			shares.release()
			return nil, noop, errors.Wrapf(err, errors.ErrInternal, op,
				"failed to move the shares into protected memory")
		}
		shares = append(shares, buf)
	}
	if len(shares) == 0 {
		return nil, noop, &BarrierSharesFileEmptyError{Path: path, Op: op}
	}
	return shares, shares.release, nil
}
