# pgislet

[English README](../README.md)

pgisletは、共有のPostgreSQLデータベース内で、ユーザーごとに独立したSQLワークスペースを提供するGoライブラリです。

SQL学習プラットフォーム、クエリ演習、ブラウザベースのPlaygroundなど、ユーザーがSQLを直接記述するアプリケーションを対象としています。ユーザーは割り当てられたワークスペース内でテーブルの作成、データの参照・更新、オブジェクトの削除を行えます。アプリケーションはGo APIを通じてワークスペースの作成、初期化、復旧、削除を管理します。

各ワークスペースを **Islet** と呼びます。Isletには専用のPostgreSQLスキーマと、ログイン可能な **Runtime Role** が割り当てられます。ユーザーSQLは、そのロールで認証された接続上で実行されます。

> [!WARNING]
>
> pgisletは任意のPostgreSQL管理コマンドを許可する汎用SQLプロキシではありません。ロールと権限によるオブジェクトの分離を提供しますが、ユーザーごとに別のデータベースや専用のCPU、メモリ、ディスクリソースを割り当てるものではありません。コンテナやVMレベルの分離は提供しません。

## 目的と責任範囲

一般的なWebアプリケーションでは、サーバーがあらかじめ定義されたSQLを実行します。一方、SQL学習プラットフォームでは、ユーザー自身によるテーブル作成やデータ挿入が必要です。すべてのユーザーが同じ認証主体と権限を使用すると、他のユーザーのデータを変更したり、共有環境を破損したりする可能性があります。

pgisletは独立したSQLワークスペースを提供し、その境界を管理します。

| pgisletの責任                                 | アプリケーションの責任                         |
| --------------------------------------------- | ---------------------------------------------- |
| IsletスキーマとRuntime Roleの作成             | ログイン、認証、アプリケーションの認可         |
| SQLポリシーの検証とユーザーSQLの実行          | ユーザーへのIsletの割り当て                    |
| Islet単位の操作の直列化と内部バージョンの検証 | ユーザーとIslet IDの対応関係の永続化           |
| 実行時間と結果サイズの制限                    | リクエストのレート制限と全体の同時実行数の制御 |
| Reset、Reinitialize、Recover、Delete          | 初期化SQLとサンプルデータの定義                |
| エラーの分類とPostgreSQLエラーの保持          | HTTPレスポンス、UI、再試行ポリシーの定義       |
| 内部RegistryとRuntime Gatewayの管理           | 有効期限、使用量の追跡、バックアップ、監視     |

## 基本概念とアーキテクチャ

### Manager

`Manager`は管理用接続プール、Runtime接続設定、実行制限を保持します。サーバー起動時に作成し、複数のリクエストで再利用します。

管理接続は、Isletの作成・削除、Registryの参照、内部オブジェクトの準備に使用します。ユーザーが提供するSQLを管理接続で実行することはありません。

### Islet

Isletは次の要素で構成されます。

| 要素             | 役割                                                                 |
| ---------------- | -------------------------------------------------------------------- |
| ID               | アプリケーションが保存し、ワークスペースを再度開くための識別子       |
| 専用スキーマ     | ユーザーのテーブル、ビュー、関数などのローカルオブジェクトの名前空間 |
| 専用Runtime Role | ユーザーSQLの実行に使用するログイン主体                              |
| Registry行       | スキーマ、ロール、内部認証情報、状態、バージョンの記録               |
| 内部Generation   | ResetやReinitializeより前の古いハンドルや操作の検出                  |
| 初期化トークン   | 続行を許可された初期化操作の識別                                     |

スキーマ自体は管理ロールが所有します。Runtime Roleには、そのスキーマの`USAGE`権限と`CREATE`権限を付与します。Runtime Roleが作成したオブジェクトは、そのロールの権限で管理できます。

### アーキテクチャ図

![pgisletアーキテクチャ](diagrams/diagram.png)

SQL PolicyはアプリケーションのGoプロセス内で、Runtime GatewayはPostgreSQL内で動作します。Runtime GatewayからIsletへの接続線は、検証後に同じRuntimeトランザクション内でSQLが実行されることを示します。Gatewayが呼び出し元に代わってユーザーSQLを実行するわけではありません。Management Poolは接続を提供し、実際のライフサイクル操作はManagerの管理コードが行います。ロールはクラスタ全体、スキーマはデータベース単位のオブジェクトですが、図ではIsletとの論理的な関連に基づいてまとめています。

複数のManagerが同じPostgreSQLデータベースを利用できます。Islet単位の同時実行制御にはRegistryのPostgreSQL行ロックを使用し、プロセス内のMutexや特定サーバーへのリクエスト固定には依存しません。

## 動作要件とインストール

pgisletは**PostgreSQL 17.xと18.xをサポートします**。サンプル、統合テスト、ComposeのデフォルトはPostgreSQL 18です。CIでは両バージョンで全テストを実行します。

| 項目                 | 要件                                         |
| -------------------- | -------------------------------------------- |
| Go                   | `go.mod`で指定されている1.26.1以降           |
| PostgreSQL           | 17.x、18.x                                         |
| SQLパーサー          | `pg_query_go/v6`。CGOとCコンパイラが必要     |
| PostgreSQLドライバー | `pgx/v5`                                     |
| Docker               | コンテナを使用するサンプルと統合テストで必要 |

SQLパーサーはPostgreSQL 18の文法に対応する`pg_query_go/v6`の開発版コミット`e6a9b9881a8b`に固定しています。正確な依存バージョンは`go.mod`に記録されています。

既存のGoプロジェクトにライブラリを追加します。

```sh
go get github.com/swualabs/pgislet
```

公開パッケージをインポートします。

```go
import "github.com/swualabs/pgislet"
```

### データベースの準備

> [!WARNING]
> **アプリケーションのデプロイ時には、pgislet専用のデータベースを使用してください。**
>
> Bootstrapは内部テーブルの作成に加え、データベースと`public`スキーマの`PUBLIC`権限を変更します。それらの権限に依存する既存アプリケーションのデータベースにpgisletを接続しないでください。

管理アカウントには、ロールとスキーマの作成・削除、内部関数とテーブルの管理、必要な権限の付与・取り消しを行う権限が必要です。サンプルでは`postgres`管理アカウントを使用します。権限を制限したアカウントを使用する場合は、これらの操作が可能か確認してください。すべてのマネージドPostgreSQLサービスの権限モデルとの互換性は保証していません。

