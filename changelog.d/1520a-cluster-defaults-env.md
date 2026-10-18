### Fixed

- **`TestClusterBuilderDefaults` holds in any environment (go-quicbaas#1520).**
  The test pins the `QUICBAAS_NODE{n}_GRPC` and `QUICBAAS_NODE{n}` variables
  and asserts the `localhost:5005{1,2,3}` defaults; new cases cover the gRPC
  variables and the `QUICBAAS_NODE{n}` fallback.
