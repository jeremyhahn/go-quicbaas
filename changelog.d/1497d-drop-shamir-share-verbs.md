### Removed

- **The never-served Shamir share verbs.** `BarrierShamirListShares`,
  `BarrierShamirDeleteShare`, `BarrierShamirDeleteAllShares` and
  `BarrierShamirVerify` are gone from `quicbaas.barrier.v1`, along with their
  messages, their authz rows and the `shamir-verify` sealed-window verb. The
  TypeScript SDK drops `shamirListShares`, `shamirDeleteShare`,
  `shamirDeleteAllShares` and `shamirVerify` from every transport. No
  transport served them and they back no go-qrdb operation. Barrier share
  management belongs to go-qrdb.
- **The non-REST bootstrap stubs in the TypeScript SDK.** Trust bootstrap is
  a client-side pre-trust fetch served over REST only
  (`POST /api/v1/bootstrap/{auto,dane,noise,spki}`). `bootstrap` is now a
  `RestClient` member, not part of the shared `Client` interface, and the MCP,
  QUIC and grpc-web stub clients are deleted. `createBootstrapClient(config)`
  always builds the REST client and takes no transport argument.
