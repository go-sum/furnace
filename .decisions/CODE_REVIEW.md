---
title: Code Review Standards
description: "code review workflow, pre-review checklist, error handling review, concurrency review, interface and type review, resource lifecycle review, naming conventions, security review checklist, performance review, severity calibration, critical block-merge, major should-fix, minor, verification protocol, valid patterns, anti-patterns, architecture compliance"
weight: 30
---

# Code Review Standards

> This guide is the authoritative source for reviewing Go code in this
> repository. It consolidates review checklists, severity calibration,
> verification protocol, and recognized valid patterns into one reference.
>
> It complements [`ARCHITECTURE_GUIDE.md`](./ARCHITECTURE_GUIDE.md) (project
> structure, wiring, and shutdown), [`ERROR_HANDLING.md`](./ERROR_HANDLING.md)
> (boundary and resilience patterns), [`MIDDLEWARE_AND_CONTEXT.md`](./MIDDLEWARE_AND_CONTEXT.md)
> (middleware and request context flow), [`DATA_STORAGE.md`](./DATA_STORAGE.md)
> (persistence patterns), and [`WEB_DESIGN.md`](./WEB_DESIGN.md) (concurrency
> and runtime safety).

---

## 0. Quick Reference

- §1 Review workflow: pre-review prep, during review process, output format
- §2 Review checklists: error handling, concurrency, interfaces, resources, naming, security, performance
- §3 Common mistakes: resource leaks, naming violations, sync misuse, anti-patterns
- §4 Severity calibration: critical (block merge), major (should fix), minor, informational
- §5 Verification protocol: general rules, by issue type, checklist
- §6 Valid patterns: patterns that look wrong but are correct — do NOT flag
- §7 Context-sensitive rules: when standard rules have exceptions
- §8 Architecture and layer compliance checks

---

## 1. Review Workflow

### 1a. Pre-Review Preparation Steps

1. **Check `go.mod` for the Go version.** The Go version determines which
   patterns are applicable:
   - Go 1.18+: `any` replaces `interface{}`; generics available
   - Go 1.20+: `errors.Join` for multi-error accumulation
   - Go 1.21+: `slog` replaces `log` for structured output
   - Go 1.22+: loop variable capture is fixed; `range` over integers
   - Go 1.23+: iterator functions (`iter.Seq`, `iter.Seq2`)

2. **Read full functions, not just diffs.** Surrounding code often contains
   guards, cleanup, or context that invalidates a finding.

3. **Work through each checklist category** in section 2.

4. **Verify every finding** against the protocol in section 5 before reporting.

### 1b. During Review Process

- Review one checklist category at a time. Do not mix concerns.
- For each potential issue, read the full function and its callers before
  writing a comment.
- Classify every finding by severity (section 4) in the report.

### 1c. Review Output Format and Comment Structure

Report each finding as:

```
[FILE:LINE] ISSUE_TITLE
Severity: Critical | Major | Minor | Informational
Description and why it matters.
```

Group findings by file, then by severity (critical first).

---

## 2. Review Checklists

### 2a. Error Handling Review Checklist

- [ ] All errors checked (no `_ = err` without a justifying comment)
- [ ] Errors wrapped with context (`fmt.Errorf("pkg: op: %w", err)`)
- [ ] `errors.Is` / `errors.As` used instead of string matching or `==`
- [ ] `errors.Join` used for aggregating multiple errors (Go 1.20+)
- [ ] Zero values returned alongside non-nil errors
- [ ] Sentinel errors declared in a single `var ( ... )` block per file
- [ ] Error message strings follow `"<pkg>: <lowercase, no trailing period>"` format
- [ ] `%w` used when wrapping (never `%v` or `%s` on error values)
- [ ] Domain errors returned from services; transport mapping at handler boundary
- [ ] No logging-and-returning the same error in intermediate layers
- [ ] `*web.Error` not constructed with `&web.Error{...}` outside `pkg/web`
- [ ] Typed errors used only when callers need to extract structured data via `errors.As`; sentinels for all other branching
- [ ] Const message strings used when the same literal appears in 2+ places in a package
- [ ] `errors.Join` used for multi-error accumulation (Go 1.20+), not string concatenation
- [ ] No `fmt.Errorf("...: %w: %w", ...)` double-wrapping — use `errors.Join` instead

