set positional-arguments
set shell := ["bash", "-cue"]
# Modules don't inherit settings from the root justfile.
set dotenv-load
ENC_SUFFIX := ".enc"
DEC_SUFFIX := ".dec"

root_dir := `git rev-parse --show-toplevel`
config_file := root_dir / "tools/config/sops.yaml"

# Default recipe to list all recipes.
[private]
default:
    just --list sops

# Run any sops command.
[no-cd]
run *args:
    sops --config "{{config_file}}" "$@"

# Generate encrypted age key file
keygen key_file:
    mkdir -p $(dirname "{{key_file}}") \
      && age-keygen | age -p -e > "{{key_file}}" \
      && chmod 400 "{{key_file}}"

# Edit an encrypted values file in place.
[no-cd]
edit file:
    just sops::run edit "{{file}}"


# Encrypt a secrets file ('*.dec.*' -> '*.sops.*', removing the source).
[no-cd]
encrypt file:
    #!/usr/bin/env bash
    set -eu -o pipefail

    fbase_dec=$(basename "{{file}}")
    fbase="${fbase_dec/{{DEC_SUFFIX}}/}"
    if [ "$fbase" = "$fbase_dec" ]; then
        echo "Can only encrypt files with '{{DEC_SUFFIX}}' suffix."
        exit 1
    fi

    fdir=$(dirname "{{file}}")
    fext="${fbase##*.}"
    fname="${fbase%.*}"
    enc_file="${fdir}/${fname}{{ENC_SUFFIX}}.${fext}"


    just sops::run encrypt "{{file}}" > "$enc_file" &&
        rm "{{file}}" || {
            rm "$enc_file"
            exit 1
        }


# Decrypt an encrypted file to stdout.
[no-cd]
decrypt file:
    just sops::run decrypt "{{file}}"

# Encrypt all secrets.
encrypt-all:
    fd -gu  '**/*{{DEC_SUFFIX}}*' "{{root_dir}}" \
        -x just sops::encrypt "{}"

# Sync keypairs with sops.yaml in all encrypted files.
updatekeys:
    fd -gu '**/*{{ENC_SUFFIX}}*' "{{root_dir}}" \
        -x just sops::run updatekeys -y {}

# Rotate data encryption key in all encrypted files.
rotate:
    fd -gu '**/*{{ENC_SUFFIX}}*' "{{root_dir}}" \
        -x just sops::run rotate -i "{}"
