# Results

The results pane lists the hits of the last run of the open request. They are
kept in `runs/<id>/` (see [Collection](./03-Collection.md)), so when you reopen
the request, they are still there.

Keep in mind that the pane only has what the `match.*` and `filter.*` settings
let through during the run (see [Request](./04-Requests.md#settings)). A filter
here can't bring back what they dropped.

During a run, a progress line shows `done/total`, the requests per second and
the errors.

- `enter` opens the response of the hit in the response pane.
- The pane hides itself when there's nothing to compare: one hit, no filter and
  no run going.
- `o` sorts by the next column, `O` reverses the order.

## Filtering

`/` filters the results with
[HTTPQL](https://docs.caido.io/reference/httpql.html), Caido's query language.

A clause looks like `namespace.field.operator:value`. You can combine clauses
with `AND`, `OR` and parentheses:

```
resp.code.eq:200
resp.code.gte:500 AND req.path.cont:"admin"
resp.len.gt:1000 OR (resp.code.eq:200 AND req.ext.eq:".php")
"flag{"
```

A string on its own is searched in the raw request and the raw response.

Learn more about [HTTPQL in steep here](./10-HTTPQL.md)

## Saved filters and history

`ctrl+r` lets you pick one of your saved filters (`filters:` in the
[configuration](./02-Configuration.md)), or a recent one.

Inside a query, `preset:"name"` stands for a saved filter, and you can combine
it with others:

```
preset:"errors" OR preset:"denied"
preset:"marked" AND resp.len.gt:500
```

While you type a filter, `↑` and `↓` go through the history. It keeps the last
200 filters, in `$XDG_STATE_HOME/steep/filters`
(`~/.local/state/steep/filters`).

## Marking

`m` marks the hit (or unmarks it), then moves down. Marks are kept with the run.
`row.marked.eq:true` (the `marked` saved filter) shows only the marked hits. So
you can go through the results, mark the interesting ones, then export only
those, or forget the others (see [Forgetting](#forgetting)).

## Exporting

`y` copies the selected hit, `Y` copies every hit shown (so after the filter).
Both ask you what to copy:

| Key | Copies                         |
| --- | ------------------------------ |
| `c` | the request, as a curl command |
| `h` | the request, as raw HTTP       |
| `r` | the response                   |
| `b` | the response body              |
| `e` | the response headers           |
| `u` | the URL                        |
| `i` | the wordlist values            |

With `Y`, URLs and values are one per line. The rest is copied hit by hit, each
one under a comment like `# #12 · 200 · admin`. Above 1 MB, steep asks you
first, because terminals and tmux may cut a copy that big.

- `f` copies the path of the response file of the hit, `F` the path of the run
  folder. Useful to open them with other tools.
- `e` saves the request of the hit as a new request, as it was sent (with the
  wordlist values in it), and with the settings of the open request. That's how
  you go on from one specific hit.

## Forgetting

`x` forgets the selected hit, at once, and the selection goes to the next one.

`X` forgets the hits shown, so only what the filter lets through, once you
confirm. Without a filter, that's every hit of the request. With the `unmarked`
saved filter, mark what you want to keep, then `X` forgets everything else. It
is off while the filter is being computed or is invalid.

The other hits keep their number and their mark. Neither works while the run is
going.

The `forget-collection` command forgets every response stored in the
collection, once you confirm. It has no key, you find it in the palette
(ctrl+p).
