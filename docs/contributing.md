## Development Environment Setup

### IDE

- Install required Go plugin in your IDE preferrably Huawei-VScode (mainly Go Extension)
- Clone the repository
- go mod tidy
- go mod download
- Open the IDE and connect to the cloned project using the remote development plugin

### Kubernetes Cluster

- Make sure you can access the k8s cluster
- Add this to the end of your `~/.bashrc` file `export KUBECONFIG=<PATH-TO-CONFIG-FILE>/yz-p2p.conf`
- Run source ~/.bashrc to load the new config in to the shell or restart the new shell
- In your dev node install kubectl e.g. using `https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/`. Remember to choose the right archtecture..
- Test if kubectl is configured properly by running `kubectl get pods`
