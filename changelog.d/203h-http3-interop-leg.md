### Added

- **An HTTP/3 interop leg driven by quic-go's own client (go-quicbaas#203).**
  `pkg/api/rest/http3_interop_test.go` serves the full REST router over
  HTTP/3 on a loopback UDP socket (TLS 1.3, ALPN `h3`) and drives it with
  quic-go's `http3.Transport`, not our SDK or `RESTClient`. It asserts that
  the discovery document is answered over HTTP/3, that a client-credentials
  token obtained through x/oauth2 over HTTP/3 introspects as active (and a
  tampered one as inactive), and that a raw QUIC client offering only `h2` or
  `http/1.1` is refused in the handshake with `no_application_protocol`. In
  production the router is mounted on go-qrdb's unified REST server, which
  owns the QUIC listener and its Alt-Svc advertisement; that listener is
  tested in go-qrdb.
