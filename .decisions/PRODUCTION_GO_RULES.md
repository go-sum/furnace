---
title: Production Go Rules
description: "zero global state, explicit error handling, validation first, testability, documentation, declarative over imperative, six rules of production Go, no singletons, constructor injection, fake repositories, interface seams"
weight: 20
---

# Production Go Rules

> Six foundational rules that govern all Go code in this application.
> Complements [ARCHITECTURE_GUIDE.md](./ARCHITECTURE_GUIDE.md) (project structure and wiring),
> [MIDDLEWARE_AND_CONTEXT.md](./MIDDLEWARE_AND_CONTEXT.md) (middleware patterns),
> and [ERROR_HANDLING.md](./ERROR_HANDLING.md) (error taxonomy).
>
> Read this together with [CLAUDE.md](../CLAUDE.md) for behavioral rules.

---

## 0. Quick Reference

- §1 Rule 1 — Zero Global State: no package-level vars, constructor injection only
- §1b Rule 2 — Explicit Error Handling: every error checked, wrapped with context
- §1c Rule 3 — Validation First: validate at entry points before business logic
- §1d Rule 4 — Testability: interface seams, fake repos, no real DB in service tests
- §1e Rule 5 — Documentation: exported types and functions need doc comments
- §1f Rule 6 — Declarative Over Imperative: prefer declarative constructs over manual loops
- §2 Handler self-review checklist
- §3 Service self-review checklist

---

## 1. Six Rules of Production Go

These rules are non-negotiable for any production Go web application. They
govern the fundamental shape of all application code.

### 1a. Rule 1 — Zero Global State

All handlers are methods on structs. No package-level `var` holds mutable state.

**Allowed at package level:**

- constants
- pure functions (no side effects, no shared state)
- sentinel errors (`var ErrNotFound = errors.New(...)`)
- stateless validator instances
- enum-to-value maps (immutable after init)

**Forbidden at package level:**

- database connections or pools
- loggers
- HTTP clients
- config structs
- caches
- rate limiters
- any mutable state shared across requests

Every dependency a handler needs is injected through its struct or constructor:

```go
type UserHandler struct {
    svc    UserService
    logger *slog.Logger
}

func NewUserHandler(svc UserService, logger *slog.Logger) *UserHandler {
    return &UserHandler{svc: svc, logger: logger}
}
```

This makes the dependency graph explicit, testable, and free of hidden
coupling between packages.

### 1b. Rule 2 — Explicit Error Handling

Every error is checked. Every propagated error is wrapped with context using
`fmt.Errorf`:

```go
user, err := s.repo.GetByID(ctx, id)
if err != nil {
    return User{}, fmt.Errorf("getting user %s: %w", id, err)
}
```

Wrapping convention: `"verbing noun: %w"` -- lowercase, no trailing period.

For HTTP APIs, use a structured application error type that carries status,
code, and a user-safe message. See section 5a below for the canonical
`*web.Error` type and error taxonomy.

Never discard an error with `_ =`. If the error genuinely cannot be handled,
document why with a comment.

### 1c. Rule 3 — Validation First

Validate at the boundary. Trust internal data.

- Handlers validate all external input (query params, form fields, JSON bodies,
  path parameters) before calling services.
- Services trust that their inputs have been validated. They enforce business
  rules (uniqueness, state transitions, authorization) but do not re-validate
  structural constraints.
- Repositories trust that their inputs are well-typed domain values.

Never validate the same constraint twice across layers. The boundary owns
structural validation; the service owns semantic validation.

### 1d. Rule 4 — Testability

Every handler has a `_test.go` file. Tests use `httptest` with table-driven
patterns covering both happy paths and error paths.

A handler that cannot be tested with `httptest.NewRequest` and
`httptest.NewRecorder` has a design problem -- fix the design, not the test
approach.

### 1e. Rule 5 — Go Doc Comments

Every exported symbol has a Go doc comment starting with its name:

```go
// UserHandler handles HTTP requests for user operations.
type UserHandler struct { ... }

// GetByID returns a user by their unique identifier.
func (h *UserHandler) GetByID(c echo.Context) error { ... }
```

Comments describe *what* and *why*, not *how*. Implementation details belong in
the code, not in doc comments.

### 1f. Rule 6 — Declarative Over Imperative

Prefer declarative constructs over imperative loops and manual mutation for all code, 
including Go standard library features.

**Prefer:**

- `cmp.Or(a, b)` over `if a != "" { return a } return b`
- `slices.Contains(s, v)` over manual `for` + `if` search
- `slices.SortFunc`, `slices.Filter`, `maps.Keys` over hand-rolled loops
- Range expressions and iterators over index-based `for i := 0; i < len(...)`
- Struct literals with named fields over incremental field assignment

**Why:** Declarative code states *what* the result should be rather than *how*
to compute it step by step. This reduces surface area for off-by-one errors,
makes intent immediately visible, and lets the standard library handle edge
cases.

Imperative code is acceptable when no declarative equivalent exists or when
the declarative form would obscure the logic (e.g., complex multi-step
mutations with early exits).

---

## 2. Handler Self-Review Checklist

Before merging application-layer code, confirm every applicable item:

- [ ] All handlers are methods on a struct, not free functions with global state
- [ ] All dependencies are injected through the struct constructor
- [ ] Input is validated at the handler boundary before calling services
- [ ] Request body size is limited with `http.MaxBytesReader`
- [ ] Domain errors are mapped to appropriate HTTP status codes
- [ ] Unknown errors produce 500 with a generic message; internal details are logged
- [ ] Context is propagated from the request to all downstream calls
- [ ] Path parameters are parsed and validated (e.g., `uuid.Parse`)

---

## 3. Service Self-Review Checklist

Before merging application-layer code, confirm every applicable item:

- [ ] Services accept `context.Context` as the first parameter
- [ ] Services depend on repository interfaces, not concrete types
- [ ] Services return domain errors, not HTTP-aware errors
- [ ] Services do not import handler or transport packages
- [ ] Business rules are enforced in the service, not the handler or repository

---

## 4. Sources

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [100 Go Mistakes](https://100go.co/)
