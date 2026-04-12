# Contributing to tf-preflight

Thanks for contributing.

## Development setup

- Install Go 1.21+
- Install Terraform CLI for local tests when using `--auto-plan`
- Ensure Azure CLI (`az`) is available when running Azure integration checks

## Common workflows

- Fork or branch from the current working branch.
- Make changes in focused commits.
- Run locally before opening a PR:

```bash
gofmt -w ./cmd ./internal
go test ./...
```

- Keep CLI output and error messages deterministic and user/actionable.

## Coding expectations

- Keep packages small and cohesive.
- Use TDD when adding behavior (unit tests first, then implementation).
- Prefer direct, explicit error handling with context.
- Avoid introducing heavy coupling between discovery, plan parsing, Azure checks, and reporting.

## PR checklist

- [ ] Changelog entry added in `CHANGELOG.md` under `Unreleased` or next version.
- [ ] Tests updated/added and passing (`go test ./...`).
- [ ] Documentation updated (`README.md`, and other relevant docs).
- [ ] New checks include severity behavior (`error`/`warn`) and failure impact.

## Release notes

- Bump/change the relevant release notes in `CHANGELOG.md` before merge when behavior changes.
