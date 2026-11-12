```
█▀▀ ▀█▀ █▀▀ █▀▀ █▀█
▄▄█  █  ██▄ ██▄ █▀▀
```

# Steep

> A TUI to **edit, replay and fuzz raw HTTP requests**, with live results, HTTPQL
> filtering and built-in encodings, powered by **ffuf**.

Write a raw request, put `file{wordlist}` blocks where the values go, run it:
**the hits land as they come**, and you read, filter
([HTTPQL](https://docs.caido.io/reference/httpql.html)), search and export them
**without leaving the terminal**.

Steep replaces both the **intruder** and the **repeater** of Burp Suite (or
Caido). It pairs well with [Spilltea](https://github.com/anotherhadi/spilltea), which
handles the proxy side: it intercepts requests, stores them, and lets you send
them to steep to replay them.

Steep is made for **pentesters, bug bounty hunters and CTF enjoyers**!

<img alt="demo" src="./.github/assets/demo.gif" width="700" />

## Installation

With go:

```sh
go install github.com/anotherhadi/steep/cmd/steep@latest
```

With Nix, from the [anotherhadi NUR](https://github.com/anotherhadi/nur-packages):

```sh
nix run github:anotherhadi/nur-packages#steep
```

```nix
# flake: inputs.nur-anotherhadi.url = "github:anotherhadi/nur-packages";
environment.systemPackages = [ nur-anotherhadi.packages.${system}.steep ];

# or through NUR
environment.systemPackages = [ pkgs.nur.repos.anotherhadi.steep ];
```

Each [GitHub release](https://github.com/anotherhadi/steep/releases) has an archive
per platform (Linux, macOS and FreeBSD, amd64 and arm64) with the `steep`
binary:

```sh
tar xzf steep_1.2.3_linux_amd64.tar.gz
sudo install steep /usr/local/bin/
steep --version   # 1.2.3
```

## Quick start

Just type `steep`, paste a curl command and give it a name: it is saved in your
default collection (collections are the folders where requests and responses are
stored). Press `ctrl+enter` to send the request.

To fuzz, add `file{~/my/path/to/wordlist.txt}`: each line of the file is one
request. You can also use `list{admin,root}`, `range{1-500}`, `env{MY_ENV_VAR}`,
and even the [anotherhadi/operations](https://github.com/anotherhadi/operations) syntax,
like `to-base64{my payload}` or `url-encode{my payload}`. An operation around a
wordlist is computed for each request:

```http
Authorization: Basic to-base64{admin:list{1234,password,admin}}
```

sends three requests, with `admin:1234`, `admin:password` and `admin:admin`
encoded in base64.

When sending multiple requests, you can filter the results with the
[HTTPQL](https://docs.caido.io/reference/httpql.html) syntax. In the response
pane, you can search for words (`/`), or pipe the response into another shell
command (`|`).

Everything works with the keyboard (`?` shows the shortcuts, `ctrl+p` the
command palette), and with the mouse too.

There are plenty of other features, learn more in
[`docs/`](./docs/01-Quick-start.md)!
