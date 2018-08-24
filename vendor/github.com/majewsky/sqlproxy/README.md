# sqlproxy

[![GoDoc](https://godoc.org/github.com/majewsky/sqlproxy?status.svg)](https://godoc.org/github.com/majewsky/sqlproxy)

This is a driver for the standard library's [`database/sql` package][go-sql]
that passes SQL statements through to another driver, but adds hooks to extend
the standard library's API.

## Example

Augment your existing database driver with statement logging:

```go
import "github.com/majewsky/sqlproxy"

...

sql.Register("postgres-with-logging", &sqlproxy.Driver {
    ProxiedDriverName: "postgresql",
    BeforeQueryHook: func(query string, args[]interface{}) {
        log.Printf("SQL: %s %#v", query, args)
    },
})
```

As always, `sql.Register()` may only be called once per driver name, so put
this in `func init()` or a `sync.Once`.

## Caveats

**Do not use this code on production databases.** This package is intended for
development purposes only, and access to it should remain behind a debugging
switch. It only implements the bare necessities of the database/sql driver
interface and hides optimizations and advanced features of the proxied SQL
driver.

[go-sql]: https://golang.org/pkg/database/sql/
