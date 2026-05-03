---
title: Error Handling and Resilience
description: "AppHandler pattern, domain errors, error sentinels, error taxonomy, web.Error, error codes, error stability, structured error events, OpenTelemetry tracing, panic policy, recovery middleware, security at boundaries, retry, transient failures, backoff, jitter, circuit breakers, bulkheads, resilience"
weight: 23
---

# Error Handling and Resilience

> Governing patterns for error handling, panic recovery, and resilience in Go web applications.
> Complements [PRODUCTION_GO_RULES.md](./PRODUCTION_GO_RULES.md) §1b (explicit error handling rule),
> [MIDDLEWARE_AND_CONTEXT.md](./MIDDLEWARE_AND_CONTEXT.md) §1 (middleware chain where recovery is ordered),
> [ARCHITECTURE_GUIDE.md](./ARCHITECTURE_GUIDE.md) §3 (server struct and AppHandler wiring),
> and [DATA_STORAGE.md](./DATA_STORAGE.md) §4d (repository sentinel errors).
>
> Read this together with [CLAUDE.md](../CLAUDE.md) for behavioral rules.

---

## 0. Quick Reference

- §1 AppHandler Pattern: error-returning handlers, domain error → HTTP status mapping
- §1a AppHandler error-returning handler signature
- §1b Domain error sentinel definitions
- §1c Predefined web.Error responses
- §1d Centralized error handler function
- §2 Error Taxonomy: web.Error transport primitive, boundary design, notification policy
- §3 Error Code Stability: versioning rules for public error codes
- §4 Structured Error Events: schema for error telemetry
- §5 OpenTelemetry Tracing: span and error event instrumentation
- §6 Panic Policy: allowed panics, recovery rules, goroutine panics
- §7 Security at Boundaries: deadline provenance, context error handling
- §8 Retry and Resilience: transient errors, backoff, circuit breakers, bulkheads
- §9 Recovery Middleware: panic recovery, AppHandler integration
- §10 Error handling self-review checklist
- §11 Error handling anti-patterns

---

## 1. Centralized Error Handling

### 1a. AppHandler Error-Returning Handler Pattern

Define a custom handler type that returns an error, separating error handling
from business logic:

```go
// AppHandler is an HTTP handler that returns an error for centralized handling.
type AppHandler func(w http.ResponseWriter, r *http.Request) error

func (fn AppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if err := fn(w, r); err != nil {
        handleError(w, r, err)
    }
}
```

This eliminates repetitive error-handling boilerplate in every handler.

### 1b. Domain Error Sentinels and HTTP Status Mapping

Use a structured error type that carries a machine-readable code, user-safe
message, and optional detail:

```go
type AppError struct {
    Code    int    // HTTP status code
    Message string // user-safe message
    Detail  string // internal detail for logging (never sent to client)
}

func (e *AppError) Error() string {
    return e.Message
}
```

### 1c. Predefined web.Error Response Constants

Define constructor functions for common error categories:

```go
func ErrNotFound(msg string) *AppError {
    return &AppError{Code: 404, Message: msg}
}

func ErrUnauthorized(msg string) *AppError {
    return &AppError{Code: 401, Message: msg}
}

func ErrForbidden(msg string) *AppError {
    return &AppError{Code: 403, Message: msg}
}

func ErrBadRequest(msg string) *AppError {
    return &AppError{Code: 400, Message: msg}
}

func ErrConflict(msg string) *AppError {
    return &AppError{Code: 409, Message: msg}
}
```

### 1d. Centralized Error Handler Function

Map known errors to specific responses; unknown errors become 500:

```go
func handleError(w http.ResponseWriter, r *http.Request, err error) {
    var appErr *AppError
    if errors.As(err, &appErr) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(appErr.Code)
        json.NewEncoder(w).Encode(map[string]string{
            "error":  appErr.Message,
            "detail": appErr.Detail,
        })
        return
    }

    // Unknown error -- log internally, return generic message
    slog.ErrorContext(r.Context(), "unhandled error",
        slog.String("error", err.Error()),
        slog.String("request_id", RequestIDFrom(r.Context())),
    )

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusInternalServerError)
    json.NewEncoder(w).Encode(map[string]string{
        "error": "internal server error",
    })
}
```