リポジトリには、サンプル用PostgreSQL 18インスタンスを起動するCompose設定が含まれています。PostgreSQL 18のDockerイメージ構成に合わせ、`pgdata18`ボリュームを`/var/lib/postgresql`にマウントします。既存の`pgdata`にあるPostgreSQL 17のデータは再利用も削除もしません。既存データの移行にはダンプとリストア、または`pg_upgrade`を使用してください。イメージタグの変更だけではデータベースはアップグレードされません。

以下の接続設定はローカルサンプル専用です。本番環境では使用しないでください。

| 設定     | デフォルト値 |
| -------- | ------------ |
| Host     | `localhost`  |
| Port     | `5432`       |
| Database | `appdb`      |
| User     | `postgres`   |
| Password | `password`   |

## クイックスタート

次のプログラムは、ワークスペースを作成し、初期データを挿入してクエリを実行した後、ワークスペースを削除する完全なサンプルです。

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

別のディレクトリに`main.go`として保存し、モジュールの初期化と依存関係のインストールを行って実行します。

```sh
go mod init example.com/pgislet-demo
go get github.com/swualabs/pgislet
export PGISLET_DSN='postgres://postgres:password@localhost:5432/appdb?sslmode=disable'
go run .
```

実行結果:

```text
1 Alice
2 Bob
```

このサンプルは終了時にIsletを削除します。ユーザーのワークスペースを永続化する場合は、リクエストごとに削除せず、アプリケーションでIslet IDを保存してください。Managerを閉じてもIsletは削除されません。

## 接続設定とDSNヘルパー

### DSN文字列の指定

`Config.DSN`は管理アカウントの接続文字列です。

```go
manager, err := pgislet.New(ctx, pgislet.Config{
    DSN: "postgres://postgres:password@localhost:5432/appdb?sslmode=disable",
})
```

ライブラリは`pgxpool.ParseConfig`でDSNを解析します。Runtime接続では管理接続の設定をコピーしたうえで、ユーザー名とパスワードをIsletのRuntime Roleの認証情報に置き換え、必要なセッション設定を適用します。

管理接続が成功しても、Runtime Roleがログインできるとは限りません。PostgreSQLの認証設定は、生成されたRuntime Roleによる接続も許可する必要があります。

### ConnectionParamsの使用

`BuildDSN`は、認証情報やデータベース名に特殊文字が含まれる場合も、手動の文字列連結を必要とせず接続文字列を生成します。

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

| フィールド | 意味とデフォルト値                                                                 |
| ---------- | ---------------------------------------------------------------------------------- |
| `Host`     | 必須。ホスト名、角括弧なしのIPv6アドレス、またはUnixソケットディレクトリの絶対パス |
| `Port`     | `0`の場合は`5432`。指定する場合は1～65535                                          |
| `Database` | 必須。データベース名                                                               |
| `Username` | 必須。管理アカウント名                                                             |
| `Password` | パスワード。空文字列も受け付けますが、認証の成否はサーバーが判断                   |
| `SSLMode`  | 空の場合は`verify-full`                                                            |

SSLモードは`disable`、`allow`、`prefer`、`require`、`verify-ca`、`verify-full`をサポートします。ローカルコンテナのサンプルはTLSを構成しないため、`disable`を明示します。デフォルトの`verify-full`を使用する環境では、サーバー証明書とホスト名の検証が可能な設定が必要です。

ヘルパーはユーザー名、パスワード、データベース名をURLエスケープします。NUL文字、不正なポート、不正なSSLモードは拒否します。証明書ファイルのパスなど、この構造体にない接続オプションが必要な場合は、完全なDSNを直接指定してください。返される文字列にはパスワードが含まれるため、そのままログに記録しないでください。

`BuildDSN`は文字列の生成のみを行います。PostgreSQLへの接続やBootstrapは実行しません。

### アプリケーション設定との連携

`ConnectionConfig`インターフェースには、次のメソッドがあります。

```go
type ConnectionConfig interface {
    PostgreSQLParams() pgislet.ConnectionParams
}
```

既存の設定型にこのメソッドを実装すると、その値をヘルパーに直接渡せます。

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

`pgislet.BuildDSN(settings)`で変換します。`ConnectionParams`自身もこのインターフェースを実装しています。

## Managerの設定

数値または期間に`0`を指定するとデフォルト値が適用されます。負の値やサポートされる最小値未満の値は拒否されます。`0`は制限の無効化を意味しません。

| 設定                     | デフォルト値                   | 適用範囲                                                                   |
| ------------------------ | ------------------------------ | -------------------------------------------------------------------------- |
| `DSN`                    | ライブラリ独自のデフォルトなし | 管理接続の設定                                                             |
| `OperationTimeout`       | 30秒                           | Bootstrap、実行、作成、クリーンアップなど主要操作のGoコンテキスト期限      |
| `StatementTimeout`       | 10秒                           | Runtime接続でのPostgreSQL文の実行時間制限                                  |
| `LockTimeout`            | 1秒                            | PostgreSQLのロック待機時間制限                                             |
| `IdleTransactionTimeout` | 15秒                           | 開いたトランザクション内でアイドル状態になった接続のサーバー側タイムアウト |
| `MaxSQLBytes`            | 1 MiB                          | 実行または初期化バッチに含まれるSQL文字列の合計サイズ                      |
| `MaxBatchStatements`     | 100                            | バッチに含まれるSQL文字列数                                                |
| `MaxRows`                | 1,000                          | バッチ全体で返す行数                                                       |
| `MaxResultBytes`         | 4 MiB                          | バッチ全体に適用する内部計算上の結果バイト予算                             |

期間は1ms以上、件数とサイズは1以上である必要があります。呼び出し元のコンテキスト期限が先に到来する場合は、そちらが優先されます。`Open`は`OperationTimeout`の期限を追加せず、渡されたコンテキストでRegistryを参照するため、適切な期限を指定してください。

`LockTimeout`とIsletの直列化は異なります。同じIsletのRegistry行ロックには`NOWAIT`を使用するため、別の操作がロックを保持している場合、キューで待機せず`ErrBusy`を返します。

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

