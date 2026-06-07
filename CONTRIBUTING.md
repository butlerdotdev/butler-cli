# Contributing

Contributing to this project in any way legally acknowledges acceptance by the contributor of the terms outlined in the Developer Certificate of Origin in the DCO file.

Any merge request, issue posts, commit, or any other content development on this project legally constitutes signed acceptance of the terms in the DCO, meaning that any contribution to this project in any way releases any legal claim on those contributions including copyright, patents, trademarks, and any other intellectual property rights. It is the responsibility of all would-be contributors to legally ensure that they indeed have those rights themselves to release in the first place (and that those that employ them agree).

Developers sometimes do not realize that at the moment of their employment they may have signed away their rights to contribute to such projects without explicit (often written) permission from their employer. Doing so can result in immediate termination when discovered. Notwithstanding such violations, in such unfortunate cases, any contribution remains as a donation and newly acquired intellectual property of the project owners. This project, its owners, and other contributors shall be legally indemnified when unapproved contributions are made. Any conflict or litigation must only be between the contributor who violated the terms of their employment and their employer.

## Conventions

These conventions hold across both binaries (`butleradm`, `butlerctl`) and
should be matched by any new command.

- **Confirmation-skip flag is `--yes`.** Any command that mutates state and
  prompts for confirmation skips the prompt with `--yes` (not `--force`). For
  high-risk operations that must not be triggered by a single stray flag in CI,
  pair `--yes` with a typed-name (or typed-value) gate, as `cluster certs
  rotate --type=ca` does with `--cluster-name-confirm`.

- **Flag set depends on how the command reaches the platform.** Commands that
  talk to the Kubernetes API directly (ADR-002) take `--kubeconfig` and
  `--context`. Commands that are server-orchestrated (they call butler-server
  over HTTP via the login credential, e.g. `cluster certs rotate` and the
  `gitops` command group) take neither; they authenticate with the active
  `butlerctl login` / `butleradm login` credential and must not offer
  `--kubeconfig`/`--context`.
