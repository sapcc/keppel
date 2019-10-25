### Orchestration driver: `kubernetes`

Runs one `keppel-registry` process per Keppel account in a Kubernetes cluster. The `keppel-api` processes must be running
in the same cluster. When using this driver, it is safe to scale `keppel-api` horizontally. The `keppel-api` replicas
will share the same set of `keppel-registry` Deployments and Services when configured identically.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_KUBERNETES_NAMESPACE` | *(required)* | All objects managed by this driver are placed in this Kubernetes namespace. |
| `KEPPEL_KUBERNETES_MARKER` | `registry` | All objects managed by this driver will have this string in their name (see table below), and carry a label `owner` with this value, as well as the label `heritage: keppel-api`. |
| `KEPPEL_REGISTRY_IMAGE` | *(required)* | Your Docker registry image. The image must have the binary `keppel-registry` in its PATH. This will usually be the same image as for your `keppel-api` instance, since Keppel's Dockerfile produces an image with both `keppel-api` and `keppel-registry`. |

This driver manages:

- one Deployment per Keppel account that spawns the registry pods
- one Service per Keppel account to assign a cluster-internal service IP to the registry pods
- a ConfigMap that all `keppel-registry` Deployments reference

| Object type | Name |
| ----------- | ---------------- |
| Deployment | `${MARKER}-${ACCOUNT_NAME}` |
| Service | `${MARKER}-${ACCOUNT_NAME}` |
| ConfigMap | `${MARKER}` |

This driver does **not** look at or touch the `keppel-registry` pods in any way. It is your responsibility to monitor
the health of the registry pods.

## How to work on this driver

It is not feasible to run `keppel-api` outside of the Kubernetes cluster, because it needs to talk to the
`keppel-registry` instances using their cluster-internal service IPs. To work on this driver, set up a development
environment in a pod in the target cluster, and run `keppel-api` there (either by doing all your work in the development
pod, or by copying the file into the target container). For example:

```sh
$ make
$ kubectl cp ./build/keppel-api dev-toolbox:/keppel-api
$ kubectl exec -ti dev-toolbox /keppel-api
```