> `MaxRows`は変更行数ではなく、返される行数を制限します。たとえば`RETURNING`のない`UPDATE`による変更行数は制限しません。`MaxResultBytes`も、データベースの保存容量やPostgreSQLプロセスのメモリ使用量を制限するものではありません。

## 公開APIとIsletハンドル

### APIリファレンス

`New`はパッケージ関数です。表内のその他の操作は`*pgislet.Manager`のメソッドです。

| API                                         | 戻り値              | 動作                                             |
| ------------------------------------------- | ------------------- | ------------------------------------------------ |
| `New(ctx, Config)`                          | `(*Manager, error)` | 設定の検証、管理プールの準備、Bootstrapの実行    |
| `Close()`                                   | なし                | Isletを削除せず管理プールを閉じる                |
| `Create(ctx)`                               | `(Islet, error)`    | 空のIsletを作成                                  |
| `Open(ctx, id)`                             | `(Islet, error)`    | 保存したIDに対応する現在のハンドルを取得         |
| `Execute(ctx, islet, sql)`                  | `(Result, error)`   | SQL文を1つ実行                                   |
| `Batch(ctx, islet, statements)`             | `([]Result, error)` | 単一トランザクションで複数の文を順番に実行       |
| `Reset(ctx, islet)`                         | `(Islet, error)`    | ワークスペースを空にして新しいハンドルを返す     |
| `CreateWithInitialization(ctx, statements)` | `(Islet, error)`    | 作成後、呼び出し元が渡したSQLで初期化            |
| `Reinitialize(ctx, islet, statements)`      | `(Islet, error)`    | ワークスペースを空にして新しい初期化SQLを実行    |
| `Recover(ctx, islet)`                       | `(Islet, error)`    | 中断された初期化を含め、空のワークスペースに復旧 |
| `Delete(ctx, islet)`                        | `error`             | スキーマ、Runtime Role、Registry行を削除         |

`BuildDSN`もパッケージ関数です。`BuildDSN(config ConnectionConfig)`は`(string, error)`を返します。

### Isletの公開フィールド

| フィールド  | 型          | 意味                                         |
| ----------- | ----------- | -------------------------------------------- |
| `ID`        | `string`    | ワークスペースの識別子                       |
| `State`     | `string`    | 値の取得時または返却時の状態                 |
| `CreatedAt` | `time.Time` | 作成日時                                     |
| `UpdatedAt` | `time.Time` | Registryのライフサイクルまたは状態の更新日時 |

**ハンドル**とは、ライブラリが返す`Islet`値です。データベース接続ではなく、状態のスナップショットです。別のリクエストがワークスペースを変更しても、既存の値のフィールドは自動更新されません。`UpdatedAt`はSQLの実行履歴を記録しないため、最終アクティビティ時刻として使用しないでください。

### 内部Generationの管理

Generationは非公開フィールドであり、呼び出し元から読み取り・設定はできません。増分操作の公開APIもありません。作成・初期化操作が返すハンドルには、内部検証に必要な情報が含まれます。

次の規則に従ってください。

1. `Create`または`Open`が返したハンドルを使用します。
2. `Reset`、`Recover`、`Reinitialize`が成功したら、返された値で古いハンドルを置き換えます。
3. 長期保存にはIslet IDを使用します。
4. 保存したIDを再利用するときは`Open`で現在のハンドルを取得します。

`pgislet.Islet{ID: savedID}`を直接作成したり、JSONから復元した値を実行用ハンドルとして使用したりする方法はサポートされません。JSONには内部Generationが保持されないためです。

```go
islet, err := manager.Open(ctx, savedID)
if err != nil {
    return err
}

result, err := manager.Execute(ctx, islet, `SELECT current_schema()`)
```

## SQL実行と結果

### Execute

`Execute`は、トップレベルのSQL文を正確に1つ含む文字列を受け取ります。

```go
result, err := manager.Execute(ctx, islet, `SELECT name FROM learners ORDER BY id`)
if err != nil {
    return err
}

for _, row := range result.Rows {
    fmt.Println(row[0])
}
```

`SELECT 1; SELECT 2`を1つの文字列として渡すとポリシーエラーになります。複数の文には`Batch`を使用します。関数定義内のSQL本体は別途解析するため、入力を単純にセミコロンで分割して検証する方式ではありません。

現在のAPIはバインド引数を受け付けません。`Execute(ctx, islet, sql, args...)`形式はサポートされません。ユーザーが記述したSQLを実行することと、ユーザー入力値からアプリケーションのSQLを組み立てることは異なります。後者では安全でない文字列連結を避けてください。

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

スライスの各要素には1つの文を指定します。バッチ全体を1つのRuntime接続とトランザクションで実行するため、後続の文は先行する文で作成したテーブルや関数を使用できます。

すべての文とCOMMITが成功した場合にのみ操作が成功します。文のエラーやポリシー違反が発生すると、トランザクションをロールバックします。ただし、シーケンス値の進行など、PostgreSQL自体がロールバックしない効果をpgisletが取り消すことはありません。

エラーとともに、実行途中で収集した一部の`Result`が返される場合があります。これは先行する文の変更がコミットされたことを意味しません。成功と判断する前に、必ず`err`を確認してください。

### 結果の表現

| フィールド     | 意味                                                 |
| -------------- | ---------------------------------------------------- |
| `Columns`      | カラム名とPostgreSQL型OID                            |
| `Rows`         | `[][]any`形式の結果行                                |
| `CommandTag`   | `SELECT 2`や`INSERT 0 1`などのPostgreSQLコマンドタグ |
| `RowsAffected` | コマンドタグが報告する行数                           |
| `Duration`     | 文の発行と結果の読み取りに要した時間                 |
| `Truncated`    | 結果予算を超過したことを示すフラグ                   |

各`Column`は`Name string`と`DataTypeOID uint32`を持ちます。Runtimeの結果はPostgreSQLのテキスト形式で読み取ります。

- SQLの`NULL`はGoの`nil`になります。
- NULL以外の値はGoの`string`になります。
- 整数の`42`も`int`ではなく`"42"`として返されます。
- 空文字列`""`とNULLは区別されます。
- JSON、配列、日付などもPostgreSQLのテキスト表現です。必要な変換は呼び出し元が行います。

`Duration`はAPI呼び出し全体の所要時間ではありません。Runtime接続の準備、Gatewayへの進入、ポリシー検証、最終COMMITのすべてを含む値ではありません。

