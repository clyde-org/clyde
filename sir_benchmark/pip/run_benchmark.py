import time
import yaml
from kubernetes import client, config, utils
from kubernetes.client.rest import ApiException

NAMESPACE = "clyde"

def get_job_name_from_yaml():
    yaml_file = "pip_job.yaml"
    with open(yaml_file, "r") as f:
        docs = list(yaml.safe_load_all(f))
        for doc in docs:
            if doc and doc.get("kind", "").lower() == "job":
                metadata = doc.get("metadata", {})
                return metadata.get("name"), yaml_file
    raise ValueError("No Job resource with metadata.name found in YAML.")

def deploy_job(api_client, yaml_file):
    with open(yaml_file, "r") as f:
        docs = list(yaml.safe_load_all(f))
    print(f"Deploying job from: {yaml_file}")
    for doc in docs:
        try:
            utils.create_from_dict(api_client, data=doc, namespace=NAMESPACE)
        except ApiException as e:
            print("Failed to create resource:", e.body)

def wait_for_job_phase(v1, batch_v1, job_name, target_phase):
    print(f"Waiting for Job '{job_name}' to reach '{target_phase}' phase...")
    start_time = time.time()

    while True:
        try:
            job = batch_v1.read_namespaced_job(name=job_name, namespace=NAMESPACE)

            # Current status
            succeeded = job.status.succeeded or 0
            failed = job.status.failed or 0
            completions = (job.spec.completions or 1)
            parallelism = (job.spec.parallelism or completions)
            print(f"Job progress: {succeeded}/{completions} succeeded, {failed} failed")

            if target_phase == "Running":
                pods = v1.list_namespaced_pod(
                    namespace=NAMESPACE, label_selector=f"job-name={job_name}"
                ).items
                running = sum(1 for pod in pods if pod.status.phase in ["Running", "Succeeded"])
                expected_running = parallelism
                print(f"Running pods: {running}/{expected_running}")
                if running >= expected_running:
                    return time.time() - start_time

            elif target_phase == "Succeeded":
                if succeeded >= completions:
                    return time.time() - start_time

            # Fail fast if any failures are recorded
            if failed and failed > 0:
                raise RuntimeError(f"Job '{job_name}' failed with {failed} failed pods.")

        except ApiException as e:
            if e.status == 404:
                print("Job not found yet...")
            else:
                print(f"Error reading Job: {e}")

        time.sleep(2)

def main():
    config.load_kube_config()
    api_client = client.ApiClient()
    v1 = client.CoreV1Api()
    batch_v1 = client.BatchV1Api()

    job_name, yaml_file = get_job_name_from_yaml()

    end_to_end_start = time.time()
    deploy_job(api_client, yaml_file)

    time_to_running = wait_for_job_phase(v1, batch_v1, job_name, "Running")
    total_time = wait_for_job_phase(v1, batch_v1, job_name, "Succeeded")

    end_to_end_duration = time.time() - end_to_end_start

    print(f"\nTime to reach 'Running': {time_to_running:.2f} seconds")
    print(f"Total time until 'Succeeded': {total_time:.2f} seconds")
    print(f"End-to-end time: {end_to_end_duration:.2f} seconds")

if __name__ == "__main__":
    main()
