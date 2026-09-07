# Using `dcp` with Zed

## The constraint to understand first

Zed does **not** shell out to the [devcontainer CLI](https://github.com/devcontainers/cli).
Since [#52338](https://github.com/zed-industries/zed/pull/52338) ("Dev
containers native implementation", 1 April 2026) it has its own Rust
implementation in `crates/dev_container/` — it talks to Docker/Podman directly,
resolves Features itself (`features.rs`, `oci.rs`) and parses `devcontainer.json`
with serde.

Two consequences:

- **`dcp up` is not the integration path for Zed.** There is no CLI invocation
  to wrap, and no `--override-config` to pass. (Older Zed builds *did* call the
  `devcontainer` binary on `PATH` — that is what
  [#46852](https://github.com/zed-industries/zed/issues/46852) is about — but
  that is no longer how it works.)
- The integration is the same as for VS Code: **`dcp install` writes the merged
  config to a path Zed already scans.**

## Zed's configuration discovery

Read straight from `crates/dev_container/src/devcontainer_api.rs`
(`find_configs_in_snapshot`), Zed scans three locations and offers **all** of
them in a picker:

| Location | Picker label |
| --- | --- |
| `.devcontainer/devcontainer.json` | `default` |
| `.devcontainer/<subfolder>/devcontainer.json` | `<subfolder>` |
| `.devcontainer.json` | `root` |

`default` and `root` sort first, the rest alphabetically. Sub-folder detection
landed in [#47411](https://github.com/zed-industries/zed/pull/47411) (3 February
2026); `.devcontainer.json` support in
[#48814](https://github.com/zed-industries/zed/pull/48814).

So `dcp install`'s default profile mode is compatible with Zed — with one
wrinkle.

---

## Recommended: profile mode + one Zed setting

### The wrinkle

`dcp install` adds the generated directory to `.git/info/exclude` so your
personal config never dirties the repository. Zed honours `.git/info/exclude`
(`crates/worktree/src/worktree.rs`, `REPO_EXCLUDE`), and **does not scan inside
ignored directories** — `should_scan_directory` returns false for an ignored
entry unless it is "always included":

```rust
let scannable = state.scanning_enabled
    && (!entry.is_external || …)
    && (!(entry.is_ignored || beyond_scan_depth) || entry.is_always_included);
```

The generated `.devcontainer/local/` directory is therefore invisible to the
picker by default. `is_always_included` is driven by Zed's
`file_scan_inclusions` setting, which is exactly the escape hatch:

> "Files or globs of files that will be included by Zed, even when ignored by
> git."

### Setup

```sh
cd ~/code/some-project
dcp install                 # default: --mode profile --name local
```

```jsonc
// Zed settings.json  (F1 → "zed: open settings")
{
  // Default is [".env*"] — keep it, this list replaces rather than extends.
  "file_scan_inclusions": [".env*", ".devcontainer/**"]
}
```

This is a one-time, global setting; it applies to every project you use `dcp`
in.

### Using it

1. Open the project in Zed.
2. Zed offers to reopen in a dev container, or run **F1 → `dev containers:
   reopen in container`**.
3. Pick **`local`** from the list. `default` is the repo's unmodified config,
   still available for an A/B test.

Verify what you're about to build first:

```sh
dcp render --explain
```

### Note on labels

Zed labels picker entries by **sub-folder name**, not by the config's `name`
field. `dcp install --name <x>` therefore controls the label directly:

```sh
dcp install --name perf-tuning     # appears as "perf-tuning"
```

(The `name` suffix `dcp install` writes into the config — `"project (local)"` —
is for VS Code's picker, which uses that field. It is harmless in Zed.)

---

## Alternative: in-place mode

If you would rather not add `file_scan_inclusions`, or you want a single
configuration with no picker step:

```sh
dcp install --mode in-place
```

This replaces `.devcontainer/devcontainer.json` — the `default` entry — with
the merged version, keeps the original in `.devcontainer/.dcp/base.json`, and
marks the tracked file `skip-worktree` so `git status` stays clean. Zed needs
no configuration, because the file is exactly where it already looks.

> **Run `dcp uninstall` before `git pull`.** Git refuses to update a
> `skip-worktree` file that changed upstream. `dcp status` reports when the
> committed config has moved on and your generated file is stale.

### Choosing

| | profile | in-place |
| --- | --- | --- |
| Repo files modified | none | `devcontainer.json` (hidden via `skip-worktree`) |
| Zed setting required | `file_scan_inclusions` | none |
| Picker step | yes — pick `local` | no |
| Original still selectable | yes (`default`) | no (backed up on disk) |
| `git pull` hazard | none | uninstall first |
| Same config seen by the `devcontainer` CLI | no (needs `--config`) | yes |

---

## Keeping the generated config fresh

Two Zed-specific facts make this matter more than it does in VS Code:

1. **Zed does not rebuild automatically.** Per its docs: "Zed does not
   currently rebuild or reload the container automatically" after
   `devcontainer.json` changes. You must stop the container and reopen.
2. Zed has no equivalent of VS Code's `runOn: folderOpen` tasks, so the
   editor-side automation from the VS Code guide does not transfer.

That leaves git hooks, which is the better answer anyway because it is
editor-agnostic:

```sh
cd ~/code/some-project
for hook in post-checkout post-merge post-rewrite; do
  printf '#!/bin/sh\ncommand -v dcp >/dev/null || exit 0\ndcp install >/dev/null 2>&1 || true\n' \
    > ".git/hooks/$hook"
  chmod +x ".git/hooks/$hook"
done
```

Set `core.hooksPath` to a shared directory to apply it everywhere.

> With `--mode in-place`, do **not** put this on `post-merge`: the pull that
> would trigger it is the operation `skip-worktree` blocks. Uninstall, pull,
> reinstall.

`dcp status` reports staleness either way:

```
install:
  mode:      profile
  generated: .devcontainer/local/devcontainer.json
  STALE: the committed devcontainer.json changed since install; re-run `dcp install`
```

---

## Zed settings that are not devcontainer.json settings

`use_podman`, `podman_path` and `dev_container_use_buildkit` are **Zed
settings**, read from `settings.json` — not properties of `devcontainer.json`.
Zed's parser ignores unknown `devcontainer.json` keys silently, so putting them
in the wrong file fails quietly:

```jsonc
// Zed settings.json — correct
{
  "use_podman": true,
  "podman_path": "podman",
  "dev_container_use_buildkit": false
}
```

```jsonc
// .devcontainer/devcontainer.json — silently ignored by Zed
{
  "use_podman": true,
  "podman_path": "podman"
}
```

`dcp` passes such keys through untouched (it merges generic JSON rather than a
typed schema), so it will neither fix nor break this — but if your container
starts with Docker instead of Podman, check which file they are in.

---

## Known Zed limitations worth knowing before you debug `dcp`

These are Zed-side issues, not `dcp` ones. If a merged config behaves
differently in Zed than in VS Code, check here first:

- **Features may be ignored.**
  [zed#56608](https://github.com/zed-industries/zed/issues/56608) (open, May
  2026) reports Features from `devcontainer.json` not being installed, while
  the same config works in VS Code. If a Feature you added via `dcp` does not
  appear, confirm with `dcp render` that it is in the config, then test the same
  file with `devcontainer up --config .devcontainer/local/devcontainer.json` to
  isolate which side is at fault.
- **Strict deserialization of some properties.**
  [zed#53686](https://github.com/zed-industries/zed/issues/53686) covers
  `portsAttributes` entries failing to deserialize when optional fields are
  omitted. If Zed rejects a config the CLI accepts, compare against the
  unmerged original — `dcp render --explain` shows which layer introduced the
  property.
- **No automatic rebuild** on configuration change, as above.

---

## Troubleshooting

**The picker doesn't list my `local` configuration.**
Almost always the `file_scan_inclusions` setting. Confirm the file exists
(`ls .devcontainer/local/devcontainer.json`), add `".devcontainer/**"` to
`file_scan_inclusions`, and reopen the project. Note that `file_scan_exclusions`
takes precedence over inclusions. If you would rather not configure Zed, use
`--mode in-place`.

**The picker lists it but the container is missing my tools.**
See the Features caveat above — verify with `dcp render` that the Feature is in
the generated config before assuming `dcp` dropped it.

**My changes to the overlay aren't reflected.**
Two snapshots to refresh, in order: run `dcp install` again, then stop the
container and reopen the project. Zed will not rebuild on its own.

**`git pull` fails with "local changes would be overwritten".**
You are in `--mode in-place`. `dcp uninstall`, pull, `dcp install --mode
in-place`.

**Podman isn't being used.**
Check that `use_podman` is in Zed's `settings.json` and not in
`devcontainer.json`.
