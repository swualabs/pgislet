# pgislet

[日本語のREADME](./docs/README.ja.md)

pgislet is a Go library that provides each user with an isolated SQL workspace within a shared PostgreSQL database.

It is designed for applications where users write SQL directly, such as SQL learning platforms, query exercises, and browser-based playgrounds. Users can create tables, query and modify data, and drop objects within their assigned workspace. Applications manage workspace creation, initialization, recovery, and deletion through the library's Go API.

Each workspace is called an **Islet**. An Islet has a dedicated PostgreSQL schema and a login-capable **Runtime Role**. User SQL runs on a connection authenticated as that role.

> [!WARNING]
>
> pgislet supports PostgreSQL 17. It is not a general-purpose SQL proxy that permits arbitrary PostgreSQL administrative commands. It provides object isolation through roles and privileges, but does not allocate a separate database or dedicated CPU, memory, or disk resources to each user. It does not provide container- or VM-level isolation.

## Purpose and scope

In a typical web application, the server executes predefined SQL. A SQL learning platform, however, must allow users to create tables and insert data themselves. If every user connects with the same identity and privileges, they can modify other users' data or damage the shared environment.

pgislet provides independent SQL workspaces and manages the boundaries between them.

| pgislet responsibilities                                      | Application responsibilities                                        |
| ------------------------------------------------------------- | ------------------------------------------------------------------- |
| Create Islet schemas and Runtime Roles                        | Login, authentication, and application authorization                |
| Validate SQL policy and execute user SQL                      | Assign Islets to users                                              |
| Serialize operations per Islet and validate internal versions | Persist user-to-Islet ID mappings                                   |
| Enforce execution time and result limits                      | Rate-limit requests and control aggregate concurrency               |
| Reset, Reinitialize, Recover, and Delete                      | Define initialization SQL and sample data                           |
| Classify errors and preserve PostgreSQL errors                | Define HTTP responses, UI behavior, and retry policies              |
| Manage the internal Registry and Runtime Gateway              | Define expiration policies, usage tracking, backups, and monitoring |

## Core concepts and architecture

### Manager

A `Manager` holds the management connection pool, Runtime connection settings, and execution limits. Create it at server startup and reuse it across requests.

Management connections are used for Islet creation and deletion, Registry lookups, and internal object setup. User-provided SQL never runs on a management connection.

### Islet

An Islet consists of the following components.

| Component              | Purpose                                                                   |
| ---------------------- | ------------------------------------------------------------------------- |
| ID                     | Identifier that the application persists and uses to reopen the workspace |
| Dedicated schema       | Namespace for user tables, views, functions, and other local objects      |
| Dedicated Runtime Role | Login identity used to execute user SQL                                   |
| Registry row           | Records the schema, role, internal credentials, state, and version        |
| Internal generation    | Detects handles and operations that predate a reset or reinitialization   |
| Initialization token   | Identifies the initialization operation authorized to proceed             |

The management role owns the schema itself. The Runtime Role receives `USAGE` and `CREATE` privileges on that schema. Objects created by the Runtime Role can be managed under that role's privileges.

### Architecture diagram

![pgislet architecture](./docs/diagrams/diagram.png)

SQL Policy runs in the application's Go process; the Runtime Gateway runs inside PostgreSQL. The connection from the Runtime Gateway to an Islet represents SQL execution in the same Runtime transaction after validation. The Gateway does not execute user SQL on the caller's behalf. The Management Pool provides connections; the Manager's management code performs lifecycle operations. Roles are cluster-wide objects and schemas are database-local objects; the diagram groups them by their logical association with an Islet.

Multiple Managers can use the same PostgreSQL database. Per-Islet concurrency is coordinated through PostgreSQL row locks on the Registry, rather than process-local mutexes or routing requests to a particular server.

## Requirements and installation

| Component         | Requirement                                                 |
| ----------------- | ----------------------------------------------------------- |
| Go                | 1.26.1 or later, as specified in `go.mod`                   |
| PostgreSQL        | 17.x                                                        |
| SQL parser        | `pg_query_go/v6`; requires CGO and a C compiler             |
| PostgreSQL driver | `pgx/v5`                                                    |
| Docker            | Required for container-based examples and integration tests |

Add the library to an existing Go project:

```sh
go get github.com/swualabs/pgislet
```

Import the public package:

```go
import "github.com/swualabs/pgislet"
```

### Database setup

> [!WARNING]
> **Use a dedicated database for pgislet when deploying an application.**
>
> Bootstrap does more than create internal tables: it modifies `PUBLIC` privileges on the database and the `public` schema. Do not point pgislet at an existing application database that relies on those privileges.

The management account must be able to create and drop roles and schemas, manage internal functions and tables, and grant or revoke the required privileges. The examples use the `postgres` management account. If you use an account with restricted privileges, verify that it can perform these operations. Compatibility with every managed PostgreSQL service's privilege model is not guaranteed.

The repository includes a Compose configuration for starting the example PostgreSQL instance.

The default connection settings below are for local examples only. Do not use these credentials in production.

| Setting  | Default     |
| -------- | ----------- |
| Host     | `localhost` |
| Port     | `5432`      |
| Database | `appdb`     |
| User     | `postgres`  |
| Password | `password`  |

## Quick start

The following complete program creates a workspace, inserts initial data, executes a query, and cleans up the workspace.

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/swualabs/pgislet"
)

