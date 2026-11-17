---
name: make-perfect-route
description: Create a perfect implementation of HTTP route on my API
---
API Route Implementation — Mandatory Project Rules
When implementing a new API route, you MUST strictly respect the existing architecture of the project.
Never create a new abstraction simply because it seems more convenient. Before coding, first check whether an abstraction, function, repository, service, or model already exists and reuse it.
The goal is to keep the architecture clean, consistent, and compliant with the project's DDD design.

1. Mandatory General Structure
An API route consists of multiple layers:
```
routes.go
    ↓
handler
    ↓
business service
    ↓
cache service / repository
    ↓
Redis / MongoDB / PostgreSQL
```
And when the operation is asynchronous:
```
handler
    ↓
business service
    ↓
EnqueueDB / Queue
    ↓
worker
    ↓
repository
    ↓
MongoDB / PostgreSQL / Redis
```
Bypassing these layers is forbidden.
For example:
- handler → redis directly
- handler → postgres directly
- handler → mongo directly
- service → Redis client directly
- service → SQL query directly

These architectures are forbidden when an abstraction already exists in the project.

2. Before Coding: Inspect the Project
Before creating or modifying anything, you must inspect the existing codebase.
In particular, search for:
- similar handlers;
- similar services;
- similar models;
- existing Redis abstractions;
- existing cache services;
- existing MongoDB repositories;
- existing PostgreSQL repositories;
- existing events/queues;
- existing workers;
- naming conventions used by similar features.

Never assume a function does not exist before searching.
If a similar feature already exists, use it as a reference to follow project conventions.

3. Route Declaration
The route declaration MUST be placed in:
`routes.go`
Do not declare routes directly inside the handler file.
Principle example:
`router.POST("/conversations", handler.CreateConversationHandler)`
The `routes.go` file must remain responsible for route wiring, while endpoint logic stays in the handler/service.

4. Handler
Handlers must be placed in a dedicated folder for their domain.
For conversations:
`conversation_handlers/`
For example:
```
conversation_handlers/
└── create_conversation_handler.go
```
The handler must remain thin.
It must NOT contain complex business logic.
Its role is essentially to:
1. retrieve HTTP data;
2. retrieve required parameters;
3. check/retrieve user identity if needed;
4. parse received data;
5. build the expected input type for the use case/service;
6. call the business service;
7. handle the service result;
8. return the appropriate HTTP response.

The handler must NOT:
- perform Redis operations directly;
- perform MongoDB operations directly;
- perform PostgreSQL operations directly;
- contain SQL queries;
- contain business logic;
- manipulate DB clients directly when an abstraction exists.

5. Handler Godoc / Swagger Documentation
Every endpoint must be properly documented with the Swagger/Godoc annotations used in the project.
Documentation must be precise enough to describe:
- what the endpoint does;
- its authentication;
- its parameters;
- its request body;
- the different HTTP status codes;
- possible errors;
- response formats;
- the route path and HTTP method.

Use existing handlers as reference.
Expected documentation level example:
```go
// CreatePostHandler godoc
// @Summary Create a new post
// @Description Creates a post with text content, hashtags, user mentions, and between 1 and 4 images.
// @Description This route requires JWT authentication and a valid HMAC signature.
// @Tags posts
// @Accept multipart/form-data
// @Produce json
// @Param Authorization header string true "Bearer <your_jwt>"
// @Param X-Signature header string true "HMAC signature of the request"
// @Param X-Timestamp header string true "Unix timestamp of the request"
// @Success 201 {object} domain.CreatePostResponse
// @Failure 400 {object} domain.ErrorResponse
// @Failure 401 {object} domain.ErrorResponse
// @Failure 500 {object} domain.ErrorResponse
// @Router /posts [post]
```
HTTP status codes and response types must match the actual behavior of the endpoint.
Do not document a response that is not actually returned by the code.

6. Input/Output Types
Models used by handlers must be placed in:
`internal/domain/models/`
For conversations:
`internal/domain/models/conversation_models/`
Example:
```
conversation_models/
└── create_conversation_io_models.go
```
The naming convention is:
`<action>_<domain>_io_models.go`
or, if only an input is needed:
`<action>_<domain>_i_models.go`
or, if only an output is needed:
`<action>_<domain>_o_models.go`

