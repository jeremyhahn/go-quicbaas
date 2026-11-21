package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// permutations returns every ordering of mods.
func permutations(mods []qrdbsdk.ServiceModule) [][]qrdbsdk.ServiceModule {
	if len(mods) <= 1 {
		return [][]qrdbsdk.ServiceModule{append([]qrdbsdk.ServiceModule(nil), mods...)}
	}
	var out [][]qrdbsdk.ServiceModule
	for i := range mods {
		rest := make([]qrdbsdk.ServiceModule, 0, len(mods)-1)
		rest = append(rest, mods[:i]...)
		rest = append(rest, mods[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]qrdbsdk.ServiceModule{mods[i]}, p...))
		}
	}
	return out
}

// guardOutcome runs the route guard over every ordering of mounted and hosts
// and requires one and the same outcome from all of them.
func guardOutcome(t *testing.T, mounted, hosts []qrdbsdk.ServiceModule) error {
	t.Helper()
	var first error
	for i, m := range permutations(mounted) {
		for j, h := range permutations(hosts) {
			err := assertNoHostModuleRouteConflicts(m, h)
			if i == 0 && j == 0 {
				first = err
				continue
			}
			require.Equal(t, first, err,
				"the guard's outcome must not depend on registration order (mounted %d, hosts %d)", i, j)
		}
	}
	return first
}

// Two host modules contesting a route: every declaration order names the
// same owner and the same offender, by the stated precedence (the module whose
// name sorts first owns the route).
func TestAssertNoHostModuleRouteConflicts_HostConflictIsOrderIndependent(t *testing.T) {
	err := guardOutcome(t, nil, []qrdbsdk.ServiceModule{
		newHostTestModule("platform", "/api/v1/shared"),
		newHostTestModule("fleet", "/api/v1/shared"),
		newHostTestModule("attest", "/api/v1/attest"),
	})

	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "fleet", conflict.Owner)
	assert.Equal(t, "platform", conflict.Module)
}

// Several host faults at once: which one is reported does not depend on the
// order the hosts were declared in.
func TestAssertNoHostModuleRouteConflicts_FirstFaultIsOrderIndependent(t *testing.T) {
	err := guardOutcome(t, nil, []qrdbsdk.ServiceModule{
		&hostTestModule{name: "zeta", routes: []hostTestRoute{{method: http.MethodGet, pattern: "/kv/range"}}},
		newHostTestModule("platform", "/api/v1/shared"),
		newHostTestModule("fleet", "/api/v1/shared"),
		&bypassModule{name: "beta", routes: []hostTestRoute{{method: http.MethodGet, pattern: "/api/v1/beta"}}},
	})

	var bypass *HostModuleUnregisteredRouteError
	require.ErrorAs(t, err, &bypass, "the fault of the module first in precedence is reported")
	assert.Equal(t, "beta", bypass.Module)
}

// A host route overlapping a route two mounted modules both hold is reported
// against the same incumbent whichever order the mounted modules came in.
func TestAssertNoHostModuleRouteConflicts_MountedOwnerIsOrderIndependent(t *testing.T) {
	err := guardOutcome(t,
		[]qrdbsdk.ServiceModule{
			newHostTestModule("xkms-like", "/api/v1/tenants"),
			newHostTestModule("quicbaas-like", "/api/v1/tenants"),
		},
		[]qrdbsdk.ServiceModule{
			newHostTestModule("platform", "/api/v1/tenants"),
			newHostTestModule("fleet", "/api/v1/fleet"),
		})

	var conflict *HostModuleRouteConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "quicbaas-like", conflict.Owner)
	assert.Equal(t, "platform", conflict.Module)
}

// Modules that do not overlap compose cleanly in every order.
func TestAssertNoHostModuleRouteConflicts_DisjointIsOrderIndependent(t *testing.T) {
	err := guardOutcome(t,
		[]qrdbsdk.ServiceModule{
			newHostTestModule("quicbaas-like", "/api/v1/q"),
			newHostTestModule("xkms-like", "/api/v1/x"),
		},
		[]qrdbsdk.ServiceModule{
			newHostTestModule("platform", "/api/v1/platform"),
			newHostTestModule("fleet", "/api/v1/fleet"),
			newHostTestModule("attest", "/api/v1/attest"),
		})
	assert.NoError(t, err)
}
