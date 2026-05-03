# CLAUDE.md — Architectural Constitution

> A Go cli and web application for vps app deployment and container orchestration.
>
> See `README.md` for project-specific architecture, module names, and current state.

---

## Behavioral Rules (always enforced)

- ONLY do what has been asked — recommend and get approval before any additions
- NEVER create documentation files (`*.md`) unless explicitly requested
- NEVER hardcode API keys, secrets, or credentials in source files
- NEVER commit secrets, credentials, or `.env` files
- ALWAYS validate user input at system boundaries; sanitize file paths (prevent `../` traversal)
- ALWAYS ensure implementations leverage the project's shared security module
- ALWAYS run tests after making code changes
- NEVER add dev-only shortcuts that bypass production behavior (disabled auth, mock services, skipped middleware) — dev mirrors production; see ARCHITECTURE_GUIDE.md §1e
- ALWAYS trace ALL callers when refactoring Go config structs or YAML mappings
- ALWAYS account for HTML-encoded entities in test assertions for HTML output
- ALWAYS enforce exact-match test assertions — never substring matching
- ALWAYS use MCP LSP tools for Go symbol navigation and MCP decisions tools for governance docs
- ALWAYS create config defaults and rules in owner packages apply default override values in config files
- ALL features that are not absolutely unique to a single application, should be placed to the relevant package
- Use native `rg` (ripgrep) for content search and `find` for file search
- NEVER use bash `grep`, `find`, or `Read` on `.decisions/` files when MCP is available
- NEVER load a full `.decisions/` file via `Read` — always use the section-aware tools below
- NEVER provide any deprecation, shims or backward-copatable patterns during this initial development phase; prior to v1.0.0 release

**Go symbol navigation — in order:**
1. `mcp__gomcp__lsp_workspace_symbols` — find any symbol by name (fast, indexed)
2. `mcp__gomcp__lsp_definition` — jump to the definition of a known symbol
3. `mcp__gomcp__lsp_find_references` — find all callers / implementors
4. `mcp__gomcp__lsp_file_symbols` — list all symbols in a specific file

**File and content search (native tools):**
- `rg` (ripgrep) for content search — smart-case by default, regex-capable
- `find` for file name lookup across the workspace

**Governance docs — in order:**
1. `mcp__gomcp__decisions_list` — discover docs and their section index
2. `mcp__gomcp__decisions_search` — locate relevant sections by keyword
3. `mcp__gomcp__decisions_read` — read a specific section: `section: "5a"` (not the full doc)

---

## Guide Index
> Before writing code, depending on the requirement consult:
- [`ARCHITECTURE_GUIDE.md`](.decisions/ARCHITECTURE_GUIDE.md): project structure, dependency injection, routing, server design, graceful shutdown
- [`CODE_REVIEW.md`](.decisions/CODE_REVIEW.md): review checklists, severity calibration, verification protocol, valid patterns
- [`DATA_STORAGE.md`](.decisions/DATA_STORAGE.md): driver selection, connection pooling, migrations, transactions, repository pattern
- [`ERROR_HANDLING.md`](.decisions/ERROR_HANDLING.md): AppHandler pattern, error taxonomy, panic policy, recovery middleware, retry and resilience
- [`HANDLER_TESTING.md`](.decisions/HANDLER_TESTING.md): httptest, table-driven tests, middleware testing, integration tests, golden files
- [`INPUT_VALIDATION.md`](.decisions/INPUT_VALIDATION.md): custom validators, cross-field validation, error formatting, body size limiting
- [`MIDDLEWARE_AND_CONTEXT.md`](.decisions/MIDDLEWARE_AND_CONTEXT.md): middleware chains, context propagation, request IDs, multi-tenant context
- [`PRODUCTION_GO_RULES.md`](.decisions/PRODUCTION_GO_RULES.md): five rules of production Go — zero globals, explicit errors, validation first, testability, documentation
- [`STRUCTURED_LOGGING.md`](.decisions/STRUCTURED_LOGGING.md): slog setup, log levels, logging middleware, child loggers
- [`UI_GUIDE.md`](.decisions/UI_GUIDE.md): visual design, component library, view composition
- [`WEB_DESIGN.md`](.decisions/WEB_DESIGN.md): concurrency, worker pools, rate limiting, race detection, runtime safety
- [`AGENT_GUIDE.md`](.decisions/AGENT_GUIDE.md): document structure rules for MCP efficiency

---

## MCP Server — gomcp

Registered in `.mcp.json`. Available in all agents. Source lives in `starter/docker/mcp/`. Start with `task mcp:up`.

| Tool | Use |
|------|-----|
| `mcp__gomcp__lsp_workspace_symbols` | Find types, functions, interfaces by name (indexed, fast) |
| `mcp__gomcp__lsp_find_references` | All callers / all implementors |
| `mcp__gomcp__lsp_definition` | Jump to any symbol definition |
| `mcp__gomcp__lsp_file_symbols` | List symbols in a Go file by path — preferred over `lsp_document_symbols` |
| `mcp__gomcp__lsp_document_symbols` | List symbols from arbitrary Go source content |
| `mcp__gomcp__decisions_list` | List governing docs with section index — start here |
| `mcp__gomcp__decisions_read` | Read a doc or a specific section (`section: "5a"`) |
| `mcp__gomcp__decisions_search` | Search all governing docs by keyword |

---

## Development Phase Guide

Invoke the right agent for each phase. Each agent reads its paired rules file first.

| Phase | Agent | Rules | When |
|-------|-------|-------|------|
| Analysis & Design | `cc-plan` | `.claude/rules/r-plan.md` | Before any code — layer assignment, architecture |
| Implementation | `cc-dev` | `.claude/rules/r-code.md` | After plan approved — write code in correct layers |
| Testing | `cc-test` | `.claude/rules/r-test.md` | After implementation — happy-path + failure tests |
| Architecture Review | `cc-plan` | `.claude/rules/r-plan.md` | After tests pass — refactor planning |

Agent flow: `cc-plan` → `cc-dev` → `cc-test` → (if issues) back to `cc-plan`

Agents and rules live in `.claude/agents/` and `.claude/rules/`.
