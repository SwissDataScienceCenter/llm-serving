set positional-arguments
set shell := ["bash", "-cue"]
set dotenv-load
root_dir := `git rev-parse --show-toplevel`
flake_dir := root_dir / "tools/nix"
output_dir := root_dir / ".output"
build_dir := output_dir / "build"
go_modules := "src/init src/otlp-openmeter-bridge"

# Manage nix environment.
[group('modules')]
mod nix "./tools/just/nix.just"
# Manage the helm chart.
[group('modules')]
mod helm "./tools/just/helm.just"

# Default target if you do not specify a target.
default:
    just --list --unsorted

# Enter the default Nix development shell and execute the command `"$@`.
[group('tooling')]
develop *args:
    just nix::develop "default" "$@"

# Run commands over the ci development shell.
[group('tooling')]
ci *args:
    just nix::develop "ci" "$@"

# Format the repository.
[group('tooling')]
format *args:
    treefmt --config-file ./tools/config/treefmt.toml "$@"

# Setup the repository.
[group('tooling')]
setup:
    cd "{{root_dir}}" && prek install

# Lint the repository.
[group('tooling')]
lint:
    #!/usr/bin/env bash
    set -eu
    cd "{{root_dir}}"
    just helm::lint
    for d in {{go_modules}}; do (cd "$d" && go vet ./...); done

# Build the Go modules.
[group('general')]
build *args:
    #!/usr/bin/env bash
    set -eu
    for d in {{go_modules}}; do
        cd "{{root_dir}}/$d"
        echo "building $d"
        go build -v -o "{{build_dir}}/$(basename "$d")" ./... 
    done

# Clean up generated files.
[group('general')]
[confirm("Delete everything in:\n" + output_dir + "?\n [y/n]")]
clean: helm::clean
    rm -fr "{{output_dir}}"/*

# Test the Go modules.
[group('general')]
test *args:
    #!/usr/bin/env bash
    set -eu
    cd "{{root_dir}}"
    for d in {{go_modules}}; do (cd "$d" && go test ./... "$@"); done

# Deploy Helm chart.
[group('chart')]
deploy namespace release values_file:
    helm upgrade --install -n "{{namespace}}" "{{release}}" . --values "{{values_file}}"

# init roles and databases. psql prompts for the admin password.
[group('chart')]
db-bootstrap host admin_user="postgres" openwebui_db="openwebui" authentik_db="authentik":
    #!/usr/bin/env bash
    set -eu
    : "${OPENWEBUI_PG_PASSWORD_FILE:?not set, see .tpl.env}"
    : "${AUTHENTIK_PG_PASSWORD_FILE:?not set, see .tpl.env}"
    OPENWEBUI_PG_DATABASE="{{openwebui_db}}" \
    AUTHENTIK_PG_DATABASE="{{authentik_db}}" \
    OPENWEBUI_PG_PASSWORD="$(< "$OPENWEBUI_PG_PASSWORD_FILE")" \
    AUTHENTIK_PG_PASSWORD="$(< "$AUTHENTIK_PG_PASSWORD_FILE")" \
        psql "postgresql://{{admin_user}}@{{host}}/postgres" \
            --set ON_ERROR_STOP=1 --file "{{root_dir}}/tools/scripts/bootstrap-db.sql"

# Errors if the repository contains unformatted files.
[private]
check-format *args:
    just format --fail-on-change --no-cache || \
    { echo "Unformatted files. Run 'just format' locally."; exit 1; }

# Check for secret leaks.
[private]
check-leaks *args:
    gitleaks git {{args}}
