# Checkpoint and Restore in Kubernetes - crik

`crik` is a project that aims to provide checkpoint and restore functionality for Kubernetes pods mainly targeted for
node shutdown and restart scenarios. It is a command wrapper that, under the hood, utilizes
[`criu`](https://github.com/checkpoint-restore/criu) to checkpoint and restore process trees in a `Pod`.

> `crik` is first revealed at KubeCon EU 2024:
> [The Party Must Go on - Resume Pods After Spot Instance Shut Down - Muvaffak Onuş, QA Wolf](https://kccnceu2024.sched.com/event/1YeP3)

It is a work in progress and is not ready for production use.

`crik` has two components:

- `crik` - a command wrapper that executes given command and checkpoints it when SIGTERM is received and restores from
  checkpoint when image directory contains a checkpoint.
- `manager` - a kubernetes controller that watches `Node` objects and updates its internal map of states so that `crik`
  can check whether it should checkpoint or restore depending on its node's state.

## Quick Start

The only pre-requisite is to have a Kubernetes cluster running. You can use `kind` to create a local cluster.

```bash
kind create cluster
```

Then, you can deploy the simple-loop example where a counter increases every second and you can delete the pod and see
that it continues from where it left off in the new pod.

```bash
kubectl apply -f examples/simple-loop.yaml
```

Watch logs:

```bash
kubectl logs -f simple-loop-0
```

In another terminal, delete the pod:

```bash
kubectl delete pod simple-loop-0
```

Now, a new pod is created. See that it continues from where it left off:

```bash
kubectl logs -f simple-loop-0
```

## Usage

The application you want to checkpoint and restore should be run with `crik` command, like the following:

```bash
crik run -- app-binary
```

The following is an example `Dockerfile` for your application that installs `crik` and runs your application. It assumes
your application is `entrypoint.sh`.

```Dockerfile
FROM ubuntu:22.04

RUN apt-get update && apt-get install --no-install-recommends --yes gnupg curl ca-certificates

# crik requires criu to be available.
RUN curl "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x4E2A48715C45AEEC077B48169B29EEC9246B6CE2" | gpg --dearmor > /usr/share/keyrings/criu-ppa.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/criu-ppa.gpg] https://ppa.launchpadcontent.net/criu/ppa/ubuntu jammy main" > /etc/apt/sources.list.d/criu.list \
    && apt-get update \
    && apt-get install --no-install-recommends --yes criu iptables

# Install crik
COPY --from=ghcr.io/qawolf/crik/crik:v0.1.2 /usr/local/bin/crik /usr/local/bin/crik

# Copy your application
COPY entrypoint.sh /entrypoint.sh

# Run your application with crik
ENTRYPOINT ["crik", "run", "--", "/entrypoint.sh"]
```

### Configuration

Not all apps can be checkpointed and restored and for many of them, `criu` may need additional configurations. `crik`
provides a high level configuration interface that you can use to configure `crik` for your application. The following
is the minimum configuration you need to provide for your application and by default `crik` looks for `config.yaml` in
`/etc/crik` directory.

```yaml
kind: ConfigMap
metadata:
  name: crik-simple-loop
data:
  config.yaml: |-
    imageDir: /etc/checkpoint
```

Configuration options:

- `imageDir` - the directory where `crik` will store the checkpoint images. It needs to be available in the same path
  in the new `Pod` as well.
- `additionalPaths` - additional paths that `crik` will include in the checkpoint and copy back in the new `Pod`. Populate
  this list if you get `file not found` errors in the restore logs. The paths are relative to root `/` and can be
  directories or files.
- `inotifyIncompatiblePaths` - paths that `crik` will delete before taking the checkpoint. Populate this list if you get
  `fsnotify: 	Handle 0x278:0x2ffb5b cannot be opened` errors in the restore logs. You need to find the inode of the
  file by converting `0x2ffb5b` to an integer, and then find the path of the file by running `find / -inum <inode>` and
  add the path to this list. See [this comment](https://github.com/checkpoint-restore/criu/issues/1187#issuecomment-1975557296) for more details.

### Node State Server

> Alpha feature. Not ready for production use.

You can optionally configure `crik` to take checkpoint only if the node it's running on is going to be shut down. This is
achieved by deploying a Kubernetes controller that watches `Node` events and updates its internal map of states so that
`crik` can check whether it should checkpoint or restore depending on its node's state. This may include direct calls
to the cloud provider's API to check the node's state in the future.

Deploy the controller:

```bash
helm upgrade --install node-state-server oci://ghcr.io/qawolf/crik/charts/node-state-server --version 0.1.2
```

Make sure to include the URL of the server in `crik`'s configuration mounted to your `Pod`.

```yaml
# Assuming the chart is deployed to default namespace.
kind: ConfigMap
metadata:
  name: crik-simple-loop
data:
  config.yaml: |-
    imageDir: /etc/checkpoint
    nodeStateServerURL: http://crik-node-state-server.default.svc.cluster.local:9376
```

`crik` will hit the `/node-state` endpoint of the server to get the state of the node it's running on when it receives
SIGTERM and take checkpoint only if it returns `shutting-down` as the node's state. However, it needs to provide the
node name to the server so make sure to add the following environment variable to your container spec in your `Pod`:

```yaml
env:
  - name: KUBERNETES_NODE_NAME
    valueFrom:
      fieldRef:
        fieldPath: spec.nodeName
```

## Developing

Build `crik`:

```bash
go build -o crik cmd/crik/main.go
```

## Why not upstream?

Taking checkpoints of processes and restoring them from within the container requires quite a few privileges to be given
to the container. The best approach is to execute these operations at the container runtime level and today, container
engines such as CRI-O and Podman do have native support for using `criu` to checkpoint and restore the whole containers
and there is an ongoing effort to bring this functionality to Kubernetes as well. The first use case being the forensic
analysis via checkpoints as described [here](https://kubernetes.io/blog/2023/03/10/forensic-container-analysis/).

While it is the better approach, since it's such a low-level change, it's expected to take a while to be available in
mainstream Kubernetes in an easily consumable way. For example, while taking a checkpoint is possible through `kubelet`
API if you're using CRI-O, restoring it as another `Pod` in a different `Node` is not natively supported yet.

`crik` allows you to use `criu` to checkpoint and restore a `Pod` to another `Node` today without waiting for the native
support in Kubernetes. Once the native support is available, `crik` will utilize it under the hood.

## License

This project is licensed under the Apache License, Version 2.0 - see the [LICENSE](LICENSE) file for details.
