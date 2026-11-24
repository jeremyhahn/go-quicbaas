package grpc

import "testing"

// userGrants is the grant of each quicbaas.iam.v1.IAMService user verb the
// CLI calls over gRPC that is mapped by parity with the other gRPC user
// surface. Unmapped, RequireAuthz refuses each one for every caller, so
// `iam user create` over gRPC fails with "method not authorized".
var userGrants = map[string]methodPermission{
	// Mirrors /qrdb.v1.QRDBService/CreateUser (users:create).
	"/quicbaas.iam.v1.IAMService/CreateUser": {"users", "create"},
}

func TestUserGRPCResourceAndAction(t *testing.T) {
	requireGRPCGrants(t, userGrants)
}

func TestUserGRPCAuthorization(t *testing.T) {
	requireGRPCGrantEnforced(t, userGrants)
}
