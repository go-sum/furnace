---
title: Structured Logging
description: "slog setup, structured logging, log levels, logging middleware, child loggers, StatusWriter, request ID in logs, log best practices, notification policy, slog.Default, slog.With"
weight: 22
---

# Structured Logging

> Governing patterns for structured logging with Go's `slog` package.
> Complements [MIDDLEWARE_AND_CONTEXT.md](./MIDDLEWARE_AND_CONTEXT.md) §2b (request ID propagation),
> [ERROR_HANDLING.md](./ERROR_HANDLING.md) §5a (notification policy for error logging),
> and [ARCHITECTURE_GUIDE.md](./ARCHITECTURE_GUIDE.md) §3 (server struct dependencies).
>
> Read this together with [CLAUDE.md](../CLAUDE.md) for behavioral rules.

---

## 0. Quick Reference

- §1a Slog logger setup and global default configuration
- §1b Log level selection (Debug, Info, Warn, Error) rules
- §1c Logging middleware — request/response structured log entry
- §1d Level-based conditional logging pattern
- §1e Child loggers with WithGroup and With for scoped context
- §1f Structured logging best practices (no string interpolation, use attrs)
- §1g StatusWriter pattern for capturing response status codes
- §2 Logging self-review checklist

---

## 1. Structured Logging with slog

### 1a. Slog Logger Setup and Configuration

Use `log/slog` (Go 1.21+) with handler selection based on environment:

```go
func NewLogger(env string) *slog.Logger {
    var handler slog.Handler
    switch env {
    case "production":
        handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
            Level: slog.LevelInfo,
        })
    default:
        handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
            Level: slog.LevelDebug,
        })
    }
    return slog.New(handler)
}
```

JSON in production for machine parsing. Text in development for human
readability.

### 1b. Log Level Selection Rules

| Level | Value | Use |
|-------|-------|-----|
| Debug | -4 | Detailed diagnostic information; disabled in production |
| Info | 0 | Normal operations: startup, shutdown, successful requests |
| Warn | 4 | Client errors, safe degradations, unusual but handled conditions |
| Error | 8 | Server failures, dependency failures, recovered panics |

Do not use log level as a substitute for notification policy. See section 5a
below for notification policy.

### 1c. Request Logging Middleware Implementation

Wrap `http.ResponseWriter` to capture the status code, then log method, path,
status, duration, and request ID:

```go
type statusWriter struct {
    http.ResponseWriter
    status int
    written bool
}

func (w *statusWriter) WriteHeader(code int) {
    if !w.written {
        w.status = code
        w.written = true
    }
    w.ResponseWriter.WriteHeader(code)
}

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

            next.ServeHTTP(sw, r)

            duration := time.Since(start)
            attrs := []slog.Attr{
                slog.String("method", r.Method),
                slog.String("path", r.URL.Path),
                slog.Int("status", sw.status),
                slog.Duration("duration", duration),
                slog.String("request_id", RequestIDFrom(r.Context())),
            }

            level := slog.LevelInfo
            if sw.status >= 500 {
                level = slog.LevelError
            } else if sw.status >= 400 {
                level = slog.LevelWarn
            }

            logger.LogAttrs(r.Context(), level, "http.request", attrs...)
        })
    }
}
```

### 1d. Level-Based Conditional Logging Pattern

Map response status to log level:

- 5xx: `Error` -- server-side failure
- 4xx: `Warn` -- client-caused issue
- 1xx-3xx: `Info` -- normal operation

### 1e. Child Loggers with Scoped Attributes

Use `slog.With` to create request-scoped loggers that carry common attributes:

```go
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    logger := h.logger.With(
        slog.String("request_id", RequestIDFrom(r.Context())),
        slog.String("method", r.Method),
        slog.String("path", r.URL.Path),
    )
    // logger carries request context for all downstream log calls
}
```

### 1f. Structured Logging Best Practices

- Use consistent key names across the application (`request_id`, not sometimes
  `req_id` and sometimes `requestId`)
- Use `slog.Group` for namespacing related attributes
- Never log sensitive data: passwords, tokens, session IDs, PII
- Use the `"error"` key consistently for error values:
  `slog.String("error", err.Error())`
- Use `slog.ErrorContext(ctx, ...)` to associate log entries with the request
  context

### 1g. StatusWriter for Response Code Capture

If your application uses streaming, WebSocket upgrades, or server-sent events,
the status-capturing writer must also implement the interfaces the downstream
handler expects:

- `http.Flusher` for SSE and streaming responses
- `http.Hijacker` for WebSocket upgrades

Check interface satisfaction at compile time:

```go
var _ http.Flusher = (*statusWriter)(nil)
var _ http.Hijacker = (*statusWriter)(nil)
```

---

## 2. Logging Self-Review Checklist

Before merging application-layer code, confirm every applicable item:

- [ ] JSON handler in production, text handler in development
- [ ] Log level matches severity: 5xx=Error, 4xx=Warn, else=Info
- [ ] No sensitive data in log output (passwords, tokens, PII)
- [ ] Consistent key names across all log calls
- [ ] Request ID included in every request-scoped log entry
- [ ] No duplicate logging between middleware and error handlers
