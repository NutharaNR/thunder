# Thunder — Architecture & Auth Summary

## Overview

Thunder is a lightweight identity and access management (IAM) product written in Go, serving a REST API alongside two React single-page applications. It implements OAuth 2.0 and OIDC, a pluggable authentication executor system, and a graph-based flow engine that orchestrates every authentication and registration journey.

The core design philosophy is that authentication and registration are not hard-coded procedures — they are flexible, configurable graphs of nodes that can be arranged, extended, and swapped without changing server code. This means the same server can handle simple username/password login, multi-factor authentication, federated social login, or complex self-registration workflows, all driven by JSON flow definitions and registered executor implementations.

---

## Repository Layout

```text
backend/cmd/server/
  main.go                  # binary entry point
  servicemanager.go        # wires all packages in dependency order
  bootstrap/flows/         # seeded JSON flow definitions
  repository/conf/         # deployment.yaml config

backend/internal/
  oauth/                   # OAuth 2.0 + OIDC server
  flow/
    core/                  # graph/node/executor abstractions
    flowexec/              # runtime execution engine + service
    executor/              # one file per executor implementation
    mgt/                   # flow CRUD management API
  authn/                   # credential, OTP, passkey, social-login services
  user/ group/ role/ ou/   # identity management domains
  application/             # OAuth application configs
  idp/                     # identity provider configs
  consent/                 # OIDC consent handling
  system/                  # config, database, cache, JWT/JOSE, logging, i18n

frontend/apps/
  thunder-gate/            # login/registration SPA (app-native flow mode)
  thunder-console/         # admin SPA (redirect/standard OIDC mode)

frontend/packages/         # shared React contexts, design system, hooks, utils
samples/apps/              # sample apps demonstrating SDK integration
```

The two frontends serve different audiences and use different SDK modes. `thunder-gate` is the login UI rendered during an authentication flow — it directly calls the flow execution API and renders form prompts returned by the engine. `thunder-console` is the administrator UI for managing users, applications, flows, and identity providers — it authenticates itself through the standard redirect-based OIDC flow.

---

## Backend Package Pattern

Every domain package follows the same layered structure, ensuring concerns are cleanly separated and each layer is independently testable:

```
handler.go  →  service.go  →  store.go
```

- **handler.go** — Responsible only for the HTTP boundary: parsing the incoming request, calling the appropriate service method, and serialising the result into a JSON response. It never contains business logic.
- **service.go** — Owns all business logic. Depends on a store interface (not the concrete implementation), which makes it easy to test with mocked stores. This is where validation, orchestration, and decisions live.
- **store.go** — Owns all database interactions. Executes SQL via `DBClient`, maps rows to domain models, and returns them to the service. It has no knowledge of HTTP or business rules.
- **init.go** — The package's entry point called during startup. It instantiates the store, injects it into the service, injects the service into the handler, registers HTTP routes with the mux, and returns the service interface to the caller (so other packages can depend on it).

All domain service interfaces are exported (e.g. `UserServiceInterface`) but all implementations are unexported (e.g. `userService`). This keeps the public API surface minimal and enforces that external packages only interact through the declared interface. The `servicemanager.go` in `cmd/server/` orchestrates the initialization of every package in strict dependency order, passing the returned service interfaces as arguments where needed.

---

## Database Model

Thunder uses three separate databases, which can be SQLite files for development or PostgreSQL for production:

| Database | Purpose |
|---|---|
| `configdb.db` | Applications, IDPs, flows, roles, groups, OUs |
| `runtimedb.db` | Active flow executions, authorization codes, tokens |
| `userdb.db` | Users, credentials, passkeys |

The separation is intentional. Configuration data (flows, applications, IDPs) changes rarely and can be read-heavy. Runtime data (active sessions, authorization codes) is high-churn and time-sensitive. User data has different access patterns and stricter security requirements. Keeping them in separate databases allows each to be tuned, backed up, and secured independently.

All tables carry a `DEPLOYMENT_ID` column as part of the composite primary key. This means a single Thunder binary can serve multiple logical deployments from the same database without data leaking between them, making it suitable for multi-tenant hosting.

---

## Flow Engine

Authentication and registration are modelled as **directed node graphs** stored as JSON and seeded at startup. Rather than encoding authentication steps in imperative code, Thunder describes them declaratively: a flow is a set of nodes connected by edges (`onSuccess`, `onIncomplete`, `onFailure`), and the engine simply traverses the graph one node at a time.

