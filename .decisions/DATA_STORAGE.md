---
title: Data Storage Guide
description: "raw SQL vs ORM, pgx driver, database/sql, connection pooling, pgxpool configuration, pool sizing, cloud database limits, PgBouncer, DNS failover, repository pattern, interface at consumer, concrete implementation, model struct tags, sentinel errors, PostgreSQL error codes, schema migrations, atlas, migration file naming, transactional DDL, transaction management, DBTX interface, WithTx helper, isolation levels, deadlock prevention, long-running transactions, idempotency, anti-patterns"
weight: 28
---

# Data Storage Guide

> This guide is the authoritative source for data persistence patterns in this
> application.
>
> It complements [`ARCHITECTURE_GUIDE.md`](./ARCHITECTURE_GUIDE.md) (project
> structure, wiring, and shutdown), [`ERROR_HANDLING.md`](./ERROR_HANDLING.md)
> (error taxonomy, retry, and resilience patterns), and
> [`CODE_REVIEW.md`](./CODE_REVIEW.md) (review checklists).
>
> Read this together with [`CLAUDE.md`](../CLAUDE.md) for behavioral rules.
>
> Use this guide to answer:
>
> - which database driver or ORM to use
> - how to size and monitor connection pools
> - how to write and manage schema migrations
> - how to structure transactions across service and repository layers
> - how to map database errors to domain errors
> - what patterns to avoid

---

## 0. Quick Reference

- §1 Driver choice: raw SQL vs ORM decision, pgx driver, connection setup
- §2 Connection pooling: pool settings, sizing formula, pgxpool configuration, health checks
- §3 Cloud databases: connection limits by provider, PgBouncer, DNS failover
- §4 Repository pattern: interface at consumer, concrete implementation, sentinel errors, error code mapping
- §5 Schema migrations: atlas tool, file naming, format, transactional DDL, safe practices, CI/CD
- §6 Transaction management: service-layer transactions, DBTX interface, WithTx helper, isolation levels, deadlock prevention
- §7 Anti-patterns: common data access mistakes to avoid
- §8 Review checklist

## 1. Choosing Your Approach

### 1a. Raw SQL vs ORM Trade-off Analysis

| Criterion | Raw SQL (pgx / sqlx) | ORM (Ent / GORM) |
|---|---|---|
| Query control | Full — write exact SQL | Limited — generated or builder-based |
| PostgreSQL-specific features | Native (`LISTEN/NOTIFY`, `COPY`, advisory locks, custom types) | Partial — depends on dialect support |
| Performance tuning | Direct `EXPLAIN ANALYZE`, index hints | Abstracted — harder to inspect generated queries |
| Type safety | Manual mapping (struct tags or scan) | Generated code provides compile-time safety |
| Schema evolution | Manual migrations | ORM-managed or manual migrations |
| Learning curve | SQL knowledge required | Framework-specific DSL |
| Prototyping speed | Slower for CRUD | Faster for standard CRUD patterns |
| Debugging | Readable — the query is what you wrote | Indirect — must inspect generated SQL |

### 1b. When to Use Raw SQL

- Complex queries with joins, window functions, CTEs, or recursive queries
- Full performance control over query plans and index usage
- PostgreSQL-specific features: advisory locks, `LISTEN/NOTIFY`, custom types,
  full-text search, `COPY` protocol
- When the team already has strong SQL skills and prefers explicit control

### 1c. When to Use an ORM

- Type-safe generated query builders reduce mapping bugs in CRUD-heavy domains
- Rapid prototyping where schema iteration speed matters more than query control
- Teams that prefer a Go-native DSL over writing SQL directly

### 1d. PostgreSQL Driver Recommendations (pgx, lib/pq)

**pgx** is the recommended PostgreSQL driver. It provides:

- native PostgreSQL wire protocol (no `database/sql` overhead by default)
- connection pooling via `pgxpool`
- support for `COPY`, `LISTEN/NOTIFY`, large objects, and custom types
- `pgx/v5/stdlib` adapter for `database/sql` compatibility when needed

**sqlx** is appropriate when:

- multi-database store support is a requirement
- the project already depends on `database/sql` and benefits from `sqlx`
  extensions (`NamedExec`, `StructScan`, `Select`, `Get`)

### 1e. ORM Recommendations (sqlc, gorm, ent)

**Ent** is preferred over GORM for new projects:

- generates type-safe Go code from a schema graph
- explicit edge (relationship) definitions
- built-in support for hooks, policies, and interceptors
- cleaner migration integration

**GORM** is acceptable for existing projects already using it but is not
recommended for greenfield work due to implicit behavior, magic struct tags,
and less predictable query generation.

---

## 2. Connection Pool Management

### 2a. How database/sql Connection Pooling Works

When code calls `db.QueryContext`, the pool follows this sequence:

1. Check for an idle connection in the pool
2. If none available and under `MaxOpenConns`, open a new connection
3. If at `MaxOpenConns`, block until a connection is returned or context expires

### 2b. Connection Pool Configuration Settings

| Setting | Purpose | Default | Guidance |
|---|---|---|---|
| `MaxOpenConns` | Maximum simultaneous open connections | Unlimited | Always set explicitly — unlimited causes connection exhaustion under load |
| `MaxIdleConns` | Maximum idle connections retained | 2 | Set equal to `MaxOpenConns` to avoid connection churn |
| `ConnMaxLifetime` | Maximum time a connection stays open | Unlimited | Set to 30-60 minutes to rotate connections and respect DNS/failover changes |
| `ConnMaxIdleTime` | Maximum time an idle connection stays in pool | Unlimited | Set to 5-10 minutes to release unused connections during low traffic |

### 2c. Pool Size Calculation Formula

```
MaxOpenConns = (database max_connections - reserved_connections) / app_instances
```

Reserve connections for:

- superuser access (maintenance, migrations)
- monitoring agents
- connection poolers (PgBouncer)
- replication slots

### 2d. Workload-Based Pool Sizing Guidance

| Workload | MaxOpenConns | MaxIdleConns | ConnMaxLifetime | ConnMaxIdleTime |
|---|---|---|---|---|
| Low traffic (< 100 req/s) | 10 | 10 | 30m | 10m |
| Medium traffic (100-1000 req/s) | 25 | 25 | 30m | 5m |
| High traffic (> 1000 req/s) | 50 | 50 | 15m | 5m |
| Batch / background jobs | 5 | 5 | 60m | 15m |

These are starting points. Tune based on observed pool wait times and database
connection counts.

### 2e. pgx Native Pooling vs database/sql Comparison

| Feature | `pgxpool.Pool` | `database/sql` |
|---|---|---|
| Protocol | Native PostgreSQL wire protocol | Generic `database/sql` interface |
| Connection lifecycle | Automatic health checks on acquire | No built-in health check |
| `COPY` protocol | Supported | Not supported |
| `LISTEN/NOTIFY` | Supported natively | Requires workarounds |
| Custom types | Full support via `pgtype` | Limited |
| Prepared statements | Per-connection automatic | Driver-dependent |
| `database/sql` compatibility | Via `pgx/v5/stdlib` adapter | Native |
| Pool statistics | `pgxpool.Stat` | `sql.DBStats` |

Use `pgxpool.Pool` directly when the project is PostgreSQL-only and benefits
from native features. Use `pgx/v5/stdlib` when `database/sql` compatibility is
required for libraries like sqlx or existing middleware.

### 2f. pgxpool Configuration and Recommended Settings

```go
config, err := pgxpool.ParseConfig(databaseURL)
if err != nil {
    return fmt.Errorf("db: parse config: %w", err)
}

config.MaxConns = 25
config.MinConns = 5
config.MaxConnLifetime = 30 * time.Minute
config.MaxConnIdleTime = 5 * time.Minute
config.HealthCheckPeriod = 30 * time.Second

pool, err := pgxpool.NewWithConfig(ctx, config)
if err != nil {
    return fmt.Errorf("db: connect: %w", err)
}
```

### 2g. Connection Pool Health Check Pattern

**pgxpool** performs automatic health checks at a configurable interval
(`HealthCheckPeriod`). Additionally, expose a manual health endpoint:

