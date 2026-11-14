### Added

- The server serves go-xkms's `TCGCAService` at
  `/api/v1/xkms/ca/tcg/{ek,ak,sign-csr,enroll}` (REST, and QUIC as REST over
  HTTP/3), backed by the wired go-xkms TCG CA (#1330).
