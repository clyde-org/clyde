# Documentation

Apull is a high-performance container image loading solution that combines kernel EROFS filesystem with fscache for bootstrap loading and userspace processing via apulld to enable lazy loading. This architecture aims to provide faster container startup times, improved network bandwidth efficiency, reduced storage footprint, and built-in data integrity verification.

## System Requirements
- Linux Kernel >= 5.10
- Kernel: EROFS over fscache support
- containerd ≥ 1.6.14
- Registry: Register-v2 protocol

## Kubernetes Integration

This guide follows the complete process to deploy apull-stack in Kubernetes. For simplicity we provide [helm charts available available at](/apull-snapshotter/)

### Helper Script Execution

Once the compilation is completed, binaries are generated. These two binaries should be moved to /usr/bin/ of each node. We provided a helper script to do this

```bash
# If you are using the tool edit the tools/servers.txt file to match your servers. Then execute

./tools/sync_bin.sh

```

### Check Status of EROFS

```bash
# Check if module is loaded
lsmod | grep erofs

# Check current activation status
cat /sys/module/fs_ctl/parameters/erofs_enabled
cat /sys/module/fs_ctl/parameters/cachefiles_ondemand_enabled
```

• **If both show 'Y'**: Configuration complete  
• **Otherwise**: Proceed below  

### Mount ISO

If EROFS is missing, please execute the following:

```bash
mkdir -p /media/iso
mount -o loop EulerOS-V2.0SP13-aarch64-dvd.iso /media/iso
```

### Upgrade Supported Kernel

```bash
rpm -ivh /media/iso/kernel-5.10.0-182.0.0.95.h2826.eulerosv2r13.aarch64.rpm
```

### Enable Backend Modules

```bash
echo 1 > /sys/module/fs_ctl/parameters/erofs_enabled
echo 1 > /sys/module/fs_ctl/parameters/cachefiles_ondemand_enabled
```

### Verification

```bash
cat /sys/module/fs_ctl/parameters/erofs_enabled
cat /sys/module/fs_ctl/parameters/cachefiles_ondemand_enabled
```

### Helm Setup

The helm charts automate the installation of apull and its stack starting all relevant services. 

```bash
helm install apull-snapshotter ./apull-snapshotter --namespace apull-install --create-namespace

# Check if the deployment pods are running. If so after a few seconds the installation should be completed.

kubectl get pods -n apull-install

NAME                            READY   STATUS    RESTARTS      AGE
apull-snapshotter-setup-22dfw   1/1     Running   1 (46m ago)   15h
apull-snapshotter-setup-2vvj7   1/1     Running   1 (46m ago)   15h
apull-snapshotter-setup-5dpgk   1/1     Running   1 (46m ago)   15h
apull-snapshotter-setup-89cbm   1/1     Running   1 (45m ago)   15h
apull-snapshotter-setup-b879z   1/1     Running   1 (46m ago)   15h
apull-snapshotter-setup-nhbcj   1/1     Running   1 (53m ago)   15h
apull-snapshotter-setup-rq7cs   1/1     Running   1 (46m ago)   15h
apull-snapshotter-setup-z84wl   1/1     Running   1 (46m ago)   15h

# Verify the installation by checking the status of snapshotter service

sudo systemctl status apull-snapshotter
```

## Image Conversion

```bash
sudo apull-image-build convert --oci --oci-ref --source 7.212.124.4:30443/p2p/nvidia-cuda-12.2.0-devel-ubuntu20.04:v1 --target 7.212.124.4:30443/p2p/nvidia-cuda-12.2.0-devel-ubuntu20.04:v1-ref --source-insecure --target-insecure
```

## Testing

```yaml
# Copy and paste this into a file and run using e.g. apull-test.yaml

  apiVersion: apps/v1
  kind: DaemonSet
  metadata:
    name: cuda-minimal
    namespace: default
  spec:
    selector:
      matchLabels:
        app: cuda-minimal
    template:
      metadata:
        labels:
          app: cuda-minimal
      spec:
        # Allow running on master nodes if needed
        tolerations:
        - key: node-role.kubernetes.io/control-plane
          operator: Exists
          effect: NoSchedule
        
        containers:
        - name: cuda
          image: 7.212.124.4:30443/p2p/nvidia-cuda-12.2.0-devel-ubuntu20.04:v1
          command: ["/bin/sh", "-c"]
          args: ["while true; do sleep 86400; done"]  # Sleep for 24 hours in a loop

# Run using

kubectl create -f filename.yaml
```

## Apull Deployment Automation

The instructions provided earlier can be automated using a number of scripts that have been put in place. Please follow these instructions:

Firstly, update the configuration file under `apull-snapshotter/config.txt` by updating the `SSH_USER`, `SSH_PASSWORD`, and `KUBECONFIG_PATH` corresponding to the username and password of the Kubernetes nodes and the path of the KUBECTL configuration file respectively.

Secondly, execute the following commands:

```
cd apull-snapshotter
make all
```

This will clean up the Kubernetes cluster, update relevant binaries and configuration files on all nodes within the cluster and install a helm deployment for starting the Apull daemon on all the nodes accordingly.



## Reference Documentation
