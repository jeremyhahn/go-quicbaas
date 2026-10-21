### Fixed

- The init backend matrix (`test/integration/init`, `TestBackendMatrix`) now starts each `quicbaasd start` on the same config file its `quicbaasd init` used, so start carries `iam.spiffe_trust_domain` and the storage backend under test. On the memory engine first-boot runs again at start, and without a trust domain it correctly refused to boot, so every memory cell failed; the backend had also been set through an environment variable the daemon child never received. The first-boot refusal is unchanged (#1732).