Important:
Do not create an Out type merely out of habit.
If the endpoint simply returns:
`200 OK`
with no business payload, no output model is necessary.
However, if the endpoint must return useful data to the client, create an appropriate output model.
Example for creating a conversation:
```json
{
  "id": "..."
}
```
In this case, create an output type allowing the client to receive the created conversation ID.

7. Handler → Service Flow
The handler must transform HTTP data into a clean domain input.
Conceptual flow:
```
HTTP request
    ↓
parse
    ↓
CreateConversationInput
    ↓
CreateConversationService
```
The service must not receive unnecessary HTTP objects if the domain does not need them.
The service must receive only the business data required for its execution.

8. Business Service
Business services live in:
`internal/service/`
For conversations:
`internal/service/conversation_service/`
For example:
```
conversation_service/
└── create_conversation_service.go
```
The service contains the business logic of the use case.
Example:
```
CreateConversation
    ↓
business validation
    ↓
fetch required data
    ↓
modify/create data
    ↓
cache update
    ↓
publish event if needed
```
The service must NOT contain HTTP logic.
It must remain unaware of HTTP framework details.

9. ABSOLUTE RULE: No Direct Database Access from the Service
The following calls are forbidden:
- `redisClient.Get(...)`
- `redisClient.Set(...)`
- `mongoCollection.Find(...)`
- `db.Query(...)`
- `db.Exec(...)`
whenever a project abstraction exists.

The project's DDD architecture requires going through the designated layers:
For Redis:
```
business service
    ↓
cache service
    ↓
repository/redis
    ↓
Redis
```
For MongoDB:
```
business service
    ↓
repository/mongo
    ↓
MongoDB
```
For PostgreSQL:
```
business service
    ↓
repository/postgres
    ↓
PostgreSQL
```

10. Redis: Search Existing Collections First
When data must be fetched from Redis, you MUST first inspect:
`internal/repository/redis/manager.go`
Check whether the Redis collection/structure you need already exists.
If it exists: reuse it.
If it truly does not exist:
- first verify whether an equivalent abstraction exists elsewhere;
- check the corresponding cache services;
- only then, if necessary, create a new collection/structure.

Never create a new Redis collection simply because you did not immediately find the data.

11. Redis Cache Services
Cache services live under:
`internal/service/cache_service/`
A Redis collection is generally paired with a dedicated cache service.
When you need to:
- fetch data;
- create data;
- update data;
- delete data;
you must use the functions exposed by the corresponding cache service.
Conceptual flow:
```
conversation service
        ↓
conversation cache service
        ↓
redis repository
        ↓
Redis
```
Very important rule:
If you notice: "The function I need does not exist in the cache service", you MUST NOT conclude: "I will call Redis directly."
A missing function means the missing abstraction belongs in the proper layer.
The absence of a function is never an authorization to bypass DDD.

12. MongoDB
MongoDB follows the same abstraction principle.
Existing schemas are referenced in:
`internal/domain/schemas/`
Before creating a new schema, check whether an existing schema fits your needs.
Important rule:
Do not create a new MongoDB schema if an existing schema can be reused.

13. MongoDB Repository
MongoDB repository functions live in:
`internal/repository/mongo/`
Before writing a MongoDB query:
1. inspect `internal/repository/mongo/`;
2. verify whether a function already performs the operation;
3. reuse it if it exists.
If a new function is needed, add it to `internal/repository/mongo/`.
That function must stay in the repository.
Never leak raw MongoDB code up into the business service.

14. PostgreSQL
For PostgreSQL, start by inspecting:
`schema.sql`
to identify existing tables.
Then inspect available functions in:
`internal/repository/postgres/`
Reuse them whenever possible.

15. ABSOLUTE PostgreSQL RULE: No Hardcoded SQL Queries in Go
It is strictly forbidden to write directly inside Go code:
- `db.Query("SELECT ...")`
- `db.Exec("INSERT ...")`
or any other hardcoded business SQL query.
If a new query is needed:
1. add the SQL function to the project's SQL schema system;
2. expose that function via the PostgreSQL repository;
3. call that abstraction from the service.

