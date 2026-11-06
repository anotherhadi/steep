# Request

A request is a raw HTTP request, like the ones Burp or Caido show you:

```http
POST /api/login HTTP/1.1
Host: example.com
Content-Type: application/json

{"user":"list{admin,root}","password":"file{$w/passwords}"}
```

- Line ends can be `\n` or `\r\n`, and you can leave out the HTTP version.
- `HTTP/2` as the version sends the request over HTTP/2. Only over https, and if
  the server does not offer HTTP/2, the request goes over HTTP/1.1 without a
  word.
- If the target has no scheme, the request goes to the `Host` header, over
  `proto` (https by default).
- Every change is written to disk, you never have to save.
- The editor works like vim. `U`, `H` and `B` jump to the path, the headers and
  the body.

To start from a curl command, paste it or launch `steep curl ...`, see
[Quick start](./01-Quick-start.md).

## Settings

The `# steep key: value` lines at the top of the file are the settings of this
request. You can edit them with a form using ctrl+o:

```http
# steep id: 3f9a1c
# steep match.status: 200-299,403
# steep rateLimit: 20
GET /file{$w/common} HTTP/1.1
Host: example.com
```

They override the `request:` section of the
[configuration](./02-Configuration.md). If a value is invalid, it's ignored and
shown as a problem of the request.

You don't touch `id`, steep sets it: it's the name of the `runs/<id>/` folder
where the responses of the request are kept (see
[Collection](./03-Collection.md)).

## Wordlists

A wordlist sends one request per value.

- `file{path}`: each line of a file. Press `ctrl+t` to pick it with `filePicker`
  (fzf by default), starting in `pickerDir`.
- `list{admin,root}`: each comma-separated value. Write `\,` for a comma inside
  a value.
- `range{1-500}`: each number. `range{001-500}` pads with zeros. 10 000 000
  values max.

In a `file{}` path:

- `$w/common` uses the shortcut `w` from the `shortcuts` setting
- `~/` is your home
- a relative path starts from the directory you launched steep in, not from the
  collection
- without an extension, steep uses `x.txt` if it's the only one that exists

With several wordlists, `mode` decides how they combine:

- `clusterbomb` tries every combination: 3 users and 100 passwords make 300
  requests
- `pitchfork` takes them side by side: the 1st user with the 1st password, the
  2nd with the 2nd, and so on until the longest one ends

## Operations

Operations come from [operations](https://github.com/sectui/operations):
encodings, hashes, JWT, case... Press ctrl+e to insert one at the cursor, or to
wrap the visual selection in it.

```
Authorization: Basic to-base64{admin:file{$w/passwords}}
X-Token: to-hex(delimiter=colon){hi}
X-Both: to-base64|url-encode{x}
```

- Blocks can be nested, take arguments in parentheses, and can be chained with
  `|`, from left to right.
- An operation with no wordlist inside is computed once per run. With a wordlist
  inside, it's computed for each request: above, each password is encoded
  together with `admin:`.
- Only known names at the start of a word are blocks, so your JSON stays as it
  is. Inside a block, braces must be balanced or escaped (`\{`, `\}`), and `\\`
  is a backslash.

## Variables

`env{name}` is the value of `env.name` in the configuration, or else of the
`$name` environment variable. If neither exists, the run fails.

It's a good place for your tokens, instead of the `.http` files that you may
share or put in git:

```yaml
# ~/.config/steep/config.yaml
env:
    token: eyJ...
```

```http
Authorization: Bearer env{token}
```

## Sending

Under the editor, steep shows how many requests the run will send, and about how
long it will take if you set a `rateLimit`: `30 000 req · ~25m`. A `+` means
recursion may add more.

`s`, `ctrl+enter` or `ctrl+n` starts the run, ctrl+x stops it, ctrl+f pauses it.
If the run has more than `confirmAbove` requests (10 000 by default), steep asks
you first.

steep also changes every request a bit:

- it adds the `http.headers` of the configuration
- it adds `http.userAgent`, if the request has no `User-Agent`
- it adds `http.userAgentSuffix` at the end of the `User-Agent` (useful for bug
  bounty programs)
- it removes `Content-Length`: ffuf computes it for each request, so you can
  fuzz the body without worrying about it
- it removes `X-PwnFox-Color`: the header is a tag for you, the target never
  sees it (see [Collection](03-Collection.md#pwnfox-colors)). Set
  `http.stripPwnFox: false` to keep it, for a proxy behind steep that reads it
