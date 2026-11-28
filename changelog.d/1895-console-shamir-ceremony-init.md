### Fixed

- The console's Barrier page initializes a fresh Shamir system barrier through the initialization ceremony (`POST /api/v1/init/initialize`): the form takes the threshold, the SO and user PINs and the officers with their CSRs, and each share is sealed to an officer, who claims it on the Ceremony page. The page no longer calls the bare Shamir init the system barrier refuses, and no share reaches the browser (#1895).
