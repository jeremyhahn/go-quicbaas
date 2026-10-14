### Documentation

- **Four trust-boundary rulings recorded (go-quicbaas#402, #416, #422, #329, #301).** The TOTP secret's unprotected KV read is an accepted boundary; verified hardware attestation alone satisfies `mfa_verified` for the release candidate; a PIV slot attestation reaches the server in the TLS handshake only, which browsers cannot do; and the embedded transport is not an authorization boundary — linking the SDK is the grant — while gRPC and the other network transports stay boundaries.
