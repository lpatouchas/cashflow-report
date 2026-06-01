# Cashflow Report — Agent Instructions


## Architecture

This project follows **Clean Architecture** as described in [pkritiotis.io/clean-architecture-in-golang](https://pkritiotis.io/clean-architecture-in-golang/): group-by-layer at the top, group-by-feature within each layer.

### Three Layers (dependency rule: inward only)

```
domain/ (innermost) ← app/ ← infra/ (outermost)
```

- **`internal/domain/`** — Pure business logic. Zero external imports. Contains entities, value objects, repository interfaces, and domain errors. No `json` or `db` struct tags.
- **`internal/app/`** — Application services (use cases). Depends only on domain. Accepts **app-defined commands / inputs** from callers, constructs domain entities internally, calls repositories. Contains business logic, ownership checks, password hashing, input sanitization, JWT signing. Grouped by feature: `auth/`, `user/`, `review/`.
- **`internal/infra/`** — Infrastructure. Depends on app and domain. Contains:
    - `http/` — Inbound handlers (Chi router, middleware, DTOs with `json`/`validate` tags)
    - `storage/postgres/` — Outbound providers (repository implementations, row models with `db` tags)
    - `metrics/` — Prometheus registry

### Key Architectural Rules

1. **No cross-layer struct sharing.** Each layer has its own structs. Conversion happens at the boundary.
    - Domain entities: no tags, constructed via builders
    - Handler DTOs (`infra/http/dto/`): `json` + `validate` tags (HTTP transport only)
    - Application use-case inputs (`internal/app/<feature>/`, e.g. command structs): transport-agnostic; **defined in app** so `app` never imports `infra/http/dto`
    - Repository models (`infra/storage/postgres/model/`): `db` tags for pgx
2. **Handlers do not pass domain entities into application services (inbound writes).** Decode requests to **handler DTOs** only. Map `dto` → **app-layer command / input structs** (same feature package as the service), then call the service. **Application services** load aggregates from repositories as needed, validate, sanitize, and **build domain entities** via builders and value objects. Repositories still expose **domain** types only; handlers **never** construct `internal/domain/...` values for write use cases—**infra maps DTO → app input → service owns domain**. For **responses**, handlers may map domain results to response DTOs. The app layer **must not import** `infra/http/dto` or storage row models (inward dependency rule). *Exception / legacy: review create/update still builds `*review.Review` in `infra/http` and passes it to the service—Known issue #4; new code must follow the command pattern.*
3. **Repository public API returns domain entities.** Row model conversion is internal to the repository.
4. **Builder pattern on domain entities only** (especially `Review` which has optional fields).
5. **Per-layer wiring** via `services.go` in `app/` and `infra/` — keeps `main.go` clean.
6. **Mocks live next to the interface they mock**, not next to tests (e.g. `domain/review/mock_repository.go`).
7. **Keep HTTP handlers thin.** Decode and validate transport DTOs, extract auth and route/query params, **map DTO → app-layer command**, invoke the application service, map errors and marshal responses. **Do not** construct domain entities or encode use-case merge rules in handlers—those stay in `internal/app` (see rule 2). Small **DTO→command** mappers in `infra/http` are fine; they must not return `internal/domain` types for writes.
8. **Keep SQL out of repository methods.** Do not inline query strings inside repository methods; store SQL in dedicated `*_queries.go` files in the same package and reference constants from repository code.
9. **Instrument cross-cutting concerns with decorators, not inline calls.** For Prometheus (or similar), wrap the type that implements the interface: an outer struct holds `inner` (the real implementation), implements the same interface by delegating to `inner`, and records duration/errors around each method. Never import observability clients in `internal/domain/` or `internal/app/`; wrappers live in `internal/infra/` and are wired in `infra/services.go` (see Metrics below).

### Validation — Two Layers

- **Infra HTTP** (`go-playground/validator` struct tags on DTOs): catches missing fields, type/range errors → `400 Bad Request`
- **Domain** (value object constructors, `ReviewBuilder.Build()`): enforces business invariants (0.5 increment, lat/lng pairing) → `422 Unprocessable Entity`

### Error Handling

- Domain errors (`ErrNotFound`, `ErrForbidden`, `ErrAlreadyExists`, `ValidationError`) defined in `internal/domain/errors.go`
- Errors flow upward unchanged through layers
- Only `infra/http` maps domain errors to HTTP status codes via `mapServiceError()`
- 500 errors: log the real error, return generic message to client

### Authentication

- Username/password signup, bcrypt hashing (cost 10) in app layer
- JWT (HS256) via `Authorization: Bearer <token>` header
- Auth middleware on all `/api/v1/*` routes
- Failed login returns generic "invalid email or password" — never leaks email existence

### Response Shapes

- **2xx single:** return resource directly as JSON
- **2xx list:** `{ items: [...], total, page, per_page, total_pages }`
- **4xx/5xx:** `{ code: "ERROR_CODE", message: "...", details?: [...] }`

### Logging

- `log/slog` structured logging. JSON in production, text in dev.
- Request ID propagated via `context.Context` (like Java MDC)
- Never log passwords, JWT tokens, or full request bodies

### Metrics

- **HTTP:** `infra/http/middleware` — request count, duration, in-flight, sizes (registered on the app registry).
- **Database repositories:** **decorator pattern** in `internal/infra/storage/postgres/` — types like `MetricsUserRepository` / `MetricsReviewRepository` implement the same `domain/*/Repository` interface as the Postgres repos, delegate every call to an `inner` repository, and observe `db_query_duration_seconds` and `db_query_errors_total` (labels `method`, `entity`). Constructors: `NewDBQueryMetrics`, `NewMetrics*Repository`. Composition in `infra/services.go`: `NewMetrics*(New*Repository(pool), dbm)`.
- **Application services (use cases):** use the **same decorator pattern** when you need per-use-case metrics (latency, errors, or custom counters). Add `Metrics*Service` types under `internal/infra/` (e.g. next to HTTP or storage wiring) that implement `app/<feature>.Service`, wrap the concrete service from `app.NewServices`, and register dedicated metric vectors on the Prometheus registry. Do not add Prometheus or timing code inside `internal/app/*` service implementations.
- **Pool:** background collector in `postgres/pool_metrics.go` for pgx pool stats.
- **System:** Go runtime + process collectors via `infra/metrics.NewRegistry()` (automatic).

## Tech Stack

- **Module:** `github.com/lpatouchas/cashflow-report`
- **Language:** Go 1.23
- **Router:** chi/v5
- **Database:** PostgreSQL via pgx/v5
- **Migrations:** golang-migrate
- **Testing:** testify + testcontainers-go
- **Metrics:** prometheus/client_golang
- **API docs:** swaggo/swag + http-swagger

## Testing Requirements

- **close to 100% test coverage** — CI fails below this threshold, but keep it realistic
- **TDD (Red-Green-Refactor)** for all new features
- **All tests must be table-driven** (use test case structs/slices with subtests).
- **Hand-written testify mocks** (no code generation tools)
- Domain: unit test value objects and builders
- App: unit test with mocked repository interfaces
- Infra HTTP: httptest with mocked service interfaces
- Infra storage: integration tests with testcontainers-go (real PostgreSQL)

## Code Style

- Idiomatic Go: follow standard Go conventions (effective Go, Go proverbs)
- Comments only on methods with non-obvious business logic — do not narrate what code does
- No `json`, `db`, or framework tags on domain entities
- Always use `context.Context` as first parameter in interface methods

## Known issues

## Plans

Project plans are stored in `docs/plans/` with the naming format `yyyy-mm-dd_<descriptive-name>.md`.

When creating a new plan:
1. Save it to `docs/plans/` following the naming convention
2. Include date, status, and overview at the top
3. Document key decisions and their rationale
4. Add a reference to the list below
5. **Run a plan review pass before declaring it ready.** Once a plan looks complete, deliberately re-read it as a critic and surface issues before any code is written. At minimum, check for:
    - Missing edge cases (empty/null inputs, error/loading states, offline, auth failure, rate limits, quota).
    - Integration gotchas with third-party SDKs/APIs (deprecated APIs, version-specific behavior, billing/cost surface, required Cloud Console setup, custom-element TS typing).
    - Hidden cross-cutting concerns (theming/dark mode, accessibility, i18n, CSP, observability, telemetry).
    - Architectural consistency with this file (clean-architecture layers, decorator pattern for cross-cutting concerns, two-layer validation, SQL kept out of repo methods, etc.).
    - Test plan completeness (happy path, error path, fallback when an optional dep is missing, mock strategy for external SDKs).
    - Backwards compatibility / data migration implications, and whether a deferred item (e.g. a missing column) will be expensive to add later.
    - Documentation impact: `README.md`, `.env.example`, `web/README.md`, Swagger annotations, and any Makefile target the change touches.

   Update the plan file in place with whatever the review pass uncovered. Only after that pass is the plan ready to execute.

- [Finance Report MVP](docs/plans/2026-05-29_finance-report-mvp.md) — CLI that summarises bank CSVs into an HTML report, excluding inter-account transfers.
- [Monthly Average](docs/superpowers/plans/2026-05-30-monthly-average.md) — per-month average (income/expenses/savings) over the data's full calendar span, shown as a second row of cards in the HTML report.
- [Month Transaction Detail](docs/superpowers/plans/2026-05-30-month-transaction-detail.md) — click a month row to open a modal of that month's transactions, one line each, sortable by column.

## Branching

Each plan is implemented on its own branch using gitflow semantics so `main` stays releasable and reviews are scoped:

- Branch from up-to-date `main`: `git checkout -b feature/<short-name>`.
- `<short-name>` mirrors (or shortens) the plan's filename slug — e.g. `feature/google-maps-location-picker` for `docs/plans/2026-05-07_google-maps-location-picker.md`. One branch per plan; follow-up tweaks that extend the same plan (e.g. read-only map on the detail page) ride on that branch until it merges.
- **Place / subject roadmap (`docs/plans/2026-05-08_place-review-grouping.md`):** implement each sub-phase in a **git worktree** under **`.worktrees/<branch-suffix>/`** (e.g. `feature/subject-1.7-write-service-dual-shape` → `.worktrees/subject-1.7-write-service-dual-shape`). Keep the **root clone on `main`** while the feature branch is checked out only in that worktree. See the plan’s **Phase boundary checklist** and Lessons Learned for commands.
- Use `fix/<short-name>` for bug fixes or workflow changes that don't have a plan, and `chore/<short-name>` for tooling-only changes.
- Open the PR against `main` only after the workflow below is green (tests, coverage, code-review pass, docs).
- Never commit feature work directly to `main`. If you started on `main` by mistake, branch off before committing (`git checkout -b feature/<name>` on dirty worktree carries the changes over).

## Lessons Learned

## Git Commit Message Template

Use `.gitmessage-template.txt` as the canonical commit message structure when creating commits in this repository.

## Updating This File

Update `AGENTS.md` when any of the following occur:
- **`README.md`** should stay accurate for local development and common commands; treat mismatches as bugs to fix alongside code changes.
- A new architectural decision is made (e.g. new layer, new pattern, new convention)
- A new technology or dependency is added to the stack
- A new coding convention or rule is established
- The project structure changes significantly
- A new plan is created (add reference under Plans section)
- A lesson is learned during implementation
- Known issues are added or resolved (update **Known issues**)
- Testing strategy changes

Keep this file concise and actionable. It should be a quick reference, not a full design document — link to `docs/plans/` for detailed plans.