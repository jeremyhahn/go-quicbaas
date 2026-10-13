### Fixed

- **`storage.wal_dir` follows `storage.data_dir` from every source (#176).**
  A daemon whose config file or `QUICBAAS_STORAGE_DATA_DIR` set
  `storage.data_dir` and left `storage.wal_dir` unset kept the absolute
  default WAL directory (`/var/lib/quicbaas/wal`) and failed the boot-time
  writable-directory check; only the `--data-dir` flag derived it. The WAL
  directory is now `<data_dir>/wal` whichever source set `data_dir`, unless
  `wal_dir` is set explicitly, in which case it wins. The derivation is one
  function, `config.DeriveWALDir`, run by the daemon and by `config.Loader`,
  which now also reads `QUICBAAS_STORAGE_DATA_DIR` and
  `QUICBAAS_STORAGE_WAL_DIR`.
