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

Run `just` (or `just --list`) to see everything. Tooling recipes are in the justfile (e.g. `just lint`),
whereas specialized recipes (e.g. `just helm::template`) are in just modules under `tools/just`.

## pre-commit

Pre-commit hooks are set up via `prek` and run some `just` recipes.
