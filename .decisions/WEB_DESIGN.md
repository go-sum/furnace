---
title: Web Concurrency, Performance, and Runtime Safety
description: "goroutine rules, goroutine lifecycle, handler concurrency safety, worker pool pattern, errgroup, bounded concurrency, rate limiting, token bucket, per-IP rate limiting, API key rate limiting, 429 response, race detection, sync.Mutex, sync.RWMutex, sync.Map, channel safety, goroutine leaks, context propagation, concurrency anti-patterns, Go 1.21 sync.OnceValue, Go 1.22 loop variables"
weight: 35
---

# Web Concurrency, Performance, and Runtime Safety

> This guide is the authoritative source for concurrency, performance, and
> runtime safety patterns in Go web applications.
>
> It complements [`ARCHITECTURE_GUIDE.md`](./ARCHITECTURE_GUIDE.md) (project
> structure, wiring, and shutdown), [`ERROR_HANDLING.md`](./ERROR_HANDLING.md)
> (error taxonomy and resilience patterns), and [`CODE_REVIEW.md`](./CODE_REVIEW.md)
> (review checklists).
>
> Read this together with [`CLAUDE.md`](../CLAUDE.md) for behavioral rules.
>
> Use this guide to answer:
>
> - how to safely share state across concurrent HTTP handlers
> - when and how to use goroutines, worker pools, and errgroup
> - how to implement rate limiting at multiple granularities
> - how to detect, prevent, and test for data races
> - which synchronization primitive to choose for a given problem

---

## 0. Quick Reference

- §1 Goroutine rules: cost model, shutdown paths, channels vs mutexes, no raw goroutines in handlers
- §2 Handler safety: what is safe without sync, atomic types, mutex for complex state
- §3 Worker pool: implementation, sizing, backpressure, graceful shutdown, error handling
- §4 errgroup: basic usage, bounded concurrency with SetLimit, panic recovery
- §5 Rate limiting: token bucket, global middleware, per-IP, API key, 429 responses
- §6 Race detection: running the detector, sync primitives decision table, CI integration
- §7 Concurrency anti-patterns: goroutine leaks, unbounded channels, missing WaitGroup
- §8 Modern Go features: sync.OnceValue (Go 1.21+), errgroup.SetLimit (Go 1.20+), loop vars (Go 1.22+)

This guide is **prescriptive**. It defines the concurrency and safety patterns
that application code follows. Deviations are surfaced in code review and
addressed in the next refactor pass.

---

## 1. Core Concurrency Rules for Web Applications

### 1a. Goroutine Cost and Lifecycle Rules

Each goroutine starts with a 2-8 KB stack that grows as needed. Under
unbounded spawning, thousands of goroutines accumulate memory, exhaust file
descriptors, and cause OOM kills. Treat goroutine creation as a resource
allocation decision, not a zero-cost abstraction.

### 1b. Every Goroutine Must Have a Shutdown Path

A goroutine without a termination mechanism is a goroutine leak. Every
goroutine must terminate through one of:

- `context.Context` cancellation
- a signal on a dedicated channel
- a `sync.WaitGroup` completion
- natural return from a bounded operation

### 1c. Prefer Channels for Goroutine Communication

Use channels to pass data and coordinate work between goroutines. Use mutexes
to protect shared state within a single goroutine's critical section. Do not
mix the two for the same concern.

### 1d. Use Mutexes for Shared State Protection

When goroutines share mutable state that does not flow through a channel, protect
it with `sync.Mutex`, `sync.RWMutex`, or `sync/atomic` types. Choose the
narrowest primitive that covers the access pattern.

### 1e. No Raw Goroutines Spawned in HTTP Handlers

An HTTP handler that calls `go func() { ... }()` directly creates an
uncontrolled goroutine with no backpressure, no lifecycle management, and no
panic recovery. Use a worker pool, `errgroup`, or an owned background subsystem
instead.

```go
// Do not do this in a handler.
func handleOrder(c echo.Context) error {
    go sendConfirmationEmail(order) // leaked goroutine, no recovery, no backpressure
    return c.JSON(http.StatusCreated, order)
}

// Use a worker pool or queue instead.
func handleOrder(c echo.Context) error {
    if !pool.TrySubmit(func() { sendConfirmationEmail(order) }) {
        slog.WarnContext(ctx, "worker pool full, email deferred")
    }
    return c.JSON(http.StatusCreated, order)
}
```

