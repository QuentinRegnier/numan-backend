---
name: mandatory-testing-standards
description: "Enforce mandatory automated testing for every production code addition or modification, strict test-file organization, comprehensive behavioral coverage, reuse of centralized infrastructure fakes, regression prevention, and verified test execution."
---
# MANDATORY TESTING STANDARDS — NO UNTESTED CODE CHANGES

## 1. MISSION

You are responsible for ensuring that every production code change is accompanied by appropriate automated tests.

Testing is not a final phase that can be postponed until implementation is complete.

Testing must happen continuously, alongside development.

Every new behavior, modified behavior, bug fix, validation rule, error-handling path, logging behavior, infrastructure interaction, and security-sensitive change must receive appropriate test coverage.

These requirements supplement every existing project instruction.

They must never be used to justify removing existing functionality, weakening architectural rules, or reducing previously established test coverage.

## 2. FUNDAMENTAL RULE: EVERY CODE CHANGE REQUIRES TEST COVERAGE

### 2.1 When modifying an existing production code file

Whenever you modify an existing production code file:

1. Identify its existing test file or files.
2. Inspect the existing tests.
3. Identify the behavior affected by the modification.
4. Modify or extend the corresponding tests.
5. Add tests for any newly introduced behavior.
6. Add regression tests for defects that have been corrected.
7. Execute the relevant tests.
8. Verify the actual results.

Do not modify a production file and postpone the corresponding test changes until later.

Do not assume that existing tests are sufficient merely because they already pass.

Determine whether they actually exercise the modified behavior.

If a change affects multiple behaviors, update all relevant test cases.

If a shared function is used by several packages or components, inspect the affected callers and their tests.

### 2.2 When creating a new production code file

Whenever you create a new production code file, you MUST also create a corresponding test file.

For Go, the default rule is:

- `example.go` -> `example_test.go`

The new test file must contain meaningful automated tests for the behavior introduced by the production file.

Do not create an empty test file merely to satisfy the filename requirement.

Do not consider a test file complete because it contains only a placeholder, a trivial assertion, or a test that cannot fail when the implementation is incorrect.

The test file must exercise the actual behavior introduced by the new source file.

If the new source file contains several independently testable behaviors, cover them appropriately.

### 2.3 When creating multiple production files

If a task introduces multiple production code files, create a corresponding test file for each new production code file.

For example:

- `handler.go` -> `handler_test.go`
- `service.go` -> `service_test.go`
- `repository.go` -> `repository_test.go`

Each test file must have a clear purpose and cover the relevant behavior of its associated production file.

Avoid creating redundant tests that all exercise the same behavior without adding meaningful coverage.

If Go package organization, exported APIs, shared package-level behavior, or other technical constraints require a different arrangement, document the constraint and provide an explicit, technically justified alternative that still tests the new code.

Do not silently waive the rule.

### 2.4 Changes that do not contain executable production code

Pure documentation, generated artifacts, static configuration, and other non-executable files may require a different validation mechanism.

Do not create meaningless unit tests for files that cannot reasonably be unit-tested.

Instead, identify the appropriate validation method, such as:

- Configuration parsing tests.
- Schema validation.
- Contract validation.
- Build checks.
- Generated-file consistency checks.
- Documentation link checks.
- Static analysis.

Explain any exception explicitly.

Never use this exception to avoid testing executable production code.

## 3. DISCOVER THE EXISTING TESTING CONVENTIONS

Before writing tests, inspect the repository.

Determine:

- The test framework and libraries.
- The Go version.
- Existing test-file naming conventions.
- Package naming conventions.
- Whether tests use the same package or an external `_test` package.
- Existing test helpers.
- Existing assertion conventions.
- Table-driven testing patterns.
- Test setup and cleanup mechanisms.
- Existing fake infrastructure.
- Mocking conventions.
- Integration-test organization.
- Build tags and test environment requirements.
- Relevant CI commands.
- Existing coverage expectations.

Study neighboring test files and reuse their established patterns when appropriate.

