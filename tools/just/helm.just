set positional-arguments
set shell := ["bash", "-cue"]

root_dir := `git rev-parse --show-toplevel`
charts_dir := root_dir / "charts"

# Default recipe to list all recipes.
[private]
default:
    just --list helm

clean:
  rm "{{charts_dir}}"/*

# Lint chart and templates.
lint:
  helm lint "{{root_dir}}"

# Fetch subchart archives (charts/*.tgz) when absent.
[private]
fetch-deps:
    #!/usr/bin/env bash
    set -eu
    cd "{{root_dir}}"
    helm repo add bitnami https://charts.bitnami.com/bitnami
    helm repo add authentik https://goauthentik.github.io/helm
    helm dependency build

# Render chart manifests to stdout
template *args: fetch-deps
    #!/usr/bin/env bash
    set -eu
    cd "{{root_dir}}"
    helm template . "$@"

# Package the chart for a release tag (e.g. v0.3.0) and push it to GHCR as an OCI artifact.
publish tag: fetch-deps
    #!/usr/bin/env bash
    set -eu
    cd "{{root_dir}}"
    version="{{tag}}"
    version="${version#v}"
    helm package . --version "$version" --app-version "{{tag}}"
    helm push "sdsc-vllm-${version}.tgz" oci://ghcr.io/swissdatasciencecenter/sdsc-llm-deployment/charts
