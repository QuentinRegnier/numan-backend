---
name: api-error-standards
description: "Enforce consistent, secure, traceable, and testable error handling across the API, including domain errors, application errors, infrastructure failures, HTTP responses, and error propagation."
---
# API ERROR STANDARDS — STRICT ENGINEERING RULES

## 1. MISSION

You are responsible for enforcing consistent error-handling standards across the entire API.

Every error-related implementation, modification, refactoring, and test must comply with this skill.

Your responsibilities include:

- Discovering and respecting the project's existing error-handling conventions.
- Maintaining consistent error semantics across architectural layers.
- Preserving error causes and contextual information.
- Preventing internal implementation details from leaking through API responses.
- Ensuring that errors are correctly classified and translated at architectural boundaries.
- Ensuring that error behavior is covered by automated tests.
- Preventing inconsistent, duplicated, or improvised error-handling implementations.

These requirements supplement all existing project instructions. They never authorize removing existing functionality, weakening validation, bypassing architectural boundaries, or reducing test coverage.

## 2. INSPECT THE EXISTING ERROR ARCHITECTURE FIRST

Before modifying error handling, inspect the repository.

Identify:

- Existing domain error types and error codes.
- Application and use-case error conventions.
- Infrastructure error handling.
- HTTP handlers, middleware, and centralized error translators.
- Existing response envelopes and JSON error schemas.
- Error wrapping and unwrapping conventions.
- Existing sentinel errors and typed errors.
- Validation error formats.
- Authentication and authorization error handling.
- Logging behavior associated with errors.
- Existing error-related tests.
- OpenAPI specifications or API contracts defining error responses.
- Existing conventions for request IDs, correlation IDs, and trace IDs.

Search the codebase for representative examples before introducing any new pattern.

Do not invent a new error response schema when the project already has one.

Do not introduce a second centralized error handler, another error registry, or a parallel set of error codes without a demonstrated architectural need.

If existing implementations disagree, identify the inconsistency, determine which behavior is authoritative, and assess compatibility before changing anything.

Never silently change the public API contract.

## 3. CLASSIFY ERRORS BY MEANING AND LAYER

Distinguish between:

1. Domain errors.
2. Application and use-case errors.
3. Validation errors.
4. Authentication errors.
5. Authorization errors.
6. Resource-not-found errors.
7. Conflict and concurrency errors.
8. Infrastructure and dependency failures.
9. Internal programming errors.
10. Cancellation, deadline, and timeout errors.

Use the project's established error types and conventions wherever possible.

An error must communicate a meaningful failure condition. Do not classify errors solely by matching their human-readable message.

Do not use arbitrary string comparisons when typed errors, sentinel errors, or established error codes are available.

Do not expose database-specific errors directly as public API semantics.

Translate infrastructure-specific failures into the appropriate application-level or API-level meaning at the correct architectural boundary.

Preserve the original cause when wrapping errors.

## 4. ERROR PROPAGATION AND WRAPPING

When an operation fails:

- Handle the error explicitly.
- Propagate it when the current layer cannot meaningfully resolve it.
- Wrap it with useful context when that context improves diagnosis.
- Preserve error identity and causes when the language and existing conventions support it.
- Avoid repeatedly wrapping the same error with redundant messages.
- Avoid silently ignoring errors.
- Avoid converting an error into a success result.
- Avoid returning nil or a zero-value result when doing so would conceal a failure.
- Avoid replacing a meaningful underlying error with a generic error too early.

In Go:

- Check every relevant returned error.
- Use `errors.Is` and `errors.As` where appropriate.
- Use `%w` when wrapping an error that must remain discoverable through error unwrapping.
- Follow the project's established conventions for sentinel errors and typed errors.
- Do not use `panic` for ordinary operational failures.
- Do not recover from panics indiscriminately or convert programming defects into misleading client errors.

Do not expose internal error details merely because they are available in the underlying error.

## 5. HTTP ERROR CONTRACT

Inspect the existing HTTP response contract before changing handlers or middleware.

Maintain consistency in:

- HTTP status codes.
- Error codes.
- Response structure.
- Field names and JSON serialization.
- Validation error representation.
- Request or correlation identifiers.
- Content types.
- Error message language and conventions.
- Backward compatibility.

Use the project's established error response format.

Do not create a new response envelope for an individual endpoint unless explicitly required by the API contract.

Use HTTP status codes according to the actual semantics of the failure and the project's established conventions.

For example, distinguish appropriately between malformed input, invalid field values, missing resources, conflicts, authentication failures, authorization failures, rate limiting, unavailable dependencies, and unexpected internal errors.

Do not blindly apply a status-code mapping without examining the actual API contract and error semantics.

If the correct mapping is unclear, inspect neighboring endpoints, shared middleware, OpenAPI documentation, and existing tests before deciding.

Any deliberate change to a public error response must be identified and its compatibility implications evaluated.

## 6. SECURITY AND INFORMATION DISCLOSURE