### 1f. Prefer Synchronous Code Before Reaching for Goroutines

Do not add goroutines, workers, or async dispatch until there is a concrete
correctness, latency, or throughput requirement. Synchronous code is simpler
to reason about, test, and debug. Concurrency is justified when measured
performance or architectural needs demand it.

---

## 2. Handler Safety

Every HTTP request runs in its own goroutine. Any mutable state on the server
struct, in package-level variables, or in shared closures is accessed
concurrently by every in-flight request.

### 2a. Handler Operations Safe Without Synchronization

| Resource | Why |
|---|---|
| Request-scoped locals | Each handler invocation has its own stack frame |
| `*sql.DB` / `*pgxpool.Pool` | Internal connection pooling with its own synchronization |
| Immutable config loaded at startup | No writes after initialization |
| `context.Context` values | Immutable once set; read-only in downstream code |

### 2b. Handler Operations Requiring Synchronization

| Resource | Risk | Solution |
|---|---|---|
| `map` on server struct | Concurrent read/write panics | `sync.RWMutex` or `sync.Map` |
| `[]T` on server struct | Concurrent append corrupts backing array | `sync.Mutex` |
| Counter or flag | Lost updates, torn reads | `atomic.Int64`, `atomic.Bool` |
| In-memory cache | Stale reads, corrupt entries | `sync.RWMutex` for read-heavy; `sync.Mutex` for write-heavy |
| Lazy-initialized singleton | Double initialization, partial state | `sync.Once` or `sync.OnceValue` |

### 2c. Atomic Types for Simple Flags and Counters

Use `atomic.Bool`, `atomic.Int64`, `atomic.Int32`, and `atomic.Pointer[T]` for
single-value state that does not require multi-field consistency.

```go
type Server struct {
    healthy    atomic.Bool
    reqCount   atomic.Int64
}

func (s *Server) handleHealth(c echo.Context) error {
    if !s.healthy.Load() {
        return c.NoContent(http.StatusServiceUnavailable)
    }
    s.reqCount.Add(1)
    return c.NoContent(http.StatusOK)
}
```

### 2d. Mutex for Complex Shared State

Use `sync.RWMutex` when reads vastly outnumber writes. Use `sync.Mutex` when
the read/write ratio is balanced or writes are frequent.

```go
type Server struct {
    mu    sync.RWMutex
    cache map[string]CachedItem
}

func (s *Server) handleGetCached(c echo.Context) error {
    key := c.Param("key")
    s.mu.RLock()
    item, ok := s.cache[key]
    s.mu.RUnlock()
    if !ok {
        return c.NoContent(http.StatusNotFound)
    }
    return c.JSON(http.StatusOK, item)
}

func (s *Server) handleInvalidate(c echo.Context) error {
    key := c.Param("key")
    s.mu.Lock()
    delete(s.cache, key)
    s.mu.Unlock()
    return c.NoContent(http.StatusOK)
}
```

---

## 3. Worker Pool Pattern

### 3a. Worker Pool Rationale and Use Cases

Worker pools solve three problems that raw goroutines do not:

1. **Bounded concurrency** -- a fixed number of workers prevents OOM under load
2. **Backpressure** -- a full job queue signals the caller to shed load
3. **Graceful shutdown** -- workers drain in-flight work before the process exits

### 3b. Worker Pool Implementation with Buffered Channels

```go
type WorkerPool struct {
    jobs chan func()
    wg   sync.WaitGroup
}

func NewWorkerPool(workers, queueSize int) *WorkerPool {
    p := &WorkerPool{
        jobs: make(chan func(), queueSize),
    }
    p.wg.Add(workers)
    for range workers {
        go p.worker()
    }
    return p
}

func (p *WorkerPool) worker() {
    defer p.wg.Done()
    for job := range p.jobs {
        func() {
            defer func() {
                if r := recover(); r != nil {
                    slog.Error("panic.goroutine",
                        "panic", r,
                        "stack", string(debug.Stack()),
                    )
                }
            }()
            job()
        }()
    }
}

// Submit blocks until the job is accepted or the pool is shut down.
func (p *WorkerPool) Submit(job func()) {
    p.jobs <- job
}

// TrySubmit returns false immediately if the queue is full.
func (p *WorkerPool) TrySubmit(job func()) bool {
    select {
    case p.jobs <- job:
        return true
    default:
        return false
    }
}

// Shutdown closes the job channel and waits for in-flight work to complete.
func (p *WorkerPool) Shutdown() {
    close(p.jobs)
    p.wg.Wait()
}
```