This design makes it possible to change authentication behaviour — add MFA, insert a consent step, switch from passwords to passkeys — entirely through configuration, without touching Go code.

### Node Types

| Type | Role |
|---|---|
| `START` | Entry point of the graph; execution always begins here |
| `PROMPT` | Pauses execution and returns a description of needed inputs to the frontend; resumes when the user submits them |
| `TASK_EXECUTION` | Runs a named executor (e.g. `BasicAuthExecutor`) synchronously and routes to the next node based on the result |
| `END` | Terminal node; signals that the flow is complete and an assertion can be issued |

### Node Context

Every node receives a `NodeContext` that carries all state relevant to the current step. Rather than relying on global state, each node reads from and writes to this context object, which is persisted between requests:

- **UserInputs** — form data submitted by the user in the current round-trip (username, password, OTP, etc.)
- **RuntimeData** — a key-value map that accumulates state across all nodes (authenticated userID, granted scopes, required claims, attribute cache TTL, etc.). Nodes read what earlier nodes wrote here.
- **ForwardedData** — typed objects forwarded between nodes when structured data needs to pass through (e.g. a challenge object for passkey verification)
- **AuthenticatedUser** — populated by an authentication executor once the user's identity is established; subsequent nodes (like `AuthorizationExecutor`) use this to make decisions
- **ExecutionHistory** — an ordered log of every node that has executed, including the result of each. Useful for auditing and for conditional logic in later nodes.

### Engine Execution Loop (`flowexec/engine.go`)

The engine is a simple loop that drives the graph forward. Its job is to resolve which node runs next, execute it, and decide whether to continue or pause:

```
currentNode = START
loop:
  NodeContext = hydrate(currentNode, EngineContext)
  response    = currentNode.Execute(NodeContext)

  if response.Status == COMPLETE   → advance to next node (follow onSuccess edge)
  if response.Status == INCOMPLETE → persist state, return PROMPT to caller
  if terminal node reached         → return COMPLETE with assertion JWT
```

When a `PROMPT` node is reached, the engine does not block waiting for user input. Instead, it serialises the current `EngineContext` into `runtimedb`, returns the list of required inputs to the caller, and exits. When the user submits the form, the frontend sends a new request with the `flowID`, the engine reloads the persisted context, and resumes from where it left off. This stateless-but-resumable design means the server holds no in-memory session; everything needed to resume is in the database.

State is persisted to `runtimedb` between requests, allowing multi-step flows to survive across HTTP round-trips with a 30-minute TTL for authentication flows (60 minutes for registration, 24 hours for onboarding).

### Executor System

Each `TASK_EXECUTION` node names an executor by string (e.g. `"BasicAuthExecutor"`). The engine looks this up in a thread-safe registry at runtime and delegates execution to it. This is the extension point for adding new authentication mechanisms or integration steps without modifying the engine itself.

Executors implement `ExecutorInterface`:

```go
Execute(ctx *NodeContext) (*ExecutorResponse, error)
GetName() string
GetType() ExecutorType           // AUTHENTICATION, UTILITY, etc.
HasRequiredInputs(ctx, resp) bool
ValidatePrerequisites(ctx, resp) bool
GetRequiredInputs(ctx) []Input
```

`HasRequiredInputs` lets the executor inspect the context and declare whether the inputs it needs are already present. If they are not, the engine knows to return a `PROMPT` response asking for them before the executor runs. `ValidatePrerequisites` lets an executor verify that earlier nodes have set the state it depends on (e.g. that a user has been identified before the authorization executor runs).

All executors are registered in a thread-safe registry during startup (`executor/init.go`). The full set available out of the box:

| Executor | Purpose |
|---|---|
| `BasicAuthExecutor` | Validates a username/password pair against the credential store |
| `SMSOTPAuthExecutor` | Triggers SMS OTP delivery and verifies the submitted code |
| `PasskeyAuthExecutor` | Conducts the full WebAuthn/FIDO2 ceremony (challenge generation, assertion verification) |
| `OAuthExecutor` | Initiates and completes a federation handshake with any OAuth 2.0 provider |
| `OIDCAuthExecutor` | Initiates and completes a federation handshake with any OIDC provider |
| `GithubOAuthExecutor` | Pre-configured GitHub OAuth federation |
| `GoogleOIDCAuthExecutor` | Pre-configured Google OIDC federation |
| `IdentifyingExecutor` | Looks up a user by an identifier (email, username) without verifying credentials; used to resolve who the user is before selecting an auth method |
| `AuthAssertExecutor` | The final step in every authentication flow — generates a short-lived signed JWT assertion proving the user authenticated successfully. This assertion is what the OAuth layer receives to issue an authorization code. |
| `ProvisioningExecutor` | Creates a new user record, assigns them to groups and roles; used in registration flows after credential collection |
| `AttributeCollector` | Collects additional profile attributes (display name, phone number, etc.) from the user |
| `AuthorizationExecutor` | Evaluates whether the authenticated user has the permissions required for the requested scopes |
| `PermissionValidator` | Validates that the user holds a specific named permission |
| `ConsentExecutor` | Presents an OIDC consent screen and records the user's approval of requested scopes |
| `CredentialSetter` | Updates a user's credentials; used in password-reset or post-registration flows |
| `OUExecutor` | Creates an organisational unit node during complex provisioning flows |
| `OUResolverExecutor` | Resolves the organisational unit the current user belongs to, making it available to downstream nodes |
| `UserTypeResolver` | Determines which user types are available for registration in the current context |
| `AttributeUniquenessValidator` | Checks that a submitted attribute value (e.g. email, username) is not already in use before provisioning |
| `HTTPRequestExecutor` | Makes an HTTP call to an external system; enables webhook-style integration without writing a custom executor |
| `InviteExecutor` | Processes invite tokens during invite-based registration flows |
| `EmailExecutor` | Sends a transactional email using the configured email client (e.g. for verification or notification) |

Adding a new executor requires: implement `ExecutorInterface`, add the name to `executor/constants.go`, register the instance in `executor/init.go`. No changes to the engine or node model are needed.

### Seeded Flow Definitions

Bootstrap flows live in `backend/cmd/server/bootstrap/flows/`. They are automatically loaded and stored in `configdb` on first startup so the server is usable out of the box. Administrators can later modify or replace these through the flow management API.

Each JSON file defines one complete flow. The `PROMPT` node's `prompts` array carries UI metadata — field types, labels, validation hints — so the frontend can render the form dynamically without any hardcoded UI logic:

```json
{
  "name": "Basic Authentication",
  "handle": "default-basic-auth-flow",
  "flowType": "AUTHENTICATION",
  "nodes": [
    { "id": "start",       "type": "START",          "onSuccess": "prompt_creds" },
    { "id": "prompt_creds","type": "PROMPT",          "prompts": [...], "onSuccess": "task_auth" },
    { "id": "task_auth",   "type": "TASK_EXECUTION",  "executor": {"name": "BasicAuthExecutor"}, "onSuccess": "task_assert" },
    { "id": "task_assert", "type": "TASK_EXECUTION",  "executor": {"name": "AuthAssertExecutor"}, "onSuccess": "end" },
    { "id": "end",         "type": "END" }
  ]
}
```

Flows are selected by `flowType` (`AUTHENTICATION`, `REGISTRATION`, `USER_ONBOARDING`). The OAuth application's configuration determines which specific flow handle is used — different applications can have different login flows. For example, a consumer-facing app might use a social-login flow while an admin tool uses a stricter MFA flow.

---

## OAuth 2.0 / OIDC Server

The OAuth layer lives in `backend/internal/oauth/` and is a full standards-compliant OAuth 2.0 authorisation server with OIDC extensions. It is responsible for the protocol handshake — validating clients, initiating flows, issuing codes and tokens — but it delegates the actual credential verification entirely to the flow engine. This separation means the OAuth layer never needs to know whether the user authenticated with a password, a passkey, or a Google account.

### Exposed Endpoints

| Endpoint | Purpose |
|---|---|
| `GET /oauth2/authorize` | Starts the authorisation request; validates the client and redirects to the login UI |
| `POST /oauth2/auth/callback` | Internal endpoint called by the login SPA once the flow completes; receives the assertion JWT and issues an authorization code |
| `POST /oauth2/token` | Issues tokens; accepts all supported grant types |
| `GET /oauth2/userinfo` | Returns identity claims for the holder of a valid access token |
| `POST /oauth2/introspect` | Lets resource servers check whether a token is active and retrieve its metadata |
| `GET /oauth2/jwks` | Publishes the server's public keys so clients can verify JWT signatures independently |
| `POST /oauth2/dcr` | Dynamic Client Registration — allows applications to register themselves programmatically |
| `GET /.well-known/openid-configuration` | OIDC discovery document; clients use this to auto-configure themselves |