func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func run() error {
    ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
    defer cancel()

    manager, err := pgislet.New(ctx, pgislet.Config{
        DSN: os.Getenv("PGISLET_DSN"),
    })
    if err != nil {
        return err
    }

    defer manager.Close()

    islet, err := manager.Create(ctx)
    if err != nil {
        return err
    }

    defer func() {
        cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
        defer done()

        current, err := manager.Open(cleanup, islet.ID)
        if err != nil {
            fmt.Fprintln(os.Stderr, "cleanup open:", err)
            return
        }

        if err := manager.Delete(cleanup, current); err != nil {
            fmt.Fprintln(os.Stderr, "cleanup delete:", err)
        }
    }()

    _, err = manager.Batch(ctx, islet, []string{
        `CREATE TABLE learners (id integer PRIMARY KEY, name text NOT NULL)`,
        `INSERT INTO learners VALUES (1, 'Alice'), (2, 'Bob')`,
    })
    if err != nil {
        return err
    }

    result, err := manager.Execute(ctx, islet, `SELECT id, name FROM learners ORDER BY id`)
    if err != nil {
        return err
    }

    for _, row := range result.Rows {
        fmt.Println(row[0], row[1])
    }

    return nil
}
```

Save the program as `main.go` in a separate directory, initialize the module, install the dependency, and run it:

```sh
go mod init example.com/pgislet-demo
go get github.com/swualabs/pgislet
export PGISLET_DSN='postgres://postgres:password@localhost:5432/appdb?sslmode=disable'
go run .
```

Expected output:

```text
1 Alice
2 Bob
```

This example deletes the Islet on exit. For persistent user workspaces, store the Islet ID in your application instead of deleting the workspace after each request. Closing a Manager does not delete its Islets.

## Connection settings and the DSN helper

### Passing a DSN string

`Config.DSN` is the connection string for the management account.

```go
manager, err := pgislet.New(ctx, pgislet.Config{
    DSN: "postgres://postgres:password@localhost:5432/appdb?sslmode=disable",
})
```

The library parses it with `pgxpool.ParseConfig`. Runtime connections copy the management connection settings, replace the username and password with the Islet's Runtime Role credentials, and apply the required session settings.

A successful management connection does not guarantee that a Runtime Role can log in. PostgreSQL authentication rules must also permit connections authenticated as the generated Runtime Roles.

### Using ConnectionParams

`BuildDSN` constructs a connection string without requiring manual concatenation, including when credentials or database names contain special characters.

```go
dsn, err := pgislet.BuildDSN(pgislet.ConnectionParams{
    Host:     "localhost",
    Port:     5432,
    Database: "appdb",
    Username: "postgres",
    Password: os.Getenv("DB_PASSWORD"),
    SSLMode:  "disable",
})
if err != nil {
    return err
}

manager, err := pgislet.New(ctx, pgislet.Config{DSN: dsn})
if err != nil {
    return err
}

defer manager.Close()
```

| Field      | Meaning and default                                                                                 |
| ---------- | --------------------------------------------------------------------------------------------------- |
| `Host`     | Required. A hostname, an IPv6 address without brackets, or an absolute Unix socket directory path   |
| `Port`     | Defaults to `5432` when `0`; otherwise must be between 1 and 65535                                  |
| `Database` | Required. Database name                                                                             |
| `Username` | Required. Management account name                                                                   |
| `Password` | Password. The helper accepts an empty string; the server determines whether authentication succeeds |
| `SSLMode`  | Defaults to `verify-full` when empty                                                                |

Supported SSL modes are `disable`, `allow`, `prefer`, `require`, `verify-ca`, and `verify-full`. The local container examples explicitly use `disable` because they do not configure TLS. Environments using the default `verify-full` mode must support server certificate and hostname verification.

The helper URL-escapes usernames, passwords, and database names. It rejects NUL characters, invalid ports, and invalid SSL modes. For connection options not represented by this structure, such as certificate file paths, supply a complete DSN directly. The returned string contains the password and must not be logged verbatim.

`BuildDSN` only constructs a string. It does not connect to PostgreSQL or run Bootstrap.

### Adapting application configuration

The `ConnectionConfig` interface has one method:

```go
type ConnectionConfig interface {
    PostgreSQLParams() pgislet.ConnectionParams
}
```

Implement the method on an existing configuration type to pass that value directly to the helper:

```go
type DatabaseSettings struct {
    Address  string
    Name     string
    User     string
    Password string
}

