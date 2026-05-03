---
title: Architecture Guide
description: "project structure, flat vs modular layout, server struct pattern, Go 1.22+ routing, dependency injection, constructor functions, functional options, graceful shutdown, signal handling, package design, DRY, YAGNI, SOLID, rendering model, leaf-node rule, starter application, pkg modules, anti-patterns"
weight: 15
---

# Architecture Guide

> This guide is the authoritative source for how Go applications are structured,
> wired, and run.
>
> It complements [`MIDDLEWARE_AND_CONTEXT.md`](./MIDDLEWARE_AND_CONTEXT.md)
> (middleware and request context flow), [`ERROR_HANDLING.md`](./ERROR_HANDLING.md)
> (boundary and resilience patterns), [`CODE_REVIEW.md`](./CODE_REVIEW.md)
> (review checklists), [`DATA_STORAGE.md`](./DATA_STORAGE.md) (persistence patterns),
> and [`WEB_DESIGN.md`](./WEB_DESIGN.md) (concurrency and runtime safety).
>
> Read this together with [`CLAUDE.md`](../CLAUDE.md) for behavioral rules.
>
> Use this guide to answer:
>
> - how to structure a Go project from scratch or evolve an existing one
> - how to wire dependencies without globals or frameworks
> - how to design the server, routes, and middleware
> - how to shut down cleanly under container orchestration
> - which structural anti-patterns to reject in review

---

## 0. Quick Reference

- §1 Core principles: standard library first, DI over globals, explicit over magic, small interfaces, prod-faithful dev
- §2/2a Project structure: flat vs modular layout, migration signals, technology stack
- §3 Server struct: single struct owns all dependencies, middleware wrapping
- §4 Routing: Go 1.22+ method-based routing, path params, wildcard matching, precedence
- §5 Dependency injection: constructors, layered DI, interfaces for testability, functional options
- §6 Graceful shutdown: run() pattern, signal handling, shutdown timeout, cleanup ordering
- §7/7a-c Package design: inward dependencies, DRY, YAGNI, SOLID, rendering model
- §8 Anti-patterns: global DB vars, framework-first thinking, god packages, init() misuse
- §9 Leaf-node rule: reusable `pkg/*` modules stay application-independent
- §10 Review checklist: architecture review items
- §11 Guide relationships and how to navigate the decision docs
- §12 Additional sources

---

## 1. Core Architectural Principles

### 1a. Standard Library Preference over Third-Party

Prefer `net/http`, `encoding/json`, `log/slog`, `errors`, `context`, and
`database/sql` (or `pgx` for PostgreSQL) before reaching for a framework.
Go 1.22+ `http.ServeMux` supports method-based routing, path parameters, and
wildcard matching. A framework is justified only when its benefits are concrete
and measurable against the standard library surface for the problem at hand.

### 1b. Dependency Injection over Global State

Never use `init()` for setup. Never use package-level mutable `var` for
runtime dependencies. Every dependency is constructed explicitly and passed
through constructors or function parameters.

The composition root (`main.go` or a `run()` function) is the single place
where concrete implementations are created and wired together. No other
package reads environment variables, opens database connections, or
initializes shared state.

### 1c. Explicit Configuration over Convention Magic

The program's assembly is readable top-to-bottom in the composition root.
There are no service locators, no annotation-driven wiring, and no
reflection-based dependency graphs. A new developer reads `main.go` and
understands what is constructed, in what order, and what depends on what.

### 1d. Small Interfaces and Large Concrete Structs

Define interfaces at the consumer, not the provider. A concrete type does not
need to declare which interfaces it satisfies. The consumer defines the
narrowest interface it needs, and the concrete type satisfies it implicitly.

```go
// Defined by the consumer, not the store package.
type UserFinder interface {
    FindByID(ctx context.Context, id uuid.UUID) (User, error)
}
```

This keeps coupling minimal and testability high without premature abstraction.

### 1e. Production-Faithful Dev Environment over Simplified Scaffolding