### 1e. Error Handler Rules and Constraints

- Never leak internal error details to clients. The user sees the `Message`;
  the server logs the full causal chain.
- Use a consistent JSON error response format: `{"error": "...", "detail": "..."}`
  where `detail` is omitted for unknown errors.
- Map domain errors to application errors at the handler layer, not in services
  or repositories.

For the canonical transport error type used in this project, see
section 5a below.

---

## 2. Error Handling Taxonomy

### 2a. Core Error Handling Principles

- **Return by default.** Unexpected behavior is returned as an error to the owning boundary. Do not log at every stack frame.
- **Log at ownership boundaries.** The code that can add operational context and decide impact should log. Intermediate layers wrap and return.
- **Classify at transport boundaries.** Reusable packages do not decide HTTP status codes. Handlers and boundaries own mapping.
- **Notify only on actionable server-side failures.** Notification is stricter than logging.
- **Panic only for programmer or invariant faults.**

### 2b. Shared Error Type Taxonomy

| Category | Examples | Handling |
|----------|----------|----------|
| Expected domain outcomes | Not found, validation failures, version conflicts, unauthorized | Return typed/sentinel errors; do not log; map at boundary; do not notify |
| Client or input misuse | Invalid content type, oversized body, malformed headers | Return contextual errors; classify 4xx at boundary; log only if rate/pattern matters |
| Dependency or integration failures | DB timeout, failed write, upstream 5xx | Wrap with operation context; log once at boundary; notify if actionable |
| Internal invariant faults | Impossible state, nil required dependency, invalid config | Fail fast; use panic only for invariant violations; recover at boundaries; notify |

### 2c. The web.Error Transport Primitive

`*web.Error` (in `pkg/web/errors.go`) is the canonical transport-facing error type. It carries HTTP status, a machine-readable `Code`, a user-safe `Message`, an internal `Cause`, and RFC 7807 fields (`TypeURI`, `Instance`).

**Construction rule:** Always use constructors in `pkg/web/errors.go` (`web.ErrBadRequest`, `web.ErrInternal`, `web.ErrConflict`, etc.). Never build `&web.Error{...}` outside `pkg/web`.

**Rendering rule:** `PublicMessage()` is the user-safe accessor. `.Error()` returns `Message`, never surfacing `Cause`. Do not call `.Error()` on `*web.Error` in intermediate code — use `errors.Is`/`errors.As` for branching.

### 2d. Per-Package Error Ownership Rules

Packages should:
- return ordinary Go errors
- wrap with `%w` and operation context
- keep sentinel errors near the owning domain
- document errors callers branch on
- use `errors.Is` and `errors.As`

Packages should not:
- decide HTTP status codes
- emit duplicate logs for returned failures
- notify external systems directly (unless owning a background boundary)
- panic for expected runtime failures

### 2e. Boundary Two-Signal Error Design

The HTTP boundary emits two log entries per request:

- **`http.error`**: causal error, request ID, status, error code — for alerting and trace correlation
- **`http.request`**: method, path, latency, status — for traffic analysis and audit

The "do not log at every stack frame" rule applies to intermediate layers. It does not restrict the boundary from emitting both signals.

### 2f. Error Notification and Logging Policy

Notify when the event is:
- production-impacting and server-side
- likely to require operator action
- a repeated dependency failure
- a recovered panic
- security-relevant
- a data-loss or consistency risk

Do not notify for: routine 4xx, expected domain errors, one-off user mistakes, self-healing failures, `context.Canceled` from client disconnect.

### 2g. Error Type Decision Table

| Situation | Return | Log | Notify | Panic |
|----------|--------|-----|--------|-------|
| Validation or not-found | Yes | No | No | No |
| Malformed request | Yes | Usually no | No | No |
| Dependency timeout or failed I/O | Yes | Yes, once at boundary | Maybe | No |
| Recovered runtime panic | Converted at boundary | Yes, error with stack | Yes | Originating code may |
| Client context cancelled | Yes | No (or DEBUG) | No | No |
| Server-set deadline exceeded | Yes | Yes, at boundary (ERROR) | Maybe | No |