### 結果制限の超過

行数とバイト予算は文ごとにリセットせず、バッチ全体で共有します。バイト予算にはカラム名、型メタデータ、返されるテキスト値、内部計算上のオーバーヘッドが含まれます。HTTP JSONレスポンスのシリアライズ後のサイズとは一致しません。

制限を超えると`ErrResultLimit`を返し、該当する結果の`Truncated`を設定して操作をロールバックします。変更をコミットしながら一部の結果だけ返すモードではありません。`LIMIT`、取得カラム、検索条件を調整してから再試行してください。

## 初期化とライフサイクル

### 状態

| 状態           | 意味                                       | 対応                                                                     |
| -------------- | ------------------------------------------ | ------------------------------------------------------------------------ |
| `active`       | 通常実行が可能                             | SQL実行またはライフサイクル操作                                          |
| `initializing` | 初期化の準備済み、または実行中             | 通常実行を拒否。中断されている場合はRecoverを検討                        |
| `failed`       | 初期化またはクリーンアップの失敗を記録済み | 通常実行を拒否。原因を解消してReset、Recover、Reinitialize、Deleteを使用 |

内部テーブルは`deleting`も許可しますが、現在のDelete実装はこの状態を永続化せず、トランザクション内でオブジェクトを削除します。削除が成功するとRegistry行は存在しなくなります。

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

ライブラリは最初に`initializing`状態のワークスペースを作成し、Runtime Roleで初期化バッチを実行します。初期化SQLにも通常実行と同じポリシー、権限、結果制限が適用されます。

初期化SQLと`active`への状態遷移は、同じRuntimeトランザクションで完了します。失敗した場合は初期化トランザクションをロールバックし、別の管理操作で`failed`状態の記録を試みます。

作成自体が失敗した場合、有効なIsletが存在しないことがあります。作成後の初期化が失敗した場合は、IDを持つIsletとエラーが同時に返されることがあります。エラーだからオブジェクトが何も残っていないとは判断せず、返されたIDと`Open`の結果を確認してください。

### Reset

```go
next, err := manager.Reset(ctx, islet)
if err != nil {
    return err
}

islet = next
```

Resetは専用スキーマを削除し、同じワークスペースに空のスキーマを再作成します。内部Generationを増加させ、`active`状態の新しいハンドルを返します。Runtime RoleとIslet IDは維持します。現在の実装はReset時にRuntimeパスワードをローテーションしません。

サンプルデータは復元しません。元のサンプルテーブルが変更・削除されていても、ワークスペース全体を空にします。スキーマの削除と再作成は、単一の管理トランザクションで実行します。

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

Reinitializeはワークスペースを空にし、状態を`initializing`に変更して、その呼び出しで渡されたSQLを実行します。以前の初期化SQLを保存・再利用することはありません。

**ワークスペースを空にする処理と初期化バッチは、別々のトランザクションです。** 初期化SQLが失敗しても、Reset前のデータは復元されません。以前のデータはすでに削除されており、初期化SQLによる変更だけがロールバックされます。

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

初期化の準備直後にプロセスが終了すると、ワークスペースが`initializing`のまま残ることがあります。Recoverはこの状態からのクリーンアップを許可し、空の`active`ワークスペースに戻します。

Recoverは中断されたSQLを再開したり、データを復元したりする操作ではありません。ワークスペースを空にするReset系の操作です。実行中の操作がまだロックを保持している場合は、強制終了せず`ErrBusy`を返します。サンプルを復元する場合は、復旧後の新しいハンドルでReinitializeを呼び出します。

### Delete

```go
if err := manager.Delete(ctx, islet); err != nil {
    return err
}
```

Deleteは専用スキーマ、Runtime Role、Registry行を削除します。存在しないIDの削除は成功として扱います。ただし、ワークスペースが存在し、ハンドルが古い場合はGenerationエラーが返されることがあります。

ResetとDeleteは、外部オブジェクトに影響する依存関係を先に確認します。たとえば別スキーマのビューがIslet内のオブジェクトに依存する場合、`CASCADE`で外部オブジェクトを無条件に削除せず、`ErrExternalDependency`を返します。管理者が依存関係を解消してから、現在のハンドルを再取得して再試行してください。

## PostgreSQL内のIslet Registry構造

Isletの管理情報は`pgislet_internal.islets`に、Isletごとに1行保存されます。Goの`Islet`値はその一部のスナップショットであり、正しい状態の基準はRegistry行です。データベースが維持されている限り、アプリケーションを再起動してもRegistryとユーザーワークスペースは維持されます。

### テーブル定義

[Bootstrapの実装](../internal/engine/bootstrap.go)は次のテーブルを作成します。

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

| カラム        | 意味                                                                                  |
| ------------- | ------------------------------------------------------------------------------------- |
| `id`          | Islet識別子と主キー。アプリケーションが保存して`Open`に渡す値                         |
| `schema_name` | 一意の専用スキーマ名。`pgislet_i_`にIslet IDを連結                                    |
| `role_name`   | 一意のRuntime Role名。`pgislet_r_`にIslet IDを連結                                    |
| `password`    | Runtimeログイン用パスワード。現在はRegistryに平文で保存                               |
| `state`       | `active`、`initializing`、`failed`などの現在の状態                                    |
| `generation`  | 古いハンドルや操作を検出する内部バージョン。1から開始し、Reset系の操作成功時に増加    |
| `init_token`  | 現在の初期化操作を識別するトークン。初期化完了時、または失敗状態の記録時に削除        |
| `created_at`  | 作成日時                                                                              |
| `updated_at`  | ライフサイクル・状態管理コードが明示的に更新する日時。SQL実行ごとには自動更新されない |

テーブル制約は`deleting`を許可しますが、現在のDelete実装はこの状態を永続化せず、トランザクション内でオブジェクトとRegistry行を削除します。状態の更新はライブラリコードと内部保存関数が行います。

### ユーザーデータとの関係

Registryはユーザーテーブルのデータ行を保存する場所ではありません。説明用IDを`abc123`とすると、次の関係になります。

```text
pgislet_internal.islets
└── id: abc123
    ├── schema_name: pgislet_i_abc123
    │   └── PostgreSQLスキーマ
    │       ├── learners
    │       └── products
    └── role_name: pgislet_r_abc123
        └── PostgreSQL Runtime Role
```

