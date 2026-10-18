# Multiprotocol API — gRPC-Web for the TypeScript SDK (design note)

> **Status: DESIGN** · Last verified: 2026-10-18 (main `128e6a224`)
> The seam for serving the gRPC-Web wire to `sdk/typescript` (go-quicbaas#1529).
> Nothing here is built. A seam needs the CA's confirmation before code, so the
> choices are listed under [Open questions](#open-questions), not decided.

Browsers cannot speak native gRPC: they cannot read HTTP/2 trailers or
control framing. gRPC-Web carries the same protobuf messages over HTTP/1.1 or
HTTP/2, with the trailers moved into the response body. The TypeScript SDK
needs a server that speaks it for the existing services. Today none does.

## Facts on main

| Fact | Where |
|---|---|
| Network gRPC (`server.grpc.port`) is served by the **embedded go-qrdb node's unified-server listener**, not by quicbaas. quicbaas hands the node its port and protocol switches and registers its services as qrdb modules (`qrdbsdk.WithModule`) | `pkg/server/qrdb_node.go:211-216`, `:627`, `:671` |
| quicbaas's own grpc-go server (`ggrpc.NewServer`) serves **only the Unix socket**; the code says "There is no TCP listener here anymore: the node already owns server.grpc.port" | `pkg/server/lifecycle.go:2332`, `:2560-2566`, `:2600`, `:2616` |
| The sealed window runs a separate grpc-go server on a loopback TCP listener until unseal | `pkg/server/sealed_window.go:368`, `:388`, `:395` |
| The Unix server's chain: timeout, logging, metrics, recovery, tenant (from the verified mTLS peer), go-qrdb's sealed-window interceptors (`qrdbsdk.SealedWindow*Interceptor`), the SO-PIN seal gate, the platform-token authz interceptors | `pkg/server/lifecycle.go:2271-2285`, `:2299-2302`, `:2306`, `:2316-2324` |
| The tenant interceptor reads the client certificate from the grpc-go peer; the authz interceptor reads the token from incoming metadata | `pkg/api/grpc/tenant_interceptor.go:41-43`, `pkg/api/grpc/authz_interceptor.go:57` |
| TLS: one `*tls.Config` (`s.tlsConfig`, ALPN `h2`, `http/1.1`) wrapped for grpc-go with `credentials.NewTLS` | `pkg/server/lifecycle.go:742`, `:2244`, `pkg/api/grpc/tls.go:94` |
| The REST port is go-qrdb's REST server; quicbaas mounts its router there. Over TCP it is **HTTP/1.1 only** (HTTP/2 deliberately disabled), and it also serves HTTP/3 | `pkg/server/lifecycle_rest.go:398-406`, `pkg/api/rest/server.go:991`, [engineering.md](engineering.md) (QUIC row) |
| `sdk/typescript/src/grpc_web.ts` refuses every verb through `notImpl` (`GrpcWebNotImplementedError`), for example `ceremony.claimShareBegin` | `sdk/typescript/src/grpc_web.ts:147`, `:158`, `:332` |
| `connectrpc.com/connect v1.21.1` is in `go.mod`, used only by the interop test (a connect client against grpc-go) | `go.mod:10`, `pkg/api/grpc/interop_connect_test.go:40` |
| The proto toolchain generates Go only: `buf.gen.yaml` runs `protocolbuffers/go` and `grpc/go`; the Makefile pins `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc` | `buf.gen.yaml:12`, `:18`, `Makefile:440` |
| `sdk/typescript/package.json` has no gRPC-Web, Connect or protobuf runtime; its only dev dependencies are `typescript` and `vitest` | `sdk/typescript/package.json` |

**Consequence.** The interceptor chain that guards network gRPC is go-qrdb's
unified server (authorization over the `qrdb.v1.permission` method option),
and its `*grpc.Server` is not reachable from quicbaas. Any option that puts
gRPC-Web on the network must either run inside that server or reproduce its
chain. Reproducing it would be a second authorization path, which this repo
does not own.

## Server options

### (a) A translating handler in front of a grpc-go server

`github.com/improbable-eng/grpc-web` (`grpcweb.WrapServer`) wraps a
`*grpc.Server` as an `http.Handler`. Upstream archived the repository and
points users to Connect. grpc-go's `ServeHTTP` path fills the peer's
`credentials.TLSInfo` from `r.TLS`, so the tenant interceptor would see the
mTLS client certificate. The token arrives as a header, so the authz
interceptor would read it as metadata. Calls go through the wrapped server's
whole chain.

- **Which server.** The only `*grpc.Server` quicbaas holds is the Unix one.
  Wrapping it and mounting it on the REST port gives browsers the Unix chain,
  not go-qrdb's network chain. To get the network chain, go-qrdb would have to
  expose its server, or do the wrapping itself (option c2).
- **Listener.** None new: an `http.Handler` on the REST port, which is
  HTTP/1.1 over TCP, and HTTP/3. gRPC-Web needs neither HTTP/2 nor trailers.
  It uses the REST server's TLS config, so a browser's client certificate
  arrives the way it arrives for REST.
- **Cost.** An archived dependency on the authentication path. Not in the
  module cache.

### (b) connect-go handlers generated from the same protos

`protoc-gen-connect-go` generates `http.Handler`s that serve gRPC, gRPC-Web
and Connect natively. It could run beside grpc-go or replace it.

- **Interceptors.** Connect handlers take `connect.Interceptor`, not grpc-go
  interceptors. The tenant, sealed-window (go-qrdb's, typed for grpc-go) and
  authz interceptors would each need a Connect form. For the sealed-window
  and permission interceptors that means a go-qrdb change. Done in this repo,
  it would be a second authorization implementation.
- **Instead of grpc-go.** go-qrdb owns the network gRPC server, so replacing
  grpc-go is a go-qrdb decision for every module, not quicbaas's.
- **Beside grpc-go.** A second set of generated service code (`*connect.go`)
  for every quicbaas proto, and a new `buf.gen.yaml` plugin and Makefile pin.
- **Listener.** Mounts on the REST port (HTTP/1.1, HTTP/3) with the shared TLS
  config. No new port.
- **Cost.** `connectrpc.com/connect v1.21.1`, including
  `cmd/protoc-gen-connect-go`, is already in the module cache. The interceptor
  parity is the cost, and it is upstream.

### (c1) An external proxy (Envoy `envoy.filters.http.grpc_web`)

A proxy translates gRPC-Web to native gRPC on `server.grpc.port`, so calls
reach go-qrdb's unchanged chain.

- **Authn.** The platform token passes through as metadata. The browser's mTLS
  peer does **not** pass through: the proxy terminates TLS and dials upstream
  with its own identity. Carrying the client certificate in a forwarded header
  would be a new trust path.
- **Listener.** A new process and port, with its own TLS config and
  deployment. No repo code, but every deployment needs the proxy.
- **Cost.** No Go or npm dependency. Loses the mTLS peer, so tenant derivation
  from the certificate does not work.

### (c2) go-qrdb serves gRPC-Web from its unified server

go-qrdb adds an option to its unified server that serves gRPC-Web for the
modules it already hosts: an `http.Handler` built over its own `*grpc.Server`,
mounted on its REST server. quicbaas turns it on through a `qrdbsdk.With*`
option, the way it already passes `WithModule`.

- **Authn and authz.** Calls run through go-qrdb's own network interceptor
  chain, the one native gRPC uses: one path, and authorization stays go-qrdb's.
  The mTLS peer reaches the grpc-go peer as in (a).
- **Listener.** The REST port (HTTP/1.1, HTTP/3) with the REST server's TLS
  config. No new port.
- **Cost.** An upstream change in go-qrdb, and a translating library there.
  The maintained translator is `connectrpc.com/vanguard` (its `vanguardgrpc`
  package transcodes to a `*grpc.Server`); improbable-eng is archived. Neither
  is in the module cache.

## Client options (`sdk/typescript`)

| Client | Code generation | In the repo's toolchain? |
|---|---|---|
| `@connectrpc/connect-web` (+ `@connectrpc/connect`, `@bufbuild/protobuf`), `createGrpcWebTransport` | `protoc-gen-es` (v2 emits the messages and service descriptors) | No: no TS plugin in `buf.gen.yaml`, none pinned in the Makefile, `protoc-gen-es` not on the node |
| Google `grpc-web` (+ `google-protobuf`) | `protoc-gen-grpc-web` plus `protoc-gen-js` (split out of `protoc` since 3.21) | No: neither binary is on the node |

connect-web speaks gRPC-Web to any of the server options. It also speaks
the Connect protocol, which only option (b) serves. Google's client works
only against gRPC-Web, and generates CommonJS/closure-style code that does not
fit the SDK's ESM `"type": "module"` package.

## Dependencies offline on this node

| Dependency | Status |
|---|---|
| `connectrpc.com/connect v1.21.1` (+ `protoc-gen-connect-go`) | In the Go module cache |
| `github.com/improbable-eng/grpc-web` | Missing |
| `connectrpc.com/vanguard` | Missing |
| Envoy | Not a module; not on the node |
| `@connectrpc/connect`, `@connectrpc/connect-web`, `@bufbuild/protobuf`, `protoc-gen-es` | Missing: the npm cache is empty and `sdk/typescript` has no `node_modules` |
| `grpc-web`, `google-protobuf`, `protoc-gen-grpc-web`, `protoc-gen-js` | Missing |

These are facts for the coordinator. Nothing was fetched.

## Standard

The code would follow the gRPC project's
[`doc/PROTOCOL-WEB.md`](https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-WEB.md)
(gRPC-Web wire format, read with `doc/PROTOCOL-HTTP2.md`, which it amends).
The lane cannot reach a copy: none is in the module cache or on the node. The
revision to pin (a grpc/grpc commit) is for the coordinator, who places the
copy in [`docs/reference/`](../reference/README.md).

## Recommendation

**(c2), with `@connectrpc/connect-web` on the client.** It is the only option
where a gRPC-Web call reaches the same interceptor chain as a native gRPC call
on the network port, with the mTLS peer intact. It needs no new port, and
quicbaas adds no authorization. It consumes go-qrdb through a `With*` option,
as the layer rules ask. Option (a) on quicbaas's Unix server would give browsers
a different chain from network gRPC. Option (b) needs upstream interceptor
parity and a second generated surface. Option (c1) loses the mTLS peer.

The client generator is new to the toolchain whichever server option is chosen.
Its output replaces the `notImpl` stubs one service at a time, in later slices.

## Open questions

1. **Upstream.** Does the CA confirm (c2)? It needs a go-qrdb issue: a
   unified-server option that serves gRPC-Web for hosted modules on the REST
   server, behind the network interceptor chain. This lane files nothing.
2. **Translator.** go-qrdb's choice under (c2), but `vanguard` is the
   maintained candidate. It must be vendored into the module cache on the node.
3. **Toolchain.** May `buf.gen.yaml` and the Makefile gain a pinned
   `protoc-gen-es`, and `sdk/typescript` the `@connectrpc/*` and
   `@bufbuild/protobuf` dependencies (npm cache population is the
   coordinator's)?
4. **Standard copy.** Which grpc/grpc revision of `PROTOCOL-WEB.md` is pinned
   into `docs/reference/`?
5. **Browser mTLS.** Browsers send client certificates only on a TLS
   handshake the browser controls. Is a browser expected to present one, or is
   the platform token its only credential (which would make (c1) acceptable)?