```go
// Bad: string matching on error
if err.Error() == "not found" {
    return http.StatusNotFound
}

// Bad: bare equality breaks when error is wrapped
if err == ErrNotFound {
    return http.StatusNotFound
}

// Good: unwrap-aware comparison
if errors.Is(err, ErrNotFound) {
    return http.StatusNotFound
}
```

```go
// Bad: error without context
return err

// Bad: severs error chain with %v
return fmt.Errorf("save failed: %v", err)

// Good: wraps with operation context
return fmt.Errorf("orders: save: %w", err)
```

```go
// Bad: log and return (double-signals the same failure)
if err != nil {
    slog.Error("save failed", "err", err)
    return err
}

// Good: return with context, let the boundary log
if err != nil {
    return fmt.Errorf("orders: save: %w", err)
}
```

### 2b. Concurrency Safety Review Checklist

- [ ] No goroutine leaks (context cancellation or shutdown signal exists)
- [ ] Channels closed by sender only, exactly once
- [ ] Shared state protected by mutex or sync types
- [ ] `sync.WaitGroup` used for goroutine completion tracking
- [ ] Context propagated through call chain (never discarded)
- [ ] Loop variable capture safe (pre-Go 1.22: explicit copy required)
- [ ] Every goroutine has a clear owner and shutdown path
- [ ] Channel direction specified in function parameters (`chan<-`, `<-chan`)
- [ ] No fire-and-forget goroutines from handlers or services
- [ ] Every goroutine installs its own `recover` (panics do not cross goroutine boundaries)
- [ ] `errgroup` goroutines have `recover` installed (errgroup does not recover panics)

```go
// Bad (pre-Go 1.22): loop variable captured by reference
for _, item := range items {
    go func() {
        process(item) // captures loop variable
    }()
}

// Good (pre-Go 1.22): explicit copy
for _, item := range items {
    item := item
    go func() {
        process(item)
    }()
}

// Good (Go 1.22+): loop variable semantics fixed, no copy needed
for _, item := range items {
    go func() {
        process(item)
    }()
}
```

```go
// Bad: goroutine without shutdown path
go func() {
    for msg := range ch {
        handle(msg)
    }
}()

// Good: goroutine with context cancellation and recover
go func() {
    defer func() {
        if r := recover(); r != nil {
            slog.ErrorContext(ctx, "panic.goroutine",
                "panic", r,
                "stack", string(debug.Stack()),
            )
        }
    }()
    for {
        select {
        case <-ctx.Done():
            return
        case msg := <-ch:
            handle(msg)
        }
    }
}()
```

### 2c. Interface and Type Design Review

- [ ] Interfaces defined by consumers, not producers
- [ ] Interface names follow `-er` convention where possible
- [ ] Minimal interfaces (1-3 methods preferred)
- [ ] Concrete types returned from constructors
- [ ] `any` used instead of `interface{}` (Go 1.18+)
- [ ] Generics used where type-safe abstraction reduces code
- [ ] No field-for-field wrapper types that add no semantics
- [ ] No speculative interfaces (interface has a real consumer with a substitution need)

```go
// Bad: producer defines interface and returns it
type UserService interface {
    GetByID(ctx context.Context, id uuid.UUID) (User, error)
}

func NewUserService(repo UserRepo) UserService {
    return &userService{repo: repo}
}

// Good: constructor returns concrete type; consumer defines interface
func NewUserService(repo UserRepo) *UserService {
    return &UserService{repo: repo}
}
```

```go
// Bad: Go < 1.18 style
func Process(data interface{}) {}

// Good: Go 1.18+ style
func Process(data any) {}
```

### 2d. Resource and Lifecycle Management Review

- [ ] Resources closed with `defer` immediately after creation
- [ ] HTTP response bodies always closed
- [ ] No `defer` in loops without closure wrapping
- [ ] `init()` functions avoided (prefer explicit construction)
- [ ] No hidden singletons or implicit globals
- [ ] Package-level infrastructure initialization avoided
- [ ] Constructors accept dependencies as parameters

