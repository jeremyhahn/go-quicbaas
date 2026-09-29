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
// QuicBaaSError is the base error class for all SDK errors.
export class QuicBaaSError extends Error {
    constructor(message) {
        super(message);
        this.name = 'QuicBaaSError';
    }
}
// ConnectionError indicates a failure to establish or maintain a connection.
export class ConnectionError extends QuicBaaSError {
    constructor(message) {
        super(message);
        this.name = 'ConnectionError';
    }
}
// NotConnectedError indicates an operation was attempted before connecting.
export class NotConnectedError extends QuicBaaSError {
    constructor() {
        super('Not connected to QuicBaaS server');
        this.name = 'NotConnectedError';
    }
}
// APIError indicates the REST API did not give a usable answer: a non-2xx
// response, or a 2xx whose body violates the endpoint's contract. `status`
// carries the HTTP status either way, so a 2xx APIError means the request
// reached something that answered successfully without answering the question
// — see RestAuthClient.authValidate, where an uninterpretable body must not be
// read as an affirmative session.
export class APIError extends QuicBaaSError {
    status;
    constructor(status, message) {
        super(message);
        this.name = 'APIError';
        this.status = status;
    }
}
// DeployNotSupportedError is thrown when deployFleetNode is invoked over a
// transport other than Wails (desktop). Deploys orchestrate a local SSH
// session against a remote host — only the desktop/CLI runtimes can do
// that safely. Other transports must surface this error so callers know
// to fall back to the CLI or GUI.
export class DeployNotSupportedError extends QuicBaaSError {
    constructor(transport) {
        super(`deploy is desktop-only — use the CLI or GUI desktop app (current transport: ${transport})`);
        this.name = 'DeployNotSupportedError';
    }
}
// =============================================================================
// SSH typed errors
// =============================================================================
// SSHError is the base error class for all SSH service errors.
export class SSHError extends QuicBaaSError {
    /** HTTP status code from the server response. */
    status;
    /** Resource family: 'ssh-keys', 'known_hosts', 'authorized_key', etc. */
    resource;
    constructor(status, message, resource = 'ssh-keys') {
        super(message);
        this.name = 'SSHError';
        this.status = status;
        this.resource = resource;
    }
}
// SSHNotFoundError is thrown when the server returns 404 for an SSH resource.
export class SSHNotFoundError extends SSHError {
    constructor(message, resource) {
        super(404, message, resource);
        this.name = 'SSHNotFoundError';
    }
}
// SSHDuplicateError is thrown when the server returns 409 for a duplicate key or pin.
export class SSHDuplicateError extends SSHError {
    constructor(message, resource) {
        super(409, message, resource);
        this.name = 'SSHDuplicateError';
    }
}
// SSHInvalidError is thrown when the server returns 400 for an invalid request.
export class SSHInvalidError extends SSHError {
    constructor(message, resource) {
        super(400, message, resource);
        this.name = 'SSHInvalidError';
    }
}
// SSHUnavailableError is thrown when the server returns 503 because xkms
// is not configured on the QuicBaaS instance.
export class SSHUnavailableError extends SSHError {
    constructor(message) {
        super(503, message);
        this.name = 'SSHUnavailableError';
    }
}
// SSHServerError is thrown when the server returns an unexpected 5xx error.
export class SSHServerError extends SSHError {
    constructor(status, message, resource) {
        super(status, message, resource);
        this.name = 'SSHServerError';
    }
}
// SSHHostMismatchError is thrown when the server returns 409 for a host-key
// mismatch during known-host verification. The expected and received fields
// carry the stored and presented fingerprints, respectively.
export class SSHHostMismatchError extends SSHError {
    /** Hostname whose key did not match the stored pin. */
    host;
    /** SHA256 fingerprint stored in the pin store. */
    expected;
    /** SHA256 fingerprint presented by the connecting host. */
    received;
    constructor(host, expected, received, message) {
        super(409, message ?? `ssh/known_hosts: host key mismatch for ${host}`, 'known_hosts');
        this.name = 'SSHHostMismatchError';
        this.host = host;
        this.expected = expected;
        this.received = received;
    }
}
//# sourceMappingURL=errors.js.map