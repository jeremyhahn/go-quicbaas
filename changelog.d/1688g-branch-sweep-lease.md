### Fixed

- The branch sweep deletes a carried factory branch only while the forge
  confirms it still points at the tip the sweep proved carried, so a push
  that lands after the sweep's read is never deleted (#1688).