Do not introduce a second assertion framework, test framework, or mocking library without a clear justification.

Do not invent conventions that conflict with the repository.

## 4. REQUIRED TEST-FILE ORGANIZATION

Follow the existing repository's organization wherever possible.

For Go projects, use the following default rules.

### 4.1 Naming

Production source:

`component.go`

Corresponding test:

`component_test.go`

Test files must end in `_test.go` so that Go recognizes them as test files.

Use meaningful test names that identify the behavior being verified.

For example:

- `TestCreateUser_Success`
- `TestCreateUser_InvalidInput`
- `TestCreateUser_DuplicateEmail`
- `TestGetUser_NotFound`
- `TestUpdateUser_DatabaseFailure`

These are naming examples, not mandatory names for every project.

Follow established repository naming conventions where they differ.

### 4.2 Test package selection

Choose the test package deliberately.

Use the same package when testing package-private implementation details is necessary and consistent with repository conventions.

Use an external test package when the behavior can be tested through the public API and doing so improves isolation.

Do not change package declarations merely to work around an inaccessible fake without investigating the architecture first.

Do not weaken encapsulation or expose production internals solely to make a test easier to write.

### 4.3 File responsibilities

A test file should have a clear scope.

Avoid:

- Giant test files containing unrelated subsystems.
- Duplicated setup logic in every test.
- Generic helper functions with unclear responsibilities.
- Helpers that conceal what a test actually verifies.
- Test files that only check implementation details while ignoring observable behavior.
- Helpers that reimplement existing centralized infrastructure fakes.

A shared test helper is appropriate when it genuinely reduces duplication and remains easy to understand.

Use a separate shared test helper only when multiple tests benefit from it and the repository's package organization supports it.

## 5. TEST BEHAVIOR, NOT JUST IMPLEMENTATION DETAILS

Tests should verify externally observable behavior and important architectural contracts.

Prefer tests that establish:

- Inputs.
- Expected outputs.
- Expected errors.
- State changes.
- Side effects.
- Persistence behavior.
- Interactions with relevant dependencies.
- Security properties.
- Failure recovery.
- Public API responses.

Do not write tests that merely verify that a function was called unless the interaction itself is an essential contract.

Do not rely exclusively on private implementation details when a stable behavioral assertion is possible.

Do not make tests pass by weakening assertions or removing failing cases.

A passing test is valuable only if it can detect meaningful regressions.

## 6. MINIMUM TEST COVERAGE CATEGORIES

Determine which categories apply to each modification.

### 6.1 Successful behavior

Verify the expected behavior for valid inputs.

Check meaningful outputs and side effects.

### 6.2 Invalid inputs

Test malformed values, missing required fields, invalid identifiers, out-of-range values, and relevant boundary conditions.

### 6.3 Error paths

Test expected errors from domain logic, application services, infrastructure dependencies, and transport boundaries.

Verify that errors are propagated, classified, and translated correctly.

### 6.4 Edge cases

Test relevant empty values, zero values, nil values, boundary conditions, duplicate operations, and unexpected state transitions.

Do not add arbitrary edge cases that have no meaningful relationship to the code.

### 6.5 Regression cases

Whenever a defect is corrected, add a test that would have detected the defect before the fix.

A regression test must fail against the defective behavior and pass against the corrected behavior whenever practical.

Do not rely solely on an informal statement that the bug is fixed.

### 6.6 Security cases

When relevant, test:

- Unauthorized access.
- Forbidden operations.
- Invalid authentication.
- Invalid signatures.
- Malformed external input.
- Injection attempts.
- Sensitive-data disclosure.
- Tenant isolation.
- Resource ownership.
- Invalid state transitions.

### 6.7 Concurrency and asynchronous behavior

When relevant, test concurrent access, asynchronous operations, cancellation, timeouts, race conditions, and eventual consistency.

Use deterministic synchronization whenever possible.

### 6.8 Error and logging contracts

When error handling or logging changes, test the observable contract.

