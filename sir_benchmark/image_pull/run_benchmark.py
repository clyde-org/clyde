import time
import yaml
import argparse
from kubernetes import client, config, utils
from kubernetes.client.rest import ApiException

NAMESPACE = "clyde"

def parse_args():
    parser = argparse.ArgumentParser(description="Deploy and monitor Kubernetes resources")
    parser.add_argument(
        '--kind', choices=['pod', 'daemonset', 'job'], default='pod', 
        help="Specify whether to deploy as a Pod, DaemonSet, or Job"
    )
    return parser.parse_args()

def get_resource_name_from_yaml(kind):
    yaml_map = {
        'pod': "llama.yaml",
        'daemonset': "llama_daemonset.yaml",
        # 'job': "llama_job.yaml",
        'job': "deepseek_r1_distill_llama_job.yaml"
    }
    yaml_file = yaml_map[kind]
    with open(yaml_file, "r") as f:
        docs = list(yaml.safe_load_all(f))
        for doc in docs:
            if doc and doc.get("kind", "").lower() == kind:
                metadata = doc.get("metadata", {})
                return metadata.get("name"), yaml_file
    raise ValueError(f"No {kind} kind with metadata.name found in YAML.")

def deploy_workload(api_client, yaml_file):
    with open(yaml_file, "r") as f:
        docs = list(yaml.safe_load_all(f))
    print(f"Deploying workload from: {yaml_file}")
    for doc in docs:
        try:
            utils.create_from_dict(api_client, data=doc, namespace=NAMESPACE)
        except ApiException as e:
            print("Failed to create resource:", e.body)

def wait_for_phase(v1, apps_v1, batch_v1, resource_name, target_phase, kind, expected_count=None):
    print(f"Waiting for {kind} '{resource_name}' to reach '{target_phase}' phase...")
    start_time = time.time()

    while True:
        try:
            if kind == 'pod':
                resource = v1.read_namespaced_pod(name=resource_name, namespace=NAMESPACE)
                current_phase = resource.status.phase
                print(f"Current pod phase: {current_phase}")
                if current_phase == target_phase:
                    return time.time() - start_time
                elif current_phase == "Failed":
                    raise RuntimeError("Pod failed to start.")

            elif kind == 'daemonset':
                resource = apps_v1.read_namespaced_daemon_set(name=resource_name, namespace=NAMESPACE)
                desired = resource.status.desired_number_scheduled
                ready = resource.status.number_ready
                print(f"DaemonSet status: {ready}/{desired} pods ready")
                if target_phase == "Running" and ready == desired:
                    return time.time() - start_time

            elif kind == 'job':
                job = batch_v1.read_namespaced_job(name=resource_name, namespace=NAMESPACE)
                succeeded = job.status.succeeded or 0
                failed = job.status.failed or 0
                desired = job.spec.completions or 1

                print(f"Job progress: {succeeded}/{desired} succeeded, {failed} failed")

                if target_phase == "Running":
                    # Count running pods (transitional state before success)
                    pods = v1.list_namespaced_pod(namespace=NAMESPACE, label_selector=f"job-name={resource_name}").items
                    running = sum(1 for pod in pods if pod.status.phase in ["Running", "Succeeded"])
                    expected = expected_count or desired
                    print(f"Running pods: {running}/{expected}")
                    if running >= expected:
                        return time.time() - start_time

                elif target_phase == "Succeeded" and succeeded >= desired:
                    return time.time() - start_time

                if failed > 0:
                    raise RuntimeError(f"Job '{resource_name}' failed with {failed} failed pods.")

        except ApiException as e:
            if e.status == 404:
                print(f"{kind.capitalize()} not found yet...")
            else:
                print(f"Error reading {kind}: {e}")
        time.sleep(2)



def main():
    args = parse_args()
    config.load_kube_config()

    api_client = client.ApiClient()
    v1 = client.CoreV1Api()
    apps_v1 = client.AppsV1Api()
    batch_v1 = client.BatchV1Api()

    resource_name, yaml_file = get_resource_name_from_yaml(args.kind)

    end_to_end_start = time.time()
    deploy_workload(api_client, yaml_file)

    if args.kind in ['pod', 'daemonset', 'job']:
        expected_count = 28 if args.kind == "job" else None  # Set the number of job pods expected
        time_to_running = wait_for_phase(v1, apps_v1, batch_v1, resource_name, "Running", args.kind, expected_count)


    if args.kind == 'pod':
        total_time = wait_for_phase(v1, apps_v1, batch_v1, resource_name, "Succeeded", args.kind)
    elif args.kind == 'job':
        total_time = wait_for_phase(v1, apps_v1, batch_v1, resource_name, "Succeeded", args.kind)
    else:
        total_time = time_to_running

    end_to_end_duration = time.time() - end_to_end_start

    print(f"\nTime to reach 'Running': {time_to_running:.2f} seconds")
    if args.kind in ['pod', 'job']:
        print(f"Total time until 'Succeeded': {total_time:.2f} seconds")
    print(f"End-to-end time: {end_to_end_duration:.2f} seconds")

if __name__ == "__main__":
    main()
