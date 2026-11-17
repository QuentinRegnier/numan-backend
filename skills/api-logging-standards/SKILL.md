---
name: api-logging-standards
description: "Enforce consistent structured logging, appropriate log levels, request correlation, observability, security, and diagnostic quality throughout the API."
---
# API LOGGING STANDARDS — STRICT ENGINEERING RULES

## 1. MISSION

You are responsible for maintaining a consistent, structured, secure, and operationally useful logging strategy across the API.

Logging must help engineers:

- Understand application behavior.
- Diagnose production incidents.
- Identify failures and their causes.
- Correlate events across requests and services.
- Distinguish expected business outcomes from operational failures.
- Investigate dependency failures and performance problems.
- Monitor system reliability without exposing sensitive information.

Logs must be intentional and useful.

Do not add arbitrary logging statements simply to increase log volume.

These requirements supplement all existing instructions and must never be used to justify removing existing functionality or weakening observability.

## 2. DISCOVER THE EXISTING LOGGING SYSTEM

Before changing logging, inspect the repository.

Identify:

- The logging library and its configured version.
- The centralized logger and its initialization.
- Existing logging interfaces and adapters.
- Structured logging conventions.
- Existing field names.
- Log-level policies.
- Request and correlation ID middleware.
- Trace context propagation.
- Existing HTTP middleware.
- Error-handling and logging integration.
- Redaction and sensitive-data handling.
- Logging conventions in handlers, use cases, repositories, and background workers.
- Existing logging tests.
- Deployment configuration controlling log levels and output.

Use the existing logging infrastructure.

Do not introduce another logging library, a parallel logger, a separate logging abstraction, or a competing field schema without a demonstrated requirement and explicit architectural justification.

Do not initialize a new global logger independently in every package.

Do not silently change the logger's initialization or output format.

## 3. STRUCTURED LOGGING

Use structured logging through the project's existing logging API.

Prefer stable fields over embedding all context into free-form strings.

Where appropriate and supported by the project, use fields representing:

- Timestamp.
- Severity.
- Event or operation.
- Component or service.
- Request ID.
- Correlation ID.
- Trace ID.
- Span ID.
- HTTP method.
- Route template.
- HTTP status code.
- Duration.
- Error classification.
- Dependency name.
- Operation name.
- Safe resource identifiers.

Follow the established field names and conventions.

Do not introduce synonymous field names for the same concept without a compatibility reason.

For example, do not inconsistently alternate between `request_id`, `requestId`, and `req_id` when the project already defines one standard.

Do not log the entire HTTP request or response merely to capture a few useful attributes.

Prefer bounded, predictable fields that can be searched and aggregated.

Avoid high-cardinality or unbounded fields when they are not necessary for diagnosis.

## 4. LOG LEVELS

Use log levels according to operational meaning, not personal preference.

Follow the project's existing level policy.

General guidance:

### DEBUG

Use for detailed diagnostic information that is normally disabled in production.

Examples include internal decision details that are useful during investigation and safe to record.

Do not use DEBUG to justify logging secrets or complete payloads.

### INFO

Use for meaningful operational events.

Examples may include service lifecycle events, important state transitions, or significant operations that the project's observability policy expects to record.

Do not log every ordinary function call at INFO.

### WARN

Use for unexpected or degraded situations that the application can handle or recover from.

Examples may include a recoverable dependency problem or a degraded optional feature, depending on the actual operational semantics.

Do not use WARN as a substitute for correctly classifying errors.

### ERROR

Use for failures requiring operational attention or representing an unsuccessful operation that should be recorded at the appropriate boundary.

Do not log every propagated error at ERROR in every architectural layer.

### FATAL OR PANIC-LEVEL LOGGING

Use only if the existing logger and application lifecycle explicitly require it.

Do not introduce process termination, fatal logging, or panic behavior for ordinary request failures.

A log level must reflect the event's meaning and recovery implications.

## 5. AVOID DUPLICATE LOGGING

Avoid logging the same error repeatedly as it propagates through the system.

For example, if a repository returns an error, a use case wraps it, and an HTTP handler converts it into a response, do not automatically log the same failure independently at all three layers.

Choose the appropriate logging boundary.

Lower layers may add context through error wrapping or return structured error information without logging it.

The boundary responsible for recording the failure should have enough context to make the log useful.

Additional logging is justified only when it records a distinct event or adds independently valuable operational information.

Do not suppress logs that are explicitly required by the project's observability or audit policy.

## 6. SECURITY AND PRIVACY

Never log:

- Passwords.
- Authentication tokens.
- API keys.
- Session secrets.
- Private keys.
- Database credentials.
- Connection strings containing credentials.
- Sensitive cookies.
- Authorization headers.
- Complete sensitive request bodies.
- Complete sensitive response bodies.
- Personal information that is not necessary and authorized for the diagnostic purpose.
- Raw secrets contained in dependency errors.

Treat incoming headers, query parameters, request bodies, and external error messages as potentially sensitive.

Apply the project's redaction and data-retention policies.

Use allowlists for logged fields when practical.

Do not assume that a field is safe simply because it is part of a structured log.

If an error message may contain credentials or sensitive payloads, sanitize it according to the established project policy before logging.

Never invent a redaction mechanism if a suitable centralized implementation already exists.

If the project lacks adequate protection, identify the gap and implement a consistent, testable solution.

## 7. REQUEST AND TRACE CORRELATION

