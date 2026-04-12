# Changelog

## Unreleased

- Add `tf-preflight reconcile` as a read-only import-gap detection stage before `scan`, with exact `terraform import` recommendations for supported Azure resource types.
- Add dedicated reconcile reporting, JSON output, CLI validation, and tests covering candidate filtering, import ID generation, Azure probe outcomes, and auto-plan execution.
- Add progress and verbose output in the CLI flow (`tf-preflight scan`) for long-running checks.
- Add module import validation against local `source` paths and root `modules/` directory.
- Add direct Azure REST behavior metadata (`tf-preflight version`) including query backend and dependency details.
- Improve install and run guidance for binary installation, including `make install`/`make install-system` and curl bootstrap.
- Complete remote curl bootstrap commands in docs/installer messaging with concrete upstream repo defaults and fork fallback examples.
- Default installer repo when `PRE_FLIGHT_REPO` is unset (`tf-preflight/tf-preflight`), enabling one-command upstream bootstrap without env vars.
- Add `agents.md` to document engineering standards (SOLID, DevOps/GitOps, TDD, review expectations).
- Fix HCL discovery parsing to tolerate nested provider config blocks (for example provider `features`) by using partial content parsing and best-effort extraction.
- Fix HCL discovery behavior to resolve `var`/`local` expressions and function calls (`format`, `join`, `lower`, `upper`) reliably, including the CLI-preflight plan bootstrap path.
- Fix bootstrap installer to resolve `latest` to an actual GitHub release tag and fallback to source install when release assets are unavailable.
- Add interactive scan mode (`--interactive`) with guided module discovery, plan selection, verbose default, module warning filtering, and preflight confirmation before Azure checks.
- Add pre-run human-readable summary output for text mode in both interactive and non-interactive paths (candidate/type/action breakdown, key candidates, and blocking/optional findings) before Azure checks.
- Document verification by recording that the complete test suite passes after summary changes.

## 1.0.0

- Initial implementation of the Go-based Azure preflight checker:
  - Terraform directory and plan discovery.
  - Plan-backed candidate extraction.
  - Azure REST checks (subscription locations, provider registration, quotas where mapped, existence probing).
  - Text and JSON reporting with severity thresholds.
  - CLI support for `--auto-plan`, `--plan`, and output/report options.
  - Install wrapper script for local and release bootstrap.