### Supported Grant Types

- `authorization_code` (with PKCE) — The standard interactive user login flow. PKCE (Proof Key for Code Exchange) protects public clients (SPAs, mobile apps) against authorization code interception.
- `refresh_token` — Allows clients to obtain new access tokens without user interaction, using the refresh token issued alongside the original access token.
- `client_credentials` — Service-to-service authentication where there is no user involved; the client authenticates with its own credentials.
- `urn:ietf:params:oauth:grant-type:token-exchange` (RFC 8693) — Allows one token to be exchanged for a different token, enabling delegation and impersonation scenarios.

---

## End-to-End Authentication Flow

### Mode 1 — Redirect (standard browser flow)

This is the classic OAuth 2.0 authorisation code flow. The user's browser is redirected to Thunder's login UI (`thunder-gate`), which handles the entire flow interaction. Once authentication completes, the browser is redirected back to the application with an authorization code.

```
1. Client  →  GET /oauth2/authorize?client_id=…&scope=openid email&response_type=code&code_challenge=…

   The client (browser application) initiates login by directing the user's browser to the authorize
   endpoint. It includes the scopes it needs, a PKCE code_challenge, and a state value for CSRF protection.

2. authz/service:
   - Validates client_id, redirect_uri, scopes
   - Loads OAuth application config
   - Calls flowexec/service::InitiateFlow(AUTHENTICATION, runtimeData)
     - runtimeData carries: clientID, requestedScopes, required claims, attribute TTL
     - Flow ID and 30-min expiry created; state stored in runtimedb
   - Stores authRequestContext keyed by authID
     (authRequestContext holds the full original OAuth request so it can be
      recovered later when the assertion arrives, without the client sending it again)
   - Returns 302 → /gate?authID=…&appID=…&flowID=…

   The server initiates a flow execution for the application's configured authentication flow,
   then redirects the browser to the thunder-gate SPA, passing the flowID and an authID that
   ties this session back to the original OAuth request.

3. Browser loads thunder-gate SPA
   - SPA calls GET /flow/<flowID> to fetch the flow graph
   - Renders the first PROMPT node's UI (e.g. username/password form)

   The SPA is entirely data-driven. It reads the flow graph's PROMPT node metadata to know
   what form fields to render, what labels to display, and what actions to offer. There is no
   hardcoded login UI — the server defines the shape of the interaction.

4. User submits form
   - SPA posts  POST /flow/execute?flowID=…  { username, password }
   - flowexec/engine steps through the graph:
       PROMPT (credentials collected; stored in UserInputs)
         ↓
       TASK_EXECUTION: BasicAuthExecutor
         - Checks UserInputs for username and password
         - Calls credentials service to hash and compare password
         - Sets AuthenticatedUser in NodeContext on success
         - On failure, follows the onFailure edge (e.g. back to PROMPT for retry)
         ↓
       TASK_EXECUTION: AuthAssertExecutor
         - Reads AuthenticatedUser from NodeContext
         - Generates a short-lived JWT assertion signed with the server's private key
         - The assertion encodes: sub (userID), ou, user attributes, expiry
         ↓
       END  →  FlowStep { type: COMPLETE, assertion: "eyJ…" }

   If the flow has additional steps (e.g. MFA), the engine returns INCOMPLETE with the next
   PROMPT's input requirements, the SPA renders the new form, and the cycle repeats with the
   same flowID. Each round-trip, the engine reloads state from runtimedb.

5. SPA receives COMPLETE + assertion
   - Posts  POST /oauth2/auth/callback { authID, assertion }

   The authID reconnects this callback to the original OAuth request that was stored in step 2.
   The assertion is proof that the flow engine successfully authenticated the user.

6. authz/service::HandleAuthorizationCallback:
   - Retrieves authRequestContext by authID (original OAuth request parameters)
   - Validates assertion JWT signature (using server's public key) and expiry
   - Extracts userID and attributes from assertion claims
   - Generates a cryptographically random authorization code
   - Stores code in runtimedb:
       { code, userID, scopes, codeChallenge, clientID, redirectURI, expiresAt, state: ACTIVE }
   - Returns 302 → redirect_uri?code=…&state=…

   The authorization code is deliberately short-lived (minutes) and single-use. It carries
   no sensitive claims itself — it is merely a handle that the application's backend can exchange
   for tokens in the next step.

7. App backend receives code; exchanges it:
   POST /oauth2/token
   grant_type=authorization_code&code=…&code_verifier=…&client_id=…&client_secret=…

   The application's backend (not the browser) makes this request, keeping the client_secret
   out of the browser. The code_verifier is the pre-image of the code_challenge sent in step 1,
   proving this is the same client that initiated the flow.

8. token/service → authorization_code grant handler:
   - Validates code existence and that it is still ACTIVE
   - Derives code_challenge from code_verifier and compares against stored challenge (PKCE)
   - Atomically marks code INACTIVE (prevents any replay of the same code)
   - Fetches user attributes needed to populate token claims
   - TokenBuilder creates:
       access_token JWT  { sub, scope, aud, exp: +1h }
         — used by the app to call protected resource APIs
       id_token JWT      { sub, email, name, nonce, aud, exp: +1h }
         — used by the app to establish the user's identity (OIDC)
       refresh_token     { exp: +30d }
         — used to silently renew the access token without re-login
   - All tokens are signed with the server's private key (RS256 or ES256)

9. App validates id_token signature using /oauth2/jwks
   App uses access_token as a Bearer token for protected API calls
```

