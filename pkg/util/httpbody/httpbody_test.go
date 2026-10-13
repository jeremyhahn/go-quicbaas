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

package httpbody

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// trackingBody records whether Close was called and returns closeErr.
type trackingBody struct {
	io.Reader
	closed   bool
	closeErr error
}

func (b *trackingBody) Close() error {
	b.closed = true
	return b.closeErr
}

func TestDrainClose_ReadsToEOFAndCloses(t *testing.T) {
	r := strings.NewReader("unread response body")
	body := &trackingBody{Reader: r}

	if err := DrainClose(body); err != nil {
		t.Fatalf("DrainClose: %v", err)
	}
	if r.Len() != 0 {
		t.Fatalf("%d bytes left unread", r.Len())
	}
	if !body.closed {
		t.Fatal("body was not closed")
	}
}

func TestDrainClose_JoinsReadAndCloseErrors(t *testing.T) {
	readErr := errors.New("connection reset")
	closeErr := errors.New("close failed")
	body := &trackingBody{Reader: iotest.ErrReader(readErr), closeErr: closeErr}

	err := DrainClose(body)

	if !errors.Is(err, readErr) {
		t.Fatalf("error %v does not wrap the read failure", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("error %v does not wrap the close failure", err)
	}
	if !body.closed {
		t.Fatal("body was not closed after a read failure")
	}
}
