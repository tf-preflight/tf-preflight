# Agents Playbook (Terraform Preflight)

Use this file as the team operating model for contributions to this branch.

## 1. Engineering standards

- Prefer simple, explicit, and maintainable Go code over clever abstractions.
- Keep functions small and testable; each function should have a single clear purpose.
- Prefer dependency injection for external calls (filesystem, Azure HTTP client, command execution) so behavior is easy to unit test.
- Use explicit error handling with actionable context:
  - include resource identifiers and operation name in error messages.
  - avoid silent fallbacks unless they are intentionally documented.
- Fail fast for hard validation issues (invalid input, missing required values, irreversible auth/config failure).
- Avoid side effects in pure logic. Keep parse/transform logic separate from I/O and network code.

## 2. SOLID-oriented architecture

- **Single Responsibility**: split discovery, plan parsing, check execution, reporting, and CLI orchestration into dedicated packages.
- **Open/Closed**: add new resource mappings and checks by extension (registry map/strategy), not by modifying existing unrelated logic.
- **Liskov / Interface Segregation**: model interfaces narrowly (e.g., token provider, azure query client) and keep contracts minimal.
- **Dependency Inversion**: command layer should depend on interfaces (or small abstractions) instead of concrete implementation details when practical.

## 3. DevOps / GitOps mindset

- Treat every change as infrastructure automation input: deterministic behavior, reproducible outputs, and no hidden state.
- Keep commands idempotent where possible; repeated runs should not produce different results for identical inputs.
- Prefer non-interactive execution by default (supports CI/CD pipelines).
- Keep secrets out of source control; support env-backed authentication and document required env variables.
- Make output machine-friendly (`--output json`) as the default integration target for automation.
- Keep auditability high:
  - log critical decision points (candidate extraction, resolved subscription, provider checks, quota checks, failures).
  - store JSON report artifacts in CI and include them in build logs when possible.
- Document required runtime prerequisites (terraform, az cli, permissions) in `README.md`.

## 4. Documentation requirements

- Every change must include:
  - user-visible CLI/behavior update in `README.md`.
  - rationale in commit message/title.
  - migration notes when behavior changes (especially checks, output, severity logic).
- New checks require documentation of:
  - what is checked,
  - hard-fail vs warning classification,
  - fallback/unknown behavior.
- Keep examples current for common workflows:
  - local check (`--auto-plan`)
  - CI check (`--output json`, report path).

## 5. TDD workflow

- For each functional change, add or update tests first when feasible:
  - discovery parsing tests,
  - plan merge tests,
  - check rule tests,
  - report generation tests.
- Tests must cover success path, warning path, and hard-fail path.
- For Azure query logic, use mocked HTTP responses for deterministic behavior.
- Avoid broad integration tests as the only validation; retain fast unit coverage for core logic.

## 6. Review process

- Before merge, perform structured review:
  - correctness of discovery + plan merge,
  - credential/auth handling,
  - provider namespace/resource mapping updates,
  - severity/threshold semantics.
- If an automated review agent is available, run it and address actionable findings before requesting merge.
- Keep a short checklist in PR description:
  - what changed,
  - why it changed,
  - tests run,
  - risk and rollback plan.

## 7. Branch quality gate

- No production-facing change should be merged without:
  - matching tests for new behavior,
  - updated docs,
  - explicit decision on threshold effects (`warn` vs `error` behavior),
  - review feedback addressed.