```go
func HealthCheck(ctx context.Context, pool *pgxpool.Pool) error {
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()
    return pool.Ping(ctx)
}
```

### 2h. Connection Pool Monitoring and Metrics

**`database/sql` stats** — call `db.Stats()` periodically and export:

| Metric | Meaning | Healthy | Warning |
|---|---|---|---|
| `InUse` | Connections currently in use | < 70% of MaxOpenConns | > 80% |
| `WaitCount` | Total blocked requests waiting for a connection | Low / stable | Growing |
| `WaitDuration` | Cumulative time spent waiting | < 1ms average | > 10ms average |
| `MaxIdleClosed` | Connections closed due to `ConnMaxIdleTime` | Expected under variable load | Excessive churn indicates misconfigured idle settings |
| `MaxLifetimeClosed` | Connections closed due to `ConnMaxLifetime` | Steady rotation | Spikes indicate too-short lifetime |

**pgxpool stats** — call `pool.Stat()`:

| Metric | Meaning | Healthy | Warning |
|---|---|---|---|
| `AcquireCount()` | Total connection acquisitions | Steady growth | N/A |
| `AcquireDuration()` | Cumulative acquire wait time | Low per-request average | High average indicates pool exhaustion |
| `AcquiredConns()` | Currently acquired connections | < 70% of MaxConns | > 80% |
| `IdleConns()` | Currently idle connections | > 0 | 0 sustained under load |
| `TotalConns()` | Total connections (acquired + idle) | Stable | Fluctuating wildly |
| `EmptyAcquireCount()` | Acquires that found no idle connection | Low | Growing indicates insufficient MinConns |

---

## 3. Cloud Database Considerations

### 3a. Connection Limits by Cloud Provider

| Provider | Plan | Approximate max_connections |
|---|---|---|
| AWS RDS | db.t3.micro | 66 |
| AWS RDS | db.t3.medium | 150 |
| AWS RDS | db.r5.large | 700 |
| Cloud SQL | db-f1-micro | 25 |
| Cloud SQL | db-custom-1-3840 | 100 |
| Supabase | Free | 60 |
| Supabase | Pro | 200 |
| Neon | Free | 100 |
| Neon | Pro | 300-500 |
| Railway | Starter | 100 |

Always check the actual provider documentation for the current plan. These
numbers change.

### 3b. PgBouncer Pool Mode and Configuration Adjustments

When running behind PgBouncer in **transaction mode**:

- Disable prepared statements — PgBouncer in transaction mode does not support
  them:

```go
config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
```

- Do not use `SET` or session-level state — PgBouncer reassigns connections
  between transactions
- Advisory locks do not work across transaction boundaries in transaction mode
- `LISTEN/NOTIFY` requires a dedicated connection outside the pooler

### 3c. DNS-Based Failover for Cloud Databases

Cloud databases use DNS for primary/replica failover. Set a short
`ConnMaxLifetime` so connections rotate and pick up DNS changes:

```go
config.MaxConnLifetime = 1 * time.Minute
```

Without this, connections pin to a stale IP after failover and the application
continues sending traffic to a decommissioned node.

---

## 4. Repository Pattern

### 4a. Repository Interface Defined at the Consumer

Define the store interface where it is consumed, not where it is implemented.
This keeps the consumer testable without importing the concrete store:

```go
// In the service package — the consumer owns the interface
type OrderStore interface {
    GetByID(ctx context.Context, id uuid.UUID) (Order, error)
    Create(ctx context.Context, order Order) error
    ListByUser(ctx context.Context, userID uuid.UUID) ([]Order, error)
}
```

### 4b. Concrete Repository Implementation

The repository implements the interface with the chosen driver:

```go
type pgOrderStore struct {
    pool *pgxpool.Pool
}

func NewOrderStore(pool *pgxpool.Pool) *pgOrderStore {
    return &pgOrderStore{pool: pool}
}

func (s *pgOrderStore) GetByID(ctx context.Context, id uuid.UUID) (Order, error) {
    row := s.pool.QueryRow(ctx,
        `SELECT id, user_id, status, total, created_at
         FROM orders WHERE id = $1`, id)

    var o Order
    err := row.Scan(&o.ID, &o.UserID, &o.Status, &o.Total, &o.CreatedAt)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return Order{}, ErrNotFound
        }
        return Order{}, fmt.Errorf("orders: get by id: %w", err)
    }
    return o, nil
}
```

