package ca_test

import (
	"reflect"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/ca"
	xkmsstorage "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/storage"
)

// TestRevocationStore_IsTheParamsFieldType pins the #1826 re-export:
// ca.RevocationStore is the go-xkms storage backend type of
// Params.RevocationStore, so a consumer of sdk/go can name and supply the
// CA's revocation store without importing go-xkms.
func TestRevocationStore_IsTheParamsFieldType(t *testing.T) {
	field, ok := reflect.TypeOf(ca.Params{}).FieldByName("RevocationStore")
	if !ok {
		t.Fatal("ca.Params has no RevocationStore field")
	}
	storeType := reflect.TypeOf((*ca.RevocationStore)(nil)).Elem()
	if field.Type != storeType {
		t.Errorf("Params.RevocationStore is %v, ca.RevocationStore is %v", field.Type, storeType)
	}
	if want := reflect.TypeOf((*xkmssdk.StorageBackend)(nil)).Elem(); storeType != want {
		t.Errorf("ca.RevocationStore is %v, want go-xkms %v", storeType, want)
	}
	if want := reflect.TypeOf((*xkmsstorage.Backend)(nil)).Elem(); storeType != want {
		t.Errorf("ca.RevocationStore is %v, want sdk/go/xkms/storage %v", storeType, want)
	}

	var store ca.RevocationStore
	params := ca.Params{RevocationStore: store}
	if params.RevocationStore != nil {
		t.Error("a nil store must stay nil on Params")
	}
}