### 3c. Worker Pool Sizing Guidance

| Workload type | Worker count | Rationale |
|---|---|---|
| CPU-bound (image resize, hashing) | `runtime.NumCPU()` | More workers than cores adds context-switch overhead |
| I/O-bound (HTTP calls, DB queries) | 10x-100x CPU count | Workers spend most time waiting; more workers keep throughput high |
| Mixed | Profile and measure | Start with 2x CPU count and adjust based on p99 latency |

### 3d. Queue Sizing and Backpressure Handling

- **Small queue** (0-10): fast backpressure signal; callers learn immediately
  when the pool is saturated
- **Large queue** (100-1000): absorbs traffic bursts; risk of high memory usage
  and delayed processing if workers are slow

For HTTP handlers, prefer a small queue with `TrySubmit`. Return 503 Service
Unavailable when the pool is full rather than blocking the request goroutine:

```go
func (s *Server) handleWebhook(c echo.Context) error {
    ctx := c.Request().Context()
    payload := extractPayload(c)

    if !s.pool.TrySubmit(func() { s.processWebhook(payload) }) {
        slog.WarnContext(ctx, "worker pool full, rejecting webhook")
        return c.NoContent(http.StatusServiceUnavailable)
    }
    return c.NoContent(http.StatusAccepted)
}
```

### 3e. Worker Pool Graceful Shutdown Integration

Stop accepting new HTTP connections first, then drain the worker pool, then
close downstream resources:

```go
func run(ctx context.Context) error {
    pool := NewWorkerPool(runtime.NumCPU()*10, 100)

    srv := &http.Server{Addr: ":8080", Handler: router}
    go func() {
        <-ctx.Done()
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        srv.Shutdown(shutdownCtx) // stop accepting new requests
    }()

    err := srv.ListenAndServe()
    pool.Shutdown() // drain in-flight background work
    db.Close()      // close downstream resources last
    return err
}
```

### 3f. Error Handling Inside Worker Goroutines

Workers must never silently discard errors. Log all failures with structured
logging:

```go
func (s *Server) processWebhook(payload WebhookPayload) {
    ctx := context.Background() // request context is already cancelled
    if err := s.webhookSvc.Deliver(ctx, payload); err != nil {
        slog.ErrorContext(ctx, "webhook delivery failed",
            "webhook_id", payload.ID,
            "err", err,
        )
    }
}
```

### 3g. Context Propagation Through Worker Pools

Workers must use their own context, not the request context. The request
context is cancelled as soon as the HTTP response is sent. Extract trace IDs,
user IDs, and other correlation data before submitting:

```go
func (s *Server) handleOrder(c echo.Context) error {
    ctx := c.Request().Context()
    orderID := c.Param("id")
    userID := auth.UserID(ctx)
    traceID := web.RequestID(c)

    s.pool.Submit(func() {
        bgCtx := context.Background()
        s.sendReceipt(bgCtx, orderID, userID, traceID)
    })
    return c.NoContent(http.StatusAccepted)
}
```

### 3h. Retry with Exponential Backoff in Workers

For transient failures in worker tasks, use exponential backoff with jitter.
See [`ERROR_HANDLING.md`](./ERROR_HANDLING.md) §8b for the canonical backoff
policy and `pkg/web/retry` for the reference implementation.

---

## 4. errgroup as a Simpler Alternative

`errgroup.Group` from `golang.org/x/sync/errgroup` is the preferred tool for
fan-out/fan-in within a single request. It propagates errors, cancels on first
failure, and supports bounded concurrency.

### 4a. errgroup Basic Usage and Error Collection

```go
func (s *Server) handleDashboard(c echo.Context) error {
    ctx := c.Request().Context()
    g, ctx := errgroup.WithContext(ctx)

    var stats Stats
    var recent []Order
    var alerts []Alert

    g.Go(func() error {
        var err error
        stats, err = s.statsSvc.Get(ctx)
        return err
    })
    g.Go(func() error {
        var err error
        recent, err = s.orderSvc.ListRecent(ctx, 10)
        return err
    })
    g.Go(func() error {
        var err error
        alerts, err = s.alertSvc.ListActive(ctx)
        return err
    })

    if err := g.Wait(); err != nil {
        return fmt.Errorf("dashboard: %w", err)
    }
    return c.JSON(http.StatusOK, DashboardResponse{stats, recent, alerts})
}
```

