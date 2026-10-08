# scripts/

Small helper scripts for working with this repo. Not part of the app
itself — nothing here is built or shipped, they're just dev conveniences.

## git-log.sh

Dumps the commit history in a readable form, oldest first (same order as
`docs/DEVLOG.md`, so the two read side by side easily).

```bash
scripts/git-log.sh          # hash, date, commit message
scripts/git-log.sh --stat   # + which files changed, +/- line counts
scripts/git-log.sh --full   # + the full diff, every line changed
```

**Exiting the output:** if there's more output than fits on screen, Git
pipes it through `less` (a pager) automatically — you'll see a `:`
prompt at the bottom instead of your normal shell prompt, and typing
won't do anything until you deal with it. To get back to the shell:

- Press **`q`** to quit the pager.
- While inside it, `space` pages down, `b` pages up, `/text` searches.
- `Ctrl+C` also gets you out if `q` doesn't seem to respond.

To skip the pager entirely (e.g. piping into another command, or a
script), either pipe the output yourself:

```bash
scripts/git-log.sh | head -50
scripts/git-log.sh | less    # explicit, same result as the default
```

or disable Git's pager for one call:

```bash
GIT_PAGER=cat scripts/git-log.sh
```

## nook-monitor.sh

A live-refreshing terminal dashboard for the **Docker** side of this
box — daemon status, containers (running/stopped, CPU/memory per
container), networks, volumes, published ports, host CPU/RAM, and the
last log line from each running container. Redraws every 2 seconds until
stopped.

```bash
scripts/nook-monitor.sh
```

Stop it with **`Ctrl+C`** — it traps that signal to restore the cursor
and clear the screen on exit rather than just dying mid-redraw.

Nothing to configure; the refresh interval is the `REFRESH` variable near
the top of the script if you want it faster/slower.

Note: this watches **Docker containers** specifically. Since the actual
Nook dev servers (`backend` via `go run`, `frontend` via `npm run dev`)
currently run natively, not in Docker, the containers section will show
empty during normal Nook development — it's most useful when something
Docker-based is actually running (e.g. while still evaluating
AFFiNE/AppFlowy-Cloud, or once Nook itself gets a Docker deployment path
in Phase 6).
