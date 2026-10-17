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

package service

import (
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestUpdateAppRequest_OptionsCarriesEveryField sets every field of the
// request and checks that each one reaches the update options unchanged.
// A field added to iam.UpdateAppOptions without a request counterpart fails
// here, so no transport can silently drop it.
func TestUpdateAppRequest_OptionsCarriesEveryField(t *testing.T) {
	var req UpdateAppRequest
	reqValue := reflect.ValueOf(&req).Elem()
	optsType := reflect.TypeOf(iam.UpdateAppOptions{})

	for i := 0; i < optsType.NumField(); i++ {
		name := optsType.Field(i).Name
		field := reqValue.FieldByName(name)
		if !field.IsValid() {
			t.Fatalf("UpdateAppRequest has no field for UpdateAppOptions.%s", name)
		}
		if field.Kind() != reflect.Pointer {
			t.Fatalf("UpdateAppRequest.%s is %s, want a pointer so unset stays distinguishable", name, field.Kind())
		}
		field.Set(reflect.New(field.Type().Elem()))
	}

	opts := reflect.ValueOf(req.Options())
	for i := 0; i < optsType.NumField(); i++ {
		name := optsType.Field(i).Name
		want := reqValue.FieldByName(name).Pointer()
		if got := opts.Field(i).Pointer(); got != want {
			t.Errorf("Options().%s does not carry the request value", name)
		}
	}
}

func TestUpdateAppRequest_OptionsEmptyRequestSetsNothing(t *testing.T) {
	req := UpdateAppRequest{ClientID: "c1"}
	opts := reflect.ValueOf(req.Options())
	for i := 0; i < opts.NumField(); i++ {
		if !opts.Field(i).IsNil() {
			t.Errorf("Options().%s is set for an empty request", opts.Type().Field(i).Name)
		}
	}
}
