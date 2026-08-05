set positional-arguments
set shell := ["bash", "-cue"]

root_dir := `git rev-parse --show-toplevel`
charts_dir := root_dir / "charts"

# Default recipe to list all recipes.
[private]
default:
    just --list helm

# Remove fetched subchart archives.
clean:
  rm -rf "{{charts_dir}}"

# Lint chart and templates.
lint: fetch-deps
  helm lint "{{root_dir}}"
  # `helm lint` reports missing required values as INFO and exits 0.
  helm template "{{root_dir}}" --values "{{root_dir}}/tools/helm/lint-values.yaml" > /dev/null

# Fetch subchart archives (charts/*.tgz) when absent.
[private]
fetch-deps:
    #!/usr/bin/env bash
    set -eu
    cd "{{root_dir}}"
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
    helm push "sdsc-vllm-${version}.tgz" oci://ghcr.io/swissdatasciencecenter/llm-serving/charts