func (s DatabaseSettings) PostgreSQLParams() pgislet.ConnectionParams {
    return pgislet.ConnectionParams{
        Host:     s.Address,
        Database: s.Name,
        Username: s.User,
        Password: s.Password,
        SSLMode:  "verify-full",
    }
}
```

Then call `pgislet.BuildDSN(settings)`. `ConnectionParams` also implements this interface.

## Manager configuration

A numeric or duration value of `0` selects the default. Negative values and values below the supported minimum are rejected. Setting a limit to `0` does not disable it.

| Setting                  | Default                    | Scope                                                                                           |
| ------------------------ | -------------------------- | ----------------------------------------------------------------------------------------------- |
| `DSN`                    | No library-defined default | Management connection settings                                                                  |
| `OperationTimeout`       | 30 seconds                 | Go context deadline for major operations, including Bootstrap, execution, creation, and cleanup |
| `StatementTimeout`       | 10 seconds                 | PostgreSQL statement execution timeout for Runtime connections                                  |
| `LockTimeout`            | 1 second                   | PostgreSQL lock wait timeout                                                                    |
| `IdleTransactionTimeout` | 15 seconds                 | Server timeout for connections idle within an open transaction                                  |
| `MaxSQLBytes`            | 1 MiB                      | Combined size of SQL strings in an execution or initialization batch                            |
| `MaxBatchStatements`     | 100                        | Number of SQL strings in a batch                                                                |
| `MaxRows`                | 1,000                      | Total rows returned across the batch                                                            |
| `MaxResultBytes`         | 4 MiB                      | Internally calculated result byte budget across the batch                                       |

Durations must be at least 1 ms; counts and sizes must be at least 1. An earlier deadline on the caller's context takes precedence. `Open` queries the Registry using the supplied context without adding an `OperationTimeout` deadline, so callers should provide an appropriate deadline.

`LockTimeout` is distinct from Islet serialization. Registry row locks for the same Islet use `NOWAIT`: if another operation holds the lock, the request receives `ErrBusy` instead of joining a queue.

```go
manager, err := pgislet.New(ctx, pgislet.Config{
    DSN:                    dsn,
    OperationTimeout:       20 * time.Second,
    StatementTimeout:       5 * time.Second,
    LockTimeout:            500 * time.Millisecond,
    IdleTransactionTimeout: 10 * time.Second,
    MaxSQLBytes:            64 << 10,
    MaxBatchStatements:     20,
    MaxRows:                500,
    MaxResultBytes:         2 << 20,
})
```

> `MaxRows` limits returned rows, not modified rows. For example, it does not limit the number of rows changed by an `UPDATE` without `RETURNING`. `MaxResultBytes` is not a database storage quota or a PostgreSQL process memory limit.

## Public API and Islet handles

### API reference

`New` is a package-level function. The remaining operations in this table are methods on `*pgislet.Manager`.

| API                                         | Returns             | Behavior                                                                 |
| ------------------------------------------- | ------------------- | ------------------------------------------------------------------------ |
| `New(ctx, Config)`                          | `(*Manager, error)` | Validate configuration, prepare the management pool, and run Bootstrap   |
| `Close()`                                   | Nothing             | Close the management pool without deleting Islets                        |
| `Create(ctx)`                               | `(Islet, error)`    | Create an empty Islet                                                    |
| `Open(ctx, id)`                             | `(Islet, error)`    | Read the current handle for a stored ID                                  |
| `Execute(ctx, islet, sql)`                  | `(Result, error)`   | Execute one SQL statement                                                |
| `Batch(ctx, islet, statements)`             | `([]Result, error)` | Execute statements sequentially in a single transaction                  |
| `Reset(ctx, islet)`                         | `(Islet, error)`    | Empty the workspace and return a new handle                              |
| `CreateWithInitialization(ctx, statements)` | `(Islet, error)`    | Create a workspace and initialize it with caller-provided SQL            |
| `Reinitialize(ctx, islet, statements)`      | `(Islet, error)`    | Empty the workspace and run new initialization SQL                       |
| `Recover(ctx, islet)`                       | `(Islet, error)`    | Recover to an empty workspace, including from interrupted initialization |
| `Delete(ctx, islet)`                        | `error`             | Remove the schema, Runtime Role, and Registry row                        |

`BuildDSN` is also a package-level function. `BuildDSN(config ConnectionConfig)` returns `(string, error)`.

### Public Islet fields

| Field       | Type        | Meaning                                          |
| ----------- | ----------- | ------------------------------------------------ |
| `ID`        | `string`    | Workspace identifier                             |
| `State`     | `string`    | State at the time the value was read or returned |
| `CreatedAt` | `time.Time` | Creation timestamp                               |
| `UpdatedAt` | `time.Time` | Registry lifecycle or state update timestamp     |

A **handle** is the `Islet` value returned by the library. It is a snapshot, not a database connection. Its fields do not update automatically when another request changes the workspace. `UpdatedAt` does not track SQL execution history and must not be used as a last-activity timestamp.

### Internal generation management

Generation is an unexported field that callers cannot read or set. There is no public API for incrementing it. Handles returned by creation and initialization operations contain the library's internal validation information.

Follow these rules:

1. Use handles returned by `Create` or `Open`.
2. After a successful `Reset`, `Recover`, or `Reinitialize`, replace the old handle with the returned value.
3. Persist the Islet ID for long-term storage.
4. Call `Open` to obtain a current handle when using that ID again.

Constructing `pgislet.Islet{ID: savedID}` or using a JSON-deserialized value as an execution handle is not supported. JSON does not preserve the internal generation.

```go
islet, err := manager.Open(ctx, savedID)
if err != nil {
    return err
}

result, err := manager.Execute(ctx, islet, `SELECT current_schema()`)
```

## SQL execution and results

### Execute

`Execute` accepts a SQL string containing exactly one top-level statement.

```go
result, err := manager.Execute(ctx, islet, `SELECT name FROM learners ORDER BY id`)
if err != nil {
    return err
}

for _, row := range result.Rows {
    fmt.Println(row[0])
}
```

Passing `SELECT 1; SELECT 2` as one string produces a policy error. Use `Batch` for multiple statements. SQL bodies in function definitions are parsed separately; validation does not simply split the input on semicolons.

The current API does not accept bind arguments. The form `Execute(ctx, islet, sql, args...)` is not supported. Executing user-authored SQL is different from constructing application SQL from user-supplied values. Applications must avoid unsafe string concatenation in the latter case.

### Batch

```go
results, err := manager.Batch(ctx, islet, []string{
    `CREATE TABLE notes (id integer PRIMARY KEY, body text)`,
    `INSERT INTO notes VALUES (1, 'First note')`,
    `SELECT id, body FROM notes`,
})
if err != nil {
    return err
}