### 4c. Model Struct and Database Tag Conventions

Use `db` tags for column mapping when using sqlx:

```go
type Order struct {
    ID        uuid.UUID `db:"id"`
    UserID    uuid.UUID `db:"user_id"`
    Status    string    `db:"status"`
    Total     int64     `db:"total"`
    CreatedAt time.Time `db:"created_at"`
}
```

With pgx, use explicit `Scan` calls or `pgx.RowToStructByName` with matching
field names. Do not rely on implicit struct mapping without explicit tags or
scan targets.

### 4d. Domain Sentinel Errors for Repository Failures

Define domain error sentinels in the domain package, not the repository
package:

```go
var (
    ErrNotFound = errors.New("orders: not found")
    ErrConflict = errors.New("orders: conflict")
)
```

### 4e. PostgreSQL Error Code to Sentinel Error Mapping

Map database error codes to domain errors in the repository layer. The
repository is the only layer that should know about database-specific error
codes:

```go
import "github.com/jackc/pgx/v5/pgconn"

func mapPgError(err error) error {
    var pgErr *pgconn.PgError
    if !errors.As(err, &pgErr) {
        return err
    }
    switch pgErr.Code {
    case "23505": // unique_violation
        return ErrConflict
    case "23503": // foreign_key_violation
        return fmt.Errorf("orders: referenced record not found: %w", ErrNotFound)
    default:
        return err
    }
}
```

Use this mapper in every repository method that performs writes:

```go
func (s *pgOrderStore) Create(ctx context.Context, order Order) error {
    _, err := s.pool.Exec(ctx,
        `INSERT INTO orders (id, user_id, status, total, created_at)
         VALUES ($1, $2, $3, $4, $5)`,
        order.ID, order.UserID, order.Status, order.Total, order.CreatedAt)
    if err != nil {
        return fmt.Errorf("orders: create: %w", mapPgError(err))
    }
    return nil
}
```

The handler layer maps domain errors to transport responses. The repository
layer maps database errors to domain errors. No layer should skip this
translation step.

---

## 5. Schema Migrations

### 5a. Migration Tool Selection (atlas)

Use **goose** (via `github.com/go-sum/db/migrate`) as the standard migration
tool. It provides:

- PostgreSQL support with transactional DDL by default
- Single-file migrations with `-- +goose Up` / `-- +goose Down` annotations
- Pre-flight lint before applying (`migrate.Lint`)
- Schema fingerprint storage after each successful migration
- CLI and programmatic usage via `pkg/db/cli`

### 5b. Migration File Naming Convention

```
{version}_{description}.sql
```

- Version: 6-digit zero-padded sequential number
- Description: lowercase with underscores, describes the change
- Both the `Up` and `Down` directions live in the **same file**, separated by
  goose annotations

Examples:

```
000001_initial_schema.sql
000002_add_email_index.sql
000003_create_orders_table.sql
```

### 5c. Migration File Format and Structure

Each migration file contains both directions delimited by goose annotations:

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS users;
```

For statements that contain semicolons internally (functions, triggers,
procedures), wrap them in `StatementBegin` / `StatementEnd` so goose does not
split on the internal semicolons:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS update_updated_at();
```

### 5d. Always Write Up and Down Migrations

Every migration must contain both a `-- +goose Up` and a `-- +goose Down`
section. The `Down` section must precisely reverse the `Up` section. Write the
`Down` SQL manually — do not commit auto-generated Down SQL without reviewing
it.

### 5e. Migration Idempotency Requirements

