### Added

- **OIDC/OAuth2 interop leg (go-quicbaas#203).** A unit-tier test serves the IAM REST routes on a loopback listener and drives them with `golang.org/x/oauth2` and `lestrrat-go/jwx/v2`. It covers the discovery baseline, PKCE S256 code exchange, ID-token verification against the served JWKS, client credentials and RFC 7662 introspection. It also checks that a wrong PKCE verifier and a token signed by a foreign key are both refused. `lestrrat-go/jwx/v2` is now a direct dependency.

### Documentation

- **Interop claims audit (go-quicbaas#203).** A table in `docs/multiprotocol-api/engineering.md` lists each conformance claim with the test that enforces it. Claims with no enforcing test were narrowed: the incomplete browser authorization-code flow, the MCP client compatibility list, the gRPC client coverage, the unserved QUIC transport, and the RFCs that no external client exercises yet.
