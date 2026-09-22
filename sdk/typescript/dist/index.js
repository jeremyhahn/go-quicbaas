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
// Factory, transport detection, and environment helpers.
export { createClient, getClient, isDesktop, isWailsRuntime, createBarrierClient, createCeremonyClient, createBootstrapClient, } from './factory.js';
export { parseJSONPreservingLargeIntegers, quoteUnrepresentableIntegers, } from './json.js';
// SandboxMode const object (not a type — must be a named export, not type-only).
export { SandboxMode } from './types.js';
// SPEND_UNATTRIBUTED is a value, not a type: a caller drilling into a spend
// report's unattributed row supplies it as that level's scope.
export { SPEND_UNATTRIBUTED } from './types.js';
export { adoptPage, appendPageParams, appendListParam, appendCSVParam, wholeSetPage, MalformedPageError, } from './page.js';
// Typed errors.
export { QuicBaaSError, ConnectionError, NotConnectedError, APIError, DeployNotSupportedError, SSHError, SSHNotFoundError, SSHDuplicateError, SSHInvalidError, SSHUnavailableError, SSHServerError, SSHHostMismatchError, } from './errors.js';
// SSH REST client classes — exported so callers can construct them directly
// or use the top-level RestClient.ssh namespace.
export { RestSSHKeysClient, RestKnownHostsClient, RestAuthorizedKeysClient } from './rest.js';
// Barrier / Ceremony / Bootstrap REST clients — exported so callers can
// construct each sub-client directly without going through the full
// RestClient. The convenience factory functions in factory.ts wrap these.
export { RestBarrierClient, RestTenantBarrierClient, RestCeremonyClient, RestBootstrapClient, RestClusterClient, } from './rest.js';
// Certificate authority / trust / crypto REST clients — exported so callers
// can construct each sub-client directly without going through the full
// RestClient.
export { RestCAClient, RestTrustClient, RestCryptoClient } from './rest.js';
// Stub transport error types — exported so callers can catch them explicitly.
export { GrpcWebNotImplementedError } from './grpc_web.js';
export { UnixNotImplementedError } from './unix.js';
export { McpNotImplementedError } from './mcp.js';
export { QuicNotImplementedError } from './quic.js';
//# sourceMappingURL=index.js.map