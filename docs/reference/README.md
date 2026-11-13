# Reference standards

go-quicbaas implements an industry standard wherever one exists and designs nothing in its place. This directory holds a copy of each standard the code in this repository implements. The code follows the copy exactly. A draft is adopted at one pinned revision, and `ROADMAP.md` Dependencies carries a standards-watch line for it. A new revision, or the final RFC, opens an issue that updates both the copy and the code.

Not listed here:

- standards that a library implements for us, such as JOSE/JWT (go-jose, golang-jwt), WebSocket (coder/websocket), QUIC and HTTP/3 (quic-go), SSH (x/crypto/ssh) and WebAuthn (go-webauthn);
- standards owned by go-xkms or go-qrdb and re-exported through `sdk/go`: ACME, DANE, X.509 and the CA, FIDO2, SPIFFE, key wrap and RATS. Their copies live in the owning repository;
- standards the code only cites.

Each copy is the published text as fetched, with one exception: line 757 of `mqtt-v5.0-os.html`, a CSS font declaration, carries a `lab-string-ok` comment so that the `pkg/labstrings` publication gate accepts the font name.

## OAuth 2.0, OAuth 2.1 and OpenID Connect

| File | Standard | Revision | Implementing package |
|---|---|---|---|
| [rfc6749.txt](rfc6749.txt) | The OAuth 2.0 Authorization Framework | RFC 6749, October 2012 | `pkg/iam` (`oidc.go`), `pkg/api/rest` (`iam_oidc.go`) |
| [draft-ietf-oauth-v2-1-16.txt](draft-ietf-oauth-v2-1-16.txt) | The OAuth 2.1 Authorization Framework | Internet-Draft, **pinned at -16** (2026-09-03) | `pkg/iam` (`oidc_app.go` grant profile, `oidc.go` S256-only metadata) |
| [rfc7636.txt](rfc7636.txt) | Proof Key for Code Exchange (PKCE) | RFC 7636, September 2015 | `pkg/iam` (`oidc_tokens.go`, `oidc_types.go`) |
| [rfc7009.txt](rfc7009.txt) | OAuth 2.0 Token Revocation | RFC 7009, August 2013 | `pkg/iam` (`oidc.go`), `pkg/api/rest` (`iam_oidc.go`) |
| [rfc7662.txt](rfc7662.txt) | OAuth 2.0 Token Introspection | RFC 7662, October 2015 | `pkg/iam` (`oidc.go`), `pkg/api/rest` (`iam_oidc_introspect.go`), `sdk/go/platformtoken` |
| [rfc8414.txt](rfc8414.txt) | OAuth 2.0 Authorization Server Metadata | RFC 8414, June 2018 | `pkg/iam` (`oidc.go`) |
| [rfc9728.txt](rfc9728.txt) | OAuth 2.0 Protected Resource Metadata | RFC 9728, April 2025 | `pkg/api/rest` (`iam_protected_resource_metadata.go`) |
| [rfc7591.txt](rfc7591.txt) | OAuth 2.0 Dynamic Client Registration Protocol | RFC 7591, July 2015 | `pkg/api/rest` (`iam_dynamic_registration.go`), `pkg/iam` (`oidc_app_registration.go`) |
| [rfc7592.txt](rfc7592.txt) | OAuth 2.0 Dynamic Client Registration Management Protocol | RFC 7592, July 2015 | `pkg/api/rest` (`iam_dynamic_registration.go`), `pkg/iam` (`oidc_app_registration.go`) |
| [rfc8252.txt](rfc8252.txt) | OAuth 2.0 for Native Apps (custom-scheme redirect URIs) | RFC 8252, October 2017 | `pkg/api/rest` (`iam_dynamic_registration.go`) |
| [rfc8707.txt](rfc8707.txt) | Resource Indicators for OAuth 2.0 | RFC 8707, February 2020 | `pkg/iam` (`resource_indicators.go`) |
| [rfc8705.txt](rfc8705.txt) | OAuth 2.0 Mutual-TLS Client Authentication and Certificate-Bound Access Tokens | RFC 8705, February 2020 | `pkg/iam` (`oauth2_mtls.go`) |
| [rfc9449.txt](rfc9449.txt) | OAuth 2.0 Demonstrating Proof of Possession (DPoP) | RFC 9449, September 2023 | `pkg/iam/dpop` |
| [rfc7800.txt](rfc7800.txt) | Proof-of-Possession Key Semantics for JWTs (`cnf`) | RFC 7800, April 2016 | `pkg/iam` (`oidc_types.go`) |
| [rfc9068.txt](rfc9068.txt) | JSON Web Token (JWT) Profile for OAuth 2.0 Access Tokens | RFC 9068, October 2021 | `pkg/iam` (`oidc_tokens.go`), `sdk/go/platformtoken` |
| [rfc8176.txt](rfc8176.txt) | Authentication Method Reference Values | RFC 8176, June 2017 | `pkg/iam` (`oidc_tokens.go`) |
| [openid-connect-core-1_0.txt](openid-connect-core-1_0.txt) | OpenID Connect Core 1.0 | 1.0 incorporating errata set 2 | `pkg/iam` (`oidc.go` ID token and UserInfo, `federation_idtoken.go` relying-party validation) |
| [openid-connect-discovery-1_0.txt](openid-connect-discovery-1_0.txt) | OpenID Connect Discovery 1.0 | 1.0 incorporating errata set 2 | `pkg/iam` (`oidc.go`) |