```go
// Bad: resource leak
resp, err := http.Get(url)
if err != nil {
    return err
}
// forgot to close body

// Good: close immediately after error check
resp, err := http.Get(url)
if err != nil {
    return fmt.Errorf("fetch: %w", err)
}
defer resp.Body.Close()
```

```go
// Bad: defer in loop (defers stack until function returns)
for _, f := range files {
    file, err := os.Open(f)
    if err != nil {
        return err
    }
    defer file.Close() // all defers run at function exit, not loop iteration
    process(file)
}

// Good: closure wrapping
for _, f := range files {
    if err := func() error {
        file, err := os.Open(f)
        if err != nil {
            return err
        }
        defer file.Close()
        return process(file)
    }(); err != nil {
        return err
    }
}
```

### 2e. Naming Conventions and Style Review

- [ ] Exported names have doc comments
- [ ] No stuttering names (`user.UserService` should be `user.Service`)
- [ ] No naked returns in functions longer than 5 lines
- [ ] `context.Context` as first parameter
- [ ] `slog` used instead of `log` for structured output (Go 1.21+)
- [ ] Functions named verb-first (`GetByID`, `UpdateUser`, `ParseToken`)
- [ ] Error sentinels named `Err` + noun (`ErrNotFound`, `ErrExpired`)
- [ ] Constructors named `New` (exported) or `new` (package-private)
- [ ] No packages named `util`, `helper`, or `common`
- [ ] Early returns preferred over nested `if` blocks
- [ ] `cmp.Or` used for zero-value defaults (not `if x == "" { x = "default" }`)
- [ ] No `context.Background()` in services or repos (use passed context)
- [ ] `var x T` used when zero value is meaningful; `:=` for non-zero initialization
- [ ] Signal-boosting comments added when `err == nil` branch does significant work (`// if NO error`)
- [ ] `context.Context` never placed in option structs — always a direct function parameter

```go
// Bad: stuttering
package user
type UserService struct{}

// Good: package name provides context
package user
type Service struct{}
```

```go
// Bad: nested conditions
func Process(ctx context.Context, id string) error {
    if id != "" {
        u, err := repo.Get(ctx, id)
        if err == nil {
            if u.Active {
                return handle(u)
            }
        }
    }
    return ErrInvalid
}

// Good: early returns
func Process(ctx context.Context, id string) error {
    if id == "" {
        return ErrInvalid
    }
    u, err := repo.Get(ctx, id)
    if err != nil {
        return fmt.Errorf("process: get: %w", err)
    }
    if !u.Active {
        return ErrInvalid
    }
    return handle(u)
}
```

```go
// Bad: verbose zero-value default
if cfg.HeaderName == "" {
    cfg.HeaderName = "X-CSRF-Token"
}

// Good: cmp.Or for comparable zero-value defaults
cfg.HeaderName = cmp.Or(cfg.HeaderName, "X-CSRF-Token")
```

### 2f. Security Review Checklist

- [ ] No secrets, API keys, or credentials hardcoded in source
- [ ] User input validated at system boundaries
- [ ] File paths sanitized (prevent `../` traversal)
- [ ] Stack traces never reach the client
- [ ] Error messages do not leak internal paths, SQL, schema, or filesystem structure
- [ ] No PII or untrusted input embedded verbatim in log messages
- [ ] Auth-sensitive error paths use constant-time comparison
- [ ] `context.Canceled` treated as non-fault (not logged at ERROR, not notified)
- [ ] CSRF tokens validated on state-changing requests
- [ ] No `//nolint` directives without an accompanying reason

### 2g. Performance and Allocation Review

- [ ] No string concatenation with `+=` in loops (use `strings.Builder`)
- [ ] Slices preallocated only when capacity is known at call site
- [ ] No redundant nil-and-length checks (`len(nil)` returns 0)
- [ ] Switch arms with identical bodies merged with comma-separated case lists
- [ ] `range` over integer used where appropriate (Go 1.22+)

```go
// Bad: string concatenation in loop
var result string
for _, s := range items {
    result += s + ","
}

// Good: strings.Builder
var b strings.Builder
for _, s := range items {
    if b.Len() > 0 {
        b.WriteByte(',')
    }
    b.WriteString(s)
}
result := b.String()
```

```go
// Bad: redundant nil check
if items != nil && len(items) > 0 {
    process(items)
}

// Good: len handles nil slices
if len(items) > 0 {
    process(items)
}
```

