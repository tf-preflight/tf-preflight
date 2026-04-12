# Changelog

## Unreleased

- Add progress and verbose output in the CLI flow (`tf-preflight scan`) for long-running checks.
- Add module import validation against local `source` paths and root `modules/` directory.
- Add direct Azure REST behavior metadata (`tf-preflight version`) including query backend and dependency details.
- Improve install and run guidance for binary installation, including `make install`/`make install-system` and curl bootstrap.
- Complete remote curl bootstrap commands in docs/installer messaging with concrete upstream repo defaults and fork fallback examples.
- Add `agents.md` to document engineering standards (SOLID, DevOps/GitOps, TDD, review expectations).

## 1.0.0

- Initial implementation of the Go-based Azure preflight checker:
  - Terraform directory and plan discovery.
  - Plan-backed candidate extraction.
  - Azure REST checks (subscription locations, provider registration, quotas where mapped, existence probing).
  - Text and JSON reporting with severity thresholds.
  - CLI support for `--auto-plan`, `--plan`, and output/report options.
  - Install wrapper script for local and release bootstrap.
