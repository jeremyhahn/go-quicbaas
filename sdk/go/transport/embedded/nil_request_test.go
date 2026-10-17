package embedded

import (
	"context"
	"errors"
	"testing"

	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// requestlessMethods are the health probes. They carry no request fields and
// accept any request value, a nil one included, by design.
var requestlessMethods = map[string]struct{}{
	"quicbaas.Health/Ping":      {},
	"quicbaas.Health/Liveness":  {},
	"quicbaas.Health/Readiness": {},
}

// callRecovering runs fn and returns any panic value so one bad handler
// fails its own subtest instead of the whole run.
func callRecovering(fn func() error) (err error, recovered interface{}) {
	defer func() {
		recovered = recover()
	}()
	return fn(), nil
}

// TestHandlers_NilRequest walks every dispatch table and calls each handler
// with a nil request. No handler may panic: each must return an error, and
// the handlers that type-assert the request must return
// *InvalidRequestTypeError (#1467).
func TestHandlers_NilRequest(t *testing.T) {
	ctx := inProcessTenant(context.Background())
	// A non-nil service with no backends configured: the transport never
	// dispatches with a nil service, so only the request is nil here.
	svc := &mockQuicBaaS{}

	for key, handler := range unaryHandlers {
		t.Run("unary/"+key, func(t *testing.T) {
			err, recovered := callRecovering(func() error {
				_, err := handler(ctx, svc, nil)
				return err
			})
			if _, ok := requestlessMethods[key]; ok {
				if recovered != nil {
					t.Fatalf("handler panicked on a nil request: %v", recovered)
				}
				return
			}
			assertNilRequestRejected(t, err, recovered)
		})
	}

	for key, handler := range streamHandlers {
		t.Run("stream/"+key, func(t *testing.T) {
			s := newStream(ctx, 1)
			err, recovered := callRecovering(func() error {
				return handler(ctx, svc, nil, s)
			})
			assertNilRequestRejected(t, err, recovered)
		})
	}
}

func assertNilRequestRejected(t *testing.T, err error, recovered interface{}) {
	t.Helper()
	if recovered != nil {
		t.Fatalf("handler panicked on a nil request: %v", recovered)
	}
	if err == nil {
		t.Fatal("handler accepted a nil request")
	}
	// Handlers that resolve their backing service before the type assertion
	// report the unconfigured backend first; the rest must name the bad
	// request type.
	var (
		typeErr       *InvalidRequestTypeError
		notConfigured *ServiceNotConfiguredError
		unknown       *UnknownMethodError
		qbErr         *sdkerrors.QBError
	)
	if !errors.As(err, &typeErr) && !errors.As(err, &notConfigured) &&
		!errors.As(err, &unknown) && !errors.As(err, &qbErr) &&
		!errors.Is(err, pkgsdlc.ErrUnavailable) {
		t.Fatalf("got %T (%v), want a typed request rejection", err, err)
	}
}