### 2h. Function and Type Design Review

- [ ] Functions focused on one job with obvious happy path
- [ ] Early returns preferred over nested branching
- [ ] Side effects explicit (no hidden I/O in constructors)
- [ ] Concrete types preferred by default; interfaces added only when a consumer needs a substitution seam
- [ ] No field-for-field wrapper types that add no semantics
- [ ] Helper functions private unless another package genuinely needs them
- [ ] Option structs used when most callers need config; functional options when most need zero options
- [ ] Declarative constructs preferred over imperative loops when stdlib provides them (`cmp.Or`, `slices`, `maps`)

---

## 3. Common Mistakes Reference

### 3a. Resource Leak Patterns

| Mistake | Fix |
|---|---|
| Missing `defer` for `Close()` after resource creation | Add `defer` immediately after the `nil` error check |
| `defer` in loop body | Wrap loop body in a closure |
| HTTP response body not closed | `defer resp.Body.Close()` after error check |
| Database rows not closed | `defer rows.Close()` after error check |

### 3b. Naming Convention Violations

| Mistake | Fix |
|---|---|
| Stuttering names (`user.UserService`) | Drop the package prefix (`user.Service`) |
| Missing doc comments on exports | Add `// FuncName ...` doc comment |
| Naked returns in functions > 5 lines | Use explicit return values |
| Package named `util`, `helper`, `common` | Rename to what it provides |

### 3c. Initialization Anti-Patterns

| Mistake | Fix |
|---|---|
| `init()` performing side effects | Move to explicit constructor called from composition root |
| Global mutable state | Pass via constructor injection |
| Package-level infrastructure initialization | Initialize in composition root, inject as dependency |

### 3d. Structured Logging Mistakes

```go
// Bad: unstructured log
log.Printf("failed to save user %s: %v", id, err)

// Good: structured slog
slog.ErrorContext(ctx, "user: save",
    "user_id", id,
    "err", err,
)
```

### 3e. Testing Mistakes

| Mistake | Fix |
|---|---|
| Substring assertions on HTML | Use exact-match assertions |
| Mock generation libraries | Use hand-written fakes |
| Error identity by string matching | Use `errors.Is` / `errors.As` |
| Missing error path coverage | Add test cases for every handler error path |
| Unencoded HTML entities in test strings | Use `&#39;` for apostrophes, `&amp;` for ampersands |
| Missing sentinel tests | Every exported sentinel has a test triggering the condition and asserting `errors.Is` |
| Missing handler error path tests | Every error path in a handler has a dedicated test case |

### 3f. Sync Primitive Misuse

```go
// Bad: storing a pointer in sync.Pool (pointer to local may escape)
pool.Put(&buf)

// Good: store the value; let the pool manage allocation
pool.Put(buf)
```

### 3g. Functional Options Misuse

```go
// Good: functional options for optional configuration
type Option func(*Config)

func WithTimeout(d time.Duration) Option {
    return func(c *Config) {
        c.Timeout = d
    }
}

func New(opts ...Option) *Service {
    cfg := Config{Timeout: 30 * time.Second}
    for _, opt := range opts {
        opt(&cfg)
    }
    return &Service{cfg: cfg}
}
```

### 3h. Structural Anti-Patterns

| Mistake | Fix |
|---|---|
| Speculative abstractions | Only abstract when duplication is real and stable |
| Package globals as hidden request-time dependencies | Pass via constructor injection |
| Duplicated defaults across app and external-module layers | Single source of truth for defaults |
| Field-for-field wrapper types with no semantic change | Use the original type directly |
| Transport code that owns SQL or business rules | Separate into service/repository layers |
| Business logic embedded in views | Move to services; views only render |
| Hardcoded route paths where named routes exist | Use named route references |
| Pointless error wrapping (`fmt.Errorf("%w", err)` with no added context) | Return the error directly |

---

## 4. Severity Calibration

### 4a. Critical Severity — Block Merge

These findings represent correctness, safety, or security defects that must be
resolved before the code is merged:

- Unchecked errors on I/O, network, or database operations
- Goroutine leaks (no shutdown path or context cancellation)
- Race conditions on shared state (missing mutex, unsynchronized map access)
- Unbounded resource accumulation (connections, goroutines, memory)
- Security vulnerabilities (injection, auth bypass, data exposure, path traversal)
- Secrets or credentials hardcoded in source
- Panic for recoverable errors in library code
- Non-idempotent operation retried without idempotency key

### 4b. Major Severity — Should Fix Before Merge

These findings represent significant quality or maintainability issues that
should be fixed in the current review cycle:

- Errors returned without operation context
- Missing `sync.WaitGroup` for spawned goroutines
- `panic` used for recoverable errors
- Context not propagated to downstream calls
- `context.Background()` used in services or repositories
- Intermediate layers logging and returning the same error
- HTTP status mapping in services or repositories (layer violation)
- Missing `defer` for resource cleanup
- `*web.Error` constructed with struct literal outside `pkg/web`

### 4c. Minor Severity — Consider Fixing

These findings are style or minor quality issues. Fix if convenient; acceptable
to defer:

- `interface{}` instead of `any` (Go 1.18+)
- Missing doc comments on exported symbols
- Stuttering names
- Slice not preallocated when size is known at call site
- `if x == "" { x = "default" }` instead of `cmp.Or`
- Verbose nil-and-length checks
- Switch arms with identical bodies not merged

### 4d. Informational — Note Only

These are observations, not defects. They do not require action in the current
review:

- Suggestions for new dependencies or modules
- Architectural ideas for future refactoring
- Test infrastructure improvements
- Performance optimizations without measurable impact
- Alternative approaches that are equivalent in correctness

---

## 5. Pre-Report Verification Protocol

Every finding must be verified before it is reported. False positives erode
trust in reviews and waste reviewer and author time.

### 5a. General Verification Rules

Before reporting any issue:

1. **Read the actual code, not just the diff.** The surrounding function may
   contain guards, cleanup, or early returns that make the finding invalid.
2. **Search for usages before claiming "unused."** Check all references,
   exports, reflection-based access, framework callbacks, and interface
   satisfaction.
3. **Check surrounding code for guards or earlier checks.** A missing nil check
   in a helper may be guaranteed by the caller.
4. **Verify syntax and API against current Go documentation.** Do not report
   deprecated patterns unless the `go.mod` version supports the replacement.
5. **Distinguish "wrong" from "different style."** Only flag style differences
   when they violate a rule in this document or the project's coding guides.
6. **Consider intentional design.** A pattern that looks unusual may be
   deliberate. Check comments, commit messages, and related code before
   reporting.

### 5b. Verification Steps by Issue Type

**"Unused Variable/Function"**
- Search all references across the workspace (not just the current file)
- Check if the symbol satisfies an interface
- Check for reflection-based or framework callback usage
- Check if the symbol is exported and consumed by other modules

**"Missing Validation/Error Handling"**
- Check if validation occurs at a higher level (caller, middleware, framework)
- Check if the function contract guarantees valid input
- Check if the error return is actionable at this call site

**"Type Assertion/Unsafe Cast"**
- Confirm it is a runtime type assertion, not a compile-time type annotation
- Check if runtime type narrowing has already occurred (interface check, switch)
- Check if the assertion is protected by a comma-ok pattern

**"Potential Memory Leak/Race Condition"**
- Verify cleanup does not exist elsewhere (deferred in caller, pool return)
- Check for mutex protection in surrounding code
- Confirm the shared state is actually accessed concurrently

**"Performance Issue"**
- Confirm the code path is hot (called frequently under load)
- Verify the impact is measurable, not theoretical
- Check if the current approach is intentional for readability

### 5c. Pre-Report Verification Checklist

Before adding a finding to the report, answer all applicable questions:

- [ ] I have read the full function, not just the changed lines
- [ ] I have checked callers and callees for relevant context
- [ ] I have verified the Go version supports the suggested alternative
- [ ] I have confirmed the issue is a defect or rule violation, not a style preference
- [ ] I have checked for comments or documentation explaining the pattern

If any answer is "no" or "unsure," investigate further before reporting.

---

## 6. Valid Patterns (Do NOT Flag)

The following patterns are recognized as valid in Go code. Do not flag them
unless a specific additional condition makes them incorrect:

| Pattern | Why it is valid |
|---|---|
| `_ = err` with a reason comment | Explicitly acknowledged; the comment explains why |
| `any` for truly generic code | Correct use of the type constraint |
| Naked returns in short functions (< 5 lines) | Readable when the function is trivially small |
| Channel without `close()` when consumer stops via context | Consumer does not range over the channel |
| Mutex protecting struct fields | Standard synchronization pattern |
| `//nolint` directive with reason comment | Intentional linter suppression with justification |
| `defer` in loop when function-scope cleanup is intentional | Author explicitly chose function-scope lifetime |
| Functional options pattern | Recognized Go idiom for optional configuration |
| `sync.Pool` for hot-path allocation reuse | Performance optimization for known hot paths |
| `context.Background()` in `main()` or tests | No parent context available at the process root |
| `select` with `default` for non-blocking send/receive | Standard non-blocking channel pattern |
| Short variable names in small scope (`i`, `n`, `ok`, `err`) | Go convention for limited-scope variables |
| `cmp.Or` for zero-value defaults | Project convention per ARCHITECTURE_GUIDE.md |
| Table-driven tests with subtests | Preferred test structure per project rules |
| Hand-written fakes over mock libraries | Project convention per test rules |

---

## 7. Context-Sensitive Rules

These rules apply only when the stated condition is met. Do not flag them
unconditionally:

| Rule | Flag only when |
|---|---|
| Missing error check | The error return is actionable at this call site |
| Goroutine leak | No context cancellation or shutdown path exists |
| Missing `defer` for cleanup | The resource is not explicitly closed elsewhere in the function |
| Interface pollution | Interface has > 1 method AND only a single consumer |
| Loop variable capture | `go.mod` specifies Go < 1.22 |
| Missing `slog` | `go.mod` specifies Go >= 1.21 AND code uses `log` for structured output |
| Missing `any` replacement | `go.mod` specifies Go >= 1.18 AND code uses `interface{}` |
| Missing `errors.Join` | `go.mod` specifies Go >= 1.20 AND code concatenates error strings |
| Missing `range` over integer | `go.mod` specifies Go >= 1.22 AND code uses `for i := 0; i < n; i++` without modification of `i` |
| `context.Background()` misuse | Code is in a service or repository (not `main()` or test) |
| Slice preallocation | Exact capacity is known at the call site |

---

## 8. Architecture and Layer Compliance

In addition to the code-level checklists above, every review must verify layer
discipline as defined in [`ARCHITECTURE_GUIDE.md`](./ARCHITECTURE_GUIDE.md):

- [ ] Handlers do not import repositories directly
- [ ] Services do not import Echo or render HTML
- [ ] Repositories do not decide HTTP status codes or redirects
- [ ] Views do not own business rules or persistence
- [ ] Route registration happens from the app layer, not feature packages
- [ ] Dependencies flow inward (app -> pkg, never pkg -> app)
- [ ] No circular dependencies between packages
- [ ] Reusable packages do not perform application bootstrapping
- [ ] External module internals are not reached into (use public API only)
- [ ] Intermediate layers do not log returned errors; the boundary emits the error event
- [ ] Transient dependency failures marked with `web.ErrTransient` where callers may retry
- [ ] Server-owned deadlines marked with `web.ErrDependencyTimeout` before reaching boundary
- [ ] `*web.Error.Error()` not called in intermediate code outside the boundary
- [ ] Recovered panics not double-logged between boundary and `OnPanic` hook
- [ ] Structured error events conform to the field schema in [`ERROR_HANDLING.md`](./ERROR_HANDLING.md) §4
- [ ] When OTel tracing installed, `trace_id` and `span_id` included in structured events

---

## 9. Sources

- Go Code Review Comments: <https://go.dev/wiki/CodeReviewComments>
- Effective Go: <https://go.dev/doc/effective_go>
- [`ARCHITECTURE_GUIDE.md`](./ARCHITECTURE_GUIDE.md)
- [`ERROR_HANDLING.md`](./ERROR_HANDLING.md)
- [`MIDDLEWARE_AND_CONTEXT.md`](./MIDDLEWARE_AND_CONTEXT.md)
- [`DATA_STORAGE.md`](./DATA_STORAGE.md)
- [`WEB_DESIGN.md`](./WEB_DESIGN.md)
