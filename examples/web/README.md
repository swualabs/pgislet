# pgislet Web Playground

A persistent SQL playground built with Go, Gin, Bun, and two PostgreSQL databases. The browser interface retains the lightweight editor, schema sidebar, results grid, and sample queries of the original example.

## Architecture

| Component | Responsibility |
| --- | --- |
| Gin HTTP application | Authentication, request validation, workspace ownership, and HTTP responses |
| Bun application database | Accounts, password hashes, session digests, authentication rate limits, and account-to-Islet mappings |
| pgislet database | Islet Registry, Runtime Roles, isolated schemas, and user SQL execution |
| Browser | Login and registration forms, editor, schema navigation, results, recent queries, and account settings |

All implementation code stays under `examples/web`; the library remains under `internal`. The application never sends user SQL to the Bun database. Workspace IDs are loaded from the authenticated account, never accepted as ownership claims from the browser.

The two database connections must target different databases. Startup rejects identical connection targets and also compares the database name and server start time to detect common hostname-alias mistakes before applying migrations or running Bootstrap. Compose uses two separate PostgreSQL instances and persistent volumes.

## Go module

The CLI and web examples share the independent `examples/go.mod` module. Gin, Bun, and other example dependencies are declared there, while `replace github.com/swualabs/pgislet => ..` points to the library in the same repository checkout. No `go.work` file is required. Root-level `go test ./...` covers only the library module; run example tests explicitly with `go -C examples test ./...`.

Commands below use `go -C examples` from the repository root. If your shell is already in `examples`, omit `-C examples`. Relative asset paths are resolved from the process working directory, which is `examples` when using these commands.

## Quick start with disposable databases

From the repository root, with Go, a C compiler, and Docker available:

```sh
go -C examples run ./web -container
```

Open **http://localhost:8080**, create an account, and run SQL. This mode creates two PostgreSQL 18 containers and removes them on shutdown, including their account and workspace data. Use Compose or existing databases for persistence.

To use a different port:

```sh
APP_ADDR=127.0.0.1:18080 APP_ORIGIN=http://localhost:18080 go -C examples run ./web -container
```

## Persistent deployment with Compose

Create a local environment file with independent random passwords:

```sh
cd examples/web
umask 077
cat > .env <<EOF_ENV
APP_DB_ADMIN_PASSWORD=$(openssl rand -hex 24)
APP_DB_PASSWORD=$(openssl rand -hex 24)
PGISLET_DB_PASSWORD=$(openssl rand -hex 24)
APP_ORIGIN=http://localhost:8080
EOF_ENV
docker compose up --build -d
docker compose ps
```

The application listens on `127.0.0.1:8080` by default. Database ports are not published. The application container runs as a non-root user with a read-only root filesystem. Its readiness check verifies connectivity to both databases.

```sh
docker compose logs -f app
docker compose down
docker compose up -d
```

`down` preserves the database volumes. Adding `-v` deletes accounts, sessions, and workspace data. Password changes in `.env` do not rotate credentials inside already-initialized PostgreSQL volumes; rotate database roles and update connection settings together.

The application database initialization script creates a `playground_app` role with no superuser, database-creation, or role-creation privileges. It owns only its application database. The pgislet management account has the privileges required by the library and is used only on the dedicated workspace instance.

### HTTPS and reverse proxies

For public deployment, terminate HTTPS at a reverse proxy and set `APP_ORIGIN=https://playground.example.com`. Forward the original Host header. HTTPS origins enable Secure session cookies and HSTS; non-local HTTP origins are rejected.

Configure `APP_TRUSTED_PROXIES` with only the IP addresses or CIDRs of your actual proxies. With the default empty value, forwarded client IP headers are ignored. The IP used for authentication throttling must not be controlled by arbitrary clients. Do not expose the application container directly on a public interface if your deployment relies on a trusted proxy.

No CORS access is enabled. Mutating API requests require the `X-Pgislet-Request: playground` header and, when present, an Origin matching `APP_ORIGIN`. Session cookies are host-only, HttpOnly, and SameSite=Strict.

## Existing databases

```sh
export APP_DATABASE_URL='postgres://playground_app:password@localhost:5432/playground_app?sslmode=disable'
export PGISLET_DATABASE_URL='postgres://postgres:password@localhost:5433/playground_islets?sslmode=disable'
export APP_ORIGIN='http://localhost:8080'
go -C examples run ./web
```

Use TLS-validated PostgreSQL connections outside a trusted local network. pgislet Bootstrap changes database privileges: never point `PGISLET_DATABASE_URL` at your general application database.

Application migrations run at startup inside a transaction protected by a PostgreSQL advisory lock. Applied versions are recorded in `web_schema_versions`; unknown newer versions fail startup. A migration-only command is also available:

```sh
go -C examples run ./web -migrate
```

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `APP_DATABASE_URL` | Required | Bun application database connection |
| `PGISLET_DATABASE_URL` | Required | Dedicated pgislet management connection |
| `APP_ORIGIN` | `http://localhost:8080` | Exact browser origin, without a trailing slash |
| `APP_ADDR` | `127.0.0.1:8080` | HTTP listen address; container uses `0.0.0.0:8080` |
| `APP_ASSETS` | `web/static` | Static assets directory; container uses `/app/static` |
| `APP_SIGNUP` | `true` | Whether new registrations are accepted |
| `APP_MAX_ACCOUNTS` | `1000` | Registration capacity, checked under a database lock |
| `APP_MAX_CONCURRENT` | `8` | Concurrent workspace HTTP operations per application process |
| `APP_AUTH_RATE` | `20` | Authentication attempts per client IP per minute, shared through PostgreSQL |
| `APP_TRUSTED_PROXIES` | Empty | Comma-separated trusted proxy addresses or CIDRs |
| `APP_BIND_ADDRESS` | `127.0.0.1` | Compose host binding |
| `APP_PORT` | `8080` | Compose host port; update `APP_ORIGIN` when changing it |

Sessions expire after 24 hours, regardless of activity. Expired sessions and old rate-limit buckets are removed every minute. An account retains at most ten login sessions. Passwords require at least 12 Unicode characters and at most 72 UTF-8 bytes; bcrypt uses cost 12 and independently generated salts. Password hashing is limited to two concurrent authentication requests per process.

Requests have a 64 KiB JSON limit. SQL uses the library's 10-second statement timeout, 1,000-row result limit, and 4 MiB result budget. Per-process admission limits complement pgislet's per-Islet locks; they are not a cluster-wide connection quota or a per-user CPU/storage quota.

## API

Responses use JSON. Errors have an `error` object containing `kind` and `message`; SQL errors can additionally include SQLSTATE and query-specific details. Credentials and application database errors are not returned to clients.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/healthz` | Process liveness |
| GET | `/readyz` | Application and workspace database connectivity |
| GET | `/api/config` | Registration availability |
| POST | `/api/auth/register` | Register with `email`, `name`, and `password`; creates a session |
| POST | `/api/auth/login` | Sign in with `email` and `password`; issues a fresh session token |
| GET | `/api/auth/me` | Current account |
| POST | `/api/auth/logout` | Revoke the current session without deleting the workspace |
| POST | `/api/auth/password` | Change `currentPassword` to `newPassword`; revoke all sessions |
| POST | `/api/session` | Open the account's workspace, provisioning sample data on first use |
| POST | `/api/query` | Execute one statement from the `sql` field |
| GET | `/api/schema` | List the account's tables and columns |
| POST | `/api/reset` | Clear the account's workspace |
| POST | `/api/seed` | Replace the account's workspace contents with sample data |

Authentication tokens are 256-bit random values. Only SHA-256 token digests are stored in PostgreSQL. Password changes update the hash and revoke sessions atomically. Concurrent login checks the password hash again under a row lock before issuing a session.

## Persistence and failure behavior

Signing out, restarting the HTTP application, or expiring a session does not delete the user's Islet. Each operation obtains a fresh pgislet handle from the stored Islet ID. Recent query buttons are browser-memory history only; query text and results are not persisted in the application database.

Workspace provisioning locks the account row so concurrent first-use requests cannot create two mapped workspaces. A failed provisioning operation attempts compensation only after checking whether the mapping was committed. An uncertain application-database outcome does not blindly delete a possibly mapped Islet.

The two databases do not share a distributed transaction. A process crash between Islet creation and recording its ID can leave an orphan. Operators should compare `accounts.workspace_id` with the dedicated pgislet Registry during maintenance, with provisioning paused, and inspect unmatched Islets before deleting them through the library. Do not directly drop Registry rows or runtime roles. Logs include an Islet ID when compensation cannot finish. This example does not run automatic orphan deletion.

The HTTP server drains requests on shutdown and preserves accounts and Islets. Query failures retain the library's rollback and `ErrOutcomeUnknown` semantics. Do not automatically replay a query after a transport failure or an unknown commit outcome.

## Validation

From the repository root:

```sh
go -C examples test ./web/app
PGISLET_TEST_POSTGRES_MAJOR=17 go -C examples test -race ./web/tests -count=1
PGISLET_TEST_POSTGRES_MAJOR=18 go -C examples test -race ./web/tests -count=1
go -C examples vet ./web/...
```

Integration tests own two disposable PostgreSQL instances. They cover registration, duplicate accounts, login errors, persistent sessions and workspaces, password changes, expiry, logout token replay, request-origin checks, rate limits, registration capacity, SQL policy enforcement, cross-account isolation, concurrent workspace provisioning, and compensation after a mapping failure. The repository's 17/18 CI matrix includes these tests.

## Operational scope

This is a deployable service example, not a managed identity provider. Email addresses are account identifiers and are not verified; email verification, forgotten-password recovery, MFA, and account deletion/export workflows are not included. Decide on those identity and retention requirements before opening registration to the public. Registration can be closed with `APP_SIGNUP=false`.

Back up both databases and protect the workspace database backup: its internal Registry contains runtime credentials used by pgislet. Configure external monitoring, database connection/storage limits, TLS, backup recovery, and any shared admission control needed by multiple application instances. The application emits structured HTTP logs without SQL text, passwords, or session tokens.