Examples include:

- HTTP status codes.
- Error response schemas.
- Error classifications.
- Safe public messages.
- Required structured log fields.
- Log levels.
- Redaction of sensitive information.
- Request ID propagation.
- Avoidance of duplicate error logs where relevant.

Not every test needs to verify every category. Each change must cover the categories that apply to its behavior.

## 7. TABLE-DRIVEN TESTS

Use table-driven tests when several inputs exercise the same behavior under different conditions.

A useful table-driven test should make each case understandable.

Typical cases may include:

- Valid input.
- Invalid input.
- Missing fields.
- Boundary values.
- Not-found errors.
- Dependency failures.
- Conflicts.
- Cancellation or timeout.

Give each case a descriptive name.

Do not force unrelated scenarios into a single giant table.

Use separate tests when the setup, expected behavior, or assertions are materially different.

## 8. REUSE CENTRALIZED INFRASTRUCTURE FAKES

Before introducing a mock, fake server, or test helper, inspect the existing infrastructure testing package.

Relevant paths may include:

- `internal/infrastructure/test/postgres_fake.go`
- `internal/infrastructure/test/mongo_fake.go`
- `internal/infrastructure/test/redis_fake/`
- `internal/infrastructure/test/minio_fake.go`

Verify which files and APIs actually exist in the repository.

Reuse the existing fakes when they satisfy the test requirements.

Do not duplicate their implementation inside a feature package.

Do not create a local `FakeMongoServer`, PostgreSQL driver, Redis server, MinIO server, or equivalent infrastructure helper if an appropriate centralized implementation already exists.

### 8.1 PostgreSQL

Inspect and reuse the existing SQL driver fake and its supported helpers.

Where appropriate, test:

- Successful queries.
- Empty results.
- No-row conditions.
- Query failures.
- Execution failures.
- Scanning and type-conversion behavior.
- Nullable values.
- Array serialization.
- Transaction behavior when supported by the fake.
- Error propagation and classification.

Do not assume the fake supports a particular SQL operation without inspecting its implementation.

### 8.2 MongoDB

Inspect and reuse the existing MongoDB test utilities and their supported behavior.

Where appropriate, test:

- Query filters.
- Inserts and updates.
- Missing documents.
- Duplicate-key errors.
- Cursor decoding.
- BSON serialization and deserialization.
- Error handling.
- Relevant metadata and expiration behavior.

Do not claim to have tested real time-based expiration if the fake only represents expiration metadata.

### 8.3 Redis

Inspect and reuse the existing Redis fake.

Where appropriate, test:

- Cache hits and misses.
- Writes and reads.
- Expiration behavior supported by the fake.
- Sorted-set operations.
- Pub/Sub behavior.
- Error injection.
- Serialization.
- Cache invalidation.
- Relevant Lua script behavior.

Do not assume that every Redis command or production-server behavior is implemented by the fake.

### 8.4 MinIO

Inspect and reuse the existing MinIO fake or HTTP test server.

Where appropriate, test:

- Uploads.
- Downloads.
- Missing objects.
- Invalid operations.
- Storage failures.
- Object metadata.
- Error translation.

Verify what the fake actually simulates.

### 8.5 Other infrastructure

Reuse existing test infrastructure for other dependencies wherever possible.

Do not introduce Docker or Testcontainers into unit tests if the project requires in-memory or protocol-level fakes.

Do not replace a meaningful infrastructure test with a trivial repository-interface mock if doing so bypasses the serialization, query construction, or failure handling that must be tested.

## 9. TEST REAL LOGIC AT THE APPROPRIATE BOUNDARY

Use the correct testing level for the behavior.

### Unit tests

Use unit tests for isolated business rules, transformations, validation, and deterministic logic.

Keep them fast and independent.

### Infrastructure adapter tests

Exercise the actual adapter code with the appropriate fake.

Test serialization, query construction, result mapping, error translation, and other relevant behavior.

### Handler and API tests

Test handlers through their observable HTTP contract.

