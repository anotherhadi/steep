# Collection

A collection is a folder where your requests and their responses are stored. The
default collection is in `$XDG_DATA_HOME/steep/` if it's set, else in
`~/.local/share/steep/`.

When the collection pane is focused, you can create a new request (`n`), create
a folder, delete a request, search (`/`) or duplicate a request. If you name a
new request `myfolder/abc`, the folder `myfolder` is created too.

You can toggle the collection pane using `ctrl+b`.

## On disk

```
<collection>/
├── config.yaml        # optional, the collection's own settings
├── requests/          # one .http file per request, in folders
│   ├── login.http
│   └── api/
│       └── users.http
└── runs/
    └── <id>/          # the responses of the request's runs
```

## Another collection

You can open another collection with `--collection dir`, `STEEP_COLLECTION=dir`
or `collection: dir` in a config file. If it has a `config.yaml`, it's read on
top of yours, see [Configuration](./02-Configuration.md).

A collection that does not exist yet is created. Set `createCollection: false`
to get an error instead, so a typo in the path does not open a new empty
collection.

## Editing outside steep

Requests are plain files, so anything can change them: your editor, a
`git pull`, a `cp`, or an AI agent rewriting your request. steep watches
`requests/` and follows:

- the collection pane is updated
- the open request is reloaded, unless you have unsaved edits: then your edits
  are kept, and they win at the next save
- if the open request is deleted, it's closed

## PwnFox colors

A request with an `X-PwnFox-Color` header, as the
[PwnFox](https://github.com/yeswehack/PwnFox) containers send it, gets a dot of
its color in the collection, and at the end of the request pane's title. Keep
one container per account (admin in red, user in blue...) and you see at a
glance which session a request uses. steep removes the header before sending,
unless `http.stripPwnFox` is `false`: keep it when the proxy behind steep
(Spilltea, Burp, ...) colors its history with it too.