Use `IF NOT EXISTS` and `IF EXISTS` to make migrations safe for re-execution:

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS orders (
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    status  TEXT NOT NULL,
    total   BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE IF EXISTS orders;
```

### 5f. Transactional DDL for Safe Schema Changes

PostgreSQL supports transactional DDL — schema changes within a transaction
either all commit or all roll back. Goose wraps each migration in a transaction
by default. Do not disable this unless the migration explicitly requires it
(e.g., concurrent index creation).

### 5g. Running Migrations Programmatically at Startup

Use the `github.com/go-sum/db/migrate` package directly:

```go
import "github.com/go-sum/db/migrate"

func RunMigrations(ctx context.Context, dsn, migrDir string) error {
    return migrate.Up(ctx, dsn, migrDir)
}
```

`migrate.Up` runs all pending migrations in order. It is safe to call on every
startup — it is a no-op when no migrations are pending.

### 5h. Migration CLI Usage

The project CLI (`db migrate`, `db rollback`, `db status`) wraps the
`github.com/go-sum/db/migrate` package. Run it from `starter/`:

```bash
# Apply all pending migrations
go run ./cmd/db migrate

# Preview pending migrations without applying
go run ./cmd/db migrate --dry-run

# Migrate up to a specific version
go run ./cmd/db migrate --to 3

# Rollback the last applied migration
go run ./cmd/db rollback

# Roll back to a specific version
go run ./cmd/db rollback --to 2

# Show applied / pending status
go run ./cmd/db status
```

A pre-flight lint runs automatically before `migrate` applies any SQL. If lint
finds errors, no migrations are applied.

### 5i. Safe Migration Practices for Production

**Concurrent index creation** — large tables require `CREATE INDEX
CONCURRENTLY` to avoid locking reads and writes. This statement cannot run
inside a transaction. Opt the migration out of transactional wrapping with the
`NO TRANSACTION` annotation and include only the index statement:

```sql
-- +goose NO TRANSACTION

-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_email ON users (email);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_users_email;
```

**Separate data migrations from schema migrations** — do not mix DDL
(table/column changes) with DML (data backfills) in the same migration file.
Schema migrations are typically fast and transactional. Data migrations may be
long-running and require different handling.

**Zero-downtime column changes** — use a three-step approach across separate
deployments:

1. **Add new** — add the new column as nullable, deploy code that writes to
   both old and new columns
2. **Backfill** — populate the new column from the old column for existing rows,
   deploy code that reads from the new column
3. **Remove old** — drop the old column, deploy code that no longer references
   it

Never rename or remove a column in a single deployment when the old code is
still running.

### 5j. Schema Fingerprint Verification

After each successful `migrate` run, the CLI stores a schema fingerprint in
the database via `db.StoreFingerprint`. On application startup,
`db.VerifyFingerprint` compares the running code's expected fingerprint against
the stored value and refuses to start if they diverge. This prevents schema
drift from causing silent data corruption.

### 5k. Migration Authoring Rules

- Never modify an already-applied migration — create a new migration to
  correct a previous one
- Write `Down` SQL manually — never commit auto-generated rollback SQL without
  reviewing it
- Version-control all migration files alongside application code
- Review migrations in pull requests with the same rigor as application code
- Test migrations against a copy of production data when feasible
- Include migration review in the deployment checklist

### 5l. CI/CD Migration Integration

- Run migrations in the test pipeline against a clean database before running
  tests — this validates that all migrations apply cleanly from zero
- In the CD pipeline, run migrations before deploying the new application
  version
- Never run migrations manually in production when an automated pipeline exists

---

## 6. Transaction Management

### 6a. Service-Layer Transaction Ownership

Transactions are a service-layer concern, not a repository concern. Stores do
not own transaction lifecycle — they accept a transaction handle from the
caller.

This is critical because:

- a single business operation may span multiple stores
- only the service knows which operations must be atomic
- stores that own their own transactions cannot be composed

### 6b. DBTX Interface Pattern for Transaction Abstraction

Define an interface satisfied by both `*sql.DB` and `*sql.Tx` (or
`pgxpool.Pool` and `pgx.Tx`). Store methods accept this interface:

```go
// DBTX is satisfied by *sql.DB, *sql.Tx, *pgxpool.Pool, and pgx.Tx
type DBTX interface {
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
```

For pgx:

```go
type DBTX interface {
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
```

Store constructors accept the interface, and callers supply either the pool or
a transaction:

```go
type pgOrderStore struct {
    db DBTX
}

func NewOrderStore(db DBTX) *pgOrderStore {
    return &pgOrderStore{db: db}
}
```

### 6c. WithTx Helper for Transaction Wrapping

Provide a helper that manages begin, commit, and rollback:

```go
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
    tx, err := pool.Begin(ctx)
    if err != nil {
        return fmt.Errorf("db: begin tx: %w", err)
    }
    defer func() {
        if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
            slog.ErrorContext(ctx, "db: rollback failed", "error", err)
        }
    }()

    if err := fn(tx); err != nil {
        return err
    }
    if err := tx.Commit(ctx); err != nil {
        return fmt.Errorf("db: commit tx: %w", err)
    }
    return nil
}
```

Usage in a service:

```go
func (s *OrderService) PlaceOrder(ctx context.Context, input PlaceOrderInput) error {
    return WithTx(ctx, s.pool, func(tx pgx.Tx) error {
        orderStore := NewOrderStore(tx)
        inventoryStore := NewInventoryStore(tx)

        if err := inventoryStore.Reserve(ctx, input.Items); err != nil {
            return fmt.Errorf("orders: reserve inventory: %w", err)
        }
        if err := orderStore.Create(ctx, input.Order); err != nil {
            return fmt.Errorf("orders: create: %w", err)
        }
        return nil
    })
}
```

### 6d. WithTxOptions for Custom Isolation Levels

```go
func WithTxOptions(
    ctx context.Context,
    pool *pgxpool.Pool,
    opts pgx.TxOptions,
    fn func(tx pgx.Tx) error,
) error {
    tx, err := pool.BeginTx(ctx, opts)
    if err != nil {
        return fmt.Errorf("db: begin tx: %w", err)
    }
    defer func() {
        if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
            slog.ErrorContext(ctx, "db: rollback failed", "error", err)
        }
    }()

    if err := fn(tx); err != nil {
        return err
    }
    if err := tx.Commit(ctx); err != nil {
        return fmt.Errorf("db: commit tx: %w", err)
    }
    return nil
}
```

### 6e. Transaction Isolation Level Selection

| Level | Use case | Trade-off |
|---|---|---|
| Read Committed (default) | Most OLTP operations | Allows non-repeatable reads — sufficient for typical web requests |
| Repeatable Read | Reports, analytics, consistent snapshots | Serialization failures possible — must handle retry |
| Serializable | Financial transactions, inventory with strict consistency | Highest contention — must retry on `40001` (serialization failure) |

When using Repeatable Read or Serializable, retry on serialization failure:

```go
func WithSerializableTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
    opts := pgx.TxOptions{IsoLevel: pgx.Serializable}
    const maxRetries = 3

    for attempt := range maxRetries {
        err := WithTxOptions(ctx, pool, opts, fn)
        if err == nil {
            return nil
        }

        var pgErr *pgconn.PgError
        if errors.As(err, &pgErr) && pgErr.Code == "40001" {
            if attempt < maxRetries-1 {
                continue
            }
        }
        return err
    }
    return fmt.Errorf("db: serializable tx: max retries exceeded")
}
```

### 6f. Deadlock Prevention Patterns

- Access tables and rows in a consistent order across all transactions.
  If service A always locks `orders` then `inventory`, service B must follow
  the same order.
- Use advisory locks for application-level coordination when row-level locking
  is insufficient:

```go
// Acquire advisory lock (blocks until available)
_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockID)
```

Advisory locks acquired with `pg_advisory_xact_lock` are released
automatically when the transaction commits or rolls back.

### 6g. Long-Running Transaction Risks and Mitigation

Long transactions hold locks, prevent autovacuum, and cause connection pool
starvation.

Rules:

- Set a statement timeout to prevent runaway queries:

```go
_, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '30s'")
```

- Use context timeouts to bound total transaction duration:

```go
ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
defer cancel()
```

- For large data operations, process in batches with separate transactions per
  batch rather than one massive transaction

- **Never call external APIs inside a transaction.** Network latency and
  failures extend transaction duration unpredictably. Perform external calls
  before or after the transaction, and compensate on failure.

### 6h. Transaction Testing with Rollback

Use real database transactions in repository tests with rollback for cleanup:

```go
func TestOrderCreate(t *testing.T) {
    ctx := context.Background()
    tx, err := testPool.Begin(ctx)
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { tx.Rollback(ctx) })

    store := NewOrderStore(tx)
    err = store.Create(ctx, testOrder)
    assert.NoError(t, err)
}
```

For service-level tests, use fakes — do not require a database. Service tests
should verify business logic and delegation, not SQL execution.

For integration tests that need committed data visible across connections, use
`t.Cleanup` with `TRUNCATE`:

```go
func truncateOrders(t *testing.T, pool *pgxpool.Pool) {
    t.Helper()
    t.Cleanup(func() {
        _, _ = pool.Exec(context.Background(), "TRUNCATE orders CASCADE")
    })
}
```

Savepoints allow partial rollback within a transaction:

```go
_, err = tx.Exec(ctx, "SAVEPOINT sp1")
// ... attempt operation ...
_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT sp1")
```

### 6i. Idempotency for Database Write Operations

Non-idempotent operations (insert, payment capture, email dispatch) must not be
retried without an idempotency key:

- Generate or accept an **idempotency key** — a stable identifier for the
  logical request (UUID derived from request ID, or deterministic hash of
  immutable parameters).
- Deduplicate by key using persistent storage (database row or persistent cache).
  In-memory maps are not sufficient — they disappear on restart.
- Returning a cached response for a duplicate key is correct behavior; do not
  re-execute.
- The key's TTL must exceed the maximum retry window plus observable clock skew.

Retrying a non-idempotent operation without a key is a correctness bug — reject
in code review.

Reference implementation: `pkg/web/idempotency`.

---

## 7. Anti-Patterns

### 7a. Anti-Pattern — String Concatenation for SQL Queries

Never build SQL by concatenating user input. This is the most common source of
SQL injection.

```go
// WRONG — SQL injection vulnerability
query := "SELECT * FROM users WHERE name = '" + name + "'"

