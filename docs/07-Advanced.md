# Advanced

## Zoom

You can zoom (maximize) the focused pane using `alt+z`. Press it again to go
back.

## Screenshots

`ctrl+s` takes a screenshot of the open request and its response, side by side,
under a bar with the name of the request. The collection, the results and the
help bar are left out, so it's ready to go in a report.

It's saved as `steep_<request>_<date>_<time>.png` in
`$XDG_PICTURES_DIR/screenshots`. You can change that in the `screenshot:`
section of the [configuration](./02-Configuration.md):

```yaml
screenshot:
    dir: shots # relative to the collection
    format: svg # png or svg
    font: "JetBrainsMono Nerd Font"
    onCapture: wl-copy < "$SCREENSHOT"
```

`onCapture` runs after each screenshot, with its path in `$SCREENSHOT`. Above,
it copies the image to the clipboard (Wayland).

## At the end of a run

`onRunEnd` runs a command when a run finishes or is stopped. It runs with
`sh -c`, from the directory you launched steep in, and gets these variables:

| Variable      | Value                                  |
| ------------- | -------------------------------------- |
| `RUN_REQUEST` | the name of the request                |
| `RUN_STATUS`  | `finished` or `stopped`                |
| `RUN_HITS`    | hits found                             |
| `RUN_DONE`    | requests sent                          |
| `RUN_TOTAL`   | requests planned                       |
| `RUN_ERRORS`  | requests that failed                   |
| `RUN_URL`     | the target, with the wordlists in it   |
| `RUN_DIR`     | the folder holding the run's responses |

For example, a desktop notification, so you can leave a long run in the
background:

```yaml
onRunEnd: notify-send "steep: $RUN_REQUEST $RUN_STATUS" "$RUN_HITS hits, $RUN_DONE/$RUN_TOTAL sent"
```
