# Response

The response pane shows the response of the hit selected in the
[results](./05-Results.md), as it was received: status line, headers, then the
body. On top, comment lines give the wordlist values that produced it:
`# input: admin`, or `# input 2: admin` with several wordlists.

You can move with `j`/`k`, `ctrl+d`/`ctrl+u` (half a page), `space`/`pgup` (a page down, up),
`g`/`G` (top, bottom), and `h`/`l` when lines are not wrapped.

## Display

- `p` formats the body, or shows it as received. JSON is indented and HTML is
  laid out (up to 4 MB). It's on by default, see `view.prettyPrint` in the
  [configuration](./02-Configuration.md).
- `w` wraps long lines or not (`view.wrap`).

## Searching

`/` searches the response, ignoring case. `n` and `N` go to the next and
previous match, and the pane shows where you are: `3/12`. `ctrl+g` clears the
search, and the pipe if there is one.

## Pipes

`|` runs the response through a shell command, and shows what it prints:

```
| rg -i cookie
| awk 'b; /^\r?$/{b=1}' | jq .data
| grep -oE 'https?://[^"]+' | sort -u
```

- The command gets the whole response on its standard input: status line,
  headers and body (decompressed). It runs with `sh -c`, from the directory you
  launched steep in.
- The pipe stays on when you select another hit, and runs again on the new
  response. For example, go through the results with
  `awk 'b; /^\r?$/{b=1}' | jq .user.role` on, and you only see the role change.
- `/` searches in what the pipe printed. `ctrl+g` drops both the search and the
  pipe.
- A command is stopped after 3 seconds, and its output is cut at 1 MB. If it
  fails, you see the first line it wrote on its error output.

`P` lets you pick one of the saved pipes, the `pipes:` of the configuration.

Your own `pipes:` replace this list, so copy the default ones you want to keep.
A command can take several lines with a YAML block:

```yaml
pipes:
    - name: Body
      command: awk 'b; /^\r?$/{b=1}'
    - name: Links
      command: |
          rg -o 'href="[^"]+"' |
            sort -u
```

## Getting the response out

- `c` copies the response, as received.
- `o` opens it in `$VISUAL` (or `$EDITOR`). It's a temporary file, deleted when
  you close the editor.

To copy only the body or the headers, or several responses at once, see
[exporting](./05-Results.md#exporting) in the results.