fmt.Println(results[2].Rows)
```

Each slice element contains one statement. The entire batch runs on one Runtime connection in one transaction, so later statements can use tables or functions created by earlier statements.

The operation succeeds only when every statement and COMMIT succeed. A statement error or policy violation rolls back the transaction. pgislet does not reverse effects that PostgreSQL itself does not roll back, such as sequence value advancement.

An error may be returned alongside partial `Result` values collected during execution. Those results do not mean that changes from earlier statements were committed. Always check `err` before treating an operation as successful.

### Result representation

| Field          | Meaning                                                    |
| -------------- | ---------------------------------------------------------- |
| `Columns`      | Column names and PostgreSQL type OIDs                      |
| `Rows`         | Result rows represented as `[][]any`                       |
| `CommandTag`   | PostgreSQL command tag, such as `SELECT 2` or `INSERT 0 1` |
| `RowsAffected` | Row count reported by the PostgreSQL command tag           |
| `Duration`     | Time spent issuing the statement and reading its results   |
| `Truncated`    | Indicates that a result budget was exceeded                |

Each `Column` contains `Name string` and `DataTypeOID uint32`. Runtime results are read in PostgreSQL text format.

- SQL `NULL` becomes Go `nil`.
- Non-NULL values become Go `string` values.
- An integer result of `42` is returned as `"42"`, not as an `int`.
- An empty string `""` remains distinct from NULL.
- JSON, arrays, dates, and other types use their PostgreSQL text representations. Callers perform any required conversion.

`Duration` is not the end-to-end API latency. It does not include the entire Runtime connection setup, Gateway entry, policy validation, and final COMMIT sequence.

### Exceeding result limits

Row and byte budgets are shared across the batch rather than reset for each statement. The byte budget includes column names, type metadata, returned text values, and internal accounting overhead. It is not identical to the serialized size of an HTTP JSON response.

Exceeding a result limit returns `ErrResultLimit`, sets `Truncated` on the affected result, and rolls back the operation. It does not commit changes while returning only a partial result. Adjust `LIMIT`, selected columns, or predicates before trying again.

## Initialization and lifecycle

### States

| State          | Meaning                                                | Handling                                                                                          |
| -------------- | ------------------------------------------------------ | ------------------------------------------------------------------------------------------------- |
| `active`       | Available for normal execution                         | Execute SQL or perform lifecycle operations                                                       |
| `initializing` | Initialization has been prepared or is in progress     | Normal execution is rejected; consider Recover if initialization was interrupted                  |
| `failed`       | An initialization or cleanup failure has been recorded | Normal execution is rejected; resolve the cause, then use Reset, Recover, Reinitialize, or Delete |

The internal table also permits `deleting`, but the current Delete implementation removes objects transactionally without persisting that state. After successful deletion, the Registry row no longer exists.

### CreateWithInitialization

```go
islet, err := manager.CreateWithInitialization(ctx, []string{
    `CREATE TABLE products (id integer PRIMARY KEY, name text NOT NULL)`,
    `INSERT INTO products VALUES (1, 'Notebook'), (2, 'Pencil')`,
})
if err != nil {
    return err
}
```

The library first creates a workspace in the `initializing` state, then executes the initialization batch as its Runtime Role. Initialization SQL is subject to the same policy, privileges, and result limits as normal execution.

The initialization SQL and transition to `active` complete in the same Runtime transaction. On failure, the initialization transaction is rolled back and a separate management operation attempts to record the `failed` state.

If creation itself fails, there may be no valid Islet. If initialization fails after creation, an Islet with an ID may be returned together with an error. Do not assume that an error means no objects remain; inspect the returned ID and the result of `Open`.

### Reset

```go
next, err := manager.Reset(ctx, islet)
if err != nil {
    return err
}

islet = next
```

Reset drops the dedicated schema and recreates an empty schema for the same workspace. It increments the internal generation and returns a new handle in the `active` state. The Runtime Role and Islet ID are retained. The current implementation does not rotate the Runtime password during Reset.

Reset does not restore sample data. It clears the workspace regardless of whether the user has modified or dropped the original sample tables. Schema deletion and recreation occur within one management transaction.

### Reinitialize

```go
next, err := manager.Reinitialize(ctx, islet, []string{
    `CREATE TABLE products (id integer PRIMARY KEY, name text NOT NULL)`,
    `INSERT INTO products VALUES (1, 'Notebook')`,
})
if err != nil {
    return err
}

islet = next
```

Reinitialize empties the workspace, changes its state to `initializing`, and executes the SQL supplied for that invocation. It does not remember or reuse previously supplied initialization SQL.

Clearing the workspace and running the initialization batch are separate transactions. If initialization SQL fails, the data that existed before the reset is not restored. That data has already been removed; only changes made by the initialization SQL are rolled back.

### Recover

```go
current, err := manager.Open(ctx, savedID)
if err != nil {
    return err
}