Go code must call repository functions, not contain business SQL queries itself.

16. Redis / MongoDB / PostgreSQL Synchronization
The project uses a cross-tier synchronization architecture.
When data is:
- created;
- updated;
- deleted;
synchronization must follow the project's existing mechanism.
For writes/mutations:
```
Redis
+
PostgreSQL/MongoDB
    via EnqueueDB / queue
```
Do not implement your own synchronization mechanism.
Use the existing `EnqueueDB` mechanism in the project.

17. Reading Data and Rehydration
Reads work differently.
`EnqueueDB` must NOT be used for a simple read, because it is meant for mutations such as:
- `set`
- `update`
- `delete`

When data is missing from cache and must be retrieved from a persistent store:
MongoDB Lookup:
```
Service
   ↓
Mongo repository
   ↓
MongoDB
   ↓
Redis rehydration
```
Data fetched from MongoDB must rehydrate the Redis cache according to existing mechanisms.
PostgreSQL Lookup:
```
Service
   ↓
Postgres repository
   ↓
PostgreSQL
   ↓
EnqueueDB
   ↓
MongoDB
   ↓
Redis
```
In other words, when data is fetched from PostgreSQL, follow the existing propagation/rehydration mechanism to bring the upper storage tiers back in sync.
Do not reimplement this synchronization manually; use existing abstractions.

18. Workers / Events
Some operations must execute asynchronously.
In that case, you must also account for the worker system.
For a new feature (e.g., conversation), the worker must recognize and handle the corresponding event.
The initial implementation may therefore require:
1. adding the domain event;
2. registering it with the queue system;
3. routing it to the appropriate worker;
4. implementing its handler;
5. calling the required repositories;
6. handling errors and retries according to existing conventions.

Once this event infrastructure is properly registered, do not recreate it for every new route.
New routes must reuse the existing infrastructure.

19. What to Do When Something Is Missing
When you need a function that does not exist:
- **Wrong reaction:** "The function doesn't exist → I'll call Redis/Mongo/Postgres directly."
- **Right reaction:** "The function doesn't exist → I'll find where this abstraction belongs, and add it to the right layer."
Examples:
- Missing Redis function → `cache_service` → `repository/redis`
- Missing Mongo function → `repository/mongo`
- Missing PostgreSQL function → SQL function → `repository/postgres`

A missing abstraction means completing the architecture, not bypassing it.

20. Strictly Forbidden Practices
Never do the following without explicit architectural justification:
- ❌ Redis directly inside a handler.
- ❌ MongoDB directly inside a handler.
- ❌ PostgreSQL directly inside a handler.
- ❌ Redis directly inside a business service when a cache service exists.
- ❌ Hardcoded SQL query inside a Go file.
- ❌ Creating a new MongoDB schema when an existing schema fits.
- ❌ Creating a new Redis collection when an existing collection fits.
- ❌ Creating a new cache service when an existing service already covers the need.
- ❌ Duplicating an existing repository function.
- ❌ Complex business logic inside a handler.
- ❌ HTTP logic inside a business service.
- ❌ Creating a new queue system when `EnqueueDB`/worker already supports the event.
- ❌ Bypassing an abstraction just because its current API lacks the exact function needed.

21. Mandatory Pre-Completion Checklist
Before considering the route complete, systematically verify:
- **Route:**
  - Route is declared in `routes.go`.
  - Handler is in the right domain folder.
  - File naming follows existing conventions.
- **Handler:**
  - Handler properly extracts HTTP data.
  - Data is parsed into the right type.
  - Handler calls the business service.
  - No DB logic exists inside the handler.
  - Swagger documentation is complete.
  - HTTP status codes match actual behavior.
- **Domain:**
  - Models live in `internal/domain/models/`.
  - Naming follows existing conventions.
  - No unnecessary output models were created.
- **Service:**
  - Business logic lives in the service.
  - No direct Redis/Mongo/Postgres calls exist.
  - Existing abstractions are reused.
