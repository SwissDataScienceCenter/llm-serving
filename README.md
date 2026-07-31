# sdsc-vllm

Helm chart to deploy VLLM and envoy on Kubernetes

> For development, see [CONTRIBUTING.md](docs/CONTRIBUTING.md) for the dev environment and processes.

## Prerequisites

The chart only creates custom resources that rely on these systems being installed on the cluster:

- [Gateway API](https://gateway-api.sigs.k8s.io/) CRDs (`gateway.networking.k8s.io`)
- [Envoy Gateway](https://gateway.envoyproxy.io/) with the [Envoy AI Gateway](https://aigateway.envoyproxy.io/) extension (controller in `envoy-gateway-system`)
- [Knative Serving](https://knative.dev/docs/serving/) (scale-to-zero model services)
- [cert-manager](https://cert-manager.io/) with a `ClusterIssuer` matching `envoy.clusterissuer`

## Usage

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

Components have no enforced apply ordering: PostgreSQL, Authentik, OpenWebUI, the
gateway, and the models reconcile independently, and pods restart until their
dependencies are ready. The init job runs as a `post-install,post-upgrade` Helm
hook, after the rest of the release is applied.

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
    apiKey: # the API key to use for external APIs, if not hosted by us
    image: # the vllm image to use for hosting the model, can be left empty
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

### Secrets

Supply your deployment's secrets in your `values.<env>.yaml`. This repo's shared-deployment values files are kept encrypted with sops+age; if you maintain those, see [CONTRIBUTING.md](CONTRIBUTING.md#secrets).

### Manual installation steps

Some steps need to be done manually the first time this is deployed, since the relevant configuration can't be set automatically.

First, we need a regular OAUTH user with admin privileges in openwebui. For that, log in with your user account e.g. via gitlab oauth,
just to make sure the user is created in openwebui.

click on the user icon in the bottom left, go to "Admin Panel" -> "Settings" -> "Models". For each model, click on the
Pen icon to edit, then the "Access" button in the top right. Set to "Public", close and "save". This has to be done each
time models are changed.
