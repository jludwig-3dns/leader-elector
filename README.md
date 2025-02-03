# leader-elector

This project provides a simple sidecar container that performs leader election in a Kubernetes cluster. The sidecar uses the Kubernetes client-go library to create a Lease object in the Kubernetes API, allowing multiple replicas of your application to decide which is the leader.

## How it Works

The sidecar uses the Kubernetes leader election functionality, built around a Lease object. The Lease object is a Kubernetes primitive that represents a distributed lock. When multiple replicas of your application run, they will compete to acquire the lock. The one that succeeds becomes the leader.

The leader writes their identity to a file (`/tmp/leader_status`) to indicate that it's the leader. If it loses leadership, the file is deleted.

If the leader crashes or stops renewing the lease, another replica will acquire the lease and become the new leader.

The identity used for each replica is the pod's hostname.

## Deployment

To use the sidecar in your Kubernetes deployment, add it to the list of containers in your pod specification:

```yaml
spec:
  containers:
  - name: myapp
    image: myapp:1.0.0
  - name: leader-election-sidecar
    image: supporttools/leader-elector:latest
    env:
    # Core Required Variables
    - name: LEASE_NAME
      value: myapp-lock
    - name: NAMESPACE
      valueFrom:
        fieldRef:
          fieldPath: metadata.namespace
    # Optional Variables
    # - name: STATUS_DIR
    #   value: "/shared/leader_status"  # Override default /tmp/leader_status
    # - name: HEALTH_PORT
    #   value: "9090"  # Override default 8080

    # Pod Labeling Variables (requires RBAC)
    # - name: LABEL_POD_ROLE
    #   value: "true"  # Enables role=leader/follower labels
    # - name: POD_NAME  # Required when using labels
    #   valueFrom:
    #     fieldRef:
    #       fieldPath: metadata.name
    volumeMounts:
    - name: leader-status
      mountPath: /tmp/leader_status
  volumes:
  - name: leader-status
    emptyDir: {}
```

In this configuration, `myapp` is the main container of the pod, and `leader-election-sidecar` is the sidecar container that performs leader election. The `LEASE_NAME` and `NAMESPACE` environment variables specify the name of the Lease object and the namespace where it's created. The `NAMESPACE` is set from the pod's metadata, automatically matching the namespace where the pod is running.

The sidecar shares an `emptyDir` volume with the main container, where it writes the leader status file. Your application can watch this file to know if it's the leader. The leader file will only exist on the leader pod. Also, the leader_status file has the hostname of the leader pod inside it.

Here is an example of using this in your application.

```bash
#!/bin/bash

while true
do
  echo "Checking leadership."
  if [ -f /tmp/leader_status ]
  then
    echo "I am the leader!!!"
    # Start your application here
  else
    echo "I am not the leader, sleeping..."
    sleep 5
  fi
done
```

### Dynamic Pod Labeling

This updated version patches its own Pod with a `role` label:
- When the Pod becomes leader, it sets `role=leader`.
- When it loses leadership, it sets `role=follower`.

To support this, ensure:
1. Your ServiceAccount has the `patch` permission on Pods.
2. The Pod spec injects `POD_NAME` (and optionally `POD_NAMESPACE`) via the Downward API.

For example, update your Service spec to route traffic only to the leader:
```yaml
spec:
  selector:
    app: nfs-server
    role: leader
```

**Required RBAC** (only needed when using labels):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: leader-elector
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["patch"]
```

### Environment Variables

#### Core Required Variables
- **`LEASE_NAME`**: Name of the Lease object to use for leader election  
- **`NAMESPACE`**: Namespace where the Lease object exists (use Downward API)

#### Optional Variables
- **`STATUS_DIR`**: Overrides default leader status directory (`/tmp/leader_status`)
- **`HEALTH_PORT`**: Overrides default healthcheck port (`8080`)

#### Pod Labeling Variables (requires RBAC)
- **`LABEL_POD_ROLE`**: Set to "true" to enable role labeling (`role=leader`/`role=follower`)
- **`POD_NAME`**: Required when using labels (use Downward API)

## Building and Running Locally

You can build and run this project locally with Go:

```bash
go build -o leader-election-sidecar main.go
LEASE_NAME=myapp-lock NAMESPACE=default ./leader-election-sidecar
```

This will run the sidecar and attempt to perform leader election using your local Kubernetes context (either from your in-cluster configuration or from `$KUBECONFIG`).
