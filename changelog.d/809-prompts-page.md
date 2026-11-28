### Added

- The web console has a Prompts page: pick a prompt set and a scope, see each prompt's origin and the version a job would run, open its effective body and resolution chain, edit it with compare-and-swap on `expected_version`, read its history, roll back, reset it to its parent, and diff it against the parent. A job's detail page lists the prompt versions the job ran with, each linking to the page (#809).
- The TypeScript SDK's `JobStatus` carries `prompts`, the prompt versions the job ran with (#809).
