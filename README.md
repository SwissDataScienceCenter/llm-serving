# sdsc-vllm

Helm chart to deploy VLLM and envoy on Kubernetes

> For development, see [CONTRIBUTING.md](docs/CONTRIBUTING.md) for the dev environment and processes.

## Prerequisites

The chart only creates custom resources that rely on these systems being installed on the cluster:

- [Gateway API](https://gateway-api.sigs.k8s.io/) CRDs (`gateway.networking.k8s.io`)
- [Envoy Gateway](https://gateway.envoyproxy.io/) with the [Envoy AI Gateway](https://aigateway.envoyproxy.io/) extension (controller in `envoy-gateway-system`)
- [Knative Serving](https://knative.dev/docs/serving/) (scale-to-zero model services)
- [cert-manager](https://cert-manager.io/) with a `ClusterIssuer` matching `envoy.clusterissuer`
- A PostgreSQL server, with roles and databases created up front. See [postgresql.md](docs/postgresql.md)

## Installation

The repository contains a [`justfile`](justfile) to automate routine commands.
You may use it as reference, or run it with `just` (by default, just will list available recipes).
See [CONTRIBUTING.md](docs/CONTRIBUTING.md) for more information.

### Helm (Kubernetes)

Make sure you have helm and kubectl installed and that you're connected via VPN.
Look over values.yaml and change to your liking.

Make sure that you have set up the image pulling credentials.
You will also need a gitlab token with `read_user` for the oauth integration.

Take a look at `values.yaml`, create a new yaml file `values.<env>.yaml` and fill in the values not set in `values.yaml` (with `<env>` being an arbitrary string).

Run

```bash
helm dependency build
helm upgrade --install -n <your namespace> vllm . --values values.<env>.yaml
```

where `<your namespace>` is your Kubernetes namespace.

The init job runs as a `post-install,post-upgrade` Helm hook, after the rest of the
release is applied.

To preview the rendered manifests before applying:

```bash
helm template -n <your namespace> vllm . --values values.<env>.yaml
```

To uninstall:

```bash
helm uninstall -n <your namespace> vllm
```

### Adding models

To add a model, simply add a new entry in the `models:` section of the values file.

```yaml
models:
  model-short-name:
    fullName: "Qwen/Qwen2.5-Omni-7B-AWQ" # name of the hosted model on huggingface
    internal: true # true means this is a model hosted by us, false would be for forwarding to external apis (experimental)
    nodeType: "A100" # the type of GPU to use
    apiKey: # required: the API key to use for external APIs, if not hosted by us.
    image: # optional: the vllm image to use for hosting the model
      # repository:
      # tag:
    cacheDir: # the directory for the vllm cache (leaving this empty should work in most cases)
      # path: /myhome
      # claimName: # the PVC claim to mount the cache to. On runai, use something like `pvc-<project>-home` (e.g. `pvc-codev-ralf-home`)
    enableTools: false # whether to allow tool calls or not
    logRequests: false # whether to log all requests in the vllm pod
    scaleDownDelaySeconds: 3600 # the number of seconds before the model is torn down if there is no traffic
    chatTemplate: # if you want to use a custom chat template. Usually left empty. The template must exist in the docker image to work
      repository:
      tag:
    resources: # the compute resources to request
      requests: # specifies typical usage requests
        cpu: 1
        memory: "100M"
      limits: # specifies maximum available resources. Workload will be restarted if it uses more than this
        cpu: 8
        memory: "25G"
        gpu: "0.2" # fraction of a gpu to use
```

### Manual installation steps

Some steps need to be done manually the first time this is deployed, since the relevant configuration can't be set automatically.

First, we need a regular OAUTH user with admin privileges in openwebui. For that, log in with your user account e.g. via gitlab oauth,
just to make sure the user is created in openwebui.

click on the user icon in the bottom left, go to "Admin Panel" -> "Settings" -> "Models". For each model, click on the
Pen icon to edit, then the "Access" button in the top right. Set to "Public", close and "save". This has to be done each
time models are changed.

## Usage

### Web interface

Open `https://openwebui.<baseDomain>` and sign in with the "authentik" button, which
delegates to GitLab. The first sign-in creates the account; accounts are matched by
email, so signing in a different way later does not create a duplicate.

Members of the `gateway admins` group in authentik become OpenWebUI admins.

Newly added models stay private until an admin makes them public once, see
[Manual installation steps](#manual-installation-steps).

A model that has scaled to zero takes a minute or two to answer the first message.

### API access

The gateway at `https://gateway.<baseDomain>/v1` speaks the OpenAI API and takes an
authentik access token as its bearer credential. `GET /v1/models` lists the model names to
use; they are the `models.*.fullName` values.

Get a token with the device code flow. `CLIENT_ID` is `authentik.oauthApp.clientId` -- a
public client, so it is not a secret:

```bash
DOMAIN=<baseDomain>
CLIENT_ID=<authentik.oauthApp.clientId>

# 1. start the flow, then open verification_uri_complete in a browser and approve
curl -s "https://authentik.$DOMAIN/application/o/device/" \
  -d client_id="$CLIENT_ID" -d scope="openid profile email offline_access" \
  | tee /tmp/dev.json | jq

# 2. exchange the device code for a token (returns authorization_pending until approved)
TOKEN=$(curl -s "https://authentik.$DOMAIN/application/o/token/" \
  -d grant_type=urn:ietf:params:oauth:grant-type:device_code \
  -d client_id="$CLIENT_ID" \
  -d device_code="$(jq -r .device_code /tmp/dev.json)" | jq -r .access_token)

curl "https://gateway.$DOMAIN/v1/chat/completions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"<fullName>","messages":[{"role":"user","content":"hello"}]}'
```

Or point any OpenAI client at it:
`OpenAI(base_url=f"https://gateway.{DOMAIN}/v1", api_key=TOKEN)`.

The token is yours, so rate limits and usage are attributed to you. Access tokens last
`authentik.oauthApp.accessTokenValidity` (8 hours by default); the device grant also
returns a refresh token, valid for `refreshTokenValidity`, so a client can renew without
a second browser approval. A model that has scaled to zero takes a minute or two to answer
the first request.