The development environment is a fully configured production environment with
hot reload, not a stripped-down setup that only handles simple scenarios. Every
service, middleware chain, and configuration value present in production must be
present in development. If a code path will not run in production, it does not
belong in the development configuration.

Tests are the sole exception. Tests may substitute in-memory stores, shorter
timeouts, or deterministic clocks for performance, memory efficiency, and
repeatability. This exception is valid only when corresponding tests exist to
exercise all behaviors the substitution replaces.

---

## 2. Project Structure

### 2a. Flat Package Structure for Small Applications

A flat structure is appropriate when the project is small, has one developer,
and serves a single bounded context. All files live in `package main` or a
single internal package.

| File | Responsibility |
|---|---|
| `main.go` | Composition root: parse config, build dependencies, start server |
| `server.go` | Server struct, `NewServer`, `routes()`, `ServeHTTP` |
| `handlers.go` | HTTP handler methods on the server struct |
| `middleware.go` | Middleware functions |
| `models.go` | Domain types and error sentinels |
| `store.go` | Database access layer |

Rules for flat structure:

- Every file has one clear responsibility.
- No file exceeds 500 lines. When it does, split by domain or concern.
- Test files are co-located: `handlers_test.go`, `store_test.go`.

### 2b. Modular Feature Package Structure for Large Applications

A modular structure is appropriate when the project has multiple domains,
multiple developers, or needs independent test isolation.

```
cmd/
  server/
    main.go              # composition root
internal/
  app/
    app.go               # application struct, wiring
    routes.go            # route registration
    providers.go         # dependency construction
  features/
    user/
      handler.go
      service.go
      module.go
    order/
      handler.go
      service.go
      module.go
  model/
    user.go
    order.go
    errors.go
  repository/
    user_repo.go
    order_repo.go
  view/
    page/
    partial/
    layout/
```

Rules for modular structure:

- `cmd/` contains one `main.go` per binary. Each main calls a `run()` function.
- `internal/` contains all application code. Nothing outside the module imports it.
- Feature packages (`internal/features/<name>/`) own handler, service, and
  module wiring for a single domain.
- Repository packages (`internal/repository/`) own persistence for
  application-specific tables.
- Model packages (`internal/model/`) own domain types and error sentinels shared
  across features.
- Route registration lives in `internal/app/routes.go`, never inside feature
  packages.

### 2c. Migration Signals from Flat to Modular Structure

Move from flat to modular when any of these conditions hold:

- A single file exceeds 500 lines and splits by function grouping.
- Naming collisions emerge (two domains need a `Create` handler).
- Multiple developers work on the project concurrently.
- Distinct domains have independent lifecycle or deployment needs.
- Test isolation requires separate packages to avoid import cycles.
- The `store.go` file serves multiple unrelated tables.

### 2d. Incremental Migration Strategy from Flat to Modular

1. Extract one domain at a time. Start with the domain that has the most
   naming pressure or the most test isolation need.
2. Move the domain's handlers, service logic, and store into a feature package.
3. Update the composition root to wire the new package.
4. Keep the remaining flat code untouched until the next extraction.
5. Never extract "just in case." Extract when a concrete signal triggers it.

---

## 2a. This Application's Technology Stack

This is a server-rendered Go web application built around:

- **Go types that directly model W3C Web API primitives** (Request, Response,
  Headers, ReadableStream) — the HTTP layer uses these primitives directly
  rather than framework-specific abstractions
- **Gomponents** for HTML rendering — all views are composed from Go functions
  that produce `g.Node` trees
- **HTMX** for progressive enhancement — server-rendered HTML with client-side
  interactivity via HTMX attributes
- **Reusable external modules** from `github.com/go-sum/*` consumed as ordinary
  Go dependencies via `go.mod`

### 2aa. Source Zone — Checked-In Application Code

All application code lives in `internal/`. External modules are ordinary Go
dependencies — they are not part of this repository and are consumed via their
public API.