// CORRECT — parameterized query
query := "SELECT id, name FROM users WHERE name = $1"
row := pool.QueryRow(ctx, query, name)
```

### 7b. Anti-Pattern — Leaking Database Types into Handlers

Never let `pgx.ErrNoRows`, `sql.ErrNoRows`, or `*pgconn.PgError` escape the
repository layer. Map them to domain errors in the repository:

```go
// WRONG — handler sees database internals
if errors.Is(err, pgx.ErrNoRows) {
    return c.String(http.StatusNotFound, "not found")
}

// CORRECT — handler sees domain error
if errors.Is(err, ErrNotFound) {
    return web.ErrNotFound("order not found")
}
```

### 7c. Anti-Pattern — New Connection per HTTP Request

The pool exists to reuse connections. Never call `pgx.Connect` or `sql.Open`
inside a request handler. Use the shared pool injected at construction time.

### 7d. Anti-Pattern — SELECT * in Production Queries

Always specify the columns you need:

```go
// WRONG — brittle, transfers unnecessary data, breaks on schema changes
rows, err := pool.Query(ctx, "SELECT * FROM orders WHERE user_id = $1", userID)

// CORRECT — explicit columns
rows, err := pool.Query(ctx,
    "SELECT id, status, total, created_at FROM orders WHERE user_id = $1", userID)
