# Setup, Running and Testing

This documentation details the process of setting up Clyde on a Kubernetes Cluster, running and testing.

## Prerequisite
1. A kubernetes cluster: 1.20+
2. Containerd: 1.50+
3. Helm: 3.80+

## Containerd Configuration
1. On all nodes, open /etc/containerd/config.toml
2. Set containerd to config_path and set discard_unpacked_layers = false. See sample truncated config below. Make sure your root points to a directory with lot of space.

```bash
version = 2
root = "/data/var/lib/containerd"

[plugins]
    ...
    
    [plugins."io.containerd.grpc.v1.cri".containerd]
      discard_unpacked_layers = false
      ...
    
    [plugins."io.containerd.grpc.v1.cri".registry]
      config_path = "/etc/containerd/certs.d"
```
3. Restart containerd

## Install Clyde

The following commands allows Clyde to be installed on Kubernetes cluster.

```
git clone http://gitee.com/openeuler/clyde.git
cd clyde
helm upgrade --install clyde charts/clyde --create-namespace --namespace clyde -f clyde-values.yml
or
helm upgrade --install clyde charts/clyde -f clyde-values.yml
```

## Uninstall

The following command allows Clyde to be removed from a Kubernetes cluster.

```
helm delete clyde -n clyde --no-hooks
```


## Test

This test is simplified base on single runs but you can also run jobs and deamonsets to take advantage of Clyde.

### Container Images
On node a: run  ``` pull a new container: docker pull image``` record the time
On node b: pull the same image 

### Huggingface


### Pip
pip install 