Preserve existing request and trace context across architectural boundaries.

Where supported by the application, logs for the same request should be correlatable using the established request ID or trace context.

Use the project's existing middleware and context propagation mechanisms.

Do not generate a new unrelated request ID in every layer.

Do not overwrite a validated request identifier without following the established trust policy.

Do not trust arbitrary client-supplied correlation identifiers without appropriate validation and handling.

For asynchronous tasks, propagate the relevant context explicitly when supported and appropriate.

Do not retain request-scoped context indefinitely in long-lived workers.

Do not fabricate trace IDs or claim distributed tracing support exists when it does not.

## 8. HTTP AND API LOGGING

For HTTP requests, log meaningful operational information according to the project's existing policy.

Potential fields include:

- Request ID.
- HTTP method.
- Route template rather than an unnecessarily detailed raw URL.
- Status code.
- Request duration.
- Error classification.
- Relevant service or operation.

Avoid logging sensitive query parameters.

Avoid logging complete request or response payloads by default.

Distinguish between:

- Successful requests.
- Expected client errors.
- Unexpected application failures.
- Infrastructure failures.
- Timeouts and cancellations.

Do not automatically treat every 4xx response as an operational error.

Do not automatically treat every 5xx response as a distinct error event in multiple layers.

Ensure that health checks, readiness checks, and high-frequency endpoints follow the project's noise-control policy.

## 9. INFRASTRUCTURE AND BACKGROUND TASK LOGGING

Apply consistent logging to PostgreSQL, MongoDB, Redis, MinIO, external services, message brokers, and background jobs.

Where appropriate, record:

- The dependency or component.
- The operation being performed.
- The outcome.
- The duration.
- The error classification.
- Safe resource identifiers.
- Retry attempts and final retry outcomes.

Do not log entire SQL statements or payloads indiscriminately.

Do not record every cache miss as an ERROR.

Do not report an operation as successful before its result has been confirmed.

For retryable operations, distinguish between an intermediate retry and a final failure.

Avoid generating a separate error log for every retry if this creates excessive noise; follow the project's observability policy.

For asynchronous work, make failures observable even when no HTTP request is waiting for the result.

## 10. LOGGING AND ERROR HANDLING MUST AGREE

Logging must preserve the distinction between:

- Expected business outcomes.
- Client input errors.
- Authentication and authorization failures.
- Recoverable infrastructure issues.
- Unexpected application failures.
- Cancellation and timeout conditions.

Use the project's error classifications instead of guessing from error-message text.

Do not change an error's public meaning simply to simplify logging.

Do not expose internal diagnostics through API responses because they are already present in logs.

Do not silently discard an error after logging it if the caller still needs to handle it.

## 11. PERFORMANCE AND RELIABILITY

Logging must not unnecessarily degrade API performance.

Avoid:

- Logging excessively inside tight loops.
- Repeatedly serializing large payloads for diagnostic output.
- Building expensive log fields when the relevant level is disabled.
- Unbounded payload logging.
- Synchronous diagnostic operations that block critical paths unnecessarily.
- Excessive logging during retry loops.
- Unbounded metric-like values or identifiers in logs.

Follow the logger's existing lazy-evaluation and context mechanisms where available.

Do not swallow application errors merely because logging failed.

## 12. REQUIRED TESTING

Every meaningful change to logging behavior must be tested.

Use the project's existing logger test facilities, test writers, capture mechanisms, or equivalent established tools.

Tests should verify relevant behavior such as:

- Correct log level.
- Required structured fields.
- Correct error classification.
- Request ID or trace context propagation.
- Redaction of secrets.
- Absence of sensitive payloads.
- Avoidance of duplicate error logs.
- Logging of asynchronous failures where required.
- Behavior when optional context is absent.
- Compatibility with existing logging conventions.

Do not rely exclusively on visual inspection of console output.

Do not introduce a new logging abstraction solely to simplify a test if the existing logger can be tested adequately.

Do not make tests brittle by depending on irrelevant formatting details unless formatting is part of the explicit logging contract.

## 13. IMPLEMENTATION WORKFLOW

For every logging-related change:

1. Inspect the existing logger and conventions.
2. Identify the operational event being recorded.
3. Determine the appropriate layer and log level.
4. Identify the minimum useful structured context.
5. Check all fields for sensitive information.
6. Reuse the existing logging infrastructure.
7. Implement the smallest coherent change.
8. Add or update the corresponding tests.
9. Run the relevant tests.
10. Review neighboring code for duplicate logs or inconsistent field names.

Do not postpone logging tests until the end.

## 14. FINAL ACCEPTANCE CHECKLIST

- [ ] Existing logging infrastructure was reused.
- [ ] Log levels match the operational meaning.
- [ ] Structured fields follow established conventions.
- [ ] Request and trace context are preserved appropriately.
- [ ] Sensitive information is not exposed.
- [ ] Duplicate error logging was avoided.
- [ ] High-volume or performance-sensitive paths were considered.
- [ ] Relevant tests were created or updated.
- [ ] Tests were executed and results verified.
- [ ] No existing observability requirement was silently removed.

## 15. NON-NEGOTIABLE RULE

Logs must be structured, useful, consistent, secure, and actionable.

Never log everything indiscriminately. Never hide an operational failure. Never leak secrets. Never introduce a parallel logging system without justification.

Inspect before changing. Reuse before creating. Test observable behavior.
