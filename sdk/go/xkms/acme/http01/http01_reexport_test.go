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

package http01_test

import (
	"errors"
	"io"
	"net"
	"net/http"
	"testing"

	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/acme/http01"
)

// Compile-time proof the aliases are the go-xkms types, assignable both ways,
// so a responder built through the SDK is the one a go-xkms ACME client drives.
var (
	_ *xkmsacme.HTTP01Responder             = (*http01.Responder)(nil)
	_ *http01.Responder                     = (*xkmsacme.HTTP01Responder)(nil)
	_ xkmsacme.HTTP01ResponderConfig        = http01.ResponderConfig{}
	_ http01.ResponderConfig                = xkmsacme.HTTP01ResponderConfig{}
	_ xkmsacme.HTTP01ResponderBindError     = http01.ResponderBindError{}
	_ xkmsacme.HTTP01ResponderConfigError   = http01.ResponderConfigError{}
	_ xkmsacme.HTTP01ResponderShutdownError = http01.ResponderShutdownError{}
)

// TestNewResponder_ServesKeyAuthorization proves the re-exported constructor
// binds before returning and answers the well-known challenge path with the
// key authorization, which is what the ACME directory validates.
func TestNewResponder_ServesKeyAuthorization(t *testing.T) {
	const token = "tok3n"
	const keyAuth = token + ".thumbprint"

	responder, err := http01.NewResponder(http01.ResponderConfig{
		Addr:    "127.0.0.1:0",
		KeyAuth: keyAuth,
	})
	if err != nil {
		t.Fatalf("NewResponder: %v", err)
	}
	t.Cleanup(func() {
		if err := responder.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	url := "http://" + responder.Addr().String() + "/.well-known/acme-challenge/" + token
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != keyAuth {
		t.Fatalf("body = %q, want %q", body, keyAuth)
	}
}

// TestNewResponder_BindFailure proves an address already held yields the
// re-exported ResponderBindError, unwrapping to the net error, instead of a
// responder that silently never answers.
func TestNewResponder_BindFailure(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		if err := held.Close(); err != nil {
			t.Errorf("close held listener: %v", err)
		}
	})

	responder, err := http01.NewResponder(http01.ResponderConfig{
		Addr:    held.Addr().String(),
		KeyAuth: "tok3n.thumbprint",
	})
	if err == nil {
		if closeErr := responder.Close(); closeErr != nil {
			t.Errorf("Close: %v", closeErr)
		}
		t.Fatal("NewResponder on a held address returned nil error")
	}
	var bindErr http01.ResponderBindError
	if !errors.As(err, &bindErr) {
		t.Fatalf("err = %T %v, want http01.ResponderBindError", err, err)
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("err does not unwrap to *net.OpError: %v", err)
	}
}

// TestNewResponder_RejectsEmptyKeyAuth proves configuration errors surface as
// the re-exported ResponderConfigError naming the field.
func TestNewResponder_RejectsEmptyKeyAuth(t *testing.T) {
	_, err := http01.NewResponder(http01.ResponderConfig{Addr: "127.0.0.1:0"})
	var cfgErr http01.ResponderConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %T %v, want http01.ResponderConfigError", err, err)
	}
	if cfgErr.Field != "KeyAuth" {
		t.Fatalf("Field = %q, want %q", cfgErr.Field, "KeyAuth")
	}
}
