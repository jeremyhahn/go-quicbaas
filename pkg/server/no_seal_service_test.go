//go:build !integration

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

// no_seal_service_test.go pins the removal of QuicBaaS's own seal service.
// The system barrier is go-qrdb's, served through Security().Barrier();
// go-quicbaas keeps no seal manager, adapter or package of its own.

package server

import (
	"os"
	"reflect"
	"testing"
)

const barrierOwnerHint = "the barrier is go-qrdb's, served through Security().Barrier(); go-quicbaas has no seal service of its own"

func TestServer_HasNoSealServiceOfItsOwn(t *testing.T) {
	for _, dir := range []string{"../barrier", "../seal"} {
		_, err := os.Stat(dir)
		if !os.IsNotExist(err) {
			t.Errorf("pkg/%s exists (stat err: %v): %s", dir[3:], err, barrierOwnerHint)
		}
	}

	serverType := reflect.TypeOf(&Server{})
	for _, name := range []string{"GetBarrierManager", "SetBarrierManager"} {
		if _, ok := serverType.MethodByName(name); ok {
			t.Errorf("*Server has method %s: %s", name, barrierOwnerHint)
		}
	}
}