```

### 7e. Anti-Pattern — Not Using Context-Aware Query Methods

Always pass context to database operations. Without context, queries cannot be
cancelled when a request is cancelled:

```go
// WRONG — no cancellation, no timeout
rows, err := db.Query("SELECT id FROM users")

// CORRECT — context flows through
rows, err := db.QueryContext(ctx, "SELECT id FROM users")
```

With pgx, context is always the first parameter — there are no non-context
variants.

### 7f. Anti-Pattern — Transactions Managed in Repository Layer

Store methods must not begin or commit transactions. A store that manages its
own transaction cannot be composed with other stores in a single atomic
operation:

```go
// WRONG — store owns transaction lifecycle
func (s *pgOrderStore) Create(ctx context.Context, order Order) error {
    tx, _ := s.pool.Begin(ctx)
    defer tx.Rollback(ctx)
    // ... insert ...
    return tx.Commit(ctx)
}

// CORRECT — store accepts whatever DBTX the caller provides
func (s *pgOrderStore) Create(ctx context.Context, order Order) error {
    _, err := s.db.Exec(ctx,
        `INSERT INTO orders (id, user_id, status) VALUES ($1, $2, $3)`,
        order.ID, order.UserID, order.Status)
    if err != nil {
        return fmt.Errorf("orders: create: %w", mapPgError(err))
    }
    return nil
}
```

### 7g. Anti-Pattern — Ignoring Rollback Errors

The `defer tx.Rollback()` pattern requires checking for `pgx.ErrTxClosed`
(or `sql.ErrTxDone`) to avoid logging noise on successful commits:

```go
// WRONG — logs error on every successful commit
defer func() {
    if err := tx.Rollback(ctx); err != nil {
        slog.Error("rollback failed", "error", err) // fires even after Commit
    }
}()