### Mode 2 — App-native (direct flow API)

Used by `thunder-gate` itself and apps using `applicationId` (instead of `clientId`) in the Asgardeo SDK. In this mode the application owns the UI and drives the flow API directly, without any browser redirect to a separate login page. This is suitable for apps that want to render their own branded login experience while still using Thunder's flow engine for the backend logic.

```
Client → POST /flow/execute { applicationId }
         ← server initiates the flow and returns flowID + first PROMPT's required inputs

Client → POST /flow/execute { flowID, …inputs }
         ← server advances the flow; returns either next PROMPT inputs or COMPLETE + assertion
         ← repeat this step until status is COMPLETE

Client receives: { status: COMPLETE, assertion: JWT }
         — the assertion is the same JWT that Mode 1 produces at the end of step 4

Client → POST /oauth2/auth/callback { authID, assertion }
         — from here the flow is identical to Mode 1 steps 6–9
```

The key difference from Mode 1 is that there is no browser redirect. The application keeps the user on the same page, renders form fields based on the `inputs` the server returns, and calls the flow API in a loop until completion. This gives the app full control over the UI while the server handles all authentication logic.

---

## Authentication Mechanisms (authn package)

The `authn` package is the library of authentication capabilities that executors call into. Each mechanism is isolated in its own sub-package with its own service interface, making it easy to test and swap independently.

| Mechanism | Package | Notes |
|---|---|---|
| Username + password | `authn/credentials` | Validated against hashed credentials in `userdb`; hashing algorithm is configurable |
| SMS OTP | `authn/otp` | OTP generated server-side, delivered via the notification service, and verified against a session token so the code cannot be reused |
| WebAuthn / passkey | `authn/passkey` | Implements the full FIDO2 registration and authentication ceremony; credential public keys and counters are stored in `userdb`; counter validation prevents credential cloning |
| Generic OAuth | `authn/oauth` | Handles the redirect, callback, and token exchange for any OAuth 2.0 IdP configured in Thunder; user attributes are mapped from the IdP's token response |
| Generic OIDC | `authn/oidc` | Like the OAuth handler but also processes the ID token to extract OIDC standard claims |
| GitHub OAuth | `authn/github` | Pre-configured with GitHub's OAuth endpoints; maps GitHub profile fields to Thunder user attributes |
| Google OIDC | `authn/google` | Pre-configured with Google's OIDC endpoints; maps Google profile claims to Thunder user attributes |

Federation flows use the `OAuthExecutor` / `OIDCAuthExecutor` / `GithubOAuthExecutor` / `GoogleOIDCAuthExecutor` task nodes. When the engine reaches one of these nodes, the executor redirects the user to the external IdP, handles the callback (which arrives at a dedicated callback endpoint on Thunder), exchanges the code for tokens with the IdP, extracts the user identity, and populates `AuthenticatedUser` in the `NodeContext` — exactly as the `BasicAuthExecutor` would. Downstream nodes (like `AuthAssertExecutor`) do not need to know which mechanism was used.