ユーザーデータは専用スキーマ内のテーブルに保存されます。Registryはスキーマとロールを名前で参照します。これらのカラムにPostgreSQLオブジェクトを参照する外部キーはありません。上のIDは説明用で、実際のIDはライブラリが生成します。

Registryは内部管理スキーマに属し、Runtime Roleにはテーブルへの直接アクセス権限を付与しません。必要な操作は管理接続、またはアクセスを制限した内部保存関数を通じて行います。Registryの検索結果とバックアップには認証情報が含まれるため、エンドユーザーに公開しないでください。

### Registry行ロックによる同時実行制御

通常SQL実行の進入関数とライフサイクル操作は、対象Registry行に次の形式でロックを取得します。

```sql
SELECT *
FROM pgislet_internal.islets
WHERE id = $1
FOR UPDATE NOWAIT;
```

同じIsletの操作は同じ行をロックするため、複数のGoサーバー間でも直列化されます。`NOWAIT`では、別のトランザクションがすでにロックしている場合、即座にロックエラーが発生し、ライブラリは`ErrBusy`に分類します。ロックは関数の終了時ではなくCOMMITまたはROLLBACKまで保持されます。異なるIsletは異なるRegistry行を使用します。

### 内部スキーマバージョンとGenerationの違い

同じスキーマには`pgislet_internal.version`も存在します。

```sql
CREATE TABLE IF NOT EXISTS pgislet_internal.version (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version   integer NOT NULL
);
```

`singleton`カラムにより、保存できる行数は最大1行に制限されます。Bootstrapが初期行を作成します。現在の内部スキーマバージョンは`1`です。

| 値                                   | 対象範囲                             | 目的                              |
| ------------------------------------ | ------------------------------------ | --------------------------------- |
| `pgislet_internal.version.version`   | ライブラリの内部データベース構造全体 | Bootstrap時の互換性確認           |
| `pgislet_internal.islets.generation` | 個々のIslet                          | Resetより前のハンドルや操作の検出 |

IsletをResetしても内部スキーマバージョンは増加しません。アプリケーションはこれらの値を直接変更せず、公開APIを使用します。

## 内部動作と同時実行

### Bootstrap

Bootstrapは **`pgislet.New`を呼び出すたびに** 実行されます。SQL実行リクエストごとではありません。サーバー起動時にManagerを1つ作成して再利用する場合、そのManagerのBootstrapは起動時に1回実行されます。

Bootstrapは次の操作を行います。

1. PostgreSQLがサポート対象のバージョンであることを確認します。
2. トランザクション単位のPostgreSQLアドバイザリロックで、同時Bootstrapを直列化します。
3. `pgislet_internal`と`pgislet_api`スキーマを準備し、管理アカウントが所有することを確認します。
4. 内部スキーマバージョンを確認します。現在は`1`で、それ以外は拒否します。
5. Registryテーブルを準備し、内部オブジェクトへの`PUBLIC`アクセスを取り消します。
6. `public`スキーマの`PUBLIC`権限と、データベースで`PUBLIC`に付与された`CREATE`・`TEMPORARY`権限を取り消します。
7. 他の通常スキーマに安全でない`PUBLIC USAGE/CREATE`権限がないか確認します。
8. Runtime Gateway関数を作成または更新します。
9. ユーザー定義関数との名前衝突を検出するため、組み込み関数名を読み取ります。

既存のIsletを空にしたり、ユーザーテーブルを再初期化したりすることはありません。また、任意の古い内部構造を自動更新する汎用スキーママイグレーション機構でもありません。

### ユーザーSQLの実行順序

```text
SQLサイズとバッチ長を検証
    -> 管理接続でRegistryを参照
    -> ハンドルの内部Generationを確認
    -> IsletのRuntime Roleで認証
    -> Runtimeトランザクションを開始
    -> Gatewayでsession_userとロールの対応を検証
    -> Registry行をロック: FOR UPDATE NOWAIT
    -> ロック下でGeneration、状態、初期化トークンを再検証
    -> 外部依存関係を確認
    -> 各文のASTを検証して実行
    -> 返却行数とバイト予算を適用
    -> COMMIT
    -> Runtime接続を閉じる
```

最初のRegistry参照後にResetが発生する可能性があるため、Generationの検証はGo側だけでなくGatewayでも繰り返します。ロックはユーザーSQLの実行中からトランザクション終了まで保持されます。

### 同じIsletと異なるIsletへの操作

同じIsletに対するExecute、Batch、Reset、Reinitialize、Recover、Deleteが競合すると、1つの操作がロックを保持し、その他の操作は通常`ErrBusy`を受け取ります。`Open`は現在のメタデータを読み取るだけで、後続の実行用ロックを予約しません。

異なるIsletは別々のRegistry行を使用するため、独立して実行できます。ただしPostgreSQLのCPU、メモリ、I/O、接続容量は共有します。必要に応じてアプリケーション側で全体の同時実行数を制限してください。

### 内部Generationが必要な理由

ある操作がSQL実行の準備をしている間に、別のリクエストがResetを完了することがあります。ロックだけでは、以前の状態を前提に準備されたSQLがReset後のワークスペースで実行される可能性があります。Generation検証はそのような古い操作を拒否します。

初期化状態の更新時にもGenerationを検証し、以前の初期化に対する遅延した失敗処理が、新しい初期化の状態を上書きしないようにします。呼び出し元は値を直接管理せず、`ErrStaleGeneration`発生時に現在の状態を確認します。

### 接続とセッションの寿命

管理接続はプールを使用します。Runtime接続は操作ごとに開いて閉じます。ユーザー操作間で、開いたトランザクション、セッション設定、一時テーブルを維持しません。接続やトランザクションのオブジェクトを呼び出し元に公開することもありません。

呼び出し元が`BEGIN`と`COMMIT`を発行し、複数のAPI呼び出しを1つのトランザクションにまとめることはできません。複数の文を原子的に実行する場合はBatchを使用します。

## サポートするSQLと制限

SQLポリシーは、キーワードのフィルタリングだけでなくPostgreSQL ASTを検証します。PostgreSQLの権限検証も適用されるため、ポリシーを通過しても、すべてのオブジェクトへのアクセスが許可されるわけではありません。