recovered, err := manager.Recover(ctx, current)
if err != nil {
    return err
}
```

If a process exits immediately after preparing initialization, the workspace may remain `initializing`. Recover permits cleanup from that state and produces an empty, `active` workspace.

Recover does not resume interrupted SQL or restore data. It is a reset operation that clears the workspace. If an active operation still holds the lock, Recover returns `ErrBusy` rather than forcibly terminating it. To restore samples, call Reinitialize with the new handle after recovery.

### Delete

```go
if err := manager.Delete(ctx, islet); err != nil {
    return err
}
```

Delete removes the dedicated schema, Runtime Role, and Registry row. Deleting an ID that no longer exists succeeds. If the workspace still exists but the handle is stale, a generation error may be returned.

Reset and Delete first check for dependencies that could affect external objects. If a view in another schema depends on an Islet object, for example, the library returns `ErrExternalDependency` rather than unconditionally dropping external objects with `CASCADE`. An administrator must resolve the dependency before reopening the current handle and retrying.

## Islet Registry structure in PostgreSQL

Islet management information is stored in `pgislet_internal.islets`, with one row per Islet. The Go `Islet` value is a snapshot of part of that information; the Registry row is the authoritative state. Registry data and user workspaces survive application restarts as long as the database is retained.

### Table definition

The [Bootstrap implementation](internal/engine/bootstrap.go) creates the following table:

```sql
CREATE TABLE IF NOT EXISTS pgislet_internal.islets (
    id          text PRIMARY KEY,
    schema_name text UNIQUE NOT NULL,
    role_name   text UNIQUE NOT NULL,
    password    text NOT NULL,
    state       text NOT NULL
                CHECK (state IN (
                    'active',
                    'initializing',
                    'failed',
                    'deleting'
                )),
    generation  bigint NOT NULL CHECK (generation > 0),
    init_token  text,
    created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at  timestamptz NOT NULL DEFAULT clock_timestamp()
);
```

| Column        | Meaning                                                                                                                      |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `id`          | Islet identifier and primary key. Applications persist it and pass it to `Open`                                              |
| `schema_name` | Unique dedicated schema name: `pgislet_i_` followed by the Islet ID                                                          |
| `role_name`   | Unique Runtime Role name: `pgislet_r_` followed by the Islet ID                                                              |
| `password`    | Runtime login password. Currently stored in plaintext in the Registry                                                        |
| `state`       | Current state, including `active`, `initializing`, and `failed`                                                              |
| `generation`  | Internal version used to detect stale handles and operations. Starts at 1 and increases after successful reset operations    |
| `init_token`  | Token identifying the current initialization operation. Cleared when initialization completes or a failure state is recorded |
| `created_at`  | Creation timestamp                                                                                                           |
| `updated_at`  | Timestamp explicitly updated by lifecycle and state-management code; not automatically refreshed for every SQL execution     |

Although the table constraint permits `deleting`, the current Delete implementation removes objects and the Registry row in a transaction without persisting that state. Library code and internal stored functions perform state updates.

### Relationship to user data

The Registry does not contain rows from user tables. For an illustrative ID of `abc123`, the relationship is:

```text
pgislet_internal.islets
└── id: abc123
    ├── schema_name: pgislet_i_abc123
    │   └── PostgreSQL schema
    │       ├── learners
    │       └── products
    └── role_name: pgislet_r_abc123
        └── PostgreSQL Runtime Role
```

User data resides in tables within the dedicated schema. The Registry references schemas and roles by name; these columns do not have foreign keys referencing PostgreSQL objects. The ID above is illustrative; the library generates actual IDs.

The Registry belongs to an internal management schema. Runtime Roles are not granted direct access to its tables. Required operations use management connections or internal stored functions with restricted access. Registry query results and backups contain credentials and must not be exposed to end users.

### Concurrency through Registry row locks

The entry function for normal SQL execution and the lifecycle operations acquire a lock on the target Registry row in this form:

```sql
SELECT *
FROM pgislet_internal.islets
WHERE id = $1
FOR UPDATE NOWAIT;
```

Operations on the same Islet lock the same row, providing serialization across Go servers. With `NOWAIT`, a row already locked by another transaction immediately produces a lock error, which the library classifies as `ErrBusy`. The lock remains held until COMMIT or ROLLBACK, not merely until the function returns. Different Islets use different Registry rows.

### Internal schema version versus generation

The same schema also contains `pgislet_internal.version`:

```sql
CREATE TABLE IF NOT EXISTS pgislet_internal.version (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version   integer NOT NULL
);
```

The `singleton` column restricts the table to at most one row. Bootstrap creates the initial row. The current internal schema version is `1`.

| Value                                | Scope                                     | Purpose                                            |
| ------------------------------------ | ----------------------------------------- | -------------------------------------------------- |
| `pgislet_internal.version.version`   | The library's internal database structure | Verify compatibility during Bootstrap              |
| `pgislet_internal.islets.generation` | An individual Islet                       | Detect handles and operations that predate a reset |

Resetting an Islet does not increment the internal schema version. Applications use the public API rather than modifying either value directly.

## Internal operation and concurrency

### Bootstrap

Bootstrap runs each time `pgislet.New` is called, not for each SQL execution request. Creating and reusing one Manager at server startup runs Bootstrap once for that Manager.

Bootstrap performs the following operations:

1. Verify that PostgreSQL is a supported version.
2. Serialize concurrent Bootstrap operations with a transaction-level PostgreSQL advisory lock.
3. Prepare the `pgislet_internal` and `pgislet_api` schemas and verify ownership by the management account.
4. Check the internal schema version. The current version is `1`; other versions are rejected.
5. Prepare the Registry tables and revoke `PUBLIC` access to internal objects.
6. Revoke `PUBLIC` privileges on the `public` schema and the database's `CREATE` and `TEMPORARY` privileges from `PUBLIC`.
7. Check for unsafe `PUBLIC USAGE/CREATE` privileges on other ordinary schemas.
8. Create or update the Runtime Gateway functions.
9. Read built-in function names for detecting conflicts with user-defined functions.

Bootstrap does not empty existing Islets or reinitialize user tables. It is also not a general-purpose schema migration system that automatically upgrades arbitrary older internal structures.

### User SQL execution sequence

```text
Validate SQL size and batch length
    -> Read the Registry through a management connection
    -> Check the handle's internal generation
    -> Authenticate as the Islet's Runtime Role
    -> Begin a Runtime transaction
    -> Validate session_user against the role mapping in the Gateway
    -> Lock the Registry row: FOR UPDATE NOWAIT
    -> Revalidate generation, state, and initialization token under the lock
    -> Check external dependencies
    -> Validate each statement's AST and execute it
    -> Enforce returned-row and byte budgets
    -> COMMIT
    -> Close the Runtime connection
