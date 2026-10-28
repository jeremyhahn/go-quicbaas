package service

import (
	"context"
	"crypto/x509"
	"errors"
	"math/big"
	"reflect"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// ownershipFailureCAService lists certificates but cannot read the CA's
// ownership records, as go-xkms ca.CA reports a storage or decode failure.
type ownershipFailureCAService struct {
	mockListerCAService
	failingOwnershipCA
}

func newOwnershipFailureCAService() *ownershipFailureCAService {
	return &ownershipFailureCAService{
		mockListerCAService: newTenantListerCAService(issuingTenantCA{1: "acme"}).mockListerCAService,
	}
}

// requireOwnershipStoreError fails the test unless err is the package's
// internal error carrying the ownership-store failure.
func requireOwnershipStoreError(t *testing.T, err error) {
	t.Helper()
	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) || qbErr.Code != qberrors.ErrInternal {
		t.Fatalf("err = %v, want an internal *QBError", err)
	}
	if !errors.Is(err, errOwnershipStoreDown) {
		t.Fatalf("err = %v, want it to carry the ownership-store failure", err)
	}
}

// TestCertificateService_ListCertificatesReturnsOwnershipStoreFailure pins
// #2024: a tenant listing whose ownership lookup fails gets the failure, not
// a successful empty inventory with a wrong TotalCount.
func TestCertificateService_ListCertificatesReturnsOwnershipStoreFailure(t *testing.T) {
	svc := NewCertificateService(newOwnershipFailureCAService())

	resp, err := svc.ListCertificates(tenantctx.WithTenant(context.Background(), "acme"), &ListCertificatesRequest{})
	if resp != nil {
		t.Fatalf("ListCertificates = %+v, want no response", resp)
	}
	requireOwnershipStoreError(t, err)
}

// TestCertificateService_ListCertificatesExcludesForeignAndUnattributed pins
// that a tenant listing still drops certificates issued for another tenant,
// for the system tenant, and those with no ownership record.
func TestCertificateService_ListCertificatesExcludesForeignAndUnattributed(t *testing.T) {
	// Serial 2 is foreign, 3 is the system's, 9 has no ownership record.
	svc := NewCertificateService(newTenantListerCAService(issuingTenantCA{1: "acme", 2: "globex", 3: ""}))

	resp, err := svc.ListCertificates(tenantctx.WithTenant(context.Background(), "acme"), &ListCertificatesRequest{})
	if err != nil {
		t.Fatalf("ListCertificates: %v", err)
	}
	if resp.TotalCount != 1 || len(resp.Certificates) != 1 || resp.Certificates[0].SerialNumber != "1" {
		t.Fatalf("ListCertificates = %+v, want only serial 1", resp)
	}
}

// TestFilterCertificateIDsForCaller_ReturnsOwnershipStoreFailure pins #2024
// for the key-store listing: an ownership lookup failure stops the filter.
func TestFilterCertificateIDsForCaller_ReturnsOwnershipStoreFailure(t *testing.T) {
	certs := NewCertificateService(newOwnershipFailureCAService())
	lookup := func(context.Context, string) (*x509.Certificate, error) {
		return &x509.Certificate{SerialNumber: big.NewInt(1)}, nil
	}

	got, err := FilterCertificateIDsForCaller(tenantctx.WithTenant(context.Background(), "acme"), certs, []string{"acme-leaf"}, lookup)
	if got != nil {
		t.Fatalf("FilterCertificateIDsForCaller = %v, want nil", got)
	}
	requireOwnershipStoreError(t, err)
}

// TestFilterCertificateIDsForCaller_ExcludesUnattributedWithoutFailing pins
// that an unattributed or foreign certificate is dropped, not reported as a
// failure, and that a CA recording no tenants shows a tenant nothing.
func TestFilterCertificateIDsForCaller_ExcludesUnattributedWithoutFailing(t *testing.T) {
	acme := tenantctx.WithTenant(context.Background(), "acme")
	serials := map[string]int64{"acme-leaf": 1, "globex-leaf": 2, "unattributed": 9}
	lookup := func(_ context.Context, keyID string) (*x509.Certificate, error) {
		return &x509.Certificate{SerialNumber: big.NewInt(serials[keyID])}, nil
	}
	ids := []string{"acme-leaf", "globex-leaf", "unattributed"}

	certs := NewCertificateService(newTenantListerCAService(issuingTenantCA{1: "acme", 2: "globex"}))
	got, err := FilterCertificateIDsForCaller(acme, certs, ids, lookup)
	if err != nil {
		t.Fatalf("FilterCertificateIDsForCaller: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"acme-leaf"}) {
		t.Fatalf("FilterCertificateIDsForCaller = %v, want [acme-leaf]", got)
	}

	untracked := NewCertificateService(&mockCAService{})
	got, err = FilterCertificateIDsForCaller(acme, untracked, ids, lookup)
	if err != nil || len(got) != 0 {
		t.Fatalf("FilterCertificateIDsForCaller over a CA without tenants = %v, %v, want none", got, err)
	}
}
