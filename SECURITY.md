# Security

Terraform Preflight (`tf-preflight`) uses Azure access tokens at runtime to call Azure Management REST APIs in its Azure provider checks.

## Secure configuration

- Do not commit credentials in source control.
- Prefer environment-based tokens (`AZURE_ACCESS_TOKEN`, `ARM_ACCESS_TOKEN`, `AZURE_CLI_TOKEN`) or `az account get-access-token`.
- Use short-lived credentials where possible in CI.

## Reporting issues

- If you discover a security issue, do not open a public issue.
- Contact maintainers directly through project-maintained private reporting channels.

## Dependency hygiene

- Keep dependencies up to date and review dependency updates before merging.
- Prefer deterministic, offline-friendly tests for CI; mock external Azure calls for unit/integration tests.