### 主なサポート対象操作

| 分類               | 操作                                                   |
| ------------------ | ------------------------------------------------------ |
| クエリ             | SELECT、JOIN、CTE、集約、ウィンドウ関数                |
| データ変更         | INSERT、UPDATE、DELETE、MERGE                          |
| テーブル           | CREATE TABLE、許可されたALTER TABLE、DROP、TRUNCATE    |
| インデックス       | 通常のCREATE INDEX、DROP INDEX                         |
| ビュー             | VIEW、MATERIALIZED VIEW、REFRESH                       |
| 型                 | ENUM、DOMAIN、複合型、および許可された変更             |
| シーケンス         | 作成・変更、および許可されたシーケンス関数             |
| 関数とプロシージャ | ポリシーを満たすローカルな`LANGUAGE sql`定義と呼び出し |
| その他             | 許可されたオブジェクトのCOMMENT・名前変更、EXPLAIN     |

PostgreSQL 18では、仮想生成列、`RETURNING`のOLD/NEW値と別名、時間範囲制約（`WITHOUT OVERLAPS`と`PERIOD`）、`uuidv4`、`uuidv7`、`uuid_extract_version`、`uuid_extract_timestamp`もサポートします。使用するSQL機能は、接続先のPostgreSQLバージョンとSQLポリシーの両方でサポートされている必要があります。

すべての組み込み関数が許可されるわけではありません。ポリシーに登録された関数と、現在のIsletで許可されたローカル関数を使用できます。関数本体も検証され、PostgreSQL組み込み関数と名前が衝突するユーザー定義関数は制限されます。

サポートする文・オブジェクト・関数は、[ポリシー実装](../internal/policy/policy.go)と[ポリシーテスト](../internal/policy/policy_test.go)が基準です。たとえばトリガー関連のASTノードが許可されていても、参照する関数の言語や権限も要件を満たす必要があり、PostgreSQLのすべてのトリガー利用方法をサポートするわけではありません。

### 主な制限事項

- ロールやデータベースの作成・変更・削除、権限の付与・取り消しなどの管理操作
- ユーザーによるスキーマ管理とオブジェクト所有者の変更
- `SET`、`SET ROLE`、トランザクション制御などのセッション管理
- 一時リレーションとUnloggedリレーション（`ALTER TABLE ... SET UNLOGGED`と`ALTER SEQUENCE ... SET UNLOGGED`による変更を含む）
- Concurrent Indexの作成とTablespaceの指定
- ポリシー対象外の関数と、他の通常スキーマの関数
- `plpgsql`など、`LANGUAGE sql`以外のユーザー定義関数・プロシージャ
- 関数単位のセッション設定とサポート関数フック
- COPY、拡張のインストール、VACUUMなど、許可リストにない文

これは代表的な一覧であり、PostgreSQL構文全体の互換性表ではありません。PostgreSQLが受け付けるSQLでも、pgisletのポリシーで拒否される場合があります。

### 分離の境界

pgisletは、他のIsletのユーザーオブジェクトへのアクセスを分離することを目的とします。PostgreSQLカタログを完全に隠すものではありません。システムカタログを通じてオブジェクト名や一部のメタデータが見える場合があり、`current_database()`などの許可された関数も共有データベースの情報を返します。

実行時間・結果制限は、ユーザーごとのディスク容量、CPU割り当て、メモリ上限、接続数の割り当てを提供するものではありません。サーバー設定、インストール済み拡張、管理者が追加した権限も、全体のセキュリティ境界に影響します。別の特権プロセスが内部ロールやRegistryを任意に変更する状況を防ぐ設計ではありません。

## エラー処理

### errors.Isとerrors.As

文字列比較ではなく`errors.Is`でエラー分類を確認します。PostgreSQLのサーバーエラーは原因として保持されるため、`errors.As`でSQLSTATEや診断情報を取得できます。

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

この断片は、`errors`、`fmt`、`github.com/jackc/pgx/v5/pgconn`をインポートした関数内で使用します。クイックスタートの完全なプログラムを除き、このREADMEのGoコードはアプリケーションの文脈に組み込む部分例です。

### エラー一覧

| エラー                  | 意味と対応                                                                   |
| ----------------------- | ---------------------------------------------------------------------------- |
| `ErrNotFound`           | ワークスペースが存在しない。ユーザーとの対応関係や削除の有無を確認           |
| `ErrBusy`               | ロックを取得できない。進行中の操作を確認し、適切な場合のみ再試行             |
| `ErrUnavailable`        | 現在の状態または初期化トークンでは操作を許可できない                         |
| `ErrFailed`             | 失敗状態のワークスペースに通常実行を要求。ライフサイクル操作による復旧が必要 |
| `ErrStaleGeneration`    | ハンドルまたは準備済み操作が古い。Isletを再度開き現在の状態を確認            |
| `ErrPolicy`             | SQL解析またはポリシー検証の失敗                                              |
| `ErrTimeout`            | 実行期限または対応するPostgreSQLキャンセル条件に到達                         |
| `ErrResultLimit`        | 行数またはバイト予算を超過。操作はロールバック                               |
| `ErrSQLTooLarge`        | SQL合計サイズまたはバッチ文数が上限を超過                                    |
| `ErrRuntimeConnection`  | Runtime Roleの認証または接続に失敗                                           |
| `ErrInitialization`     | 初期化に失敗。返されたIDと現在のRegistry状態を確認                           |
| `ErrLifecycle`          | 作成・クリーンアップなどの管理操作に失敗                                     |
| `ErrQuery`              | PostgreSQL SQLエラー。SQLSTATEを確認                                         |
| `ErrOutcomeUnknown`     | COMMITエラーにより最終的なトランザクション結果が不明                         |
| `ErrUnsupported`        | PostgreSQLバージョン、内部構造、所有権、権限がサポート要件を満たさない       |
| `ErrExternalDependency` | クリーンアップが別のワークスペースや外部オブジェクトに影響する可能性がある   |

すべてのエラーがこれらの分類に変換されるわけではありません。設定解析エラーや一部のコンテキスト・接続エラーはそのまま返されるため、その他のエラーを処理する経路も用意してください。

`pgislet.Error`には`Kind`、`Cause`、`Statement`があります。文単位のポリシー・実行エラーでは、`Statement`は0始まりのバッチインデックスです。文に関連しないラップ済みエラーでは`-1`になります。すべての失敗に有効な文インデックスがあるとは限りません。