```

A Reset can occur after the initial Registry lookup, so generation validation is repeated in the Gateway rather than performed only in Go. The lock is retained throughout user SQL execution and until the transaction ends.

### Operations on the same or different Islets

When Execute, Batch, Reset, Reinitialize, Recover, or Delete operations contend for the same Islet, one operation holds the lock and the others generally receive `ErrBusy`. `Open` reads current metadata; it does not reserve a lock for subsequent execution.

Different Islets use separate Registry rows and can execute independently. They still share PostgreSQL CPU, memory, I/O, and connection capacity. The application must enforce an aggregate concurrency limit if required.

### Why the internal generation is required

Another request can complete a Reset while an operation is preparing to execute SQL. Locking alone could allow SQL prepared against an earlier workspace state to run against the newly reset workspace. Generation validation rejects that stale operation.

Generation is also checked when updating initialization state, preventing delayed failure handling from an earlier initialization from overwriting the state of a newer one. Callers do not manage the value directly; they inspect the current state when `ErrStaleGeneration` occurs.

### Connection and session lifetime

Management connections are pooled. Runtime connections are opened and closed for each operation. Open transactions, session settings, and temporary tables are not preserved between user operations. The library does not expose connection or transaction objects to callers.

Callers cannot issue `BEGIN` and `COMMIT` to combine multiple API calls into a transaction. Use Batch to execute multiple statements atomically.

## Supported SQL and limitations

SQL policy validation inspects the PostgreSQL AST rather than relying solely on keyword filtering. PostgreSQL privilege checks also apply; passing policy validation does not grant access to every object.

### Representative supported operations

| Category                 | Operations                                                         |
| ------------------------ | ------------------------------------------------------------------ |
| Queries                  | SELECT, JOIN, CTEs, aggregates, and window functions               |
| Data modification        | INSERT, UPDATE, DELETE, and MERGE                                  |
| Tables                   | CREATE TABLE, permitted ALTER TABLE operations, DROP, and TRUNCATE |
| Indexes                  | Standard CREATE INDEX and DROP INDEX                               |
| Views                    | VIEW, MATERIALIZED VIEW, and REFRESH                               |
| Types                    | ENUM, DOMAIN, composite types, and permitted alterations           |
| Sequences                | Sequence creation and alteration, and permitted sequence functions |
| Functions and procedures | Local `LANGUAGE sql` definitions and calls that satisfy policy     |
| Other                    | COMMENT and renaming for permitted objects, and EXPLAIN            |

Not all built-in functions are allowed. Callers can use functions registered in the policy and permitted local functions in the current Islet. Function bodies are validated, and user-defined function names that conflict with PostgreSQL built-ins are restricted.

The [policy implementation](internal/policy/policy.go) and [policy tests](internal/policy/policy_test.go) define the supported statements, objects, and functions. For example, allowing a trigger-related AST node does not imply support for every PostgreSQL trigger use case: the referenced function must also satisfy language and privilege requirements.

### Representative restrictions

- Administrative operations such as role and database creation, alteration, deletion, and privilege grants or revocations
- User-managed schemas and object ownership changes
- Session management, including `SET`, `SET ROLE`, and transaction control
- Temporary and unlogged tables
- Concurrent index creation and tablespace selection
- Functions outside the policy and functions in other ordinary schemas
- User-defined functions and procedures in languages other than `LANGUAGE sql`, including `plpgsql`
- Per-function session settings and support-function hooks
- Statements outside the allowlist, including COPY, extension installation, and VACUUM

This is a representative list, not a complete PostgreSQL syntax compatibility matrix. SQL accepted by PostgreSQL may still be rejected by pgislet policy.

### Isolation boundaries

pgislet aims to isolate access to user objects belonging to other Islets. It does not completely hide PostgreSQL catalogs. Object names and some metadata may be visible through system catalogs, and allowed functions such as `current_database()` expose information about the shared database.

Execution time and result limits do not provide per-user disk quotas, CPU allocation, memory caps, or connection quotas. Server configuration, installed extensions, and additional administrator-granted privileges affect the overall security boundary. The library does not defend against another privileged process arbitrarily modifying internal roles or Registry data.

## Error handling

### errors.Is and errors.As

Use `errors.Is` to identify error categories instead of comparing strings. PostgreSQL server errors are preserved as causes, so `errors.As` can retrieve SQLSTATE and diagnostic details.

```go
result, err := manager.Execute(ctx, islet, `SELECT missing_column FROM learners`)
if err != nil {
    switch {
    case errors.Is(err, pgislet.ErrBusy):
        fmt.Println("Workspace is busy")
    case errors.Is(err, pgislet.ErrStaleGeneration):
        fmt.Println("Workspace changed; reopen it before continuing")
    case errors.Is(err, pgislet.ErrOutcomeUnknown):
        fmt.Println("Check the data before retrying")
    default:
        fmt.Println("Execution failed")
    }

    var postgresError *pgconn.PgError
    if errors.As(err, &postgresError) {
        fmt.Println(postgresError.Code, postgresError.Message)
    }

    return err
}