Never return the following to an untrusted API client:

- Stack traces.
- SQL statements containing sensitive values.
- Database connection strings.
- Credentials, passwords, API keys, or access tokens.
- Internal filesystem paths.
- Internal network topology.
- Raw dependency error details when they reveal implementation information.
- Sensitive request or response payloads.
- Internal debugging information.

Return only the public error information permitted by the existing API contract.

Detailed diagnostic information must remain in appropriately protected internal logs.

Do not confuse an internal diagnostic message with a client-facing message.

Do not include secrets in wrapped errors, error responses, or diagnostic context.

Ensure that authentication and authorization errors do not disclose whether sensitive resources or accounts exist when the security policy requires concealment.

## 7. VALIDATION ERRORS

Use a consistent validation strategy.

When input validation fails:

- Identify the appropriate validation boundary.
- Preserve the project's established error structure.
- Report actionable field-level information where supported.
- Avoid returning an unstructured collection of unrelated strings.
- Avoid exposing implementation-specific validation details.
- Do not duplicate validation rules unnecessarily across layers.
- Do not remove domain invariants simply because an HTTP handler validates the input.

Transport-level validation and domain invariants serve different purposes.

Tests must verify invalid input, missing fields, malformed values, boundary conditions, and relevant combinations of validation failures.

## 8. INFRASTRUCTURE ERRORS

Handle errors from PostgreSQL, MongoDB, Redis, MinIO, message brokers, caches, and external services according to their actual meaning.

Examples of conditions that may require explicit handling include:

- Record not found.
- Duplicate key or uniqueness violation.
- Transaction failure.
- Connection failure.
- Timeout or deadline exceeded.
- Context cancellation.
- Serialization or deserialization failure.
- Cache miss.
- Cache service unavailable.
- Object storage failure.
- Message publication failure.

A cache miss is not automatically an operational error.

A dependency failure must not automatically be reported as a client input error.

A database error must not automatically become a generic not-found error.

Preserve enough information for internal diagnosis while exposing only the appropriate public error.

Do not assume that all infrastructure failures have identical semantics.

## 9. ERROR LOGGING INTEGRATION

Integrate error handling with the project's existing logging standards.

Avoid logging the same failure repeatedly at every layer without adding meaningful information.

Choose the appropriate layer to record an error, based on the existing architecture and operational requirements.

When an error is logged, preserve useful context such as the operation, request ID, error classification, and relevant resource identifier, provided the information is safe to log.

Do not log secrets or sensitive payloads.

Do not replace structured error fields with arbitrary formatted strings when structured fields are available.

## 10. REQUIRED TESTING

Every new or modified error-handling behavior must have automated test coverage.

Tests must cover the relevant cases, including:

- Expected successful behavior.
- Expected domain failures.
- Validation failures.
- Missing resources.
- Conflicts.
- Infrastructure errors.
- Error wrapping and classification where relevant.
- HTTP status and response schema.
- Public message safety.
- Prevention of internal-detail disclosure.
- Context cancellation and timeout behavior when relevant.
- Regression cases for previously incorrect behavior.

For HTTP handlers, verify both the status code and the response body.

For error translation, verify that the correct internal error maps to the intended public representation.

For security-sensitive errors, verify that secrets and internal diagnostic details are absent from the response.

Reuse existing infrastructure fakes whenever they accurately exercise the relevant behavior.

Do not replace meaningful integration paths with simplistic mocks merely to make tests easier.

## 11. IMPLEMENTATION WORKFLOW

For every error-related modification:

1. Inspect the existing implementation and its callers.
2. Identify the intended error semantics.
3. Locate existing error types and translation mechanisms.
4. Determine the affected architectural boundaries.
5. Implement the smallest coherent change.
6. Add or update the corresponding tests in the same work cycle.
7. Run the relevant tests.
8. Check neighboring endpoints and callers for regressions.
9. Verify that public API behavior remains compatible unless a deliberate change is required.
10. Report the actual changes and test results.

Do not postpone all error tests until the end of the assignment.

Do not silently modify unrelated error behavior.

## 12. FINAL ACCEPTANCE CHECKLIST

Before considering the work complete, verify:

- [ ] Existing error conventions were inspected.
- [ ] Error semantics are consistent across layers.
- [ ] Error causes are preserved where appropriate.
- [ ] HTTP status codes and response schemas are correct.
- [ ] Internal details and secrets are not exposed.
- [ ] Errors are not silently ignored.
- [ ] Duplicate or contradictory error handlers were not introduced.
- [ ] Relevant tests were created or updated.
- [ ] Tests were executed and their results verified.
- [ ] No existing requirements or functionality were removed.
- [ ] Remaining limitations are documented honestly.

## 13. NON-NEGOTIABLE RULE

Consistency, correctness, security, traceability, and testability take precedence over improvised error handling.

Inspect before changing. Reuse before creating. Preserve error semantics. Test every behavioral change.

Never claim compliance or successful validation without evidence.
