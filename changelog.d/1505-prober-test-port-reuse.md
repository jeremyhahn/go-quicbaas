### Fixed

- **The agent-address prober's dial-refusal test no longer races a freed
  port.** `TestAgentAddressProber_RefusesWhatItCannotReach` dialed a closed
  loopback listener's port, which a parallel test could take first. It now
  uses a dialer double that refuses the address, asserts the refusal cause,
  and checks that resolve and parse refusals never dial.