- **Redis:**
  - `internal/repository/redis/manager.go` was inspected.
  - Existing collections were searched.
  - Corresponding cache service was searched.
  - Existing functions are reused.
  - Any new needed function is placed in the right layer.
- **MongoDB:**
  - `internal/domain/schemas/` was inspected.
  - Existing schema was reused where possible.
  - `internal/repository/mongo/` was inspected.
  - Existing repository functions are reused.
- **PostgreSQL:**
  - `schema.sql` was inspected.
  - `internal/repository/postgres/` was inspected.
  - Existing functions are reused.
  - No business SQL query is hardcoded in Go.
  - Any new query goes through the SQL layer and repository.
- **Synchronization:**
  - Mutations use the intended `EnqueueDB` mechanism.
  - Reads do not go through `EnqueueDB`.
  - A MongoDB read properly rehydrates Redis.
  - A PostgreSQL read follows the existing MongoDB/Redis rehydration mechanism.
- **Worker:**
  - Required event exists.
  - Worker knows how to process it.
  - Processing follows existing conventions.
  - Infrastructure is not unnecessarily recreated.

22. Final Rule
The existing project codebase is the source of truth.
If these instructions appear to conflict with an existing project abstraction, inspect that abstraction first and understand how it works before writing code.
Do not look for the fastest code to write.
Look for the solution that respects:
`Existing architecture + DDD + Separation of concerns + Reuse of abstractions + Existing synchronization + Project conventions`.
Before creating any new abstraction, always ask yourself:
"Does this functionality already exist somewhere in the project under another name or in another layer?"
If yes: reuse it.
If no: add it to the proper architectural layer.
Never bypass the architecture just to move faster.

## 23. ABSOLUTE RULE: No URL Parameters — POST Only for Parameterized Requests

This is a STRICT and NON-NEGOTIABLE API design rule.

**API routes MUST NEVER use URL parameters to pass business data.**

This prohibition applies to BOTH path parameters and query parameters.

### 23.1. Forbidden: Path Parameters

The following route patterns are STRICTLY FORBIDDEN:

```go
router.GET("/users/:id", handler.GetUserHandler)
router.GET("/conversations/:conversation_id", handler.GetConversationHandler)
router.GET("/users/:id/posts", handler.GetUserPostsHandler)
router.GET("/conversations/:conversation_id/messages", handler.GetMessagesHandler)
```

Whether the framework uses `:id`, `{id}`, or another syntax does not matter.

Business parameters MUST NEVER be passed through the URL path.

### 23.2. Forbidden: Query Parameters

Query parameters are also STRICTLY FORBIDDEN for passing business data.

Examples of forbidden requests:

```http
GET /users?id=123
GET /posts?user_id=123
GET /conversations?limit=20&offset=0
GET /messages?conversation_id=123
GET /search?query=hello&category=posts
```

The following patterns are also forbidden:

```go
router.GET("/users", handler.GetUserHandler)
router.GET("/posts", handler.GetPostsHandler)
router.GET("/search", handler.SearchHandler)
```

If these handlers expect business parameters through query strings, their implementation violates this rule.

### 23.3. The Only Exception: Authenticated User ID from JWT

The ONLY identifier that can be obtained without being included in the request body is the authenticated user's ID.

This ID MUST be extracted directly from the JWT authentication context.

The JWT is provided through the HTTP `Authorization` header:

```http
Authorization: Bearer <JWT>
```

The handler MUST retrieve the authenticated user's identity using the project's existing authentication middleware/context mechanism.

For example:

```go
userID := c.GetString("user_id")
```

The exact implementation MUST follow the existing authentication architecture of the project.

NEVER require the client to explicitly provide its authenticated user ID when it can already be retrieved from the JWT.

The following is forbidden:

```http
GET /posts?user_id=123
```

The user ID must come from the JWT context, not from the URL or request body when it represents the authenticated user.

**Important:** This exception applies ONLY to the identity of the authenticated user. It does NOT apply to other user IDs, resource IDs, or business identifiers.

### 23.4. Mandatory: POST for Any Request Containing Business Parameters

**ABSOLUTE RULE: If an endpoint requires ANY client-provided business parameter, it MUST use HTTP POST.**