### 2ab. Runtime Assembly — Generated and External Code

The composition root lives in `internal/app/`. It wires:

- config loading and environment resolution
- logging setup
- asset registration
- security middleware and CSRF protection
- database pool and schema migrations
- session management
- queue client and background services
- external modules (auth, queue storage, sessions, senders, site metadata)
- app-owned feature modules and views

### 2ac. Hybrid Architecture Overview

The application is intentionally hybrid:

- Some domains are provided by external modules and integrated into the app
  (auth, queue storage, sessions, senders, site metadata)
- Some domains are app-owned (contact flow, availability handling, page
  composition, showcase)

When consuming external modules, use only their public API. Never reach into
an external module's internals. To change behavior, upstream the change.

---

## 3. The Server Struct Pattern

The server struct is the dependency container for the HTTP layer. It
implements `http.Handler` and holds all dependencies needed by handlers.

### 3a. Server Struct Field and Dependency Structure

```go
type Server struct {
    router *http.ServeMux
    users  UserService
    logger *slog.Logger
    // ... other dependencies
}

func NewServer(users UserService, logger *slog.Logger) *Server {
    s := &Server{
        users:  users,
        logger: logger,
    }
    s.router = s.routes()
    return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    s.router.ServeHTTP(w, r)
}
```

### 3b. Server Struct Ownership Rules

- `NewServer` accepts dependencies as parameters. It never reads environment
  variables or opens connections.
- `routes()` is a private method that returns the configured `*http.ServeMux`.
  It is called once during construction.
- `ServeHTTP` delegates to the internal router. This makes the server a
  standard `http.Handler` usable with `httptest` directly.
- Middleware wraps the router at the server level or per-route inside `routes()`.

### 3c. Middleware Wrapping on the Server Struct

```go
func (s *Server) routes() *http.ServeMux {
    mux := http.NewServeMux()

    // Per-route handlers
    mux.HandleFunc("GET /users/{id}", s.handleGetUser)
    mux.HandleFunc("POST /users", s.handleCreateUser)

    return mux
}
```

Server-level middleware wraps in `ServeHTTP` or around the router at
construction:

```go
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    chain := recoverer(requestID(s.router))
    chain.ServeHTTP(w, r)
}
```

---

## 4. Go 1.22+ Enhanced Routing

Go 1.22 introduced method-based routing, path parameters, and wildcard
matching to `http.ServeMux`. Use these features instead of third-party routers
when the standard library covers the routing need.

### 4a. Method-Based Route Registration

```go
mux.HandleFunc("GET /api/users", s.handleListUsers)
mux.HandleFunc("POST /api/users", s.handleCreateUser)
mux.HandleFunc("GET /api/users/{id}", s.handleGetUser)
mux.HandleFunc("PUT /api/users/{id}", s.handleUpdateUser)
mux.HandleFunc("DELETE /api/users/{id}", s.handleDeleteUser)
```

A request with an unmatched method returns 405 Method Not Allowed
automatically.

### 4b. Path Parameter Extraction

```go
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    // Parse and validate at the transport boundary.
    uid, err := uuid.Parse(id)
    if err != nil {
        http.Error(w, "invalid user ID", http.StatusBadRequest)
        return
    }
    // ...
}
```

Always parse and validate path parameters at the handler boundary. Never pass
raw strings into service or repository layers.

### 4c. Wildcard and Exact Match Route Patterns

```go
// Exact match: only "/"
mux.HandleFunc("GET /{$}", s.handleHome)

// Wildcard: matches any path under /static/
mux.HandleFunc("GET /static/", s.handleStatic)

// Catch-all with named wildcard
mux.HandleFunc("GET /files/{path...}", s.handleFiles)
```

- `{$}` matches only the exact path, preventing `/` from matching all routes.
- A trailing slash acts as a subtree wildcard.
- `{name...}` captures the rest of the path into a named parameter.

### 4d. Route Matching Precedence Rules

