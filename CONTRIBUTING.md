# Contributing

Thanks for your interest in Elastic GenAI SecOps. Issues and pull requests are welcome.

## Before you start

- Read the design rules in [ROADMAP.md](ROADMAP.md#design-rules): every change must keep them (LLM per
  incident only, masked input, no automatic actions, Basic license only).
- Check [ROADMAP.md](ROADMAP.md) for planned work, and open an issue before large changes.
- Record new design decisions as an ADR in [docs/architecture/adr](docs/architecture/adr/README.md).

## Development setup

Requirements: Docker (with Compose), Python 3.12, Go 1.27. Without local Go or Python, run the
checks in containers (`golang:1.27-alpine`, `python:3.12-slim`) as CI does.

```bash
# Go services and shared library (one module at the repository root)
go vet ./... && go test -race ./...

# Python services and adapters
pip install -e libs/py-common -e "integrations/outbound[dev]" -e "services/<service>[dev]" ruff
ruff check libs/py-common integrations/outbound services/<service>
(cd services/<service> && python -m pytest -q tests)

# Detection rules
python content/detection-rules/build.py --check

# Full stack and the end-to-end test
cd deploy/compose && ./init-env.sh && docker compose -f siem.yml -f platform.yml up -d --build
python ../../tests/e2e/pipeline_test.py
```

## Conventions

- English everywhere: code, comments, logs, docs, commit messages.
- Message contracts exist once per language (`libs/go-common/contracts`,
  `libs/py-common/esm_common/contracts.py`); change both together.
- No vendor SDKs in business logic; LLM providers, the bus and outbound adapters sit behind interfaces.
- Never call a billed LLM API in tests or CI; use `LLM_PROVIDER=mock`.
- PowerShell: use case-sensitive operators (`-cmatch`, `-ceq`, `-ccontains`) for identifiers;
  case-insensitive matching breaks on `I` under some cultures (for example Turkish).
- Keep secrets, internal hostnames and IP addresses out of the repository (`gitleaks` runs in CI).

## Pull requests

- Branch from `staging` and open the pull request against `staging`; `production` only takes
  promotion PRs from `staging` (see [docs/operations/ci-cd.md](docs/operations/ci-cd.md)).
- One topic per pull request, with tests for new behaviour.
- The `Pipeline gate` check must be green: lint, tests, SAST, SCA, IaC and secret scans; the
  promotion PR into `production` also runs the end-to-end test.
- Describe how you tested the change (unit, compose, Kubernetes), and what you did not test.

By contributing you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