### 4b. Bounded Concurrency with errgroup.SetLimit

```go
func (s *Server) handleBatchProcess(c echo.Context) error {
    ctx := c.Request().Context()
    items := parseItems(c)

    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(10) // at most 10 concurrent goroutines

    for _, item := range items {
        g.Go(func() error {
            return s.processSvc.Process(ctx, item)
        })
    }
    return g.Wait()
}
```

### 4c. Panic Recovery in errgroup Goroutines

`errgroup` propagates returned errors but does **not** recover panics. An
unrecovered panic in an errgroup goroutine crashes the entire process. Install
a `recover` inside each goroutine:

```go
g.Go(func() (err error) {
    defer func() {
        if r := recover(); r != nil {
            slog.Error("panic.goroutine",
                "panic", r,
                "stack", string(debug.Stack()),
            )
            err = fmt.Errorf("recovered panic: %v", r)
        }
    }()
    return s.riskyOperation(ctx)
})
```

### 4d. errgroup vs Worker Pool Decision Guide

| Scenario | Tool | Reason |
|---|---|---|
| Parallel queries within a single request | errgroup | Scoped to request lifetime; context cancellation propagates automatically |
| Fan-out to multiple APIs then combine results | errgroup | Need all results before responding; first error cancels remaining |
| Batch processing bounded items within request | errgroup with SetLimit | Bounded concurrency with automatic error collection |
| Background tasks that outlive the request | Worker pool | Request context is already cancelled; need independent lifecycle |
| Fire-and-forget work (emails, webhooks, analytics) | Worker pool | No result needed in the response; needs backpressure and shutdown |
| Long-running background processing | Worker pool or queue | Must survive process restarts; needs its own lifecycle management |

---

## 5. Rate Limiting

### 5a. Token Bucket Rate Limiting Algorithm

The standard approach is `golang.org/x/time/rate`, which implements a token
bucket limiter. Each request consumes one token; tokens refill at a configured
rate. The burst parameter controls the maximum number of tokens available at
any instant.

### 5b. Global Rate Limiting Middleware

Apply a global limiter to protect the server from aggregate overload:

```go
func RateLimit(rps float64, burst int) echo.MiddlewareFunc {
    limiter := rate.NewLimiter(rate.Limit(rps), burst)
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            if !limiter.Allow() {
                c.Response().Header().Set("Retry-After", "1")
                return c.NoContent(http.StatusTooManyRequests)
            }
            return next(c)
        }
    }
}
```

### 5c. Per-IP Rate Limiting with Visitor Map

Per-IP limiting prevents a single client from consuming the global budget.

```go
type trackedLimiter struct {
    limiter  *rate.Limiter
    lastSeen time.Time
}

type IPRateLimiter struct {
    mu       sync.RWMutex
    limiters map[string]*trackedLimiter
    rps      rate.Limit
    burst    int
}

func NewIPRateLimiter(rps float64, burst int) *IPRateLimiter {
    rl := &IPRateLimiter{
        limiters: make(map[string]*trackedLimiter),
        rps:      rate.Limit(rps),
        burst:    burst,
    }
    go rl.cleanup()
    return rl
}

func (rl *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
    rl.mu.RLock()
    tl, ok := rl.limiters[ip]
    rl.mu.RUnlock()
    if ok {
        rl.mu.Lock()
        tl.lastSeen = time.Now()
        rl.mu.Unlock()
        return tl.limiter
    }

    // Double-check after acquiring write lock.
    rl.mu.Lock()
    defer rl.mu.Unlock()
    if tl, ok := rl.limiters[ip]; ok {
        tl.lastSeen = time.Now()
        return tl.limiter
    }
    limiter := rate.NewLimiter(rl.rps, rl.burst)
    rl.limiters[ip] = &trackedLimiter{limiter: limiter, lastSeen: time.Now()}
    return limiter
}

func (rl *IPRateLimiter) cleanup() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    for range ticker.C {
        rl.mu.Lock()
        cutoff := time.Now().Add(-10 * time.Minute)
        for ip, tl := range rl.limiters {
            if tl.lastSeen.Before(cutoff) {
                delete(rl.limiters, ip)
            }
        }
        rl.mu.Unlock()
    }
}
```