Verify status codes, response schemas, headers, validation, error mapping, and relevant middleware behavior.

### Integration tests

Use integration tests when correctness depends on the interaction of multiple real components or behavior that the available fakes cannot faithfully represent.

Follow the project's existing integration-test strategy.

Do not turn every unit test into an integration test.

Do not claim that an in-memory fake validates every behavioral detail of a real database or service.

## 10. TEST SETUP, CLEANUP, AND ISOLATION

Every test must have predictable setup and cleanup.

Use the repository's established helpers.

Where appropriate in Go:

- Use `t.Cleanup` for test-scoped cleanup.
- Use `t.Helper()` in helper functions.
- Use `t.Parallel()` only when the test is genuinely safe to run concurrently.
- Avoid shared mutable global state.
- Restore modified global configuration.
- Close servers, connections, and other resources.
- Reset shared fake state.
- Avoid depending on test execution order.

Be especially careful with shared global infrastructure variables, including database handles and shared fake instances.

Do not run tests in parallel if they mutate the same global database handle, logger, environment variables, fake server, or shared collection state without appropriate isolation.

Do not introduce test flakiness through incomplete cleanup.

## 11. ASYNCHRONOUS AND CONCURRENT TESTS

Do not use arbitrary sleeps as the default way to wait for asynchronous work.

Prefer:

- Channels.
- Wait groups.
- Explicit completion signals.
- Deterministic barriers.
- Context cancellation.
- Bounded polling when appropriate.
- Test-specific synchronization provided by the implementation.

Use timeouts to prevent tests from hanging indefinitely.

Ensure goroutines are terminated and resources are released.

For asynchronous cache updates or recovery mechanisms, verify the actual state transition rather than merely waiting an arbitrary amount of time.

If a timeout occurs, make the failure diagnostic enough to identify the blocked operation.

## 12. TEST ERRORS AND LOGGING

Error-handling and logging tests must follow the project's dedicated standards.

When changing errors, verify:

- Error identity or classification where relevant.
- Correct propagation.
- Correct HTTP mapping.
- Correct public response structure.
- No accidental disclosure of internal information.

When changing logging, verify:

- Expected severity.
- Required structured fields.
- Relevant request context.
- Secret redaction.
- Correct behavior on failure.
- No unnecessary duplicate logging.

Use the existing logger and error-testing facilities.

Do not create new test-only logging or error frameworks without a concrete reason.

## 13. TEST FILES MUST BE MAINTAINABLE

Tests are production-quality code and must follow the same maintainability principles.

Avoid:

- Duplicate test cases that add no new information.
- Assertions that are too weak to detect regressions.
- Excessive mocking.
- Brittle dependencies on irrelevant formatting.
- Hidden dependencies between tests.
- Arbitrary sleeps.
- Uncontrolled goroutines.
- Ignored errors in test setup.
- Unexplained magic values.
- Empty test files.
- Tests that always pass regardless of implementation correctness.

Use descriptive names and clear assertions.

Keep test fixtures understandable.

A helper must not obscure the behavior under test.

## 14. CONTINUOUS TESTING WORKFLOW

Testing must occur throughout implementation.

For every modification, follow this sequence:

1. Inspect the production code and existing tests.
2. Identify the behavior being added or changed.
3. Identify the required test file.
4. Define the relevant test cases.
5. Implement the smallest coherent code change.
6. Add or update the tests immediately.
7. Run the focused tests.
8. Fix any failures caused by the change.
9. Run adjacent package tests when appropriate.
10. Run broader validation when appropriate.
11. Review the diff and test coverage.
12. Record the actual results.

Do not wait until all production files have been modified before writing tests.

Do not accumulate dozens of untested changes.

Do not disable or skip a failing test merely to make the build pass.

If a test exposes a pre-existing defect, investigate it and determine whether it falls within the current task. Do not silently expand the scope into unrelated changes.

## 15. REQUIRED VALIDATION COMMANDS

Discover the project's actual build and test commands before choosing validation commands.

