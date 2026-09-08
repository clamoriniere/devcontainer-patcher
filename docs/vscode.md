# Using `dcp` with VS Code

## The constraint to understand first

`dcp up` wraps the [devcontainer CLI](https://github.com/devcontainers/cli) with
`--override-config`. **VS Code cannot be made to run it.** The Dev Containers
extension does not shell out to whatever `devcontainer` binary is on your
`PATH` — it drives its own copy of that implementation, and no setting
substitutes a different one. The settings that *look* like interception points
are not: `dev.containers.dockerPath` and `dev.containers.dockerComposePath`
swap the **container runtime** (docker → podman), long after config resolution
has happened.

So the integration is not "make VS Code call `dcp`". It is:

> **`dcp` writes the merged config to a path VS Code already looks at, and you
> point VS Code at it.**

That is what `dcp install` does. The rest of this document covers which mode to
use, and how to keep the generated file from going stale.

---

## Recommended: profile mode + the configuration picker

VS Code has supported multiple `devcontainer.json` files in sub-folders of
`.devcontainer` since **v1.75** (January 2023,
[vscode-remote-release#7548](https://github.com/microsoft/vscode-remote-release/issues/7548)).
Each sub-folder acts as a named configuration and is offered in a picker when
you reopen the folder in a container. `dcp install` targets exactly that.

### Setup

```sh
cd ~/code/some-project
dcp install                 # default: --mode profile --name local
```

This writes:

```
.devcontainer/
├── devcontainer.json          ← the repo's file, untouched
├── local/
│   └── devcontainer.json      ← generated, git-ignored
└── .dcp/
    └── state.json             ← bookkeeping, git-ignored
```

Both generated paths are added to `.git/info/exclude`, not `.gitignore`, so the
repository's own ignore rules are never modified and `git status` stays clean.

### Using it

1. **F1 → Dev Containers: Reopen in Container**
2. VS Code lists the available configurations. Pick the one labelled
   `<project> (local)`.

`dcp install` appends the profile name to the config's `name` for exactly this
reason — without it the picker would show two entries with identical labels.
The label it appends to is the base config's `"name"`, or the repository folder
name when the base config has none. If your overlay sets its own `"name"`, that
is used verbatim instead.

### What you get

Everything the merge produced: your Features, mounts, extensions, environment.
Verify before opening:

```sh
dcp render --explain
```

### Why this mode is the default

- Nothing tracked is modified, so no `skip-worktree`, no `git pull` hazard.
- The repo's original config stays available in the same picker, which makes
  "does this break without my overrides?" a one-click test.
- The devcontainer lockfile the CLI writes lands in `.devcontainer/local/`,
  which is git-ignored — your personal Features cannot leak into the repo's
  committed `devcontainer-lock.json`
  ([vscode-remote-release#11616](https://github.com/microsoft/vscode-remote-release/issues/11616)).

Relative `build.dockerfile`, `build.context` and `dockerComposeFile` paths are
re-anchored automatically for the deeper directory, so Dockerfile- and
Compose-based projects work unchanged.

---

## Alternative: in-place mode

```sh
dcp install --mode in-place
```

This replaces `.devcontainer/devcontainer.json` itself, keeping the original in
`.devcontainer/.dcp/base.json` and marking the tracked file `skip-worktree` so
git reports a clean tree.

Use it when:

- you want **no picker** — every "Reopen in Container" just works;
- a project script or CI-local task runs `devcontainer up` directly against the
  canonical path.

Zed also supports profile mode; see [docs/zed.md](zed.md) for the one extra
setting it needs.

The trade-off is real and worth repeating:

> **Run `dcp uninstall` before `git pull`.** Git refuses to update a
> `skip-worktree` file that has changed upstream. `dcp status` reports when the
> committed config has moved on and your generated file is stale.

---

## Keeping the generated config fresh

`dcp install` writes a **snapshot**. If the repo's `devcontainer.json` changes
(you pull, you switch branches) or you edit your overlay, re-run it. `dcp
status` tells you when that is needed:

```
install:
  mode:      profile
  generated: .devcontainer/local/devcontainer.json
  STALE: the committed devcontainer.json changed since install; re-run `dcp install`
```

Three ways to automate it, best first.

### 1. Git hooks (recommended — editor-agnostic)

Regenerate whenever the working tree's config could have changed. This covers
VS Code, Zed, and the terminal identically.

```sh
# .git/hooks/post-checkout, post-merge, and post-rewrite
#!/bin/sh
command -v dcp >/dev/null || exit 0
dcp install >/dev/null 2>&1 || true
```

```sh
cd ~/code/some-project
for hook in post-checkout post-merge post-rewrite; do
  printf '#!/bin/sh\ncommand -v dcp >/dev/null || exit 0\ndcp install >/dev/null 2>&1 || true\n' \
    > ".git/hooks/$hook"
  chmod +x ".git/hooks/$hook"
done
```

Hooks live in `.git/`, so this is per-clone and never committed. Use
`core.hooksPath` pointed at a shared directory if you want it everywhere.

> With `--mode in-place`, do **not** put this on `post-merge`: the pull that
> would trigger it is the operation `skip-worktree` blocks. Uninstall, pull,
> reinstall.

### 2. A VS Code task that runs on folder open

VS Code can run a task when a folder is opened, via
[`runOptions.runOn`](https://code.visualstudio.com/docs/debugtest/tasks). This
fires when you open the project **locally**, i.e. just before you reopen it in
a container.

```jsonc
// .vscode/tasks.json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "dcp install",
      "type": "shell",
      // No-op when dcp is absent — this task also runs inside the container,
      // where dcp is not installed.
      "command": "command -v dcp >/dev/null && dcp install || true",
      "presentation": { "reveal": "silent", "panel": "dedicated" },
      "runOptions": { "runOn": "folderOpen" }
    }
  ]
}
```

Then either accept VS Code's prompt or run **F1 → Tasks: Allow Automatic Tasks
in Folder** (governed by the `task.allowAutomaticTasks` setting).

Two caveats:

- `.vscode/tasks.json` is often committed. If it is, adding this would push
  your personal tooling onto teammates — keep it out of git by adding
  `.vscode/tasks.json` to `.git/info/exclude`, or use the git hook instead.
- The guard in `command` matters. The task re-runs on folder open *inside* the
  container too, where `dcp` will not exist.

### 3. `initializeCommand` in your overlay

`initializeCommand` runs on the **host** before the container is created, so it
can regenerate the config:

```jsonc
// ~/.config/devcontainer-patcher/local.json
{
  "initializeCommand": "dcp install"
}
```

Be clear about what this does and does not do: VS Code has already parsed the
configuration by the time `initializeCommand` runs, so **the refresh applies to
the next open, not the current one**. It is a self-healing backstop, not a
pre-processor. Prefer options 1 or 2; reach for this when you cannot install
hooks.

---

## When you don't need `dcp` at all

VS Code has two built-in user-level settings that cover the simplest cases. If
they are enough for you, use them — they need no generated files:

```jsonc
// VS Code User settings.json
{
  "dev.containers.defaultFeatures": {
    "ghcr.io/meaningful-ooo/devcontainer-features/fish:1": {}
  },
  "dev.containers.defaultExtensions": ["vscodevim.vim"]
}
```

They stop being enough when you need any of:

| Need | `defaultFeatures` / `defaultExtensions` | `dcp` |
| --- | --- | --- |
| Extra Features | yes | yes |
| Extra extensions | yes | yes |
| Mounts, `runArgs`, `remoteEnv`, `hostRequirements` | no | yes |
| Per-project or per-profile overrides | no | yes (`--profile`, `devcontainer.local.json`) |
| Works outside VS Code (Zed, CLI, CI) | no | yes |
| Inspect the result before building | no | `dcp render --explain` |
| Keeps personal Features out of the repo lockfile | [no](https://github.com/microsoft/vscode-remote-release/issues/11616) | yes |

The two compose fine — VS Code applies its defaults on top of whatever config
it loads, including a `dcp`-generated one.

---

## Codespaces

Profile mode is deliberately local-only: the generated config is git-ignored,
so a Codespace built from the repository will never see it. That is the correct
behaviour — your personal overrides should not ship to a cloud environment
built for the team. For Codespaces personalization, use
[dotfiles](https://docs.github.com/en/codespaces/setting-your-user-preferences/personalizing-github-codespaces-for-your-account)
or commit a config the whole team wants.

---

## Troubleshooting

**The picker doesn't appear / only shows one configuration.**
Confirm the file exists (`ls .devcontainer/local/devcontainer.json`) and that
you are on VS Code 1.75 or newer. Reload the window — the extension scans for
configurations when the folder is opened.

**The picker shows two entries with the same name.**
Your overlay set `"name"` to the same value as the repo's. Change it, or remove
`"name"` from the overlay and let `dcp install` append the profile suffix.

**Changes to my overlay aren't showing up.**
The generated config is a snapshot. Run `dcp install` again, then **Dev
Containers: Rebuild Container**. `dcp status` confirms whether the install is
stale.

**`git pull` fails with "local changes would be overwritten".**
You are in `--mode in-place`. Run `dcp uninstall`, pull, then `dcp install
--mode in-place` again.

**Build fails with a missing Dockerfile after `dcp install`.**
Profile mode re-anchors relative paths; report it if one was missed. Check what
was written:

```sh
grep -E '"(dockerfile|context|dockerComposeFile)"' .devcontainer/local/devcontainer.json
```

**I want to check what VS Code will actually build.**
`dcp render` prints the same document that gets installed (minus the profile
name suffix and path re-anchoring), and `dcp render --explain` shows which
layer contributed each property.