### 5d. Client IP Extraction from Request

Extract the client IP from `X-Forwarded-For` or `X-Real-IP` headers **only**
when the application runs behind a trusted reverse proxy. When exposed
directly, use `c.RealIP()` or `r.RemoteAddr`. Trusting forwarded headers
without a trusted proxy allows clients to spoof their IP and bypass rate
limits.

### 5e. Stale Rate Limiter Cleanup

The `trackedLimiter` pattern above tracks `lastSeen` per IP. A background
ticker removes entries that have not been seen within the cleanup window. This
prevents unbounded map growth from diverse client IPs.

### 5f. Per-API-Key and Per-User Rate Limiting

For authenticated endpoints, rate limit by user identity or API key rather than
IP. Tier-based limits allow differentiated service levels:

```go
type KeyRateLimiter struct {
    mu       sync.RWMutex
    limiters map[string]*trackedLimiter
    tiers    map[string]TierConfig
}

type TierConfig struct {
    RPS   float64
    Burst int
}

var defaultTiers = map[string]TierConfig{
    "free":       {RPS: 10, Burst: 20},
    "pro":        {RPS: 100, Burst: 200},
    "enterprise": {RPS: 1000, Burst: 2000},
}

func (krl *KeyRateLimiter) GetLimiter(apiKey, tier string) *rate.Limiter {
    krl.mu.RLock()
    tl, ok := krl.limiters[apiKey]
    krl.mu.RUnlock()
    if ok {
        krl.mu.Lock()
        tl.lastSeen = time.Now()
        krl.mu.Unlock()
        return tl.limiter
    }

    cfg := krl.tiers[tier]
    krl.mu.Lock()
    defer krl.mu.Unlock()
    if tl, ok := krl.limiters[apiKey]; ok {
        tl.lastSeen = time.Now()
        return tl.limiter
    }
    limiter := rate.NewLimiter(rate.Limit(cfg.RPS), cfg.Burst)
    krl.limiters[apiKey] = &trackedLimiter{limiter: limiter, lastSeen: time.Now()}
    return limiter
}
```

### 5g. Proper HTTP 429 Too Many Requests Responses

A 429 Too Many Requests response must include headers that tell the client
when to retry and what the limits are:

```go
func rateLimitResponse(c echo.Context, retryAfter time.Duration, limit, remaining int) error {
    h := c.Response().Header()
    h.Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
    h.Set("X-RateLimit-Limit", strconv.Itoa(limit))
    h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
    return c.NoContent(http.StatusTooManyRequests)
}
```

### 5h. Combining Multiple Rate Limiter Strategies

Apply per-IP rate limiting inside a global rate limiter for defense in depth.
The global limiter protects aggregate server capacity; the per-IP limiter
prevents a single client from monopolizing it:

```go
func CombinedRateLimit(globalRPS float64, globalBurst int, ipRPS float64, ipBurst int) echo.MiddlewareFunc {
    global := rate.NewLimiter(rate.Limit(globalRPS), globalBurst)
    perIP := NewIPRateLimiter(ipRPS, ipBurst)

    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            if !global.Allow() {
                c.Response().Header().Set("Retry-After", "1")
                return c.NoContent(http.StatusTooManyRequests)
            }
            ip := c.RealIP()
            if !perIP.GetLimiter(ip).Allow() {
                c.Response().Header().Set("Retry-After", "1")
                return c.NoContent(http.StatusTooManyRequests)
            }
            return next(c)
        }
    }
}
```

---

## 6. Race Detection and Prevention

### 6a. Running the Go Race Detector in Tests and CI

The Go race detector instruments memory accesses at compile time and reports
data races at runtime. It finds races that actually execute during the test
run, not static analysis of all possible races.

```bash
# Run tests with race detection.
go test -race ./...

# Build a race-instrumented binary for manual testing.
go build -race -o app-race ./cmd/server
```

The race detector adds approximately 5-15x CPU overhead and 5-10x memory
overhead. Do not run it in production. Run it in CI and during development.

### 6b. Race Detector Capabilities and Limitations

