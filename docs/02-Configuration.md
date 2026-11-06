# Configuration

You can find the default configuration here:
[default.yaml](../internal/config/default.yaml). Don't copy all of it, only put
what you change in your own file.

steep reads its config from `~/.config/steep/config.yaml` on Linux
(`$XDG_CONFIG_HOME/steep/config.yaml` if it's set) and from
`~/Library/Application Support/steep/config.yaml` on macOS. It never writes it,
you have to create it yourself.

There are several layers, and the later wins:

- the built-in defaults: foam's, then steep's
  [default.yaml](../internal/config/default.yaml)
- `~/.config/sectui/config.yaml`, shared by every sectui tool. Some settings not
  related to steep (nerd fonts, colorscheme, basic shortcuts) come from
  [foam](https://github.com/sectui/foam)
- `~/.config/steep/config.yaml`, steep config on all collections
- `<collection>/config.yaml`, only for the collection being opened
- `STEEP_*` environment variables, e.g. `STEEP_REQUEST__CONCURRENCY=10`
- command-line flags, e.g. `--rate 50` or `-c request.concurrency=40`

Lists (`filters`, `pipes`, `http.headers`) replace the list of the layer below,
they are not added to it. So if you add a pipe, copy the default ones you want
to keep. Maps (`shortcuts`, `env`, `keys`) are merged key by key.

The `request` section only holds defaults: each request can override them in its
`# steep` block (`ctrl+o`, see [Request](./04-Requests.md#settings)), and this
block wins over every layer. So `--rate 50` does nothing on a request that sets
its own `rateLimit`.

## Example

A `~/.config/steep/config.yaml` for bug bounty can look like this:

```yaml
shortcuts: { w: ~/wordlists } # file{$w/common}
http:
    headers: ["X-Bug-Bounty: hadi"]
request:
    upstream: http://127.0.0.1:8080
    rateLimit: 20
```

## Environment variables

- They start with `STEEP_`, and `__` goes one level down:
  `STEEP_REQUEST__TIMEOUT=5` sets `request.timeout`.
- Case and `_` don't matter: `STEEP_REQUEST__RATE_LIMIT` sets
  `request.rateLimit`.
- Lists are comma-separated: `STEEP_HTTP__HEADERS="X-A: 1,X-B: 2"`.
- You can add keys to a map: `STEEP_ENV__TOKEN=eyJ...` sets `env.token`.

For example, you can launch:

```sh
export STEEP_COLLECTION="./collection"
steep
```

And all requests and responses will be stored in `./collection`. You could also
launch `steep --collection "./collection"` to do the same thing.

## Flags

| Flag                    | Sets                                        |
| ----------------------- | ------------------------------------------- |
| `--collection dir`      | `collection`                                |
| `--mode m`              | `request.mode`: clusterbomb or pitchfork    |
| `--rate n`              | `request.rateLimit`, requests per second    |
| `--concurrency n`       | `request.concurrency`                       |
| `--upstream url`        | `request.upstream`, http:// or socks5://    |
| `-c`, `--set key=value` | any key, e.g. `-c http.userAgentSuffix=me`  |
| `--print-config`        | prints the merged config and the files read |
| `-h`, `--help`          |                                             |
| `-v`, `--version`       |                                             |

You can repeat `-c`, and lists are comma-separated. Unlike with environment
variables, an unknown key gives an error, and so does a whole section
(`-c request=...`).

If a setting doesn't seem to apply, run `steep --print-config`: its first line
lists the config files steep actually loaded.

## Keys

In `keys`, each command ID takes a key, a list of keys, or `[]` for none (the
command is still in the palette, ctrl+p):

```yaml
keys:
    app:
        quit: q
    run:
        start: [s, ctrl+enter]
    results:
        clear-filter: []
```

The editor's vim keys can't be changed.
