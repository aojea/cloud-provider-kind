# cloud-provider-kind

Fake cloud provider for KIND clusters

## How to use it

Run a KIND cluster enabling the external cloud-provider

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
kubeadmConfigPatches:
- |
  kind: ClusterConfiguration
  apiServer:
    extraArgs:
      cloud-provider: "external"
      v: "5"
  controllerManager:
    extraArgs:
      cloud-provider: "external"
      v: "5"
  ---
  kind: InitConfiguration
  nodeRegistration:
    kubeletExtraArgs:
      cloud-provider: "external"
  ---
  kind: JoinConfiguration
  nodeRegistration:
    kubeletExtraArgs:
      cloud-provider: "external"
      v: "5"
nodes:
- role: control-plane
- role: worker
- role: worker
```

```sh
CLUSTER_NAME=test
kind create cluster --config kind.yaml --name ${CLUSTER_NAME}
```

Once the cluster is running, obtain the admin kubeconfig and use it to run the external cloud provider

```sh
CLUSTER_NAME=test
kind get kubeconfig --name ${CLUSTER_NAME} > bin/kubeconfig
bin/cloud-provider-kind --cloud-provider kind --kubeconfig $PWD/bin/kubeconfig --cluster-name ${CLUSTER_NAME} --controllers "*" --v 5 --leader-elect=false
```


**NOTE**

Only tested with docker