| Detected | Not detected |
|---|---|
| Concurrent read/write to the same memory location | Logical races (correct synchronization, wrong business logic) |
| Concurrent write/write to the same memory location | Deadlocks |
| Missing mutex around map access | Starvation |
| Missing atomic for flag/counter updates | Livelock |
| | Races in code paths not exercised by the test |

### 6c. Common Race Conditions in HTTP Handlers

**Shared map without lock:**

```go
// Race: concurrent handlers read and write s.cache.
type Server struct {
    cache map[string]string
}

// Fix: protect with sync.RWMutex or use sync.Map.
type Server struct {
    mu    sync.RWMutex
    cache map[string]string
}
```

**Incrementing counter without atomic:**

```go
// Race: s.count++ is a read-modify-write, not atomic.
type Server struct {
    count int64
}

// Fix: use atomic.Int64.
type Server struct {
    count atomic.Int64
}
```

**Slice append without lock:**

```go
// Race: concurrent append may corrupt the backing array.
type Server struct {
    events []Event
}

// Fix: protect with sync.Mutex.
type Server struct {
    mu     sync.Mutex
    events []Event
}
```

**Lazy initialization without sync.Once:**

```go
// Race: multiple goroutines may initialize simultaneously.
func (s *Server) getClient() *http.Client {
    if s.client == nil {
        s.client = &http.Client{Timeout: 10 * time.Second}
    }
    return s.client
}

// Fix: use sync.Once or sync.OnceValue.
func (s *Server) getClient() *http.Client {
    return s.clientOnce.Do(func() *http.Client {
        return &http.Client{Timeout: 10 * time.Second}
    })
}
```

**Read-modify-write on struct field:**

```go
// Race: s.ready may be read while another goroutine writes it.
type Server struct {
    ready bool
}

// Fix: use atomic.Bool.
type Server struct {
    ready atomic.Bool
}
```

### 6d. Sync Primitive Selection Decision Table

| Access pattern | Primitive | When to use |
|---|---|---|
| Single counter (increment, load) | `atomic.Int64` | High-frequency counting with no multi-field consistency requirement |
| Single boolean flag | `atomic.Bool` | Feature flags, health status, shutdown signal |
| Read-heavy cache (reads >> writes) | `sync.RWMutex` | Multiple concurrent readers, infrequent writers |
| Write-heavy map | `sync.Mutex` | Frequent writes; RWMutex overhead not justified |
| Cross-goroutine data flow | Channels | Producer-consumer, fan-out/fan-in, signaling |
| One-time initialization | `sync.Once` or `sync.OnceValue` | Lazy singletons, expensive setup |
| Write-once-read-many or disjoint keys | `sync.Map` | Keys are stable after initial write; no iteration needed |
| Typed lazy singleton (Go 1.21+) | `sync.OnceValue[T]` | Type-safe replacement for `sync.Once` + package variable |

### 6e. sync.Mutex vs sync.RWMutex Selection

Use `sync.RWMutex` only when reads significantly outnumber writes. `RWMutex`
has higher per-operation overhead than `Mutex` due to internal bookkeeping. If
the critical section is short and writes are frequent, a plain `Mutex` is
faster.

### 6f. sync.Map vs Mutex-Protected Map Trade-offs

`sync.Map` is optimized for two patterns:

1. Keys are written once and read many times (stable key set)
2. Multiple goroutines read, write, and overwrite entries for disjoint key sets

For all other patterns, a `sync.Mutex`- or `sync.RWMutex`-protected
`map[K]V` is simpler, more type-safe, and often faster.

Do not use `sync.Map` as a default replacement for `map` -- use it only when
profiling shows contention on a mutex-protected map, or the access pattern
matches one of the two cases above.

### 6g. Writing Tests That Expose Race Conditions

Write tests that exercise concurrent access to confirm synchronization is
correct:

```go
func TestServer_ConcurrentCacheAccess(t *testing.T) {
    srv := NewServer()
    var wg sync.WaitGroup

    // Concurrent writers.
    for i := range 100 {
        wg.Add(1)
        go func() {
            defer wg.Done()
            key := fmt.Sprintf("key-%d", i)
            srv.SetCache(key, "value")
        }()
    }

    // Concurrent readers.
    for range 100 {
        wg.Add(1)
        go func() {
            defer wg.Done()
            _ = srv.GetCache("key-50")
        }()
    }

    wg.Wait()
}
```

Write HTTP-level race tests using `httptest`:

```go
func TestHandler_ConcurrentRequests(t *testing.T) {
    srv := setupTestServer(t)
    ts := httptest.NewServer(srv.Handler())
    defer ts.Close()

    var wg sync.WaitGroup
    for range 50 {
        wg.Add(1)
        go func() {
            defer wg.Done()
            resp, err := http.Get(ts.URL + "/api/stats")
            if err != nil {
                t.Error(err)
                return
            }
            resp.Body.Close()
        }()
    }
    wg.Wait()
}
```

### 6h. Race Detector CI Pipeline Integration

Run the race detector in CI on every pull request:

```bash
go test -race -count=1 ./...
```

Set `GORACE="halt_on_error=1"` to fail the CI job on the first detected race
rather than continuing and potentially masking subsequent races:

```bash
GORACE="halt_on_error=1" go test -race -count=1 ./...
```

Use `-count=1` to disable test caching -- cached results do not re-run the
race detector.

---

## 7. Critical Concurrency Anti-Patterns

### 7a. Anti-Pattern — Goroutine Leak: No Shutdown Signal

A goroutine that blocks forever on a channel or loop without checking a
cancellation signal leaks for the lifetime of the process.

```go
// Anti-pattern: no exit path.
go func() {
    for {
        item := <-ch
        process(item)
    }
}()

// Fix: use context cancellation.
go func() {
    for {
        select {
        case <-ctx.Done():
            return
        case item := <-ch:
            process(item)
        }
    }
}()
```

### 7b. Anti-Pattern — Unbounded Channel Send Blocks Forever

A send to an unbounded or full channel blocks the goroutine indefinitely if no
receiver drains it.

```go
// Anti-pattern: blocks if ch is full and no one reads.
ch <- result

// Fix: use select with context or default.
select {
case ch <- result:
case <-ctx.Done():
    return ctx.Err()
}
```

### 7c. Anti-Pattern — Closing a Channel Multiple Times

Closing an already-closed channel panics. Only the sender should close a
channel, and it should close it exactly once.

```go
// Anti-pattern: multiple goroutines may close.
close(ch)

// Fix: use sync.Once to guarantee single close.
var closeOnce sync.Once
closeOnce.Do(func() { close(ch) })
```

### 7d. Anti-Pattern — Race Condition on Shared State

Any mutable state accessed by multiple goroutines without synchronization is a
data race. See section 6 for the full decision table.

### 7e. Anti-Pattern — Missing sync.WaitGroup for Goroutine Lifecycle

The caller returns before spawned goroutines complete, leading to lost work or
use-after-free on resources the goroutines depend on.

```go
// Anti-pattern: function returns while goroutines still run.
for _, item := range items {
    go process(item)
}
return // goroutines may still be running

// Fix: wait for all goroutines.
var wg sync.WaitGroup
for _, item := range items {
    wg.Add(1)
    go func() {
        defer wg.Done()
        process(item)
    }()
}
wg.Wait()
return
```

### 7f. Anti-Pattern — Context Not Propagated to Goroutines

Goroutines that ignore the parent context continue working after the caller
has cancelled, wasting resources and potentially writing stale results.

```go
// Anti-pattern: ignores cancellation.
go func() {
    result := expensiveQuery(context.Background()) // should use ctx
    ch <- result
}()

// Fix: propagate the parent context.
go func() {
    result := expensiveQuery(ctx)
    select {
    case ch <- result:
    case <-ctx.Done():
    }
}()
```

### 7g. Anti-Pattern — Unbounded Goroutine Spawning in Handlers

Spawning a goroutine per request under load creates thousands of goroutines
with no backpressure. Use a worker pool (section 3) or `errgroup` with
`SetLimit` (section 4).

### 7h. Anti-Pattern — context.Background() in HTTP Handlers

Handlers must use `c.Request().Context()`, not `context.Background()`. The
request context carries deadlines, cancellation, and trace propagation. Using
`context.Background()` severs all of those.

```go
// Anti-pattern.
result, err := svc.Query(context.Background(), id)

// Fix.
result, err := svc.Query(c.Request().Context(), id)
```

### 7i. Anti-Pattern — Goroutine Leak from No Channel Receiver

A goroutine that sends to a channel no one reads blocks forever. Use a buffered
channel of size 1 when the result may not be consumed:

