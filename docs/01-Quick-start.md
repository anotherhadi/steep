# Quick Start

```
steep [--collection dir] [--rate n] [--concurrency n] [--mode m] [--upstream url] [-c key=value]
```

Just type `steep`, paste a curl command and give it a name: it is saved in your
default [collection](./03-Collection.md). Press `ctrl+enter` to send the
request.

You can also just put `steep` before your curl command, with your options if you
want: steep starts on that request and asks you a name to save it under.

```sh
steep --rate 10 curl 'https://example.com/api/users' -H 'Authorization: Bearer eyJ...'
```

- You can move from pane to pane using `ctrl+h,j,k,l`.
- You can toggle a command palette containing all commands and shortcuts using
  `ctrl+p`.
- You can toggle the help menu using `?`.
- The mouse works too: click to select or focus, scroll with the wheel.

Then:

- [Configuration](./02-Configuration.md): steep is configured through a global
  config file, a collection config file, env variables or command line
  arguments.
- [Collections](./03-Collection.md): a collection is a folder where your
  requests and their responses are stored.
- [Request](./04-Requests.md): the request pane is the main area. You can craft
  a raw HTTP request, add wordlists, variables, encode things...
- [Results](./05-Results.md): the results pane is where all the responses of the
  run are listed. You can sort them by code, size, time, etc, and filter them
  using the HTTPQL syntax.
- [Response](./06-Response.md): the response pane shows the selected result. You
  can search through it, format it, or even give it to another program using
  pipes.
- [Advanced](./07-Advanced.md): zoom, screenshots, and a command to run at the
  end of each run.
- I'm a bug hunter and pentester myself, and I use steep quite a lot with some
  other TUI tools. If you want to learn more about my workflow and how I use
  steep, read it [here](./08-Workflow.md).
