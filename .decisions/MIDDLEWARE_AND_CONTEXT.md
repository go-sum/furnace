---
title: Middleware and Context Propagation
description: "middleware standard signature, middleware chain ordering, chain composition, context propagation, type-safe context keys, request ID propagation, user metadata in context, multi-tenant context, context timeout, context cancellation, middleware anti-patterns"
weight: 21
---

# Middleware and Context Propagation

> Governing patterns for middleware chains and context propagation.
> Complements [ARCHITECTURE_GUIDE.md](./ARCHITECTURE_GUIDE.md) §3 (Server Struct Pattern),
> [ERROR_HANDLING.md](./ERROR_HANDLING.md) (recovery middleware),
> and [STRUCTURED_LOGGING.md](./STRUCTURED_LOGGING.md) (logging middleware).
>
> Read this together with [CLAUDE.md](../CLAUDE.md) for behavioral rules.

---

## 0. Quick Reference

- §1 Middleware Architecture: standard signature, chain ordering, chain composition
- §1a Standard middleware function signature and wrapper pattern
- §1b Middleware chain ordering rules (auth before logging, CSRF placement)
- §1c Middleware chain composition with variadic helpers
- §2 Context Propagation: type-safe keys, request ID, user metadata, multi-tenant
- §2a Type-safe context key pattern (avoids collisions)
- §2b Request ID propagation through handler chain
- §2c User metadata and tenant data in context
- §2f Context timeout and cancellation patterns
- §3 Middleware self-review checklist
- §4 Middleware and context anti-patterns

---

## 1. Middleware Architecture

### 1a. Middleware Standard Function Signature

Middleware follows the standard `func(http.Handler) http.Handler` pattern:

```go
func RequestID(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        id := uuid.NewString()
        ctx := context.WithValue(r.Context(), requestIDKey, id)
        w.Header().Set("X-Request-ID", id)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

Key points:

- Accept a `http.Handler`, return a `http.Handler`.
- Call `next.ServeHTTP` to continue the chain. Forgetting this silently drops
  the request.
- Use `r.WithContext(ctx)` for context propagation -- never mutate the original
  request.
- Write response headers *before* calling `next.ServeHTTP` if the header must
  be present regardless of downstream behavior.

### 1b. Middleware Chain Ordering Rules

Order matters. The outermost middleware runs first and wraps all inner behavior.

Canonical ordering (outermost to innermost):

1. **Recovery** -- catches panics from all downstream handlers and middleware
2. **Request ID** -- assigns a correlation identifier before any logging occurs
3. **Logger** -- wraps the response to capture status and duration
4. **Security** -- CORS, CSRF, rate limiting, origin checking
5. **Auth** -- validates credentials and populates user context
6. **Application-specific** -- feature flags, tenant resolution, caching headers

### 1c. Middleware Chain Composition Helper

Compose middleware with a helper that applies them in declaration order:

```go
// Chain applies middleware in the order given. The first argument
// is the outermost middleware (runs first).
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
    for i := len(mw) - 1; i >= 0; i-- {
        h = mw[i](h)
    }
    return h
}

// Usage:
handler := Chain(
    appHandler,
    Recovery,
    RequestID,
    Logger(logger),
    Auth(tokenVerifier),
)
```

The reverse iteration ensures the first middleware in the slice is the outermost
wrapper, matching the mental model of "Recovery runs first."

---

## 2. Context Propagation

### 2a. Type-Safe Context Key Pattern

Always use an unexported type for context keys. Never use `string` or `int`
directly -- they collide across packages:

```go
// unexported type prevents collisions with other packages
type contextKey int

const (
    requestIDKey contextKey = iota
    userKey
    tenantKey
)
```

### 2b. Request ID Propagation Through Handler Chain

Assign the request ID as early as possible (outermost middleware). Include it in:

- all structured log entries for the request
- all outgoing HTTP requests via headers
- error responses as a support reference

```go
func RequestIDFrom(ctx context.Context) string {
    id, _ := ctx.Value(requestIDKey).(string)
    return id
}
```

Always check the type assertion with the comma-ok pattern. A missing or
wrong-typed context value must not panic.

### 2c. User Metadata and Authentication State in Context

After auth middleware validates credentials, store the authenticated user
identity in context:

```go
type User struct {
    ID       uuid.UUID
    Email    string
    Role     string
    TenantID uuid.UUID
}

func UserFrom(ctx context.Context) (User, bool) {
    u, ok := ctx.Value(userKey).(User)
    return u, ok
}

func MustUserFrom(ctx context.Context) User {
    u, ok := UserFrom(ctx)
    if !ok {
        panic("user not in context -- auth middleware missing")
    }
    return u
}
```

`MustUserFrom` is acceptable only in code paths guaranteed to run behind auth
middleware. In all other paths, use the two-return form and handle the missing
case explicitly.

### 2d. Multi-Tenant Context Isolation

For multi-tenant applications, resolve the tenant early (after auth) and
propagate via context:

```go
func TenantFrom(ctx context.Context) (uuid.UUID, bool) {
    id, ok := ctx.Value(tenantKey).(uuid.UUID)
    return id, ok
}
```

Repositories and services receive the tenant ID from context, never from global
state or ambient configuration.

### 2e. Passing Context to Downstream Services

Context flows through every layer:

- **Database queries**: use context-accepting methods
  (`pool.Query(ctx, ...)`, `tx.Exec(ctx, ...)`)
- **Outgoing HTTP**: use `http.NewRequestWithContext(ctx, ...)`
- **Service calls**: accept `context.Context` as the first parameter

Never use `context.Background()` in services or repositories. It severs
cancellation propagation and timeout enforcement.

### 2f. Context Timeout and Cancellation Patterns

Use `context.WithTimeout` for operations with bounded latency expectations:

```go
func (s *OrderService) Create(ctx context.Context, input OrderInput) (Order, error) {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    return s.repo.Insert(ctx, input)
}
```

For HTTP-level timeouts, prefer timeout middleware that wraps the entire request
lifecycle rather than per-operation timeouts in every handler.

---

## 3. Middleware Self-Review Checklist

Before merging application-layer code, confirm every applicable item:

- [ ] Recovery is the outermost middleware
- [ ] Request ID is assigned before logging middleware runs
- [ ] Context propagation uses `r.WithContext`, not request mutation
- [ ] Context keys are unexported typed constants, not strings
- [ ] Guard middleware (auth, rate limit) does not call `next` on failure
- [ ] Status-capturing writers implement `http.Flusher` if streaming is needed

---

## 4. Middleware and Context Anti-Patterns

These patterns cause bugs, test fragility, or security issues. Reject them in
code review.

### 4a. Context Anti-Patterns

- **String or int context keys.** They collide across packages. Always use an
  unexported typed constant.
- **Storing large objects in context.** Context carries request-scoped metadata
  (IDs, auth claims), not full domain objects, database results, or file
  contents.
- **Using `context.WithValue` for function parameters.** If a value is required
  by the function signature, make it an explicit parameter. Context is for
  cross-cutting concerns that flow through middleware, not for avoiding function
  arguments.

### 4b. Middleware Anti-Patterns

- **Writing the response before calling `next`.** This prevents downstream
  handlers from setting headers or status codes. Write *after* `next.ServeHTTP`
  unless the middleware is intentionally short-circuiting.
- **Forgetting to call `next.ServeHTTP`.** The request silently disappears. A
  middleware that does not call next must explicitly write a response.
- **Recovery middleware in the wrong position.** If Recovery is inside other
  middleware, panics in the outer middleware crash the process.