---

## 3. Error Code Stability

`pkg/web/errors.go` defines `Code` string constants (`CodeInternal`, `CodeForbidden`, `CodeNotFound`, ...) for machine-readable API error responses.

### 3a. Error Code Stability and Versioning Rules

- Codes are **additive-only**: new codes may be added in any release.
- Codes are **never renamed or removed**.
- Codes are **never reused** for a different meaning.
- Document the condition and HTTP status mapping for every new code.

Only boundaries assign codes. Package internals return plain or sentinel errors.

---

## 4. Structured Error Event Schema

Every boundary error event must include these fields:

| Field | Type | When present |
|---|---|---|
| `event` | string | always (`"http.error"`, `"job.error"`, `"queue.error"`, `"lifecycle.error"`, `"panic.goroutine"`) |
| `severity` | log level | always (WARN for <500, ERROR for >=500) |
| `request_id` | string | when available |
| `trace_id` / `span_id` | string | when OTel tracing enabled |
| `status` | int | HTTP only |
| `code` | string | HTTP only (web.Code constant) |
| `op` | string | always (operation name) |
| `subsystem` | string | always (package/component) |
| `cause` | string | 5xx and server-owned timeouts |
| `stack` | string | recovered panics; 5xx when enabled |
| `dedupe_key` | string | notify-worthy events |

Non-HTTP boundaries (jobs, queue consumers, background goroutines) use the same schema. Omit `status` and `code` when not applicable.

---

## 5. OpenTelemetry Tracing Integration

When an OTel tracer is installed:

- Attach `trace_id` and `span_id` to every boundary event alongside `request_id`.
- On 5xx and recovered panics, set the span status to `codes.Error` and call `span.RecordError(err)`.
- Use OTel HTTP semantic conventions: `http.request.method`, `http.response.status_code`, `error.type`.
- `request_id` is user-facing (safe in error responses). `trace_id` is engineer-facing (not in client responses). Emit both where available.

Reference: `pkg/web/otelweb` — `otelweb.Middleware`, `otelweb.ExtractTraceID`, `otelweb.MakeOnError`.

---

## 6. Panic Policy

### 6a. Panic is Allowed — Programmer Errors and Invariants

- impossible internal states
- documented `Must*` constructors and fail-fast assembly helpers
- duplicate or invalid route registration at assembly time

### 6b. Panic is Forbidden — Runtime and Business Errors

- malformed request input
- dependency outages
- validation failures
- missing records
- any runtime condition a caller can reasonably handle
- crossing a package boundary — recover before returning

### 6c. Panic Recovery Rules

- Recover at top-level boundaries only.
- Attach stack traces. Convert to safe 5xx response.
- Recovery exists to convert crashes into logged, classified errors — not to silently suppress failures.

### 6d. Goroutine Panic Propagation

A panic inside a goroutine crashes the entire process unless a `recover` is installed within that goroutine. The HTTP boundary recovery does not extend into goroutines started by package code.

- Every goroutine must install its own `recover`.
- On recovery, log panic and stack at ERROR, then exit cleanly or signal failure.
- `errgroup` propagates returned errors but does **not** recover panics. Install `recover` within each errgroup goroutine.

---

## 7. Security and Deadline Policy at Boundaries

- **Stack traces never reach the client.** Recovered panics capture `runtime/debug.Stack()` for the server log only.
- **Error messages must not leak internal paths, SQL, schema, or filesystem structure.** Wrap in `*web.Error` before responding.
- **Do not embed untrusted input verbatim in log messages.** Use `slog` structured attributes.
- **Redact PII before logging.** Log categories (`"email: present"`), not raw values.
- **Auth-sensitive paths use constant-time comparison.** Verify MAC/signature before semantic checks (expiry, scope).
- **`context.Canceled` is not a server fault.** Do not log at ERROR or notify.
- **`context.DeadlineExceeded` is ownership-dependent.** Server-set deadline → 504/ERROR. Client-set deadline → non-fault.

### 7a. Deadline Provenance and Context Error Handling

