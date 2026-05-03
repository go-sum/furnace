---
title: Input Validation
description: "input validation, custom validators, go-playground validator, JSON tag field errors, nested struct validation, slice and map validation, cross-field validation, struct-level validation, error formatting, body size limiting, validation anti-patterns"
weight: 24
---

# Input Validation

> Governing patterns for request body and input validation using `go-playground/validator`.
> Complements [PRODUCTION_GO_RULES.md](./PRODUCTION_GO_RULES.md) §1c (validation-first rule),
> [MIDDLEWARE_AND_CONTEXT.md](./MIDDLEWARE_AND_CONTEXT.md) (middleware chain where validation middleware sits),
> and [ERROR_HANDLING.md](./ERROR_HANDLING.md) §1 (AppHandler pattern for returning validation errors).
>
> Read this together with [CLAUDE.md](../CLAUDE.md) for behavioral rules.

---

## 0. Quick Reference

- §1a Custom validator registration and tag usage
- §1b JSON field name extraction for client-facing validation errors
- §1c Nested struct validation with `dive` tag
- §1d Slice and map element validation
- §1e Cross-field conditional validation
- §1f Struct-level validation for multi-field rules
- §1g Validation error response formatting
- §1h Request body size limiting middleware
- §2 Validation self-review checklist
- §3 Validation anti-patterns

---

## 1. Input Validation with go-playground/validator

Use `go-playground/validator` at the HTTP boundary. Validate once, trust downstream.

### 1a. Custom Validator Registration and Tag Usage

Register domain-specific validation rules:

```go
func registerCustomValidators(v *validator.Validate) {
    v.RegisterValidation("slug", func(fl validator.FieldLevel) bool {
        return regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`).MatchString(fl.Field().String())
    })

    // With parameters
    v.RegisterValidation("currency", func(fl validator.FieldLevel) bool {
        allowed := map[string]bool{"USD": true, "EUR": true, "GBP": true}
        return allowed[fl.Field().String()]
    })
}
```

### 1b. JSON Field Names in Validation Error Responses

Register a tag name function so validation errors report JSON field names
instead of Go struct field names:

```go
v.RegisterTagNameFunc(func(fld reflect.StructField) string {
    name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
    if name == "-" {
        return ""
    }
    return name
})
```

### 1c. Nested Struct Validation with dive

```go
type CreateOrderRequest struct {
    CustomerID string      `json:"customer_id" validate:"required,uuid"`
    Items      []OrderItem `json:"items"       validate:"required,min=1,dive"`
    Notes      *string     `json:"notes"       validate:"omitempty,max=500"`
    Address    Address     `json:"address"     validate:"required"`
}

type OrderItem struct {
    ProductID string `json:"product_id" validate:"required,uuid"`
    Quantity  int    `json:"quantity"   validate:"required,gt=0,lte=100"`
}
```

- `dive` validates each element inside a slice
- `required` on a nested struct validates the struct itself is present
- pointer fields with `omitempty` skip validation when nil

### 1d. Slice and Map Element Validation

```go
type Config struct {
    Tags     []string          `validate:"required,min=1,max=10,dive,required,min=1,max=50"`
    Settings map[string]string `validate:"required,dive,keys,required,min=1,endkeys,required"`
}
```

Use `dive` to enter the collection. For maps, `keys` and `endkeys` delimit key
validation from value validation.

### 1e. Cross-Field Conditional Validation

```go
type PasswordChange struct {
    NewPassword     string `json:"new_password"     validate:"required,min=8"`
    ConfirmPassword string `json:"confirm_password" validate:"required,eqfield=NewPassword"`
}

type DateRange struct {
    StartDate time.Time `json:"start_date" validate:"required"`
    EndDate   time.Time `json:"end_date"   validate:"required,gtfield=StartDate"`
}
```

Available cross-field tags: `eqfield`, `nefield`, `gtfield`, `gtefield`,
`ltfield`, `ltefield`.

### 1f. Struct-Level Multi-Field Validation

For complex multi-field rules that cannot be expressed with tags:

```go
v.RegisterStructValidation(func(sl validator.StructLevel) {
    order := sl.Current().Interface().(CreateOrderRequest)

    total := 0
    for _, item := range order.Items {
        total += item.Quantity
    }
    if total > 1000 {
        sl.ReportError(order.Items, "items", "Items", "max_total_quantity", "")
    }
}, CreateOrderRequest{})
```

### 1g. Validation Error Response Formatting

Format validation errors into structured, client-friendly responses:

```go
type ValidationError struct {
    Field   string `json:"field"`
    Message string `json:"message"`
}

func formatValidationErrors(err error) []ValidationError {
    var ve validator.ValidationErrors
    if !errors.As(err, &ve) {
        return nil
    }

    out := make([]ValidationError, len(ve))
    for i, fe := range ve {
        out[i] = ValidationError{
            Field:   fe.Field(),
            Message: msgForTag(fe),
        }
    }
    return out
}

func msgForTag(fe validator.FieldError) string {
    switch fe.Tag() {
    case "required":
        return "this field is required"
    case "email":
        return "must be a valid email address"
    case "min":
        return fmt.Sprintf("must be at least %s characters", fe.Param())
    case "max":
        return fmt.Sprintf("must be at most %s characters", fe.Param())
    case "uuid":
        return "must be a valid UUID"
    case "oneof":
        return fmt.Sprintf("must be one of: %s", fe.Param())
    default:
        return fmt.Sprintf("failed on '%s' validation", fe.Tag())
    }
}
```

### 1h. Request Body Size Limiting Middleware

Always limit request body size to prevent resource exhaustion:

```go
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) error {
    r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB

    var input CreateRequest
    if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
        return ErrBadRequest("invalid or oversized request body")
    }

    if err := h.validator.Struct(input); err != nil {
        return &ValidationResponse{Errors: formatValidationErrors(err)}
    }

    // proceed with validated input
}
```

---

## 2. Validation Self-Review Checklist

Before merging application-layer code, confirm every applicable item:

- [ ] Validation happens at the handler boundary only
- [ ] Validation errors produce structured field-level error responses
- [ ] Custom validators are registered at startup, not per-request
- [ ] `RegisterTagNameFunc` maps JSON tag names for client-friendly errors
- [ ] Nested structs use `dive` for slices and `required` for nested objects

---

## 3. Validation Anti-Patterns

These patterns cause bugs, test fragility, or security issues. Reject them in
code review.

- **Validating in the service layer.** Structural validation belongs at the
  handler boundary. Services enforce business rules (uniqueness, state
  transitions), not field-level constraints.
- **Not limiting request body size.** An attacker can send an arbitrarily large
  body. Always use `http.MaxBytesReader`.

---

## 4. Sources

- [go-playground/validator](https://github.com/go-playground/validator)
- [go-playground/validator documentation](https://pkg.go.dev/github.com/go-playground/validator/v10)