fmt.Println(result.Rows)
```

Use this fragment inside a function that imports `errors`, `fmt`, and `github.com/jackc/pgx/v5/pgconn`. Except for the complete Quick start program, Go examples in this README are fragments intended for use within the surrounding application context.

### Error reference

| Error                   | Meaning and handling                                                                                                       |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `ErrNotFound`           | The workspace does not exist. Check the user mapping and whether it was deleted                                            |
| `ErrBusy`               | A lock could not be acquired. Inspect ongoing work and retry only as appropriate                                           |
| `ErrUnavailable`        | The current state or initialization token does not permit the operation                                                    |
| `ErrFailed`             | Normal execution was requested for a failed workspace. Lifecycle recovery is required                                      |
| `ErrStaleGeneration`    | The handle or prepared operation is stale. Reopen the Islet and inspect its current state                                  |
| `ErrPolicy`             | SQL parsing or policy validation failed                                                                                    |
| `ErrTimeout`            | An execution deadline or corresponding PostgreSQL cancellation condition was reached                                       |
| `ErrResultLimit`        | A row or byte budget was exceeded; the operation is rolled back                                                            |
| `ErrSQLTooLarge`        | The total SQL size or batch statement count exceeds its limit                                                              |
| `ErrRuntimeConnection`  | Runtime Role authentication or connection failed                                                                           |
| `ErrInitialization`     | Initialization failed. Check the returned ID and current Registry state                                                    |
| `ErrLifecycle`          | A management operation such as creation or cleanup failed                                                                  |
| `ErrQuery`              | A PostgreSQL SQL error occurred. Inspect SQLSTATE                                                                          |
| `ErrOutcomeUnknown`     | A COMMIT error left the final transaction outcome uncertain                                                                |
| `ErrUnsupported`        | The PostgreSQL version, internal structure, ownership, or privileges do not satisfy the supported environment requirements |
| `ErrExternalDependency` | Cleanup could affect another workspace or an external object                                                               |

Not every error is converted to one of these categories. Configuration parsing errors and some context or connection errors may be returned directly, so include a fallback error-handling path.

`pgislet.Error` contains `Kind`, `Cause`, and `Statement`. For statement-specific policy or execution errors, `Statement` is the zero-based batch index. Wrapped errors not associated with a statement use `-1`. Do not assume every failure has a valid statement index.

### Retry considerations

The library does not automatically reexecute failed user SQL. In particular, a connection failure during COMMIT may leave the client unable to determine whether PostgreSQL committed the transaction.

Blindly repeating an INSERT after `ErrOutcomeUnknown` can duplicate changes. Inspect the current data or use application-level idempotency guarantees. Similarly, before reopening a stale handle and retrying, determine whether the workspace state still matches the user's intent.

## Application integration and operations

### Request processing

```text
Authenticate the user
    -> Check application authorization
    -> Look up the user's assigned Islet ID
    -> Manager.Open
    -> Execute or Batch
    -> Classify errors and convert results
    -> Respond to the user
```

An Islet ID is not an authorization credential. The application must verify ownership so a client cannot supply an arbitrary ID and access another user's workspace. Server code that can invoke the Manager belongs to the trusted management boundary.

### Multiple application servers

Servers using the same management identity and database configuration can create their own Managers and open the same Islet ID. The shared Registry and PostgreSQL locks eliminate the need for a separate distributed mutex service for per-Islet serialization.

If user-to-Islet mappings are stored only in one server's memory, other servers cannot access those mappings. Sharing mappings and authentication sessions is the application's responsibility.

### Shutdown and failures

Drain ongoing requests before calling `Manager.Close()`. Close does not delete or expire user workspaces. Per-operation Runtime connections are cleaned up by the execution path; there is no separate Shutdown API that forcibly cancels all requests.

When a client process exits, connection termination and PostgreSQL transaction cleanup release its locks. Network failures may not be detected immediately, so statement and idle-transaction timeouts remain important. Applications can inspect workspaces left in `initializing` after interruption between initialization phases and call Recover.

### Application-defined operational policies

- Workspace counts and per-user allocation policies
- Idle workspace expiration and deletion jobs
- Runtime connection concurrency and request rate limits
- Database storage monitoring and long-running query observability
- Access control for the management DSN and Registry credentials
- Procedures for initialization failures, external dependencies, and uncertain transaction outcomes

The Registry stores passwords required for Runtime authentication. These are not exposed through public Islet fields, but database administrators can access them. Protect management data and backups accordingly.

## Example applications

### CLI Playground

```sh
go run ./examples/playground -container
```

This starts a temporary PostgreSQL 17 container and demonstrates:

1. Initializing a workspace with sample SQL
2. Opening and querying the same workspace through another Manager
3. Dropping a sample table as the Runtime Role
4. Verifying that cross-Islet access is denied
5. Verifying that Reset invalidates an old handle
6. Reinitializing with caller-provided SQL
7. Cleaning up the Islets and temporary container

To use an existing database:

```sh
PGISLET_DSN='postgres://postgres:password@localhost:5432/appdb?sslmode=disable' \
    go run ./examples/playground
