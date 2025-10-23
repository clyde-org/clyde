# Setup, Running and Testing

This documentation details the process of setting up Clyde on a Kubernetes Cluster, running and testing.

## Prerequisite
1. A kubernetes cluster: 1.20+
2. Containerd: 1.50+
3. Helm: 3.80+

## Setting up Clyde

- Once you can connect to kubernetes then you can install Clyde by:
- Make sure helm is installed in your dev machine e.g. for Ubuntu you can follow `https://helm.sh/docs/intro/install/#from-apt-debianubuntu`
- Confirm the values in `clyde-values.yml` you can edit this file based how you can to customise the final system.
- Simply use `helm upgrade --install clyde charts/clyde -f clyde-values.yml` or use the below command
the charts version. 
- To uninstall `helm uninstall clyde -n clyde`. This will remove Clyde


## Test or Running the benchmarks

We build both synthetic and real workload benchmarks, these are available in sir_benchmark folder.
- ```bash cd sir_benchmark```
- source .venv/bin/activate
- Edit the `workload_images.txt` file or create a new workload file and pass it as an argument when running the program using --images_file
- Run the program synthetic workloads
```bash 

    # NO P2P i.e. Baseline
    python deploy_measure.py sequential_results.csv --deploy_type pod --dist_type normal  # one pod at a time
    python deploy_measure.py batch_results.csv --deploy_type daemonset --dist_type normal  # daemonset deployment

    # P2P
    python deploy_measure.py sequential_results.csv --deploy_type pod --dist_type p2p  # one pod at a time
    python deploy_measure.py batch_results.csv --deploy_type daemonset --dist_type p2p # Seed each image in a node
```