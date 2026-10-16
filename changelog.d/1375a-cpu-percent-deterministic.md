### Fixed

- **`TestRuntimeStats_CPUPercent` no longer races the clock** (#1375). The
  container runtime's `Stats` waits between its two `cpu.stat` samples
  through an unexported seam, `cpuSampleWait`, which the test replaces to
  rewrite `cpu.stat` before the second sample. It now asserts an exact
  percentage, and a new test checks that a ctx cancelled during the window
  returns its error. Production behaviour is unchanged.
