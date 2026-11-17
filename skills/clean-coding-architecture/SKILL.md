---
name: clean-coding-architecture
description: "Enforce high-quality Go coding, maintainable architecture, Domain-Driven Design, SOLID principles, safe refactoring, dependency management, reuse, and disciplined AI-assisted implementation."
---
# CLEAN CODING AND ARCHITECTURE — STRICT ENGINEERING STANDARDS

## 1. MISSION

Act as a senior software engineer and architecture guardian.

Your objective is to implement correct, secure, maintainable, testable, and production-quality code while preserving the existing behavior and architectural intent of the project.

You are not authorized to make arbitrary changes simply because an alternative design appears cleaner.

Every modification must have a clear purpose and must fit the actual codebase.

These rules supplement all existing instructions. They must never replace, weaken, or erase previous requirements.

## 2. REPOSITORY RECONNAISSANCE IS MANDATORY

Before implementing a feature or modifying existing code, inspect the relevant parts of the repository.

Determine:

- The language and version.
- The module structure.
- The architectural layers.
- The dependency direction.
- The package naming conventions.
- Existing interfaces and abstractions.
- Existing implementations of similar functionality.
- Error-handling conventions.
- Logging conventions.
- Testing conventions.
- Configuration and dependency-injection mechanisms.
- Existing infrastructure fakes.
- Relevant documentation and API contracts.
- The relevant build, lint, formatting, and test commands.

Read the actual code before making assumptions.

Inspect callers and consumers before changing a public function, type, interface, or response schema.

Use repository searches to find similar implementations.

Do not create a new abstraction before checking whether an existing one already solves the problem.

Do not modify unrelated code merely because you encounter an imperfection.

## 3. PRESERVE EXISTING FUNCTIONALITY

Treat existing behavior as intentional unless evidence demonstrates otherwise.

Before changing code:

1. Identify the behavior being changed.
2. Identify its callers and dependencies.
3. Determine the expected behavior.
4. Inspect relevant tests.
5. Identify compatibility constraints.
6. Plan the smallest coherent modification.

Preserve:

- Public API contracts.
- Existing business rules.
- Domain invariants.
- Authorization checks.
- Validation rules.
- Error semantics.
- Logging and observability requirements.
- Concurrency guarantees.
- Transaction boundaries.
- Data persistence semantics.
- Existing useful test coverage.

Do not silently remove functions, fields, validations, tests, logging, error handling, or configuration.

Do not weaken security checks to make an implementation easier.

If a breaking change is necessary, explain why, identify its impact, and follow the project's established migration process.

## 4. DOMAIN-DRIVEN DESIGN AND LAYER BOUNDARIES

Respect the project's actual architecture and DDD boundaries.

Where the repository follows a layered or hexagonal architecture, preserve the intended dependency direction.

Typical responsibilities include:

### Domain

The domain contains business concepts, domain rules, and invariants.

Do not introduce unnecessary dependencies on:

- HTTP frameworks.
- Database drivers.
- Redis clients.
- MongoDB drivers.
- MinIO clients.
- Transport-specific request or response types.
- Infrastructure-specific logging or configuration packages.

Keep business rules independent of transport and persistence implementation details.

### Application

Application services and use cases orchestrate business operations.

They should coordinate domain behavior and use the project's established ports or interfaces where appropriate.

Do not move domain rules into HTTP handlers or repositories merely to simplify implementation.

### Infrastructure

Infrastructure implements technical integrations and adapters.

Keep database queries, external-service interactions, and infrastructure-specific error translation in the appropriate layer.

Reuse existing infrastructure adapters and fakes.

### Transport and API

Handlers and transport adapters should translate incoming requests into application operations and application results into the established public API contract.

Avoid placing substantial business logic in HTTP handlers.

These are architectural guidelines, not permission to restructure the entire repository. Follow the project's actual architecture and established conventions.

## 5. SOLID, CLEAN CODE, AND SIMPLICITY

Apply SOLID principles where they improve the design.

Prefer:

- Clear responsibilities.
- Explicit dependencies.
- Small, cohesive functions.
- Meaningful names.
- Strongly typed values.
- Explicit error handling.
- Predictable control flow.
- Minimal side effects.
- Readable code.
- Testable behavior.
- Simple solutions that meet the actual requirements.

