# HTTPQL in Steep

- [HTTPQL](https://docs.caido.io/reference/httpql.html): Caido's query language.
- `ctrl+g` clears the filter.

A clause looks like `namespace.field.operator:value`. You can combine clauses
with `AND`, `OR` and parentheses:

```
resp.code.eq:200
resp.code.gte:500 AND req.path.cont:"admin"
resp.len.gt:1000 OR (resp.code.eq:200 AND req.ext.eq:".php")
"flag{"
```

A string on its own is searched in the raw request and the raw response.

| Field            | Type    | What it is                                  |
| ---------------- | ------- | ------------------------------------------- |
| `req.method`     | text    | `GET`, `POST`...                            |
| `req.host`       | text    | host, without the port                      |
| `req.port`       | number  | 80 or 443 when the URL has none             |
| `req.path`       | text    | path, without the query                     |
| `req.query`      | text    | query string, without the `?`               |
| `req.ext`        | text    | extension of the path, with its dot: `.php` |
| `req.tls`        | boolean | whether the request went over https         |
| `req.raw`        | text    | the request as sent                         |
| `req.len`        | number  | size of the request as sent                 |
| `resp.code`      | number  | status code                                 |
| `resp.len`       | number  | size of the response body                   |
| `resp.roundtrip` | number  | round trip, in milliseconds                 |
| `resp.raw`       | text    | the response as received                    |
| `row.id`         | number  | the number of the hit                       |
| `row.marked`     | boolean | whether the hit is marked (`m`)            |
| `fuzz.input`     | text    | a value of the wordlists of the hit         |
| `fuzz.words`     | number  | the word count of the response              |
| `fuzz.lines`     | number  | the line count of the response              |

| Type    | Operators                                                       |
| ------- | --------------------------------------------------------------- |
| number  | `eq`, `ne`, `gt`, `gte`, `lt`, `lte`                            |
| text    | `eq`, `ne`, `cont`, `ncont`, `like`, `nlike`, `regex`, `nregex` |
| boolean | `eq`, `ne`                                                      |

- `like` works like in SQL: `%` is any run of characters, `_` is one character.
- With several wordlists, `fuzz.input.cont:x` is true when one of the values
  contains `x`, and `fuzz.input.ncont:x` when none of them does.
- The `fuzz.*` fields don't exist in Caido, they are steep's own. And Caido's
  `req.created_at` and `source` are refused, since every hit comes from the same
  run.
- A filter on `req.raw`, `req.len` or `resp.raw` has to read the responses on
  disk, so it's computed in the background.

Some examples:

```
resp.code.eq:200 AND resp.raw.cont:"password"
resp.code.ne:404 AND resp.len.ne:1234
req.ext.eq:".bak" OR req.ext.eq:".old"
resp.raw.regex:"AKIA[0-9A-Z]{16}"
```
