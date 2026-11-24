### Fixed

- The xkms module's PIV login (`/api/v1/auth/login/piv`, mounted only with `allow_unattested`) now answers with platform tokens the node's OIDC provider issues and signs with its current signing key, with the same unattested claims as `/api/v1/iam/login/piv`. A tenant admin's bearer from that login is accepted on the node's REST routes. Refresh these tokens at `/api/v1/iam/refresh` (#1665).
