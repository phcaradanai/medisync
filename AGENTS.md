# Repository Guidelines

## Project Structure & Module Organization

MediSync is a Go and React monorepo. `services/core/` contains the modular Go backend; domain packages live under `internal/`, commands under `cmd/`, generated protobuf code under `internal/gen/`, and SQL migrations under `migrations/`. `services/sync-relay/` is a separate Go service. React/TypeScript frontends are in `apps/admin/` and `apps/kiosk/`; shared generated TypeScript contracts are in `packages/proto-ts/`. Treat `proto/` as the contract source of truth and regenerate outputs instead of editing generated files. Infrastructure is under `infra/`, repository scripts under `scripts/`, and architecture and operational guidance under `docs/` and `RUNBOOK.md`.

## Build, Test, and Development Commands

- `npm install`: install root workspaces (Node 20+).
- `npm run dev:all`: start Docker infrastructure and both Vite frontends.
- `npm run core`: run the core Go API locally; `npm run infra:up` starts its PostgreSQL and NATS dependencies.
- `npm run build`: type-check and build all workspaces.
- `npm run proto`: lint protobuf definitions and regenerate Go/TypeScript bindings.
- `npm run test:core`: run all core Go tests.
- `npm test -w apps/admin` or `npm test -w apps/kiosk`: run Vitest UI tests.
- `npm run test:all`: run Buf lint, frontend builds, Go unit tests, and optional integration tests. Set `TEST_DATABASE_URL` to enable its database suite.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use lowercase package names and idiomatic `PascalCase`/`camelCase` identifiers. TypeScript is strict and uses two-space indentation, `PascalCase` for React components, and `camelCase` for functions and variables. Keep domain logic within its matching `services/core/internal/<domain>` package. Name protobuf packages and files by versioned domain, for example `medisync/inventory/v1/inventory.proto`.

## Testing Guidelines

Go tests use the standard `testing` package and end in `_test.go`; database-dependent suites use `_integration_test.go` and the `integration` build tag. Frontend tests use Vitest and Testing Library under `src/__tests__/` with `*.test.tsx` names. Add focused tests for behavior changes; no numeric coverage threshold is configured.

## Commit & Pull Request Guidelines

Recent history follows Conventional Commits such as `feat(audit): ...` and `perf(kiosk): ...`. Use an imperative subject with a meaningful scope. Pull requests should explain the user-visible and architectural impact, list validation commands, link relevant issues, and include screenshots for admin or kiosk UI changes. Call out migrations, new environment variables, and regenerated protobuf artifacts explicitly.

## Security & Configuration

Copy values from `.env.example` or `.env.production.example`; never commit secrets or local environment files. Use fake printing and vending clients for development where appropriate, and review `RUNBOOK.md` before deployment changes.
