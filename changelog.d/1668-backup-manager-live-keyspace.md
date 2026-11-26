### Fixed

- Server backups now contain the live key/value data. The backup manager read a separate, empty store and had no raft node, so a backup held metadata only and a restore restored nothing. It now reads the embedded qrdb node's platform keyspace, written as one shard, and a restore writes those keys back (keys created after the backup are kept). With no qrdb node running, creating or restoring a backup fails with "backup store unavailable" (#1668).