```go
// Anti-pattern: blocks if caller times out and stops reading.
ch := make(chan Result)
go func() { ch <- computeResult() }()

// Fix: buffer of 1 lets the goroutine complete and exit.
ch := make(chan Result, 1)
go func() { ch <- computeResult() }()
```

### 7j. Anti-Pattern — time.Sleep for Goroutine Coordination

`time.Sleep` is not a synchronization primitive. It introduces flaky timing
dependencies and slows down tests.

```go
// Anti-pattern.
go updateCache()
time.Sleep(100 * time.Millisecond) // "wait" for cache update
readCache()

// Fix: use sync primitives.
var wg sync.WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    updateCache()
}()
wg.Wait()
readCache()
```

---

## 8. Modern Go Concurrency Features

### 8a. sync.OnceValue and sync.OnceFunc (Go 1.21+)

`sync.OnceValue[T]` and `sync.OnceFunc` are type-safe replacements for the
`sync.Once` + package variable pattern. They eliminate the need for a separate
variable to store the result.

```go
// Before (sync.Once + variable).
var (
    configOnce sync.Once
    config     *Config
)

func GetConfig() *Config {
    configOnce.Do(func() {
        config = loadConfig()
    })
    return config
}

// After (sync.OnceValue).
var GetConfig = sync.OnceValue(func() *Config {
    return loadConfig()
})
```

`sync.OnceFunc` is the equivalent for functions that return no value:

```go
var initMetrics = sync.OnceFunc(func() {
    prometheus.MustRegister(requestCounter, latencyHistogram)
})
```

### 8b. errgroup.SetLimit for Bounded Concurrency (Go 1.20+)

`errgroup.Group.SetLimit(n)` caps the number of goroutines that can run
concurrently. This eliminates the need for a manual semaphore channel when
using errgroup for bounded fan-out. See section 4 for examples.

### 8c. Range over Integer for Bounded Loops (Go 1.22+)

```go
// Before.
for i := 0; i < n; i++ { ... }

// After.
for i := range n { ... }
```

### 8d. Loop Variable Capture Fix (Go 1.22+)

Starting with Go 1.22, each iteration of a `for` loop creates a new variable.
The classic closure-capture bug no longer applies:

```go
// Before Go 1.22: all goroutines capture the same variable.
for _, item := range items {
    go func() {
        process(item) // bug: all goroutines see the last item
    }()
}

// Go 1.22+: each iteration has its own 'item'.
for _, item := range items {
    go func() {
        process(item) // correct: each goroutine has its own copy
    }()
}
```

---

## 9. Decision Checklist

Before merging code that introduces concurrency, confirm:

- every goroutine has an explicit shutdown path (context, channel, or WaitGroup)
- no raw `go func()` calls exist inside HTTP handlers
- shared mutable state is protected by the correct primitive (see section 6 table)
- worker pools use `TrySubmit` in handlers and return 503 when full
- errgroup goroutines install `recover` for panic safety
- background workers use their own context, not the request context
- trace IDs and correlation data are extracted before submitting to a worker pool
- rate limiters clean up stale entries to prevent unbounded map growth
- 429 responses include `Retry-After` headers
- CI runs `go test -race -count=1 ./...` with `GORACE="halt_on_error=1"`
- no `time.Sleep` is used for goroutine coordination
- no `context.Background()` is used where a request context is available
- channel direction is specified in function parameters (`chan<-`, `<-chan`)
- channels that may not be read use a buffer of 1 to prevent goroutine leaks
- `sync.Once` or `sync.OnceValue` is used for lazy initialization, not manual nil checks

---

## 10. Sources

- [`ERROR_HANDLING.md`](./ERROR_HANDLING.md) -- retry, circuit breaker, and resilience patterns
- Go Concurrency Patterns: <https://go.dev/blog/pipelines>
- Effective Go - Concurrency: <https://go.dev/doc/effective_go#concurrency>
- Go Data Race Detector: <https://go.dev/doc/articles/race_detector>
- golang.org/x/sync/errgroup: <https://pkg.go.dev/golang.org/x/sync/errgroup>
- golang.org/x/time/rate: <https://pkg.go.dev/golang.org/x/time/rate>
- Go 1.21 Release Notes (sync.OnceValue): <https://go.dev/doc/go1.21>
- Go 1.22 Release Notes (loop variable semantics): <https://go.dev/doc/go1.22>