### 再試行時の考慮事項

ライブラリは失敗したユーザーSQLを自動再実行しません。特にCOMMIT中の接続障害では、PostgreSQLがコミットしたかをクライアント側で判断できないことがあります。

`ErrOutcomeUnknown`の後にINSERTを無条件で再実行すると、変更が重複する可能性があります。現在のデータを確認するか、アプリケーションレベルの冪等性保証を使用してください。古いハンドルを再取得して再試行する場合も、現在の状態がユーザーの意図に合っているかを先に判断します。

## アプリケーションへの組み込みと運用

### リクエスト処理

```text
ユーザーを認証
    -> アプリケーションの認可を確認
    -> ユーザーに割り当てられたIslet IDを取得
    -> Manager.Open
    -> ExecuteまたはBatch
    -> エラーを分類し、結果を変換
    -> ユーザーに応答
```

Islet IDは認可情報ではありません。クライアントが任意のIDを指定して他のユーザーのワークスペースにアクセスできないよう、アプリケーションが所有関係を検証します。Managerを呼び出せるサーバーコードは信頼された管理領域に属します。

### 複数のアプリケーションサーバー

同じ管理主体とデータベース設定を使用するサーバーは、それぞれManagerを作成し、同じIslet IDを開けます。共有RegistryとPostgreSQLロックにより、Islet単位の直列化に別の分散Mutexサービスは不要です。

ユーザーとIsletの対応関係を1台のサーバーのメモリだけに保存すると、他のサーバーから参照できません。対応関係と認証セッションの共有はアプリケーションの責任です。

### 終了と障害

進行中のリクエストを処理し終えてから`Manager.Close()`を呼び出します。Closeはユーザーワークスペースを削除・失効させません。操作ごとのRuntime接続は実行経路でクリーンアップされ、すべてのリクエストを強制キャンセルする別のShutdown APIはありません。

クライアントプロセスが終了すると、接続終了とPostgreSQLのトランザクション処理によりロックが解放されます。ネットワーク障害は即座に検出されないことがあるため、文のタイムアウトとアイドルトランザクションのタイムアウトも重要です。初期化段階の間で中断され、`initializing`のまま残ったワークスペースは、アプリケーションが状態を確認してRecoverできます。

### アプリケーション側で定義する運用方針

- ワークスペース数とユーザーごとの割り当て方針
- アイドルワークスペースの有効期限と削除ジョブ
- Runtime接続の同時実行数とリクエストのレート制限
- データベース容量の監視と長時間クエリの可観測性
- 管理DSNとRegistry認証情報のアクセス制御
- 初期化失敗、外部依存関係、トランザクション結果不明時の対応手順

RegistryにはRuntime認証に必要なパスワードが保存されます。公開Isletフィールドには露出しませんが、データベース管理者は参照できるため、管理データとバックアップを保護してください。

## サンプルアプリケーション

CLIとWebアプリケーションは、`examples/go.mod` の独立したGoモジュールにまとめられています。GinやBunなどの依存関係は、ライブラリのモジュールとは分離されています。`replace github.com/swualabs/pgislet => ..` により、同じチェックアウト内のライブラリを参照します。以下のコマンドはリポジトリのルートから実行してください。`go -C examples` はサンプルのモジュールを選択します。ルートでの `go test ./...` には、このネストされたモジュールは含まれません。

### CLI Playground

```sh
go -C examples run ./playground -container
```

一時的なPostgreSQL 18コンテナを起動し、次の動作を示します。

1. サンプルSQLによるワークスペースの初期化
2. 別のManagerから同じワークスペースを開いてクエリを実行
3. Runtime Roleによるサンプルテーブルの削除
4. Islet間のアクセスが拒否されることを確認
5. Resetで古いハンドルが無効になることを確認
6. 呼び出し元のSQLによる再初期化
7. Isletと一時コンテナのクリーンアップ

既存のデータベースを使用する場合:

```sh
PGISLET_DSN='postgres://postgres:password@localhost:5432/appdb?sslmode=disable' \
    go -C examples run ./playground
```

`-dsn`でDSNを指定することもできます。

### Web Playground

WebサンプルはGinとBunを使用し、アカウントとセッションを永続化します。アプリケーションデータ用とpgisletワークスペース専用の2つのPostgreSQLデータベースを使用します。

```sh
go -C examples run ./web -container
```

