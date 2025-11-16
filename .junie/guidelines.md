# Project Guidelines — jf (Job Finder)

These are the working guidelines for Junie in this repository. Follow them for analysis, edits, testing, and submissions.

## 1) Project overview

jf is a Go 1.25 application that aggregates and scans job sources, with modules for:
- Scraping job boards and company sites (`internal/scrape/...`)
- Parsing/normalizing data (`internal/extract`, `internal/validators`)
- Storage and repositories (SQLite and DuckDB drivers in `internal/repo/...`)
- HTTP utilities and server (`internal/httpx`, `internal/server`, `cmd/server`)
- Utilities for performance, pooling, and metrics (`internal/utils`, `internal/pool`)

There is also infrastructure-as-code for running in AWS ECS (Terraform files in `tf/`).

## 2) Layout quick map

- cmd/server/main.go — entrypoint for the HTTP/server component
- internal/aggregators — registry and integration logic for data sources
- internal/scrape — scraping implementations (company sites, job boards, RSS)
- internal/extract — extraction helpers (company, date, etc.)
- internal/httpx — HTTP client helpers and tests
- internal/repo — repository implementations (sqlite, duckdb) and tests
- internal/server — HTTP server handlers, SSE, progress tracking, simple GUI
- internal/strutil, internal/utils — general-purpose utilities (lots of tests)
- tests/** — black-box tests by domain, mirroring internals where helpful
- config.yaml — runtime configuration
- Makefile — common test/coverage/bench targets
- tf/** — Terraform modules for deployment (ALB, ECS, ECR, VPC, etc.)

## 3) How to run the app

Local server (from project root):
- PowerShell:
  - `go run cmd/server/main.go`
  - Ensure `config.yaml` is present/valid if the server expects it.

Special utility:
- Shodan scraper targets exist in the Makefile:
  - `make shodan-scraper` (runs) or `make shodan-scraper-build` (builds)

## 4) Tests: how and when Junie should run them

Always run tests when you modify Go code. For documentation-only changes, skip tests.

Options:
- With Make (preferred if available):
  - All tests: `make test`
  - Unit tests: `make test-unit`
  - Integration tests: `make test-integration`
  - Performance-focused: `make test-performance`
  - Coverage (console): `make coverage`
  - Coverage HTML report: `make coverage-html` (produces `coverage.html`)
  - Race detector: `make race-test`
- Without Make (plain Go):
  - `go test -v -race ./...`
  - For package-specific runs, use paths like `go test -v ./internal/httpx/...`

Notes:
- Some tests exercise performance and concurrency; prefer running with `-race` where feasible.
- Repository tests target SQLite/DuckDB via pure Go libs; no external DB service is required.

## 5) Build guidance for submissions

- If your changes are Go code, run relevant tests before submitting. Building the binary is not required unless the issue asks for it explicitly.
- For documentation-only edits (like this guidelines file), do not run builds/tests.

## 6) Code style and conventions

- Follow idiomatic Go:
  - Format with `gofmt`/`go fmt ./...`
  - Keep functions cohesive and small; prefer clear names
  - Propagate errors; wrap with context where useful
  - Use existing utility packages when possible (see `internal/utils`, `internal/strutil`)
- Mirror patterns in nearby files (imports ordering, spacing, comments).
- Add unit tests alongside code in the same package, or under `tests/**` for black-box cases.
- Keep public surface minimal in `internal/**` (it’s module-internal by design).

## 7) Performance and concurrency

- Many utilities focus on pools, backpressure, and memory efficiency.
- When touching hot paths, consider adding/adjusting performance tests (`TestPerformance|TestConcurrency|TestMemory`).

## 8) Security and secrets

- Do not commit secrets. Use environment variables or configuration files excluded from VCS where appropriate. See `.aiignore` for AI tooling ignore rules.

## 9) Terraform (optional)

- The `tf/` folder contains modules to deploy on AWS ECS with ALB, ECR, VPC, CloudWatch, etc. Not required for local development or most issues.

## 10) Quick checklist for Junie before submit

- [ ] If changed Go code: run appropriate tests (unit/integration/perf as relevant)
- [ ] Ensure formatting with `go fmt ./...`
- [ ] Keep changes minimal and scoped to the issue
- [ ] Update docs/comments if behavior changed

Last updated: 2025-11-16