This applies regardless of whether the endpoint performs a read or a write.

For example:
- Retrieving a user by an ID other than the authenticated user's ID.
- Retrieving a conversation using its ID.
- Retrieving messages using a conversation ID.
- Searching with a search query.
- Filtering results.
- Pagination using limit, offset, cursor, or similar values.
- Sorting results.
- Retrieving data using multiple identifiers.
- Any other operation requiring client-provided business parameters.

All these operations MUST use POST, with the parameters passed through a JSON request body.

### 23.5. Correct Implementation Examples

**Example 1: Retrieve a conversation**

Forbidden:

```http
GET /conversations/123
GET /conversations?id=123
```

Correct:

```http
POST /conversations/get
Content-Type: application/json
Authorization: Bearer <JWT>
```

Request body:

```json
{
    "conversation_id": "123"
}
```

**Example 2: Retrieve messages with pagination**

Forbidden:

```http
GET /messages?conversation_id=123&limit=20&offset=0
```

Correct:

```http
POST /messages/list
Content-Type: application/json
Authorization: Bearer <JWT>
```

Request body:

```json
{
    "conversation_id": "123",
    "limit": 20,
    "offset": 0
}
```

**Example 3: Search posts**

Forbidden:

```http
GET /posts/search?query=hello&limit=10
```

Correct:

```http
POST /posts/search
Content-Type: application/json
Authorization: Bearer <JWT>
```

Request body:

```json
{
    "query": "hello",
    "limit": 10
}
```

**Example 4: Retrieve the authenticated user's own profile**

Correct:

```http
GET /users/me
Authorization: Bearer <JWT>
```

No request parameters are necessary because the user's identity is obtained directly from the JWT.

### 23.6. Request Body Models

When an endpoint requires client-provided business parameters, these parameters MUST be defined in an appropriate input model.

These models MUST follow the existing project conventions and be placed in:

```text
internal/domain/models/
```

For example:

```text
internal/domain/models/conversation_models/
└── get_conversation_i_models.go
```

The handler MUST parse the JSON request body into this model and pass the resulting domain input to the business service.

Do not pass HTTP request objects to business services.

### 23.7. Swagger Documentation Requirements

Swagger annotations MUST reflect the actual HTTP method and parameter locations.

For parameterized endpoints:

- Use `@Router /path [post]`.
- Use `@Accept json`.
- Document the JSON request body using `@Param body body ... true`.
- NEVER document business parameters using `@Param ... query`.
- NEVER document business parameters using `@Param ... path`.

The only permitted exception is authentication information supplied through HTTP headers.

### 23.8. Strictly Forbidden Practices

The following practices are STRICTLY FORBIDDEN:

- Using `GET` with business query parameters.
- Using `GET` with path parameters.
- Using `POST` with business parameters in the URL instead of the request body.
- Passing resource IDs through URL paths.
- Passing filters, pagination, sorting, or search criteria through query strings.
- Asking the client to provide its authenticated user ID when it is already available in the JWT.
- Creating exceptions to this rule for convenience or framework limitations.

### 23.9. Mandatory Pre-Completion Checklist

Before implementing or considering any API route complete, verify:

- No business parameter is passed through the URL path.
- No business parameter is passed through the query string.
- Any client-provided business parameter is included in the JSON request body.
- Any endpoint requiring client-provided business parameters uses POST.
- The authenticated user's ID is retrieved directly from the JWT context.
- GET is used only when no client-provided business parameters are required.
- Swagger annotations correctly document the HTTP method and request body.
- The handler parses the request body into the appropriate domain input model.

### 23.10. FINAL AND NON-NEGOTIABLE PRINCIPLE

**NO BUSINESS PARAMETERS IN URLS. EVER.**

**NO GET REQUESTS WHEN CLIENT-PROVIDED BUSINESS PARAMETERS ARE REQUIRED.**

The only identity that can be implicitly retrieved without being provided in the request body is the authenticated user's ID, extracted from the JWT.

If a request needs an ID, filter, search term, pagination value, or any other business parameter, use POST and put that information in the JSON request body.

There are NO exceptions to this architectural rule.
