# devcontainer-patcher

`dcp` layers your personal dev container overrides on top of a repository's
committed `.devcontainer/devcontainer.json` — extra Features, mounts,
extensions, environment — **without modifying the file tracked in git**.

The dev container spec has no user-level override mechanism.
[spec#305](https://github.com/devcontainers/spec/issues/305) proposed one in
2023 and is still open; `--override-config` replaces a config rather than
merging into it, and `--additional-features` only covers Features. This fills
that gap.

## Install

```sh
go install github.com/clamoriniere/devcontainer-patcher@latest
```

Requires the [devcontainer CLI](https://github.com/devcontainers/cli) on `PATH`
for the `up`, `build` and `exec` wrappers (`npm install -g @devcontainers/cli`).

## Quick start

Write your personal overlay — a **partial devcontainer.json**, not a patch
format, so you can copy fragments straight out of any real config:

```jsonc
// ~/.config/dcp/local.json
{
  "features": {
    "ghcr.io/meaningful-ooo/devcontainer-features/fish:1": {}
  },
  "mounts": [
    // ${dcp:remoteUser} is filled in from the merged config (see Placeholders).
    "source=${localEnv:HOME}/.zsh_history,target=/home/${dcp:remoteUser}/.zsh_history,type=bind"
  ],
  "customizations": {
    "vscode": { "extensions": ["vscodevim.vim"] }
  },
  // Object form merges by key, so the repo's own command survives.
  "postCreateCommand": { "dotfiles": "~/dotfiles/install.sh" }
}
```

Then, in any repository:

```sh
dcp render            # print the merged config
dcp render --explain  # show which layer set each property
dcp up                # devcontainer up, with your overrides applied
dcp install           # write the merged config where your editor will find it
```

## Layers

Applied lowest to highest; every layer is optional except the repo's own.

| Layer | Source |
| --- | --- |
| `repo` | `<workspace>/.devcontainer/devcontainer.json` (or `.devcontainer.json`) |
| `user` | `<config>/local.json` |
| `profile` | `<config>/profiles/<name>.json` — selected with `--profile` |
| `repo-local` | `<workspace>/.devcontainer/devcontainer.local.json` |
| `flags` | `--feature` / `--mount` / `--set` |

`<config>` is `~/.config/dcp` on every platform (override with
`DCP_CONFIG_DIR`). `--no-user` skips the user and profile layers, which
reproduces exactly what CI would build.

## Merge semantics

The merge deliberately mirrors the [spec's own metadata merge
logic](https://containers.dev/implementors/spec/#merge-logic), so the result is
unsurprising if you already know how Features merge into a `devcontainer.json`.
Each overlay is "considered last", as the spec says of `devcontainer.json`
relative to image metadata.

| Property | Behaviour |
| --- | --- |
| `capAdd`, `securityOpt`, `forwardPorts`, `overrideFeatureInstallOrder` | union, duplicates removed |
| `mounts` | appended; a mount with an existing **target** replaces it |
| `init`, `privileged` | logical OR |
| `waitFor`, `remoteUser`, `containerUser`, `shutdownAction`, … | last value wins |
| `hostRequirements` | max wins, per sub-property (`"16gb"` beats `"8gb"`) |
| lifecycle commands | collected into object form so all of them run |
| `features` | deep merged; a Feature in both layers keeps the repo's options with yours winning per key |
| `customizations` | deep merged; `*.extensions` union |
| anything else | objects deep merge, arrays append, scalars replace |

That last row matters: the merge runs over generic JSON rather than a typed
schema, so properties the spec does not define — Zed's `use_podman`, a future
spec addition, a tool's private namespace — pass through untouched instead of
being silently dropped.

`devcontainer.json` is JSONC, so comments and trailing commas are accepted in
every layer.

### Overriding the default rule

Add `$strategy`, keyed by dotted path. Values: `replace`, `append`, `prepend`,
`union`, `remove`, `merge`.

```jsonc
{
  "$strategy": {
    "postCreateCommand": "replace",
    "forwardPorts": "remove",
    "customizations.vscode.extensions": "prepend"
  },
  "postCreateCommand": "make setup-local",
  "forwardPorts": [9229],
  "customizations": { "vscode": { "extensions": ["vscodevim.vim"] } }
}
```

A `null` value deletes the property outright, following the JSON Merge Patch
convention:

```jsonc
{ "postStartCommand": null }
```

### Placeholders

The dev container spec's own variables (`${localWorkspaceFolder}`,
`${localEnv:HOME}`, `${containerWorkspaceFolder}`, `${devcontainerId}`, …) work
in every layer — `dcp` never touches `${...}`, it stays for the devcontainer CLI
to resolve. What the spec has **no** variable for is the config's own resolved
`remoteUser` / `containerUser`. `dcp` fills that gap with `${dcp:name}`.

```jsonc
{
  "mounts": [
    // spec's ${localEnv:HOME} + dcp's ${dcp:remoteUser}, side by side
    "source=${localEnv:HOME}/.claude,target=/home/${dcp:remoteUser}/.claude,type=bind"
  ]
}
```

`${dcp:...}` placeholders are resolved **after** the merge — so a value set by
one layer can be referenced from another — and every one is removed before the
config is written or handed to the devcontainer CLI. Use `${dcp:name:fallback}`
for a literal fallback (the spec's own `${localEnv:VAR:default}` shape). An
unresolved placeholder with no fallback is an error naming it and where it
appears.

Built-in names (matched case-insensitively):

| Name | Value |
| --- | --- |
| `remoteUser` | the merged `remoteUser` (error if no layer sets it and there is no fallback) |
| `containerUser` | the merged `containerUser` |

Define your own with `$vars` in any overlay — later layers override earlier
keys, and a `$vars` entry shadows a built-in of the same name:

```jsonc
{
  "$vars": { "claudeCache": "${localWorkspaceFolder}/.cache/claude" },
  "mounts": ["source=${dcp:claudeCache},target=/home/${dcp:remoteUser}/.cache/claude,type=bind"]
}
```

Resolution is a single pass: a value that itself resolves to another
`${dcp:...}` is left as written. Like `$strategy`, the `$vars` key is stripped
before the config is written.

After the placeholders resolve, `dcp` re-checks `mounts` for entries that now
share a `target` and keeps the last one, with a warning — so parametrising a
repo's mount target with `${dcp:remoteUser}` **replaces** that mount instead of
adding a second bind at the same path.

### A note on lifecycle commands

A single `devcontainer.json` field has only one representation for "run more
than one thing": the object form. So combining a repo's
`"postCreateCommand": "go mod download"` with yours produces

```jsonc
"postCreateCommand": { "repo": "go mod download", "user": "~/dotfiles/install.sh" }
```

Object-form entries run **in parallel**, whereas string form runs on its own.
`dcp` warns when it makes this conversion. Write your own commands in object
form to avoid it entirely, or use `$strategy: {"postCreateCommand": "replace"}`.

## Commands

### `dcp render`

Merges and prints. Touches nothing. `-o FILE` writes to a file; `--explain`
reports provenance per property instead of the config:

```
customizations.vscode.extensions                     repo -> user
features.ghcr.io/devcontainers/features/go:1.version repo
features.ghcr.io/.../fish:1                          user
mounts                                               user
```

### `dcp up` / `dcp build` / `dcp exec`

Render to a temporary file, then run the devcontainer CLI with
`--override-config`. Everything after `--` is passed through:

```sh
dcp up -- --remove-existing-container
```

This is safe with Dockerfile- and Compose-based configs. The CLI keeps
`configFilePath` pointing at the workspace's own `.devcontainer` while reading
content from the override file (`spec-node/configContainer.ts`), so relative
`build.dockerfile`, `build.context` and `dockerComposeFile` paths — and
`${localWorkspaceFolder}` — still resolve against the repository.

**Lockfile guard.** When your overlays change `features`, `--no-lockfile` is
added automatically. Without it the CLI would write your personal Features into
the repo's committed `.devcontainer/devcontainer-lock.json` — the same leak as
[vscode-remote-release#11616](https://github.com/microsoft/vscode-remote-release/issues/11616).
Pass `--experimental-lockfile` to opt back in.

### `dcp install`

Editors start dev containers themselves, so they need the merged config on
disk. Which path works depends on the editor, hence two modes.

**`--mode profile`** (default) writes `.devcontainer/<name>/devcontainer.json`.
VS Code lists sub-folder configs in the "Reopen in Container" picker (supported
since v1.75). Nothing tracked is modified. Relative `dockerfile`/`context`/
`dockerComposeFile` paths are re-anchored for the deeper directory, and the
profile name is appended to `name` so the picker entry is distinguishable.

See **[docs/vscode.md](docs/vscode.md)** for the full VS Code walkthrough,
including how to keep the generated config fresh automatically.

**`--mode in-place`** replaces `.devcontainer/devcontainer.json`, keeps the
original in `.devcontainer/.dcp/base.json`, and marks the tracked file
`skip-worktree` so git reports a clean tree. Use it for tools that read only
the canonical path — the reference CLI auto-discovers exactly two locations,
`.devcontainer/devcontainer.json` and `.devcontainer.json`
(`spec-configuration/configurationCommonUtils.ts`) — or to avoid the
editor-side configuration profile mode can require.

Editor guides: **[VS Code](docs/vscode.md)** · **[Zed](docs/zed.md)**

In-place mode also backs up and protects `devcontainer-lock.json` for the same
reason the `up` guard exists.

> **Run `dcp uninstall` before `git pull`.** Git refuses to update a
> `skip-worktree` file that changed upstream. `dcp status` reports when the
> committed config has moved on and your generated file is stale.

Both modes register generated paths in `.git/info/exclude` rather than
`.gitignore`, so the repo's own ignore rules are never touched.

### `dcp uninstall`

Restores the committed configuration byte-for-byte, clears `skip-worktree`, and
removes the generated files.

### `dcp status`

Which layers apply here, whether an install is active, whether it is stale, and
whether the devcontainer CLI was found.

## Prior art and related work

- [spec#305](https://github.com/devcontainers/spec/issues/305) —
  `devcontainer.override.json` proposal, open
- [spec#22](https://github.com/devcontainers/spec/issues/22) — `extends` for
  config inheritance, open
- `dev.containers.defaultFeatures` / `defaultExtensions` — VS Code's own
  user-level defaults, GUI-only and prone to the lockfile leak above
- `--dotfiles-repository` — the CLI's supported personalization hook, which
  covers inside-the-container setup but not host-level config

## Development

```sh
make check        # fmt, vet, test
make build        # build ./bin/dcp with version metadata
make test-race    # tests with -race and coverage
```

CI (`.github/workflows/ci.yml`) runs the tests with the race detector on Linux,
macOS and Windows, `golangci-lint`, and a GoReleaser snapshot build on every
push and pull request.

## Releasing

Releases are cut by [GoReleaser](https://goreleaser.com) from an annotated tag:

```sh
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

`.github/workflows/release.yml` then builds binaries for
linux/darwin/windows on amd64/arm64, generates checksums and SBOMs, and
publishes a GitHub Release with an auto-generated changelog. Run
`goreleaser release --snapshot --clean` locally to dry-run the whole pipeline.
