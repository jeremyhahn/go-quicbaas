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

package xkmsca

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestNewXKMSCAServicer_NilCA verifies the constructor returns nil when no CA
// is supplied so callers can detect the unconfigured state.
func TestNewXKMSCAServicer_NilCA(t *testing.T) {
	if got := NewServicer(nil, ""); got != nil {
		t.Fatalf("expected nil servicer for nil CA, got %#v", got)
	}
}

// TestXKMSCAServicer_ValidationErrors covers the request-validation branches
// of every CA operation. These run before any CA method is touched, so a
// zero-value servicer (nil CA) is sufficient and never panics.
func TestXKMSCAServicer_ValidationErrors(t *testing.T) {
	a := &Servicer{}
	ctx := context.Background()

	t.Run("SignCSR nil request", func(t *testing.T) {
		if _, err := a.SignCSR(ctx, nil); !stderrors.Is(err, ErrXKMSCANilRequest) {
			t.Fatalf("expected ErrXKMSCANilRequest, got %v", err)
		}
	})
	t.Run("SignCSR missing CSR", func(t *testing.T) {
		if _, err := a.SignCSR(ctx, &xkmstransport.SignCSRRequest{}); !stderrors.Is(err, ErrXKMSCAMissingCSR) {
			t.Fatalf("expected ErrXKMSCAMissingCSR, got %v", err)
		}
	})

	t.Run("IssueCertificate nil request", func(t *testing.T) {
		if _, err := a.IssueCertificate(ctx, nil); !stderrors.Is(err, ErrXKMSCANilRequest) {
			t.Fatalf("expected ErrXKMSCANilRequest, got %v", err)
		}
	})
	t.Run("IssueCertificate missing common name", func(t *testing.T) {
		if _, err := a.IssueCertificate(ctx, &xkmstransport.IssueCertificateRequest{}); !stderrors.Is(err, ErrXKMSCAMissingCommonName) {
			t.Fatalf("expected ErrXKMSCAMissingCommonName, got %v", err)
		}
	})

	t.Run("RevokeCertificate nil request", func(t *testing.T) {
		if _, err := a.RevokeCertificate(ctx, nil); !stderrors.Is(err, ErrXKMSCANilRequest) {
			t.Fatalf("expected ErrXKMSCANilRequest, got %v", err)
		}
	})
	t.Run("RevokeCertificate missing serial", func(t *testing.T) {
		if _, err := a.RevokeCertificate(ctx, &xkmstransport.RevokeCertificateRequest{}); !stderrors.Is(err, ErrXKMSCAMissingSerial) {
			t.Fatalf("expected ErrXKMSCAMissingSerial, got %v", err)
		}
	})
	t.Run("RevokeCertificate invalid serial", func(t *testing.T) {
		_, err := a.RevokeCertificate(ctx, &xkmstransport.RevokeCertificateRequest{SerialNumber: "zz-not-hex"})
		var invalid *XKMSCAInvalidSerialError
		if !stderrors.As(err, &invalid) {
			t.Fatalf("expected XKMSCAInvalidSerialError, got %v", err)
		}
	})

	t.Run("IsRevoked nil request", func(t *testing.T) {
		if _, err := a.IsRevoked(ctx, nil); !stderrors.Is(err, ErrXKMSCANilRequest) {
			t.Fatalf("expected ErrXKMSCANilRequest, got %v", err)
		}
	})
	t.Run("IsRevoked missing serial", func(t *testing.T) {
		if _, err := a.IsRevoked(ctx, &xkmstransport.IsRevokedRequest{}); !stderrors.Is(err, ErrXKMSCAMissingSerial) {
			t.Fatalf("expected ErrXKMSCAMissingSerial, got %v", err)
		}
	})
	t.Run("IsRevoked invalid serial", func(t *testing.T) {
		_, err := a.IsRevoked(ctx, &xkmstransport.IsRevokedRequest{SerialNumber: "nothex!"})
		var invalid *XKMSCAInvalidSerialError
		if !stderrors.As(err, &invalid) {
			t.Fatalf("expected XKMSCAInvalidSerialError, got %v", err)
		}
	})
}

// TestXKMSCAOperationError covers the wrapped-error type's Error/Unwrap.
func TestXKMSCAOperationError(t *testing.T) {
	underlying := stderrors.New("disk full")
	e := &XKMSCAOperationError{Op: "sign csr", Err: underlying}
	if !strings.Contains(e.Error(), "sign csr") || !strings.Contains(e.Error(), "disk full") {
		t.Fatalf("unexpected message: %q", e.Error())
	}
	if e.Unwrap() != underlying {
		t.Fatalf("Unwrap should return the underlying error")
	}
	if !stderrors.Is(e, underlying) {
		t.Fatalf("errors.Is should traverse to the underlying error")
	}
}

// TestXKMSCAInvalidSerialError covers the malformed-serial error type.
func TestXKMSCAInvalidSerialError(t *testing.T) {
	e := &XKMSCAInvalidSerialError{Value: "zz"}
	if !strings.Contains(e.Error(), "zz") {
		t.Fatalf("invalid serial message should contain the value: %q", e.Error())
	}
}
