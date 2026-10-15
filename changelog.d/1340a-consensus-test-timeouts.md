### Fixed

- **Consensus-arbitration tests no longer time out on runners with busy
  disks (#1340).** The in-process qrdb node from `pkg/testutil/qrdbnode`
  fsynced every proposal into its throwaway temp directory. On a runner
  sharing its disk with other jobs, one fsync could outlast the 5s per-record
  deadline, so `pkg/orchestration`'s concurrent job-status tests failed with
  `propose failed: quicraft: timeout`. The node now boots with
  `WithNoSyncWrites`, which leaves the Raft commit order those tests assert
  unchanged. This applies to every package that uses `qrdbnode.Shared`.