For a Go project, common checks may include:

- `gofmt` on modified Go files.
- `go test` for the affected package.
- `go test` for relevant neighboring packages.
- `go test ./...` for broader validation when feasible.
- `go vet` where appropriate.
- Race detection for relevant concurrency-sensitive code when feasible.
- Existing repository-specific linting and CI checks.

These commands are examples, not proof that the repository uses every check.

Inspect the project's module structure, build tags, dependencies, and CI configuration.

Do not claim a command passed unless it actually completed successfully.

If a command cannot run, report:

- The command attempted.
- The actual failure or blocker.
- Which validations remain outstanding.

Distinguish clearly between tests that passed, failed, were skipped, and were not run.

## 16. COVERAGE AND TEST QUALITY

Use coverage reports when available and useful.

Coverage percentages are diagnostic information, not a substitute for meaningful assertions.

Prioritize critical behavior, including:

- Business invariants.
- Security boundaries.
- Error paths.
- Data persistence.
- Serialization.
- Authorization.
- Concurrency.
- Failure recovery.

Do not inflate coverage with meaningless tests.

Do not remove assertions to improve execution speed without understanding the trade-off.

When a test cannot be written because a dependency or package boundary prevents it, investigate the architecture first and document the limitation.

## 17. SUBAGENT TESTING WORKFLOW

Use subagents to accelerate independent testing work when the environment supports it.

Suitable tasks include:

- Auditing existing test coverage.
- Identifying missing regression cases.
- Writing tests for an independently scoped component.
- Reviewing error and logging tests.
- Validating infrastructure fake behavior.
- Running independent code reviews.

For every delegated testing task:

1. Define the exact scope.
2. Identify the relevant files and behaviors.
3. Explain which existing fakes must be reused.
4. Prevent overlapping edits where possible.
5. Require the actual test commands and results.
6. Review the resulting tests before integration.

The main agent remains responsible for the final result.

Do not assume that tests are correct because a subagent reports completion.

Do not spawn additional subagents blindly when MCP calls or shared tools are stalled.

Preserve existing work and continue independent local validation where possible.

## 18. NO FALSE VALIDATION CLAIMS

Never claim that:

- A test passed when it was not executed.
- A test file covers behavior it does not exercise.
- A fake reproduces production behavior that it does not implement.
- The entire repository passes because one package passed.
- Race conditions were excluded without appropriate validation.
- Coverage is complete merely because a test file exists.

Report precisely what was executed and what the result demonstrated.

If validation is incomplete, identify the remaining gap.

## 19. FINAL ACCEPTANCE CHECKLIST

Before declaring a task complete, verify:

- [ ] Every modified production code file has had its relevant tests reviewed and updated.
- [ ] Every new production code file has a corresponding meaningful test file.
- [ ] New behaviors have automated coverage.
- [ ] Modified behaviors have regression-appropriate coverage.
- [ ] Existing tests were preserved unless a change was justified.
- [ ] Existing centralized infrastructure fakes were reused where appropriate.
- [ ] No unnecessary duplicate fake or test infrastructure was created.
- [ ] Tests cover relevant success and failure paths.
- [ ] Security-sensitive behavior has appropriate negative tests.
- [ ] Test setup and cleanup are reliable.
- [ ] Asynchronous tests use appropriate synchronization.
- [ ] Relevant focused tests were executed.
- [ ] Broader checks were executed when feasible.
- [ ] Actual results are reported accurately.
- [ ] Remaining gaps are explicitly documented.

## 20. NON-NEGOTIABLE RULE

NO UNTESTED PRODUCTION CODE CHANGES.

For every existing production code file you modify, update the relevant tests in the same work cycle.

For every new production code file you create, create its corresponding test file and test the behavior it introduces.

Reuse existing testing infrastructure. Preserve existing coverage. Test real behavior. Run tests continuously. Report results honestly.

Do not consider implementation complete until the required tests have been written and the applicable validation has been performed, or any genuine blocker has been explicitly documented.
