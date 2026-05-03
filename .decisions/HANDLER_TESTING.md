---
title: Handler and Middleware Testing
description: "httptest, handler testing, table-driven tests, middleware testing, auth testing, authorization testing, integration tests, file upload testing, streaming response testing, golden files, test fixtures, interface-based fakes, fake repositories, TestMain"
weight: 25
---

# Handler and Middleware Testing

> Governing patterns for testing HTTP handlers, middleware, and integration scenarios.
> Complements [PRODUCTION_GO_RULES.md](./PRODUCTION_GO_RULES.md) §1d (testability rule),
> [MIDDLEWARE_AND_CONTEXT.md](./MIDDLEWARE_AND_CONTEXT.md) (middleware being tested),
> [ERROR_HANDLING.md](./ERROR_HANDLING.md) §1 (AppHandler pattern under test),
> and [INPUT_VALIDATION.md](./INPUT_VALIDATION.md) (validation under test).
>
> Read this together with [CLAUDE.md](../CLAUDE.md) for behavioral rules.

---

## 0. Quick Reference

- §1a httptest.NewRequest and httptest.NewRecorder fundamentals
- §1b Table-driven handler tests with subtests
- §1c Middleware testing — verifying next called, context values set
- §1d Auth and authorization test patterns
- §1e Integration test setup with TestMain
- §1f File upload multipart form testing
- §1g Streaming response and SSE testing
- §1h Golden file test fixtures for stable HTML output
- §1i Interface-based fake repository pattern
- §2 Testing self-review checklist
- §3 Testing anti-patterns

---

## 1. Handler and Middleware Testing Patterns

### 1a. httptest Request and Response Recorder Setup

Every handler test uses `httptest.NewRequest` and `httptest.NewRecorder`:

```go
func TestGetUser(t *testing.T) {
    svc := &fakeUserService{
        user: User{ID: testID, Name: "Alice"},
    }
    handler := NewUserHandler(svc, slog.Default())

    req := httptest.NewRequest(http.MethodGet, "/users/"+testID.String(), nil)
    rec := httptest.NewRecorder()

    handler.GetByID(rec, req)

    if rec.Code != http.StatusOK {
        t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
    }
}
```

### 1b. Table-Driven Handler Tests with Subtests

Cover both happy paths and error paths in a single test function:

```go
func TestCreateUser(t *testing.T) {
    tests := []struct {
        name       string
        body       string
        svcErr     error
        wantStatus int
        wantBody   string
    }{
        {
            name:       "success",
            body:       `{"name":"Alice","email":"alice@example.com"}`,
            wantStatus: http.StatusCreated,
        },
        {
            name:       "validation error missing name",
            body:       `{"email":"alice@example.com"}`,
            wantStatus: http.StatusUnprocessableEntity,
        },
        {
            name:       "duplicate email",
            body:       `{"name":"Alice","email":"taken@example.com"}`,
            svcErr:     ErrEmailTaken,
            wantStatus: http.StatusConflict,
        },
        {
            name:       "service failure",
            body:       `{"name":"Alice","email":"alice@example.com"}`,
            svcErr:     errors.New("db connection lost"),
            wantStatus: http.StatusInternalServerError,
        },
        {
            name:       "malformed JSON",
            body:       `{invalid`,
            wantStatus: http.StatusBadRequest,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            svc := &fakeUserService{err: tt.svcErr}
            handler := NewUserHandler(svc, slog.Default())

            req := httptest.NewRequest(http.MethodPost, "/users",
                strings.NewReader(tt.body))
            req.Header.Set("Content-Type", "application/json")
            rec := httptest.NewRecorder()

            handler.Create(rec, req)

            if rec.Code != tt.wantStatus {
                t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
            }
        })
    }
}
```

### 1c. Middleware Testing — Context Values and Next Chain

Test middleware in isolation by providing a controlled `next` handler:

```go
func TestRequestIDMiddleware(t *testing.T) {
    var capturedID string
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        capturedID = RequestIDFrom(r.Context())
        w.WriteHeader(http.StatusOK)
    })

    handler := RequestID(next)
    req := httptest.NewRequest(http.MethodGet, "/", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if capturedID == "" {
        t.Error("request ID not set in context")
    }
    if rec.Header().Get("X-Request-ID") == "" {
        t.Error("X-Request-ID header not set")
    }
}
```

Test the full middleware stack to verify ordering:

```go
func TestMiddlewareStack(t *testing.T) {
    var order []string
    makeMiddleware := func(name string) func(http.Handler) http.Handler {
        return func(next http.Handler) http.Handler {
            return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                order = append(order, name+":before")
                next.ServeHTTP(w, r)
                order = append(order, name+":after")
            })
        }
    }

    final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        order = append(order, "handler")
    })

    handler := Chain(final,
        makeMiddleware("first"),
        makeMiddleware("second"),
    )

    req := httptest.NewRequest(http.MethodGet, "/", nil)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    want := []string{"first:before", "second:before", "handler", "second:after", "first:after"}
    if !slices.Equal(order, want) {
        t.Errorf("order = %v, want %v", order, want)
    }
}
```

Test recovery middleware:

```go
func TestRecoveryMiddleware(t *testing.T) {
    panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        panic("unexpected failure")
    })

    handler := Recovery(slog.Default())(panicking)
    req := httptest.NewRequest(http.MethodGet, "/", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusInternalServerError {
        t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
    }
}
```

### 1d. Auth and Authorization Testing Patterns

Test the full range of authentication states:

```go
func TestAuthMiddleware(t *testing.T) {
    tests := []struct {
        name       string
        token      string
        wantStatus int
        wantCalled bool
    }{
        {
            name:       "valid token",
            token:      "valid-token",
            wantStatus: http.StatusOK,
            wantCalled: true,
        },
        {
            name:       "invalid token",
            token:      "invalid-token",
            wantStatus: http.StatusUnauthorized,
            wantCalled: false,
        },
        {
            name:       "missing token",
            token:      "",
            wantStatus: http.StatusUnauthorized,
            wantCalled: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            called := false
            next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                called = true
                w.WriteHeader(http.StatusOK)
            })

            verifier := &fakeTokenVerifier{valid: tt.token == "valid-token"}
            handler := Auth(verifier)(next)

            req := httptest.NewRequest(http.MethodGet, "/", nil)
            if tt.token != "" {
                req.Header.Set("Authorization", "Bearer "+tt.token)
            }
            rec := httptest.NewRecorder()

            handler.ServeHTTP(rec, req)

            if rec.Code != tt.wantStatus {
                t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
            }
            if called != tt.wantCalled {
                t.Errorf("next called = %v, want %v", called, tt.wantCalled)
            }
        })
    }
}
```

For role-based authorization, test that the correct role grants access and
incorrect roles are rejected:

```go
func TestRoleAuthorization(t *testing.T) {
    tests := []struct {
        name         string
        userRole     string
        requiredRole string
        wantStatus   int
    }{
        {"admin accessing admin route", "admin", "admin", http.StatusOK},
        {"user accessing admin route", "user", "admin", http.StatusForbidden},
        {"guest accessing user route", "guest", "user", http.StatusForbidden},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(http.StatusOK)
            })

            handler := RequireRole(tt.requiredRole)(next)

            ctx := context.WithValue(context.Background(), userKey, User{Role: tt.userRole})
            req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
            rec := httptest.NewRecorder()

            handler.ServeHTTP(rec, req)

            if rec.Code != tt.wantStatus {
                t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
            }
        })
    }
}
```

### 1e. Integration Test Setup with TestMain and Database

For tests that require a real database:

```go
func TestUserRepository_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }

    pool := setupTestDB(t)
    repo := NewUserRepository(pool)

    t.Run("create and retrieve", func(t *testing.T) {
        truncateUsers(t, pool)

        user, err := repo.Create(context.Background(), CreateUserInput{
            Name:  "Alice",
            Email: "alice@example.com",
        })
        if err != nil {
            t.Fatalf("create: %v", err)
        }

        got, err := repo.GetByID(context.Background(), user.ID)
        if err != nil {
            t.Fatalf("get: %v", err)
        }

        if got.Name != "Alice" {
            t.Errorf("name = %q, want %q", got.Name, "Alice")
        }
    })
}

func truncateUsers(t *testing.T, pool *pgxpool.Pool) {
    t.Helper()
    t.Cleanup(func() {
        _, _ = pool.Exec(context.Background(), "TRUNCATE users CASCADE")
    })
}
```

### 1f. Multipart File Upload Testing

Test multipart uploads with realistic payloads:

```go
func TestFileUpload(t *testing.T) {
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)

    part, err := writer.CreateFormFile("avatar", "photo.jpg")
    if err != nil {
        t.Fatal(err)
    }
    part.Write([]byte("fake image content"))
    writer.Close()

    req := httptest.NewRequest(http.MethodPost, "/upload", body)
    req.Header.Set("Content-Type", writer.FormDataContentType())
    rec := httptest.NewRecorder()

    handler.Upload(rec, req)

    if rec.Code != http.StatusOK {
        t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
    }
}
```

Test size limits:

```go
func TestFileUpload_TooLarge(t *testing.T) {
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)
    part, _ := writer.CreateFormFile("avatar", "huge.jpg")
    part.Write(make([]byte, 11<<20)) // 11 MB, exceeds 10 MB limit
    writer.Close()

    req := httptest.NewRequest(http.MethodPost, "/upload", body)
    req.Header.Set("Content-Type", writer.FormDataContentType())
    rec := httptest.NewRecorder()

    handler.Upload(rec, req)

    if rec.Code != http.StatusRequestEntityTooLarge {
        t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
    }
}
```

### 1g. Streaming Response and SSE Testing

For server-sent events or long-lived connections, use `httptest.Server`:

```go
func TestSSE(t *testing.T) {
    srv := httptest.NewServer(handler)
    defer srv.Close()

    resp, err := http.Get(srv.URL + "/events")
    if err != nil {
        t.Fatal(err)
    }
    defer resp.Body.Close()

    scanner := bufio.NewScanner(resp.Body)
    if !scanner.Scan() {
        t.Fatal("expected at least one event")
    }

    line := scanner.Text()
    if !strings.HasPrefix(line, "data:") {
        t.Errorf("line = %q, want data: prefix", line)
    }
}
```

### 1h. Golden File Test Fixtures for HTML Output

Place test fixtures in a `testdata/` directory adjacent to the test file. Go
tooling ignores `testdata/` directories during builds.

For golden file testing, accept an `-update` flag to regenerate expected output:

```go
var update = flag.Bool("update", false, "update golden files")

func TestRender(t *testing.T) {
    got := renderComponent(input)

    golden := filepath.Join("testdata", t.Name()+".golden")
    if *update {
        os.WriteFile(golden, []byte(got), 0644)
    }

    want, err := os.ReadFile(golden)
    if err != nil {
        t.Fatal(err)
    }

    if got != string(want) {
        t.Errorf("output mismatch:\ngot:  %s\nwant: %s", got, want)
    }
}
```

### 1i. Interface-Based Fake Repository Pattern

Define narrow interfaces at the consumer and implement fakes with function
fields for flexible test control:

```go
// Defined in the handler package -- narrow interface for what the handler needs
type UserService interface {
    GetByID(ctx context.Context, id uuid.UUID) (User, error)
    Create(ctx context.Context, input CreateUserInput) (User, error)
}

// Fake implementation for tests
type fakeUserService struct {
    user User
    err  error
}

func (f *fakeUserService) GetByID(_ context.Context, _ uuid.UUID) (User, error) {
    return f.user, f.err
}

func (f *fakeUserService) Create(_ context.Context, _ CreateUserInput) (User, error) {
    return f.user, f.err
}
```

Never use mock generation libraries. Hand-written fakes are simpler, more
readable, and do not couple tests to implementation details.

---

## 2. Testing Self-Review Checklist

Before merging application-layer code, confirm every applicable item:

- [ ] Every handler has a `_test.go` file
- [ ] Table-driven tests cover happy path, validation errors, not-found, and 500
- [ ] Tests use `httptest.NewRequest` and `httptest.NewRecorder`
- [ ] Fakes implement the same interface the handler depends on
- [ ] No shared mutable state between test cases
- [ ] Integration tests use `t.Cleanup` for database teardown
- [ ] Error responses are asserted on status code, not just "not 200"

---

## 3. Testing Anti-Patterns

These patterns cause bugs, test fragility, or security issues. Reject them in
code review.

- **Calling handler methods directly in tests.** Use `ServeHTTP` through the
  standard `httptest` flow so middleware, routing, and response writing behave
  realistically.
- **Shared mutable test state.** Each test case must construct its own fakes
  and request/response objects. Shared state causes ordering-dependent failures.
- **Not testing error responses.** If a test only asserts on the happy path, a
  regression in error handling goes undetected.
- **Asserting on exact JSON strings.** Parse the JSON into a struct or map and
  assert on fields. Exact string comparison breaks when field ordering, spacing,
  or escaping changes.

---

## 4. Sources

- [Go testing package](https://pkg.go.dev/testing)
- [net/http/httptest](https://pkg.go.dev/net/http/httptest)
- [testify/assert](https://github.com/stretchr/testify)
- [100 Go Mistakes — Testing](https://100go.co/)