`http.ServeMux` uses most-specific-wins precedence:

1. Longer patterns take priority over shorter ones.
2. Exact host patterns take priority over wildcard patterns.
3. Method-specific patterns take priority over method-agnostic patterns.
4. `{$}` exact match takes priority over subtree match.

If two patterns overlap and neither is more specific, registration panics at
startup. This is correct behavior: ambiguous routes are a bug.

---

## 5. Dependency Injection

### 5a. Constructor Functions for Dependency Wiring

Every dependency is created by a constructor function. Constructors:

- Accept their own dependencies as parameters.
- Return a concrete type (not an interface).
- Perform no I/O (no database calls, no HTTP requests, no file reads).
- Apply defaults with `cmp.Or` for comparable zero-value fields.

```go
func NewOrderService(repo OrderRepository, notify Notifier) *OrderService {
    return &OrderService{
        repo:   repo,
        notify: notify,
    }
}
```

### 5b. Layered Dependency Injection Pattern

Build dependencies bottom-up in the composition root. Each layer depends only
on the layer below it:

```
Infrastructure  (logger, config, database pool, cache client)
      |
   Stores       (user repo, order repo)
      |
  Services      (user service, order service)
      |
    HTTP        (server struct with handlers)
```

```go
func run(ctx context.Context) error {
    cfg := loadConfig()

    // Infrastructure
    logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
    pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
    if err != nil {
        return fmt.Errorf("database: %w", err)
    }
    defer pool.Close()

    // Stores
    userRepo := repository.NewUserRepo(pool)
    orderRepo := repository.NewOrderRepo(pool)

    // Services
    userSvc := service.NewUserService(userRepo)
    orderSvc := service.NewOrderService(orderRepo, userSvc)

    // HTTP
    srv := NewServer(userSvc, orderSvc, logger)

    // Start
    httpServer := &http.Server{Addr: cfg.Addr, Handler: srv}
    return httpServer.ListenAndServe()
}
```

### 5c. Interface-Based Dependencies for Testability

Define interfaces at the consumer. The service defines what it needs from
the repository; the handler defines what it needs from the service:

```go
// In the service package — defines what it needs from persistence.
type OrderRepository interface {
    Create(ctx context.Context, order Order) error
    GetByID(ctx context.Context, id uuid.UUID) (Order, error)
}

// In the handler package — defines what it needs from business logic.
type OrderCreator interface {
    CreateOrder(ctx context.Context, input CreateOrderInput) (Order, error)
}
```

Never define a "god interface" that mirrors the entire concrete type. Each
consumer defines only the methods it calls.

### 5d. Configuration Struct as a Dependency

Load configuration once in the composition root. Pass individual values or
small config structs to constructors. Never pass the entire application
config to a component that needs two fields.

```go
// Prefer: pass what is needed.
func NewMailer(host string, port int, from string) *Mailer { ... }

// Avoid: passing the world.
func NewMailer(cfg AppConfig) *Mailer { ... }
```

Components never read environment variables directly. The composition root
is the only code that touches `os.Getenv`, config files, or flag parsing.

### 5e. Functional Options for Optional Dependencies

Use functional options when most callers need zero configuration and the
constructor has optional collaborators or settings:

```go
type Option func(*Server)

func WithLogger(l *slog.Logger) Option {
    return func(s *Server) { s.logger = l }
}

func WithTimeout(d time.Duration) Option {
    return func(s *Server) { s.timeout = d }
}

func NewServer(repo UserRepository, opts ...Option) *Server {
    s := &Server{
        repo:    repo,
        logger:  slog.Default(),
        timeout: 30 * time.Second,
    }
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

Use an option struct instead when most callers need multiple settings together
or when the configuration surface is large enough that variadic closures
become unwieldy.

### 5f. Cross-Domain Dependency Resolution

When one domain needs a capability from another, define a consumer-scoped
interface rather than importing the other domain's concrete type:

```go
// order package defines what it needs from the user domain.
type UserLookup interface {
    GetByID(ctx context.Context, id uuid.UUID) (User, error)
}
```

The composition root wires the user service into the order service through
this interface. Neither package imports the other.

---

## 6. Graceful Shutdown

### 6a. The run() Blocking Pattern for Server Lifecycle

Separate `main()` from `run()` to enable defer-based cleanup and testability:

```go
func main() {
    ctx := context.Background()
    if err := run(ctx); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
}

