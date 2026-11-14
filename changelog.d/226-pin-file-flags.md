- cli: `barrier init`, `init-shamir`, `rekey`, `recovery generate`,
  `init initialize` and `init sign-csr` take the PINs from `--so-pin-file` /
  `--user-pin-file` (`-` for stdin) or `QUICBAAS_SO_PIN` /
  `QUICBAAS_USER_PIN`, so they need not ride argv (#226).