```

You can also supply the DSN with `-dsn`.

### Web Playground

```sh
go run ./examples/web -container
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080).

To use a different port:

```sh
go run ./examples/web -container -addr 127.0.0.1:8090
```

You can also use an existing database or build a binary:

```sh
PGISLET_DSN='postgres://postgres:password@localhost:5432/appdb?sslmode=disable' \
    go run ./examples/web

go build -o build/pgislet-web ./examples/web
./build/pgislet-web -container -assets ./examples/web/static
```

The web example creates one Manager at startup and reuses it. The browser receives a random HttpOnly/SameSite cookie, and the server maps the cookie to an Islet ID in memory. Tabs for the same site in the same browser profile share a workspace. Separate profiles or private browsing sessions receive separate workspaces.

The example retains at most 32 sessions, expires idle sessions after 30 minutes, runs cleanup every minute, and limits JSON request bodies to 64 KiB. It sets the SQL size limit to 64 KiB and uses the library defaults of a 10-second statement timeout and result limits of 1,000 rows and 4 MiB.

| Method | Path           | Behavior                                |
| ------ | -------------- | --------------------------------------- |
| POST   | `/api/session` | Create or reuse the browser workspace   |
| GET    | `/api/schema`  | List the workspace's tables and columns |
| POST   | `/api/query`   | Execute `{"sql":"SELECT 1"}`            |
| POST   | `/api/reset`   | Empty the workspace                     |
| POST   | `/api/seed`    | Reinitialize with sample data           |
| DELETE | `/api/session` | Delete the current workspace            |

Mutation requests require the `X-Pgislet-Request: playground` header. SQL requests use `Content-Type: application/json`. The browser client manages these headers and the session cookie.

This is a single-process demo that listens on loopback addresses. It does not provide user account authentication or session sharing across web servers. Graceful shutdown cleans up the example workspaces and removes PostgreSQL when started with `-container`. An abrupt exit while using an existing database may leave Islets behind.

## Tests and coverage

### Unit tests

```sh
go test ./internal/... ./examples/...
```

To check all packages while explicitly disabling integration tests:

```sh
PGISLET_UNIT_ONLY=1 go test ./...
```

This skips integration tests; it does not demonstrate that they pass. Use the Docker-based suite below for complete validation and coverage measurement.

### PostgreSQL integration tests

```sh
go test ./tests/integration -count=1
```

The suite creates and terminates PostgreSQL 17 through Testcontainers. It does not use a supplied `PGISLET_DSN` to target an existing database. In the default mode, failure to prepare Docker causes the suite to fail.

Integration tests cover user-object isolation, Runtime identity checks, same-Islet contention, initialization, recovery, external dependencies, result limits, stale-handle rejection, and process termination.

### Full validation

```sh
go vet ./...
go test -race -coverpkg=.,./internal/... -coverprofile=coverage.out ./... -count=1
go tool cover -func=coverage.out
```

### Codecov configuration

`codecov.yaml` sets both project and patch coverage targets to 70%.

| Excluded pattern           | Reason                                                                          |
| -------------------------- | ------------------------------------------------------------------------------- |
| `**/*_test.go`             | Test code itself                                                                |
| `examples/**`              | Example executables and the web application, outside the library coverage scope |
| `pgislet.go`               | Public aliases and forwarding functions                                         |
| `internal/engine/types.go` | Data declarations without executable behavior                                   |

Error handling, SQL policy, result processing, Bootstrap, and lifecycle logic are not excluded. Each non-excluded implementation file has a matching unit test file or a corresponding test file in `tests/integration`.

## Frequently asked questions

### Can users drop sample tables?

Yes. Samples are user objects created by caller-provided Runtime SQL. To restore them, the application supplies initialization SQL to Reinitialize.

### Does Reset rerun the original initialization SQL?

No. It creates an empty workspace. The library does not retain initialization SQL.

### Does creating a new Manager reinitialize existing Islets?

No. Bootstrap prepares internal structures and privileges without clearing user data. Use Open with a stored ID to access an existing Islet.

### Why expose a generation error when generation itself is private?

The library manages the version value, but callers still need to know when a request uses a stale handle. Reopen the current state and determine whether the operation should be retried.

### Can I serialize an Islet to JSON and restore it for execution?

You can serialize its public information, but JSON does not preserve the internal execution-handle state. Persist the ID and use Open to obtain a new handle.

### What happens if two SQL operations run on the same Islet concurrently?

While one operation holds the Registry lock, the other generally receives `ErrBusy`. The library does not automatically queue or retry operations.

### Does initialization recover automatically after a server failure?

No background task monitors initialization state. The application must inspect the current state and apply its Recover or reinitialization policy.

### Are all SQL statements and extensions supported?

No. SQL must satisfy both the policy allowlist and PostgreSQL privileges. Administrative capabilities such as extension installation and arbitrary function languages are not exposed through user SQL.

### Can I use PostgreSQL 18?

The current implementation permits only 17.x. Support for another version requires validation of the parser, policy, catalogs, and privilege behavior, rather than simply removing the version check.

### Can one database support an unlimited number of users?

No. Every Islet creates a schema and a role, and running operations consume Runtime connections. Applications must manage workspace counts and concurrency according to PostgreSQL connection limits, catalog size, storage capacity, and server resources.