## HTTP

| File | Standard | Revision | Implementing package |
|---|---|---|---|
| [rfc9745.txt](rfc9745.txt) | The Deprecation HTTP Response Header Field | RFC 9745, March 2025 | none: the only implementer, `pkg/api/rest/handlers_seal.go`, was removed (#1559) |
| [rfc8594.txt](rfc8594.txt) | The Sunset HTTP Header Field | RFC 8594, May 2019 | none: the only implementer, `pkg/api/rest/handlers_seal.go`, was removed (#1559) |
| [rfc8288.txt](rfc8288.txt) | Web Linking | RFC 8288, October 2017 | `pkg/sdlc` (`forge_http.go`, `rel="next"` parsing) |
| [rfc6797.txt](rfc6797.txt) | HTTP Strict Transport Security (HSTS) | RFC 6797, November 2012 | `pkg/api/rest` (`middleware.go`) |
| [rfc7617.txt](rfc7617.txt) | The 'Basic' HTTP Authentication Scheme | RFC 7617, September 2015 | `pkg/signalbridge` (`http_transport.go`) |
| [rfc3986.txt](rfc3986.txt) | Uniform Resource Identifier (URI): Generic Syntax | RFC 3986, January 2005 | `pkg/api/rest` (`iam_dynamic_registration.go`, scheme syntax) |
| [draft-ietf-webtrans-http3-16.txt](draft-ietf-webtrans-http3-16.txt) | WebTransport over HTTP/3 | Internet-Draft, **pinned at -16** (2026-07-06) | none yet: the server endpoint and the `sdk/typescript` QUIC client land under #1534 |

## Logging, naming and messaging

| File | Standard | Revision | Implementing package |
|---|---|---|---|
| [rfc5424.txt](rfc5424.txt) | The Syslog Protocol | RFC 5424, March 2009 | `pkg/security` (`audit.go`) |
| [rfc1035.txt](rfc1035.txt) | Domain Names: Implementation and Specification (label length) | RFC 1035, November 1987 | `pkg/iam/oktaprovision` |
| [rfc6761.txt](rfc6761.txt) | Special-Use Domain Names (`.invalid`) | RFC 6761, February 2013 | `pkg/orchestration` (`fleet_reachability.go`) |
| [amqp0-9-1.xml](amqp0-9-1.xml) | AMQP 0-9-1, machine-readable specification | 0-9-1 | `pkg/api/websocket/subprotocol` (`amqp.go`, build tag `quicbaas_amqp`) |
| [stomp-1.2.txt](stomp-1.2.txt) | STOMP Protocol Specification | 1.2 | `pkg/api/websocket/subprotocol` (`stomp.go`, build tag `quicbaas_stomp`) |
| [mqtt-v3.1.1-os.html](mqtt-v3.1.1-os.html) | MQTT Version 3.1.1 | OASIS Standard, 29 October 2014 | `pkg/api/websocket/subprotocol` (`mqtt.go`, build tag `quicbaas_mqtt`; the packet codec is mochi-mqtt's) |
| [mqtt-v5.0-os.html](mqtt-v5.0-os.html) | MQTT Version 5.0 | OASIS Standard, 7 March 2019 | `pkg/api/websocket/subprotocol` (`mqtt.go`, build tag `quicbaas_mqtt`) |

## Model Context Protocol

| File | Standard | Revision | Implementing package |
|---|---|---|---|
| [mcp-2024-11-05-lifecycle.mdx](mcp-2024-11-05-lifecycle.mdx) | Model Context Protocol, lifecycle (`initialize` handshake) | 2024-11-05 | `sdk/go/transport/mcp` (`MCPVersion`), `cmd/quicbaas` (`mcp.go`) |
| [mcp-2024-11-05-schema.ts](mcp-2024-11-05-schema.ts) | Model Context Protocol, message schema | 2024-11-05 | `sdk/go/transport/mcp`, `cmd/quicbaas` |

The MCP server and its transport come from go-qrdb (mark3labs/mcp-go). go-quicbaas builds only the client's `initialize` and `notifications/initialized` messages.