Avoid:

- God objects.
- Giant functions with unrelated responsibilities.
- Deeply nested conditionals.
- Unnecessary global state.
- Hidden dependencies.
- Duplicate business rules.
- Unnecessary interfaces.
- Premature abstraction.
- Overengineering.
- Excessive indirection.
- Dead code.
- Unused parameters and fields.
- Magic numbers and unexplained constants.
- Broad catch-all error handling.
- Silent error suppression.
- Arbitrary fallback behavior.

Do not create an interface for every concrete type automatically.

Introduce interfaces where they provide a meaningful architectural boundary, substitution requirement, or testing benefit.

Do not introduce an abstraction solely to satisfy a stylistic preference.

Choose the simplest design that preserves the project's architecture and supports its requirements.

## 6. REUSE BEFORE CREATING

Before creating any helper, service, adapter, abstraction, fake, or utility:

1. Search the repository for an equivalent.
2. Inspect its implementation and public API.
3. Determine whether it satisfies the requirement.
4. Reuse it if appropriate.
5. Extend it when a genuine reusable capability is missing.
6. Create a new implementation only when necessary.

Avoid duplicating:

- Error translators.
- Logging wrappers.
- Validation helpers.
- Serialization logic.
- Infrastructure clients.
- Database utilities.
- Test helpers.
- Fake servers.
- Cache implementations.
- Configuration logic.
- Authentication and authorization mechanisms.

A new abstraction must solve a real problem, not simply relocate existing code.

## 7. CENTRALIZED INFRASTRUCTURE TESTING

The project contains or may contain shared infrastructure fakes under:

- `internal/infrastructure/test/postgres_fake.go`
- `internal/infrastructure/test/mongo_fake.go`
- `internal/infrastructure/test/redis_fake/`
- `internal/infrastructure/test/minio_fake.go`

Verify the actual repository structure and available APIs before using these paths.

When an existing fake meets the requirement, reuse it.

Do not recreate a MongoDB fake, PostgreSQL mock driver, Redis server, MinIO server, or equivalent helper inside an individual feature package merely because the test resides elsewhere.

Do not copy existing fake implementations into new test files.

If a shared fake lacks a required capability, assess whether extending the centralized implementation is appropriate.

Preserve compatibility with existing tests.

If Go package visibility or internal-package rules prevent direct reuse, investigate a valid package-level solution before duplicating the implementation.

Document any unavoidable duplication.

Do not assume a fake supports a capability until its actual implementation has been inspected.

Do not introduce Docker or Testcontainers into unit tests if the project explicitly requires driver-level or in-memory fakes.

## 8. GO-SPECIFIC CODING RULES

When working in Go:

- Follow the repository's `go.mod` and supported Go version.
- Use idiomatic Go.
- Run `gofmt` on changed Go files.
- Handle returned errors explicitly.
- Respect `context.Context` propagation.
- Avoid storing request-scoped state in unsafe global variables.
- Avoid unnecessary pointer usage.
- Keep interfaces small and purposeful.
- Use typed errors or established error conventions where appropriate.
- Use `errors.Is` and `errors.As` where suitable.
- Use `defer` for cleanup when its lifecycle semantics are appropriate.
- Close resources correctly.
- Check resource-cleanup errors when relevant.
- Respect transaction and concurrency semantics.
- Avoid goroutine leaks.
- Protect shared mutable state appropriately.
- Avoid unnecessary allocations and reflection.
- Follow existing dependency-injection conventions.
- Do not introduce dependencies without evaluating their necessity and compatibility.

Do not replace a project-specific convention with a different Go idiom if doing so would break established contracts.

## 9. API CONTRACT AND COMPATIBILITY

Before changing API-facing code, inspect:

- Routes and handlers.
- Request and response types.
- JSON field names.
- Validation rules.
- HTTP status codes.
- Authentication and authorization.
- Error response formats.
- Pagination and filtering conventions.
- OpenAPI specifications.
- Existing client expectations.
- Relevant tests.

Do not silently rename fields, alter response envelopes, change status codes, or remove response properties.

If a change affects the public contract, identify the impact and update the appropriate documentation and tests.

Do not add endpoint-specific conventions that contradict shared middleware or neighboring endpoints.

## 10. SECURITY BY DEFAULT

