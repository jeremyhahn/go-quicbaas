### Fixed

- Services: the node integration suite now deploys over the node's own five grants and checks that the workload reads its SVID and TLS credentials, holds its role under its SVID's binding, and that undeploy revokes, destroys and unbinds each one (#1687).