// CORRECT — ignores expected closed-transaction error
defer func() {
    if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
        slog.ErrorContext(ctx, "db: rollback failed", "error", err)
    }
}()
```

### 7h. Anti-Pattern — Transactions Held During External API Calls

External network calls have unpredictable latency. Holding a database
transaction open while waiting for an HTTP response or message queue
acknowledgment causes lock contention and pool starvation:

```go
// WRONG — transaction held open during network call
tx, _ := pool.Begin(ctx)
orderStore := NewOrderStore(tx)
orderStore.Create(ctx, order)
paymentClient.Charge(ctx, order.Total) // network call inside tx
tx.Commit(ctx)

// CORRECT — transaction closed before external call
err := WithTx(ctx, pool, func(tx pgx.Tx) error {
    return NewOrderStore(tx).Create(ctx, order)
})
if err != nil {
    return err
}
// External call happens outside the transaction
return paymentClient.Charge(ctx, order.Total)
```

### 7i. Anti-Pattern — Omitting Context in Transaction Operations

Every operation inside a transaction must use the same context that governs the
transaction's deadline. Dropping context inside a transaction means individual
statements cannot be cancelled:

```go
// WRONG — context dropped inside transaction
func (s *OrderService) Place(ctx context.Context, order Order) error {
    return WithTx(ctx, s.pool, func(tx pgx.Tx) error {
        store := NewOrderStore(tx)
        return store.Create(context.Background(), order) // context dropped
    })
}

// CORRECT — context flows through
func (s *OrderService) Place(ctx context.Context, order Order) error {
    return WithTx(ctx, s.pool, func(tx pgx.Tx) error {
        store := NewOrderStore(tx)
        return store.Create(ctx, order)
    })
}
```

---

## 8. Review Checklist

Before merging a data persistence change, confirm:

- connection pool settings are explicitly configured — no unlimited defaults
- `MaxIdleConns` matches `MaxOpenConns` to avoid connection churn
- `ConnMaxLifetime` is set to rotate connections for DNS failover
- parameterized queries are used for all user input — no string concatenation
- database error codes are mapped to domain errors in the repository layer
- `pgx.ErrNoRows` and `sql.ErrNoRows` never escape the repository
- transactions are managed at the service layer, not inside stores
- no external API calls occur inside a transaction
- `context.Context` is passed to every database operation
- each migration file has both `-- +goose Up` and `-- +goose Down` sections
- `Down` SQL is written and reviewed manually — not auto-generated
- migrations use `IF NOT EXISTS` / `IF EXISTS` for idempotency
- column renames or removals follow the three-step zero-downtime approach
- rollback errors check for `pgx.ErrTxClosed` before logging
- `SELECT` statements specify explicit columns, not `*`
- long-running queries have statement or context timeouts
- serializable transactions retry on `40001` serialization failures
- advisory locks use consistent ordering to prevent deadlocks

---

## 9. Sources

- pgx documentation: <https://github.com/jackc/pgx>
- goose: <https://github.com/pressly/goose>
- PostgreSQL error codes: <https://www.postgresql.org/docs/current/errcodes-appendix.html>
- `pkg/web/idempotency` — idempotency middleware and store
- Effective Go: <https://go.dev/doc/effective_go>
