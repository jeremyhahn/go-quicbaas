package iam

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
)

// The service-account writers must make their decision and their write one
// step under the store's write lock (#43, #46). These tests drive concurrent
// writers at the same record and assert the invariants that a check-then-put
// outside the lock breaks.

// concurrently runs fn n times at once and returns the errors in call order.
func concurrently(n int, fn func(i int) error) []error {
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			<-start
			errs[i] = fn(i)
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

// oneWinner asserts exactly one call succeeded and every other failed with want.
func oneWinner(t *testing.T, errs []error, want error) {
	t.Helper()
	wins := 0
	for i, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, want):
			t.Errorf("call %d: got %v, want %v", i, err, want)
		}
	}
	if wins != 1 {
		t.Fatalf("%d concurrent calls succeeded, want exactly 1", wins)
	}
}

func TestCreateServiceAccount_ConcurrentSameNameCreatesOne(t *testing.T) {
	t.Parallel()
	svc := newTestAuthService(t)
	ctx := context.Background()

	errs := concurrently(16, func(int) error {
		req := validCreateRequest()
		req.Name = "racing-worker"
		_, _, err := svc.CreateServiceAccount(ctx, req)
		return err
	})
	oneWinner(t, errs, ErrServiceAccountExists)

	accounts, err := svc.ListServiceAccounts(ctx, validCreateRequest().TenantID)
	if err != nil {
		t.Fatalf("ListServiceAccounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Errorf("stored %d accounts named racing-worker, want 1", len(accounts))
	}
}

func TestCreateServiceAccount_ConcurrentSameDeviceCertBindsOne(t *testing.T) {
	t.Parallel()
	svc := newTestAuthService(t)
	ctx := context.Background()

	errs := concurrently(16, func(i int) error {
		req := validCreateRequest()
		req.Name = "cert-worker-" + strconv.Itoa(i)
		req.DeviceCert = "device-cn-shared"
		_, _, err := svc.CreateServiceAccount(ctx, req)
		return err
	})
	oneWinner(t, errs, ErrDeviceCertAlreadyBound)
}

func TestBindDeviceCertificate_ConcurrentSameCertBindsOne(t *testing.T) {
	t.Parallel()
	svc := newTestAuthService(t)
	ctx := context.Background()

	const n = 16
	ids := make([]string, n)
	for i := range n {
		req := validCreateRequest()
		req.Name = "bind-worker-" + strconv.Itoa(i)
		account, _, err := svc.CreateServiceAccount(ctx, req)
		if err != nil {
			t.Fatalf("CreateServiceAccount: %v", err)
		}
		ids[i] = account.ID
	}

	errs := concurrently(n, func(i int) error {
		return svc.BindDeviceCertificate(ctx, ids[i], "device-cn-bound")
	})
	oneWinner(t, errs, ErrDeviceCertAlreadyBound)
}

func TestServiceAccountMutations_LeaveHandedOutRecordsAlone(t *testing.T) {
	t.Parallel()
	svc := newTestAuthService(t)
	ctx := context.Background()

	account, _, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	held, err := svc.store.GetServiceAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	before := *held

	if _, err := svc.RotateCredentials(ctx, account.ID); err != nil {
		t.Fatalf("RotateCredentials: %v", err)
	}
	if err := svc.BindDeviceCertificate(ctx, account.ID, "device-cn-held"); err != nil {
		t.Fatalf("BindDeviceCertificate: %v", err)
	}
	if err := svc.RevokeServiceAccount(ctx, account.ID); err != nil {
		t.Fatalf("RevokeServiceAccount: %v", err)
	}

	// A reader holding the record from before must not see it change under
	// it: the writers replace the stored record rather than editing it.
	if held.SecretHash != before.SecretHash || held.DeviceCert != before.DeviceCert || held.Revoked != before.Revoked {
		t.Error("a mutation edited a record the store had already handed out")
	}

	stored, err := svc.store.GetServiceAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	if stored.SecretHash == before.SecretHash || stored.DeviceCert != "device-cn-held" || !stored.Revoked {
		t.Errorf("the mutations were not all stored: %+v", stored)
	}
}

func TestServiceAccountMutations_RaceFreeAgainstReaders(t *testing.T) {
	t.Parallel()
	svc := newTestAuthService(t)
	ctx := context.Background()

	account, _, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	// Under -race, a writer editing the stored struct in place while a reader
	// reads it is reported directly.
	concurrently(8, func(i int) error {
		switch i % 4 {
		case 0:
			_, err := svc.RotateCredentials(ctx, account.ID)
			return err
		case 1:
			return svc.BindDeviceCertificate(ctx, account.ID, "device-cn-"+strconv.Itoa(i))
		default:
			got, err := svc.store.GetServiceAccount(ctx, account.ID)
			if err == nil {
				_ = got.SecretHash + got.DeviceCert
				_ = got.Revoked
			}
			return err
		}
	})
}

func TestUpdateServiceAccount_NotFoundAndMutateError(t *testing.T) {
	t.Parallel()
	store := NewInMemoryServiceAccountStore()
	ctx := context.Background()

	_, err := store.UpdateServiceAccount(ctx, "missing", func(*ServiceAccount) error { return nil })
	if !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("missing account: got %v", err)
	}

	if err := store.PutServiceAccount(ctx, &ServiceAccount{ID: "a1", TenantID: "t", Name: "n"}); err != nil {
		t.Fatalf("PutServiceAccount: %v", err)
	}
	refused := errors.New("refused")
	_, err = store.UpdateServiceAccount(ctx, "a1", func(a *ServiceAccount) error {
		a.Name = "changed"
		return refused
	})
	if !errors.Is(err, refused) {
		t.Errorf("mutate error: got %v", err)
	}
	stored, _ := store.GetServiceAccount(ctx, "a1")
	if stored.Name != "n" {
		t.Error("a refused update was stored")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.UpdateServiceAccount(cancelled, "a1", func(*ServiceAccount) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: got %v", err)
	}
	if err := store.InsertServiceAccount(cancelled, &ServiceAccount{ID: "a2"}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled insert: got %v", err)
	}
	if err := store.InsertServiceAccount(ctx, nil); !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("nil insert: got %v", err)
	}
}
