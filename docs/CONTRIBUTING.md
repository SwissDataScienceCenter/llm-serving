# Contributing

Developer setup for working on the chart repo.

## Prerequisites

- All dependencies listed in [`flake.nix`](tools/nix/flake.nix) are assumed to be available.
- _optional_: [Nix](https://nixos.org/), which takes care of installing above dependencies in a dev shell.
- _optional_: [direnv](https://direnv.net/) for automatic shell activation.

## Dev shell

Enter it either way:

- **direnv**: `direnv allow` once; the shell loads on `cd` (via `.envrc` →
  `tools/nix#default`).
- **nix**: `just develop` (drops you into the shell), or
  `just develop <cmd>` to run a single command in it.
- **manual**: bring your own shell.

On entry the shell runs `just setup`, which installs the git pre-commit hooks.
Without nix, run it manually once (it is idempotent).

## Common tasks

Run `just` (or `just --list`) to see everything. Tooling recipes are in the ### Manual installation steps
justfile (e.g. `just lint`), whereas specialized recipes (e.g. `just sops::edit <file>`) are in just modules under `tools/just`.

## pre-commit

Pre-commit hooks are set up via `prek` and run some `just` recipes.

## Secrets

Shared-deployment values files (`values.*.yaml`) are encrypted with
[sops](https://github.com/getsops/sops) and [age](https://github.com/FiloSottile/age);
`tools/config/.sops.yaml` controls which keys can decrypt them and which fields are encrypted.

One-time setup:

1. Create an encrypted age key file: `just sops::keygen <key-path>.age`
2. Ask someone already listed in `tools/config/.sops.yaml` to add your public key and
   re-encrypt (`just sops::updatekeys`).
3. Tell `just`/sops where your key is: copy `example.env` to `.env` (gitignored,
   auto-loaded by `just`) and set `SOPS_AGE_KEY_FILE` to your key's path.

To work with encrypted files, list recipes by running `just sops`.

> [!NOTE]
>
> If you wish to run sops/age directly without using just,
> make sure to export `SOPS_AGE_KEY_FILE` and point sops to
> the config using `--config tools/config/..sops.yaml`.
