set positional-arguments
set shell := ["bash", "-cue"]

default:
    just --list --unsorted -f "{{source_file()}}"

# Enter the Nix devShell `$1` and exec command `${@:2}`.
[no-cd]
develop *args:
    #!/usr/bin/env bash
    set -eu
    root_dir=$(git rev-parse --show-toplevel) || exit 1
    flake_dir="$root_dir/tools/nix"

    shell="$1"; shift 1;
    args=("$@") && [ "${#args[@]}" != 0 ] ||
        args=(env SHELL="$SHELL" "$SHELL")

    nix_args=("--no-pure-eval" "--extra-experimental-features" "nix-command flakes")

    nix develop \
        "${nix_args[@]}" \
        --accept-flake-config \
        "$flake_dir#$shell" \
        --command "${args[@]}"

# Start an Nix interpreter with loaded flake.
[no-cd]
repl *args:
    #!/usr/bin/env bash
    set -eu

    root_dir=$(git rev-parse --show-toplevel) || exit 1
    flake_dir="$root_dir/tools/nix"
    nix_args=("--no-pure-eval" "--extra-experimental-features" "nix-command flakes" "$@")

    nix repl \
        "${nix_args[@]}" \
        --accept-flake-config \
        "$flake_dir"

