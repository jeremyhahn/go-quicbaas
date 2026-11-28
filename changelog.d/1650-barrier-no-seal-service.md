### Removed

- QuicBaaS's own seal service: the `pkg/barrier` and `pkg/seal` packages, `interfaces.BarrierManager`, the five unserved `QuicBaaS` gRPC RPCs `SealStatus`, `SealInit`, `Unseal`, `Seal` and `SealRekey` with their messages, the unused seal request types in `pkg/service` and `sdk/go/service`, and the deprecated TypeScript `SealClient` (`client.seal`). The system barrier is go-qrdb's, through `Security().Barrier()` and `client.barrier` (#1650).