Preserve the project's security architecture.

Pay particular attention to:

- Authentication.
- Authorization.
- Input validation.
- SQL and query injection.
- Path traversal.
- Unsafe deserialization.
- Secrets management.
- Sensitive logging.
- Resource ownership.
- Multi-tenant isolation.
- Request size limits.
- Timeouts and resource exhaustion.
- Concurrency and race conditions.
- External input validation.

Do not rely on frontend validation as the only validation layer.

Do not expose internal errors to API clients.

Do not hardcode credentials, tokens, or environment-specific secrets.

Do not disable security checks merely to make tests pass.

Security-sensitive changes require appropriate negative tests.

## 11. REFACTORING RULES

A refactoring must preserve externally observable behavior unless the task explicitly requires a behavior change.

For each refactoring:

1. Identify the duplication, complexity, or defect being addressed.
2. Identify all affected callers.
3. Preserve the existing contract.
4. Make the smallest coherent set of changes.
5. Update tests in the same work cycle.
6. Run relevant tests and checks.
7. Inspect the diff for accidental changes.

Do not combine unrelated refactoring with feature implementation unless necessary.

Do not rewrite entire files when a targeted change is sufficient.

Do not delete existing code merely because it appears unused without verifying references, reflection, generated registrations, dependency injection, and runtime usage.

Do not claim that a refactoring is behavior-preserving without appropriate evidence.

## 12. DEPENDENCIES AND CONFIGURATION

Before adding a dependency:

- Check whether the functionality already exists in the standard library or project dependencies.
- Evaluate maintenance, compatibility, security, and operational implications.
- Avoid introducing a large dependency for a trivial requirement.
- Follow the project's dependency-management process.
- Update module files only when necessary.
- Ensure the build remains reproducible.

Do not invent environment variables or configuration fields without integrating them consistently.

Do not silently change default configuration behavior.

Do not commit local environment assumptions into production code.

## 13. SUBAGENT ORCHESTRATION

Use subagents frequently when the environment supports them and when tasks can be meaningfully parallelized.

Suitable independent tasks include:

- Architecture reconnaissance.
- Error-handling audits.
- Logging audits.
- Test coverage audits.
- Focused implementation tasks.
- Independent code review.
- Regression analysis.

Before delegating:

1. Define the scope and expected output.
2. Identify files or components the subagent may modify.
3. Identify dependencies on other tasks.
4. Avoid assigning overlapping write operations without coordination.
5. Require evidence-based findings and actual validation results.

The main agent remains responsible for integration and correctness.

Review subagent changes before accepting them.

Do not treat a subagent's completion message as proof that its code is correct.

Do not spawn additional agents indiscriminately when MCP tools or shared resources are stalled.

When an MCP call is blocked, preserve existing work, investigate the bottleneck, and let independent local work continue where practical.

## 14. CHANGE MANAGEMENT

For every task:

1. Understand the requirements.
2. Inspect relevant code.
3. Identify the smallest coherent implementation.
4. Plan affected files and tests.
5. Implement the change.
6. Add or update tests immediately.
7. Run targeted tests.
8. Run broader validation when appropriate.
9. Review the diff.
10. Check for unintended changes.
11. Report actual results and remaining risks.

Do not defer all testing, logging, error handling, or cleanup until the end.

Do not mark a task complete merely because the code compiles.

## 15. FINAL ACCEPTANCE CHECKLIST

- [ ] The actual repository conventions were inspected.
- [ ] Existing functionality was preserved.
- [ ] Architectural boundaries were respected.
- [ ] Domain rules remain in the appropriate layer.
- [ ] Existing helpers and abstractions were reused where appropriate.
- [ ] No unnecessary duplicate infrastructure was introduced.
- [ ] Error handling follows the established standard.
- [ ] Logging follows the established standard.
- [ ] Security checks remain intact.
- [ ] Relevant tests were created or updated.
- [ ] Formatting, build, and relevant checks were executed.
- [ ] The diff contains no accidental or unrelated modifications.
- [ ] Remaining issues are documented honestly.

## 16. NON-NEGOTIABLE RULE

Do not merely make code work. Make it fit the project correctly.

Inspect before changing. Reuse before creating. Preserve behavior. Respect architecture. Test continuously. Validate honestly.