func run(ctx context.Context) error {
    ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
    defer cancel()

    // Build dependencies, start server, block until signal...
    return nil
}
```

Rules:

- `main()` calls `run()` and handles the exit code. Nothing else.
- `run()` owns signal handling, construction, server start, and cleanup.
- All `defer` cleanup calls execute when `run()` returns.
- `run()` is testable: pass a cancelable context to simulate shutdown.

### 6b. OS Signal-Based Shutdown Trigger

Use `signal.NotifyContext` to derive a context that cancels on SIGINT or
SIGTERM:

```go
ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
defer stop()
```

When the signal fires, the context cancels, and the server shutdown path
begins.

### 6c. Shutdown Timeout Configuration

Always apply a timeout to `http.Server.Shutdown`:

```go
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
defer shutdownCancel()

if err := httpServer.Shutdown(shutdownCtx); err != nil {
    return fmt.Errorf("server shutdown: %w", err)
}
```

Choose timeout duration by scenario:

| Scenario | Typical timeout |
|---|---|
| API server with short-lived requests | 5-10 seconds |
| Server with long-polling or SSE connections | 15-30 seconds |
| Server with background job drain | 30-60 seconds |

The timeout must be shorter than the container orchestrator's kill grace
period (typically 30 seconds for Kubernetes, configurable).

### 6d. Shutdown Cleanup Ordering (LIFO)

Shut down in reverse order of creation. The last thing created is the first
thing stopped:

```
Start order:  DB pool -> Cache -> Workers -> HTTP server
Stop order:   HTTP server -> Workers -> Cache -> DB pool
```

```go
func run(ctx context.Context) error {
    ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
    defer cancel()

    pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
    if err != nil {
        return fmt.Errorf("database: %w", err)
    }
    defer pool.Close() // closed last

    cache := redis.NewClient(cfg.RedisAddr)
    defer cache.Close()

    worker := background.NewWorker(pool)
    defer worker.Stop()

    srv := NewServer(pool, cache, worker)
    httpServer := &http.Server{Addr: cfg.Addr, Handler: srv}

    // Start HTTP server in a goroutine.
    errCh := make(chan error, 1)
    go func() { errCh <- httpServer.ListenAndServe() }()

    // Block until signal or server error.
    select {
    case err := <-errCh:
        return err
    case <-ctx.Done():
    }

    // Shutdown HTTP server first.
    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer shutdownCancel()
    return httpServer.Shutdown(shutdownCtx)
    // defer stack unwinds: worker.Stop(), cache.Close(), pool.Close()
}
```

### 6e. Health Check Endpoint for Load Balancer Drain

Expose a health endpoint for container orchestrators (Kubernetes liveness and
readiness probes, ECS health checks):

```go
var healthy atomic.Bool

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    if !s.healthy.Load() {
        w.WriteHeader(http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
}
```

### 6f. Pre-Shutdown Connection Drain Period

For zero-downtime deployments behind a load balancer:

1. Receive shutdown signal.
2. Mark the server unhealthy (`healthy.Store(false)`).
3. Wait a short drain period (2-5 seconds) for the load balancer to detect
   the health change and stop sending new traffic.
4. Call `httpServer.Shutdown()` to finish in-flight requests.

```go
// Signal received.
s.healthy.Store(false)
time.Sleep(3 * time.Second) // drain period

shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
defer shutdownCancel()
return httpServer.Shutdown(shutdownCtx)
```

The drain period must be shorter than the load balancer's health check
interval.

---

## 7. Package Design Principles

### 7a. Dependencies Flow Inward — No Reverse Imports

Domain packages do not import each other. A `user` package never imports
`order`, and `order` never imports `user`. Cross-domain communication flows
through interfaces defined by the consumer and wired in the composition root.

```
handlers -> services -> repositories -> database
    \          \            \
     \          \            +-- domain models
      \          +-------------- domain models
       +------------------------ domain models
```

Higher layers depend on lower layers. Lower layers never import higher layers.

### 7b. Circular Dependency Prevention

When two packages seem to need each other:

1. Extract the shared type into a third package (often `model` or a domain
   types package).
2. Define a consumer interface so one package depends on an abstraction
   rather than the other package directly.
3. If two packages are truly inseparable, merge them into one package.

Never use `internal/` sub-package tricks to break cycles artificially.

### 7c. No Catch-All Utils or Helpers Packages

Never create a package named `util`, `utils`, `helper`, `helpers`, or
`common`. Name packages for what they provide:

| Instead of | Use |
|---|---|
| `util.FormatDate` | `timeformat.Date` |
| `helper.HashPassword` | `authn.HashPassword` |
| `common.Config` | `config.Config` |
| `util.Contains` | Use `slices.Contains` from the standard library |

### 7d. Minimal Exports — Only Public API Surface

Start every type, function, and method as unexported. Export only when another
package genuinely needs it. An unexported symbol can always be exported later;
an exported symbol is part of the package's public contract.

---

## 7a. Core Programming Principles

### 7aa. DRY — Don't Repeat Yourself

Reduce repetition in behavior, policies, and data mapping, but do not create a
shared abstraction until the duplication is real and stable.

### 7ab. YAGNI — You Aren't Gonna Need It

Do not add hooks, wrappers, config keys, or extension points for hypothetical
future use. Be conservative.

### 7ac. Separation of Concerns by Layer

Split code by responsibility:

- transport concerns in handlers
- orchestration and business rules in services
- persistence in repositories
- presentation in views
- assembly in the composition root

### 7ad. SOLID Principles Applied Pragmatically

- **SRP**: each function, type, and file has one primary reason to change.
- **ISP**: prefer narrow interfaces with a clear consumer.
- **DIP**: depend on abstractions only where that reduces coupling at a real
  boundary; do not create speculative interfaces.

### 7ae. Favor Composition and Encapsulation over Inheritance

Prefer small collaborating types over inheritance-style layering or broad
utility packages. Expose the smallest public API that the next caller actually
needs.

### 7af. Layer Discipline — No Skipping Layers

Do not let a lower layer depend on a higher one:

- handlers do not own data
- services do not render HTML
- repositories do not decide redirects or HTTP status codes
- views do not own business rules or persistence

---

## 7b. Recommended Design Patterns

Patterns are tools, not goals. Use them where they simplify existing code.

### 7ba. Factory and Registry Pattern

Use when protocol or provider selection is a real requirement: sender/provider
selection, transport/backend selection, constructing different implementations
behind one entry point.

### 7bb. Chain of Responsibility Pattern

Use when the request or operation passes through a sequence of orthogonal
behaviors: middleware stacks, request guards, layered cross-cutting policies.

### 7bc. Adapter Pattern for Interface Bridging

Use when an external-module interface must be satisfied by app-owned rendering,
session, form, or redirect behavior.

### 7bd. Enum-to-Value Map Pattern

When a function maps a typed enum to a fixed value, use a package-level map
literal instead of a switch:

```go
var variantClasses = map[Variant]string{
    VariantDestructive: "bg-destructive text-white",
    VariantOutline:     "border bg-background",
}

func variantClass(v Variant) string {
    if c, ok := variantClasses[v]; ok {
        return c
    }
    return "bg-primary text-primary-foreground"
}
```

This applies when every case returns a value and has no side effects.

### 7be. Pattern Selection Decision Table

Diagnose the problem before reaching for a pattern:

- **Object creation** (complex construction, too many args): Factory, Builder
- **Structural** (incompatible interfaces, adding behavior): Decorator, Adapter, Facade
- **Behavioral** (swap algorithms, notify observers, state-dependent behavior): Strategy, Observer, Command, State

If no pattern simplifies the code that exists today, do not introduce one.

---

## 7c. Rendering Model

The application supports multiple HTML response modes without splitting into
separate rendering stacks.

### 7ca. Canonical Rendering Modes (Full Page vs Fragment)

| Mode | Handler pattern |
|------|-----------------|
| Full page + HTMX partial | `view.Render(c, req, fullPage, partial)` |
| Fragment-only | `render.Fragment(c, node)` or `render.FragmentWithStatus(c, status, node)` |
| HTMX removal | `c.String(http.StatusOK, "")` |
| JSON / problem | Selected by the global error handler based on request headers |
| Redirect | HTMX-aware redirect helpers |

### 7cb. Rendering Model Rules and HTMX Integration

- Use `view.NewRequest(...)` to build request-scoped presentation state.
- Use `view.Render(...)` when one endpoint serves both full-page and HTMX
  partial modes.
- Use `render.Fragment(...)` only when the endpoint exists purely for fragment
  swapping.
- Let the global error handler decide between HTML, HTMX fragment, and problem
  JSON responses.

---

## 8. Anti-Patterns

### 8a. Anti-Pattern — Global Database Variables

Never store a database pool in a package-level variable:

```go
// Never do this.
var db *pgxpool.Pool

func init() {
    var err error
    db, err = pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }
}
```

The pool is constructed in the composition root and passed to every
constructor that needs it.

### 8b. Anti-Pattern — Framework-First Architecture

Do not begin a project by choosing a framework and building around its
conventions. Begin with the standard library. Introduce a framework only when
a specific, measurable deficiency in the standard library justifies it.

### 8c. Anti-Pattern — God Packages

A `handlers/` package with 50 files is not a package; it is a dumping ground.
Split by domain: `user/handler.go`, `order/handler.go`. The same applies to
`services/`, `repositories/`, and `models/` when they grow beyond a single
coherent domain.

### 8d. Anti-Pattern — init() for Application Setup

`init()` runs before `main()`, cannot accept parameters, cannot return
errors, and makes testing unpredictable. Never use it for database
connections, configuration loading, HTTP client construction, or any
runtime dependency. The only acceptable use of `init()` is registering
drivers with `database/sql` or similar compile-time registration schemes
(and even then, prefer explicit registration when the API supports it).

### 8e. Anti-Pattern — Config Access in Business Logic

Services and repositories never call `os.Getenv`, read config files, or
access a global config object. Configuration values arrive as constructor
parameters or typed config structs from the composition root.

### 8f. Anti-Pattern — Passing Entire Config Struct

Never pass the full application configuration to a component. Pass only the
values the component needs:

```go
// Prefer
func NewCache(addr string, ttl time.Duration) *Cache { ... }