[http://localhost:8080](http://localhost:8080)でアカウントを作成し、ワークスペースを開きます。このコマンドは一時的なPostgreSQL 18コンテナを2つ作成し、停止時にデータも削除します。

データを永続化する場合は、サンプルのCompose構成を使用するか、両方の接続情報を指定します。

```sh
APP_DATABASE_URL='postgres://playground_app:password@localhost:5432/playground_app?sslmode=disable' \
PGISLET_DATABASE_URL='postgres://postgres:password@localhost:5433/playground_islets?sslmode=disable' \
    go -C examples run ./web
```

アカウント、セッショントークンのハッシュ、アカウントとIsletの対応はアプリケーションDBに保存します。ログアウトやHTTPサーバーの再起動でワークスペースは削除されません。UIは登録、ログイン、パスワード変更、SQL実行、スキーマ参照、ワークスペースのリセットに対応します。パスワード変更時はすべてのセッションを無効化します。

Origin検証、認証のレート制限、SQLの同時実行制限、マイグレーション、readinessチェック、正常終了処理を備えています。公開環境ではHTTPSのOriginと適切な信頼済みプロキシ設定が必要です。Composeの起動方法、設定、API、テスト、DB間の作成処理の復旧や認証機能の対応範囲については、[Web Playgroundガイド](../examples/web/README.md)を参照してください。

## テストとカバレッジ

### 単体テスト

```sh
go test ./internal/...
go -C examples test ./web/app
```

すべてのパッケージを確認しつつ、統合テストを明示的に無効化する場合:

```sh
PGISLET_UNIT_ONLY=1 go test ./...
PGISLET_UNIT_ONLY=1 go -C examples test ./...
```

これは統合テストをスキップするもので、成功を確認したことにはなりません。完全な検証とカバレッジ測定には、以下のDockerベースのテストを使用します。

### SQLポリシーのファジング

2つのファズターゲットを個別に実行します。Dockerは不要で、生成したSQLをPostgreSQLに対して実行することはありません。

```sh
go test ./internal/policy -run='^$' -fuzz='^FuzzPolicySQL$' -fuzztime=30s -parallel=2
go test ./internal/policy -run='^$' -fuzz='^FuzzPolicyExpressions$' -fuzztime=30s -parallel=2
```

`FuzzPolicySQL`は最大32 KiBの任意入力について、クラッシュ、判定の不一致、呼び出し元が渡した関数権限の変更を検証します。`FuzzPolicyExpressions`は構文上有効なSQLに許可対象と禁止対象の関数呼び出しを埋め込み、入れ子、コメント、スキーマ修飾、識別子の表記を変化させます。禁止対象にはセッション設定、通知、ファイルアクセス、動的SQL、外部スキーマ、Runtime Gatewayの呼び出しを含みます。正常な入力の許可も確認するため、すべて拒否する実装ではテストを通過できません。

CIでは各ターゲットを別ジョブで30秒間実行し、ログと失敗入力を保存します。Goは再現用の失敗入力を`internal/policy/testdata/fuzz/`に保存します。修正時には回帰テストとして保持してください。シード入力は通常の`go test`でも実行されます。

統合テストはPostgreSQL 17と18でポリシー拒否とロールバックを検証し、同じバッチで先行したDDL・DML、Registryのメタデータ、別のIsletへの影響も確認します。先頭・中間・末尾で拒否された場合の文番号を検証し、シーケンスを使って後続の文が実行されていないことも確認します。正常系ではバッチ内のローカル関数の作成・置換・削除・再作成と、関数変更のロールバックを検証します。初期化失敗は意図的にIsletを`failed`へ遷移させるため、別途検証します。

### PostgreSQL統合テスト

サーバーバージョンを明示して実行できます。

```sh
PGISLET_TEST_POSTGRES_MAJOR=17 go test ./tests/integration -count=1
PGISLET_TEST_POSTGRES_MAJOR=18 go test ./tests/integration -count=1
```

```sh
go test ./tests/integration -count=1
```

テストはTestcontainersでPostgreSQLを作成・終了します。デフォルトは18です。17を検証する場合は`PGISLET_TEST_POSTGRES_MAJOR=17`を指定します。CIは両バージョンで全テストを実行します。CIではPostgreSQL 17と18を別々のジョブで実行し、一方の失敗で他方をキャンセルしません。実際のサーバーメジャーバージョンとJSON形式のテスト結果を検証し、必須の統合テストが成功したことを確認します。17でのPostgreSQL 18専用テスト以外のスキップは失敗として扱います。ログとカバレッジはバージョン別の成果物として保存します。`main`へのpushとpull requestで実行され、`workflow_dispatch`による手動実行も可能です。指定された`PGISLET_DSN`で既存データベースを対象にする構造ではありません。デフォルトモードではDockerの準備に失敗するとテストも失敗します。

統合テストは、ユーザーオブジェクトの分離、Runtimeの認証主体検証、同一Isletでの競合、初期化、復旧、外部依存関係、結果制限、古いハンドルの拒否、プロセス終了などを検証します。

### 全体の検証

```sh
go vet ./...
go -C examples vet ./...
go -C examples test -race ./... -count=1
go test -race -coverpkg=.,./internal/... -coverprofile=coverage.out ./... -count=1
go tool cover -func=coverage.out
```

### Codecov設定

`codecov.yaml`では、プロジェクト全体と変更部分のカバレッジ目標をそれぞれ70%に設定しています。

| 除外パターン               | 理由                                                                |
| -------------------------- | ------------------------------------------------------------------- |
| `**/*_test.go`             | テストコード自体                                                    |
| `examples/**`              | ライブラリのカバレッジ対象外である実行サンプルとWebアプリケーション |
| `pgislet.go`               | 公開エイリアスと委譲関数                                            |
| `internal/engine/types.go` | 実行ロジックのないデータ宣言                                        |

エラー処理、SQLポリシー、結果処理、Bootstrap、ライフサイクルのロジックは除外しません。除外対象以外の実装ファイルには、同名の単体テストファイル、または`tests/integration`内の対応するテストファイルがあります。

## よくある質問

### ユーザーはサンプルテーブルを削除できますか？

はい。サンプルも呼び出し元がRuntime SQLで作成したユーザーオブジェクトです。復元する場合は、アプリケーションが初期化SQLをReinitializeに渡します。

### Resetは元の初期化SQLを再実行しますか？

いいえ。空のワークスペースを作成します。ライブラリは初期化SQLを保存しません。

### 新しいManagerを作成すると既存Isletは再初期化されますか？

いいえ。Bootstrapは内部構造と権限を準備しますが、ユーザーデータを削除しません。保存したIDをOpenに渡して既存Isletにアクセスできます。

### Generationが非公開なのに、関連エラーが公開されるのはなぜですか？

バージョン値はライブラリが管理しますが、古いハンドルを使用したことは呼び出し元が知る必要があります。現在の状態を再取得し、再試行すべきか判断してください。

### IsletをJSONに保存して復元し、実行に使用できますか？

公開情報はシリアライズできますが、内部の実行用ハンドル状態は保持されません。IDを保存し、Openで新しいハンドルを取得してください。

### 同じIsletで2つのSQL操作を同時に実行するとどうなりますか？

一方の操作がRegistryロックを保持している間、もう一方は通常`ErrBusy`を受け取ります。ライブラリは操作を自動的にキューへ追加したり再試行したりしません。

### サーバー障害後、初期化は自動復旧しますか？

初期化状態を監視するバックグラウンドタスクはありません。アプリケーションが現在の状態を確認し、Recoverまたは再初期化の方針を適用します。

### すべてのSQL文と拡張をサポートしますか？

いいえ。SQLはポリシーの許可リストとPostgreSQL権限の両方を満たす必要があります。拡張のインストールや任意の関数言語などの管理機能は、ユーザーSQLから提供しません。

### 1つのデータベースで無制限のユーザーを扱えますか？

いいえ。Isletごとにスキーマとロールが作成され、実行中の操作はRuntime接続を消費します。PostgreSQLの接続上限、カタログ規模、保存容量、サーバーリソースに応じて、アプリケーションがワークスペース数と同時実行数を管理してください。
