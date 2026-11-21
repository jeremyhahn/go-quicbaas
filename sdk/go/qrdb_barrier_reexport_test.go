// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"context"
	"reflect"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// barrierVerb is one go-qrdb BarrierClient verb as an sdk/go caller reaches
// it: the verb's area of the system barrier and its request and response
// types spelled through the sdk/go re-exports. A nil type means the verb has
// no request (or no response beyond its error).
type barrierVerb struct {
	area string
	req  reflect.Type
	resp reflect.Type
}

func typeOf[T any]() reflect.Type { return reflect.TypeFor[T]() }

// sdkBarrierVerbs is every verb of go-qrdb's system barrier, the six areas
// of the barrier SPEC row (init, unseal, seal, rekey, recovery, Shamir
// custody), each spelled with sdk/go names only. A consumer of sdk/go can
// call every one of them through Security().Barrier() without importing
// go-qrdb.
var sdkBarrierVerbs = map[string]barrierVerb{
	"BarrierInitialize":       {"init", typeOf[*QRDBBarrierInitializeRequest](), nil},
	"BarrierInitializeShamir": {"init", typeOf[*QRDBBarrierInitializeShamirRequest](), typeOf[*QRDBBarrierInitializeShamirResponse]()},
	"BarrierEnableEncryption": {"init", typeOf[*QRDBBarrierEnableEncryptionRequest](), typeOf[*QRDBBarrierEnableEncryptionResponse]()},
	"BarrierStatus":           {"init", nil, typeOf[*QRDBBarrierStatusResponse]()},

	"BarrierUnseal":           {"unseal", typeOf[*QRDBBarrierUnsealRequest](), nil},
	"BarrierUnsealWithShare":  {"unseal", typeOf[*QRDBBarrierUnsealShareRequest](), typeOf[*QRDBBarrierUnsealShareResponse]()},
	"BarrierUnsealWithShares": {"unseal", typeOf[*QRDBBarrierUnsealSharesRequest](), nil},

	"BarrierSeal":    {"seal", nil, nil},
	"ShamirSealVote": {"seal", typeOf[*QRDBShamirSealVoteRequest](), typeOf[*QRDBShamirSealVoteResponse]()},

	"BarrierRekey":             {"rekey", typeOf[*QRDBBarrierRekeyRequest](), typeOf[*QRDBBarrierRekeyResponse]()},
	"BarrierGenerateRootToken": {"rekey", typeOf[*QRDBBarrierGenerateRootTokenRequest](), typeOf[*QRDBBarrierRootTokenResponse]()},

	"BarrierGenerateRecoveryKeys": {"recovery", typeOf[*QRDBBarrierGenerateRecoveryKeysRequest](), typeOf[*QRDBBarrierRecoveryKeysResponse]()},
	"BarrierRecoverWithKeys":      {"recovery", typeOf[*QRDBBarrierRecoverWithKeysRequest](), nil},
	"BarrierDeleteRecoveryKeys":   {"recovery", nil, nil},
	"BarrierHasRecoveryKeys":      {"recovery", nil, typeOf[*QRDBBarrierHasRecoveryKeysResponse]()},
	"RecoveryOfficerRegister":     {"recovery", typeOf[*QRDBOfficerRegisterRequest](), typeOf[*QRDBOfficerRegisterResponse]()},
	"RecoveryShareClaimBegin":     {"recovery", typeOf[*QRDBShareClaimBeginRequest](), typeOf[*QRDBShareClaimBeginResponse]()},
	"RecoveryShareClaimComplete":  {"recovery", typeOf[*QRDBShareClaimCompleteRequest](), typeOf[*QRDBShareClaimCompleteResponse]()},

	"OfficerRegister":    {"custody", typeOf[*QRDBOfficerRegisterRequest](), typeOf[*QRDBOfficerRegisterResponse]()},
	"OfficerList":        {"custody", nil, typeOf[[]*QRDBOfficerStatus]()},
	"OfficerClaimStatus": {"custody", nil, typeOf[[]*QRDBOfficerStatus]()},
	"ShareClaimBegin":    {"custody", typeOf[*QRDBShareClaimBeginRequest](), typeOf[*QRDBShareClaimBeginResponse]()},
	"ShareClaimComplete": {"custody", typeOf[*QRDBShareClaimCompleteRequest](), typeOf[*QRDBShareClaimCompleteResponse]()},
}

// TestQRDBBarrierClient_IsGoQRDBBarrierClient pins acceptance 1 of the
// barrier row: Security().Barrier() returns go-qrdb's own BarrierClient, a
// re-export and not a go-quicbaas interface of its own.
func TestQRDBBarrierClient_IsGoQRDBBarrierClient(t *testing.T) {
	if got, want := typeOf[QRDBBarrierClient](), typeOf[qrdbtransport.BarrierClient](); got != want {
		t.Fatalf("QRDBBarrierClient is %v, want go-qrdb's %v", got, want)
	}
	m, ok := reflect.TypeFor[SecurityService]().MethodByName("Barrier")
	if !ok {
		t.Fatal("SecurityService has no Barrier method")
	}
	if m.Type.NumOut() != 2 || m.Type.Out(0) != typeOf[qrdbtransport.BarrierClient]() {
		t.Fatalf("Security().Barrier() is %v, want (go-qrdb BarrierClient, error)", m.Type)
	}
}

// TestQRDBBarrierClient_EveryVerbReexported walks go-qrdb's BarrierClient
// method set. Each verb must be in sdkBarrierVerbs, and its request and
// response types must be the sdk/go re-exports listed there; a verb go-qrdb
// adds or reshapes fails here until sdk/go re-exports it.
func TestQRDBBarrierClient_EveryVerbReexported(t *testing.T) {
	ctxType := typeOf[context.Context]()
	errType := typeOf[error]()
	iface := typeOf[qrdbtransport.BarrierClient]()

	areas := map[string]bool{}
	for i := 0; i < iface.NumMethod(); i++ {
		m := iface.Method(i)
		want, ok := sdkBarrierVerbs[m.Name]
		if !ok {
			t.Errorf("go-qrdb BarrierClient.%s is not in sdkBarrierVerbs: re-export its types in sdk/go/qrdb.go and list it", m.Name)
			continue
		}
		areas[want.area] = true

		var in []reflect.Type
		for j := 0; j < m.Type.NumIn(); j++ {
			in = append(in, m.Type.In(j))
		}
		var out []reflect.Type
		for j := 0; j < m.Type.NumOut(); j++ {
			out = append(out, m.Type.Out(j))
		}

		wantIn := []reflect.Type{ctxType}
		if want.req != nil {
			wantIn = append(wantIn, want.req)
		}
		var wantOut []reflect.Type
		if want.resp != nil {
			wantOut = append(wantOut, want.resp)
		}
		wantOut = append(wantOut, errType)

		if !reflect.DeepEqual(in, wantIn) {
			t.Errorf("BarrierClient.%s takes %v, sdk/go re-exports %v", m.Name, in, wantIn)
		}
		if !reflect.DeepEqual(out, wantOut) {
			t.Errorf("BarrierClient.%s returns %v, sdk/go re-exports %v", m.Name, out, wantOut)
		}
	}

	for name := range sdkBarrierVerbs {
		if _, ok := iface.MethodByName(name); !ok {
			t.Errorf("sdkBarrierVerbs lists %s, which go-qrdb's BarrierClient no longer has", name)
		}
	}
	for _, area := range []string{"init", "unseal", "seal", "rekey", "recovery", "custody"} {
		if !areas[area] {
			t.Errorf("no go-qrdb BarrierClient verb covers the %q area of the barrier", area)
		}
	}
}