Code that sets a server-owned deadline must mark the timeout before returning. Use `web.ErrDependencyTimeout` or a typed error. Bare `context.DeadlineExceeded` reaching the boundary is treated as client-set unless documented otherwise.

---

## 8. Retry, Transience, and Resilience

### 8a. Marking Transient Failure Errors

Use `errors.Join(web.ErrTransient, cause)` to mark retryable failures:

```go
cause := fmt.Errorf("cache: get: %w", err)
return errors.Join(web.ErrTransient, cause)
```

Only the package that owns the dependency classifies it as transient.

### 8b. Exponential Backoff with Jitter

Transient retries use exponential backoff with full jitter:
```
delay = random_between(0, min(cap, base * 2^attempt))
```
- Cap attempts at ≤3.
- Never retry past the caller's deadline.
- Reference implementation: `pkg/web/retry`.

### 8c. Idempotency for Safe Retries

Non-idempotent operations (create, payment, email) must not be retried without an idempotency key. The key must be deduplicated in persistent storage (not in-memory).

### 8d. Retry Budget and Attempt Limits

Retries share a global per-upstream budget. When exhausted, fail fast. Budget exhaustion is notification-worthy. Reference: `pkg/web/retrybudget`.

### 8e. Circuit Breaker Pattern

Open the breaker when transient failure rate exceeds a threshold. While open, reject with 503 + `Retry-After`. After recovery window, allow one probe. Reference: `pkg/web/breaker`.

### 8f. Bulkhead Isolation Pattern

Assign dedicated resource pools per upstream. When exhausted, fail fast with transient error. Reference: `pkg/web/bulkhead`.

---

## 9. Recovery Middleware

Recovery middleware catches panics to prevent a single request from crashing
the entire server.

```go
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            defer func() {
                if rv := recover(); rv != nil {
                    stack := debug.Stack()
                    logger.ErrorContext(r.Context(), "panic recovered",
                        slog.Any("panic", rv),
                        slog.String("stack", string(stack)),
                        slog.String("request_id", RequestIDFrom(r.Context())),
                        slog.String("method", r.Method),
                        slog.String("path", r.URL.Path),
                    )

                    w.Header().Set("Content-Type", "application/json")
                    w.WriteHeader(http.StatusInternalServerError)
                    json.NewEncoder(w).Encode(map[string]string{
                        "error": "internal server error",
                    })
                }
            }()
            next.ServeHTTP(w, r)
        })
    }
}
```

### 9a. Recovery Middleware Rules and Ordering

- Recovery must be the **outermost** middleware. If it is inside the logging
  middleware, the logger never sees the panic.
- Log the stack trace (`runtime/debug.Stack()`) at `Error` level.
- Never expose panic details to clients. The response is always a generic 500.
- Combined with the AppHandler pattern: AppHandler catches returned errors;
  Recovery catches panics. Together they cover both failure modes.

---

## 10. Error Handling Self-Review Checklist

Before merging application-layer code, confirm every applicable item:

- [ ] Every error is checked; no `_ =` without a documented reason
- [ ] Propagated errors are wrapped with `fmt.Errorf("context: %w", err)`
- [ ] `errors.Is` / `errors.As` used for branching; never `err.Error()` comparison
- [ ] Recovery middleware catches panics; AppHandler catches returned errors
- [ ] Stack traces are logged but never sent to clients
- [ ] `context.Canceled` is treated as a non-fault event

---

## 11. Error Handling Anti-Patterns

These patterns cause bugs, test fragility, or security issues. Reject them in
code review.

- **Logging and returning the same error.** This creates duplicate log entries
  for the same failure. Return the error and let the boundary log it once.
- **Leaking internal details in error responses.** SQL errors, file paths,
  stack traces, and dependency names must never appear in client-facing output.
- **Using `err == ErrFoo` instead of `errors.Is`.** Direct comparison breaks
  when the error is wrapped. Always use `errors.Is` or `errors.As`.

---

## 12. Sources

- `pkg/web/errors.go` — `*web.Error` transport primitive and predefined responses
- `pkg/web/code.go` — `web.Code` type and registered codes
- [Go blog: Error handling and Go](https://go.dev/blog/error-handling-and-go)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/)