// Avoid
func NewCache(cfg AppConfig) *Cache { ... }
```

A component that accepts `AppConfig` is coupled to every field in the config,
even those it never reads. This makes the dependency graph opaque and testing
harder.

---

## 9. Leaf-Node Rule

The `pkg/` tree is divided into two tiers with different import rules:

### Tier 1 — Leaf Modules (`pkg/<name>/`)

Root-level `pkg/<name>` modules import only the Go standard library and external (non-foundry) modules. There are no imports from application-specific `internal/` packages and no cross-imports between sibling `pkg/` packages. This means any root-level `pkg/` module can be vendored into any Go project without pulling in application-specific code.

Current leaf modules: `auth`, `web`, `db`, `kv`, `config`, `componentry`, `notification`, `queue`, `assets`.

### Tier 2 — Bridge Sub-Modules (`pkg/<name>/<sub>/`)

Sub-modules under a `pkg/<name>/` root are **bridge modules**: integration points that compose their parent leaf plus other declared packages. They exist to wire together leaf modules without polluting the leaves themselves.

Rules for bridge modules:
- May import their parent root module (e.g., `pkg/auth/authui` may import `pkg/auth`)
- May import other `pkg/` leaves or bridges when forming a declared composition
- Must never create cycles
- Must be separate `go.mod` workspace modules (never a plain sub-package of the root)

Current bridge modules and their declared composition targets:

| Module | Composes |
|--------|----------|
| `pkg/web/authn` | `pkg/auth` (domain) + `pkg/web` (transport) |
| `pkg/web/viewstate` | `pkg/web` + `pkg/web/authn` + `pkg/componentry` |
| `pkg/auth/authui` | `pkg/auth` + `pkg/web` + `pkg/web/authn` + `pkg/componentry` |
| `pkg/auth/pgstore` | `pkg/auth` + pgx (external) |
| `pkg/auth/provider` | `pkg/auth` + `pkg/web/authn` + `pkg/web` |
| `pkg/kv/redisstore` | `pkg/kv` + redis (external) |
| `pkg/docs` | `pkg/web` |
| `pkg/showcase` | `pkg/web` + `pkg/componentry` + `pkg/kv` |

---
## 10. Review Checklist

Before merging a structural change, confirm:

- Dependencies are constructed in the composition root and passed through constructors.
- No package reads environment variables outside `main.go` or the config loader.
- No `init()` functions exist for runtime dependency setup.
- Interfaces are defined at the consumer, not the provider.
- No package imports a sibling domain package directly; cross-domain communication uses consumer-defined interfaces.
- The server struct implements `http.Handler` via an internal router.
- Route registration is centralized and every route has a name.
- Path parameters are parsed and validated at the handler boundary.
- Graceful shutdown follows reverse-creation order with a bounded timeout.
- Health endpoints reflect actual readiness and are wired to the shutdown sequence.
- No global mutable state is used for runtime dependencies.
- Package names describe what they provide, not how they are used.

---

## 11. How The Guides Fit Together

| Guide | Answers |
|-------|---------|
| **ARCHITECTURE_GUIDE.md** (this guide) | Where code belongs, how to structure, wire, and run |
| [**PRODUCTION_GO_RULES.md**](./PRODUCTION_GO_RULES.md) | Non-negotiable production Go rules: globals, errors, validation, testability |
| [**MIDDLEWARE_AND_CONTEXT.md**](./MIDDLEWARE_AND_CONTEXT.md) | How request middleware, context keys, and request-scoped metadata flow |
| [**ERROR_HANDLING.md**](./ERROR_HANDLING.md) | How boundary errors, resilience, panic recovery, and structured error events work |
| [**STRUCTURED_LOGGING.md**](./STRUCTURED_LOGGING.md) | How `slog`, request logging, and scoped loggers are configured |
| [**HANDLER_TESTING.md**](./HANDLER_TESTING.md) | How handlers and middleware are exercised with `httptest` and fakes |
| [**INPUT_VALIDATION.md**](./INPUT_VALIDATION.md) | How request validation, error formatting, and body limits are applied |
| [**CODE_REVIEW.md**](./CODE_REVIEW.md) | How to review Go code: checklists, severity, verification |
| [**DATA_STORAGE.md**](./DATA_STORAGE.md) | How to persist: drivers, pooling, migrations, transactions |
| [**WEB_DESIGN.md**](./WEB_DESIGN.md) | How to handle concurrency, rate limiting, race detection |
| [**UI_GUIDE.md**](./UI_GUIDE.md) | Visual and UI composition guidance |

---

## 12. Additional Sources

- Go 1.22 ServeMux enhancements: <https://go.dev/blog/routing-enhancements>
- Effective Go: <https://go.dev/doc/effective_go>
- Go Code Review Comments: <https://go.dev/wiki/CodeReviewComments>
- Refactoring Guru, design patterns in Go: <https://refactoring.guru/design-patterns/go>