---

## Security Architecture

Thunder's security model distinguishes between the unauthenticated perimeter (where the login flow lives) and the protected management surface (where administrative operations live).

- **Public paths** (no JWT required): `/auth/**`, `/flow/execute/**`, `/oauth2/**`, `/.well-known/**`, `/gate/**`, `/console/**`. These paths must be reachable without a token because they are part of the login journey itself or are public protocol metadata.
- **All management APIs** (user CRUD, application config, flow management, etc.) require a valid Bearer access token. The JWT middleware in `system/security` validates the token's signature against the JWKS, checks expiry, and extracts the subject before the request reaches any handler.
- **Error handling**: Errors propagate as `ServiceError` structs internally. At the HTTP boundary, `apierror.ErrorResponse` serialises them to JSON. For 5xx errors, the internal detail is never exposed — only a generic message is returned, while the full error is logged server-side.
- **PII protection**: All log statements must pass sensitive fields (email, username, phone) through `MaskString` from `internal/system/log` before logging. Debug-level constructions are guarded with `IsDebugEnabled` to avoid the cost of string formatting when debug logging is off.
- **Authorization codes are single-use**: When redeemed, the code is atomically transitioned from `ACTIVE` to `INACTIVE` in the same database operation. A second redemption attempt will find no active code and be rejected, preventing authorization code replay attacks.
- **PKCE (S256) is enforced for public clients**: The `code_challenge` is stored with the authorization code; the `code_verifier` submitted at the token endpoint is hashed and compared. This prevents an attacker who intercepts the authorization code from being able to exchange it.
- **Tokens are signed JWTs**: Clients verify token integrity and authenticity by fetching the public keys from `/oauth2/jwks` and checking the signature. The server never needs to be consulted to validate a token at every API call.

---

## Frontend SDKs

Thunder's frontend uses the Asgardeo JavaScript SDK (`@asgardeo/react`), which supports two distinct operational modes. The mode is determined entirely by which prop is passed to `AsgardeoProvider` — `clientId` for redirect mode, `applicationId` for app-native mode:

| Mode | SDK Config | Used In |
|---|---|---|
| Redirect (server-hosted login) | `clientId` + `baseUrl` + `platform="AsgardeoV2"` | `thunder-console`, `react-sdk-sample` |
| App-native (inline flow) | `applicationId` + `baseUrl` + `platform="AsgardeoV2"` | `thunder-gate`, `react-api-based-sample` |

In redirect mode the SDK behaves like a standard OIDC client — it redirects to the authorization endpoint and handles the callback. In app-native mode the SDK drives the flow API loop internally and renders prompts within the same application frame.

Common primitives across both modes: `useAsgardeo()` hook for auth state and actions, `<SignedIn>` / `<SignedOut>` conditional rendering, `<SignInButton>` / `<SignOutButton>`, and `<ProtectedRoute>` from `@asgardeo/react-router@2` for guarding routes.

---

## Service Startup Order (summarised)

Startup is orchestrated by `servicemanager.go`, which initialises every package in topological dependency order. Each package's `Initialize()` function returns its service interface, which is then passed as a constructor argument to packages that depend on it.

```
PKI → JWT → Observability → I18N → SystemAuthZ →
OU → UserSchema → User → Group → Resource → Role → AuthZ →
IDP → Notification/OTP → AuthnProvider → UserProvider → AuthnServices →
AttributeCache → FlowFactory + GraphCache →
EmailClient → TemplateService →
FlowMgt → ExecutorRegistry → FlowExecService →
OAuth(authz, token, jwks, dcr, userinfo, introspect, discovery) →
Application → HTTP route registration
```

**Why this order matters**: The flow execution service (`FlowExecService`) must come after the executor registry, because the engine looks up executors by name at runtime. The executor registry must come after all the services executors depend on (user service, credential service, authn services, etc.). The OAuth authorization service must come after the flow execution service because it calls `InitiateFlow` on every authorization request.

Circular dependencies (e.g. OU ↔ AuthZ, User ↔ AuthN) are broken by two-phase initialisation: the service is created first with a placeholder for the circular dependency, then a resolver function is injected after both sides are ready. This avoids import cycles while preserving the clean interface-based design.
