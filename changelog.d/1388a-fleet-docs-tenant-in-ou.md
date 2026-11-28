### Documentation

- The fleet deployment docs now describe the tenant carried in the node certificate's subject OU, which the enrollment handler reads and checks against the tenant-scoped SPIFFE SAN and the request's `tenant_id`, in place of the retired `OU=agents` and Organization tenant (#1388).
