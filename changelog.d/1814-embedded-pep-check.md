### Fixed

- The embedded transport serves `PEPClient.Check`: it unwraps the request the client sends and evaluates it through the policy engine in the bound tenant, as `CheckBatch` does, instead of refusing the request type and answering "not configured" (#1814).
