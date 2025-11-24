import json
from pathlib import Path
import random
import os
import time
import logging
import argparse
from datetime import datetime
from kubernetes import client, config, watch
from kubernetes.client.rest import ApiException
from filelock import FileLock


def setup_logging(log_file, log_level, log_to_stdout):
    """Set up logging configuration with path resolution."""
    log_level = getattr(logging, log_level.upper(), logging.INFO)
    log_file = os.path.abspath(log_file)  # Ensure absolute path
    
    # Create log directory if it doesn't exist
    log_dir = os.path.dirname(log_file)
    if log_dir and not os.path.exists(log_dir):
        os.makedirs(log_dir, exist_ok=True)
    
    logging.basicConfig(
        level=log_level,
        format='%(asctime)s - %(name)s - %(levelname)s - %(message)s',
        handlers=[]
    )
    
    # File handler
    file_handler = logging.FileHandler(log_file)
    file_handler.setLevel(log_level)
    file_handler.setFormatter(logging.Formatter('%(asctime)s - %(name)s - %(levelname)s - %(message)s'))
    logging.getLogger().addHandler(file_handler)
    
    # Stdout handler if enabled
    if log_to_stdout:
        stdout_handler = logging.StreamHandler()
        stdout_handler.setLevel(log_level)
        stdout_handler.setFormatter(logging.Formatter('%(asctime)s - %(name)s - %(levelname)s - %(message)s'))
        logging.getLogger().addHandler(stdout_handler)

class ClydeSeeder:
    """A Kubernetes utility for managing node seeders and image pre-pulling."""
    
    DEFAULT_CONFIG = {
        'seeder_tracker_file': "seeder_tracker.json",  # Now relative to working dir
        'images_to_seed': "images.txt",               # Now relative to working dir
        'namespace': "clyde",
        'watch_interval': 60,
        'seeder_percentage': 40,
        'log_file': "seeder.log",                    # Now relative to working dir
        'log_level': "INFO",
        'log_to_stdout': True,
        'pull_daemonset_schedule_timeout': 30
    }

    @classmethod
    def create_config(cls, base_dir=None, **overrides):
        """Create configuration with resolved paths."""
        
        cfg = cls.DEFAULT_CONFIG.copy()
        cfg.update(overrides)
        
        if base_dir:
            base_dir = Path(base_dir)
            path_keys = ['seeder_tracker_file', 'images_to_seed', 'log_file']
            for key in path_keys:
                if key in cfg and not os.path.isabs(cfg[key]):
                    cfg[key] = str(base_dir / cfg[key])
        return cfg

    def __init__(self, app_config):
        """Initialize with resolved configuration."""
        self.app_config = app_config
        self.logger = logging.getLogger(self.__class__.__name__)
        
        # Initialize Kubernetes client
        try:
            config.load_config()
            self.core_v1 = client.CoreV1Api()
            self.apps_v1 = client.AppsV1Api()
            self.batch_v1 = client.BatchV1Api()

            # Resolve lock file path relative to tracker file
            tracker_dir = os.path.dirname(self.app_config['seeder_tracker_file'])
            lock_path = os.path.join(tracker_dir, f"{os.path.basename(self.app_config['seeder_tracker_file'])}.lock")
            self.lock = FileLock(lock_path)
            self.logger.info("Kubernetes client initialized successfully")
        except Exception as e:
            self.logger.error(f"Failed to initialize Kubernetes client: {e}")
            raise

    def create_namespace(self):
        """Create namespace if it does not exist"""
        try:
            self.core_v1.read_namespace(name=self.app_config['namespace'])
            self.logger.info(f"Namespace {self.app_config['namespace']} already exists")
        except ApiException as e:
            if e.status == 404:
                ns = client.V1Namespace(metadata=client.V1ObjectMeta(name=self.app_config['namespace']))
                self.core_v1.create_namespace(ns)
                self.logger.info(f"Namespace {self.app_config['namespace']} created")
            else:
                self.logger.error(f"Error checking namespace: {e}")
                raise
        except Exception as e:
            self.logger.error(f"Unexpected error in create_namespace: {e}")
            raise
        return 0

    def _get_optimal_seeders(self):
        """Calculate optimal number of seeders"""
        # Placeholder for future implementation
        return 0

    def _load_current_seeders(self):
        """Load current seeders from tracker file"""
        try:
            with self.lock:
                if os.path.exists(self.app_config['seeder_tracker_file']):
                    self.logger.debug(f"Loading seeders from {self.app_config['seeder_tracker_file']}")
                    with open(self.app_config['seeder_tracker_file'], 'r') as f:
                        content = f.read().strip()
                        if not content:
                            self.logger.debug("Tracker file is empty")
                            return []
                        return json.loads(content)
            return []
        except json.JSONDecodeError as e:
            self.logger.error(f"Error decoding JSON from tracker file: {e}")
            return []
        except Exception as e:
            self.logger.error(f"Error loading seeders: {e}")
            return []

    def _save_seeders(self, node_names):
        """Atomically save seeder list"""
        try:
            with self.lock:
                temp_file = f"{self.app_config['seeder_tracker_file']}.tmp"
                with open(temp_file, 'w') as f:
                    json.dump(node_names, f)
                os.replace(temp_file, self.app_config['seeder_tracker_file'])
                self.logger.debug(f"Saved {len(node_names)} seeders to tracker file")
        except Exception as e:
            self.logger.error(f"Error saving seeders: {e}")
            raise

    def label_seeders(self, seeder_percentage=None):
        """Label nodes with persistence"""
        if seeder_percentage is None:
            seeder_percentage = self.app_config['seeder_percentage']
            
        self.logger.info(f"Starting seeder labeling with percentage: {seeder_percentage}%")
        
        existing_seeders = self._load_current_seeders()
        self.logger.debug(f"Existing seeders: {existing_seeders}")
        
        try:
            nodes = self.core_v1.list_node().items
            self.logger.info(f"Found {len(nodes)} nodes in the cluster")
        except Exception as e:
            self.logger.error(f"Failed to list nodes: {e}")
            raise

        # Validate existing seeders
        healthy_seeders = []
        for node_name in existing_seeders:
            try:
                node = self.core_v1.read_node(node_name)
                if node.metadata.labels.get("clyde-seeder") == "true":
                    healthy_seeders.append(node_name)
                    self.logger.debug(f"Validated existing seeder: {node_name}")
            except ApiException:
                self.logger.warning(f"Seeder node no longer exists: {node_name}")
                continue

        # Calculate needed new seeders
        target_count = max(1, int(len(nodes) * seeder_percentage / 100))
        new_seeders_needed = target_count - len(healthy_seeders)
        
        self.logger.info(f"Target seeders: {target_count}, Current healthy seeders: {len(healthy_seeders)}, New seeders needed: {new_seeders_needed}")

        # Label new seeders if needed
        if new_seeders_needed > 0:
            candidates = [n for n in nodes if n.metadata.name not in healthy_seeders]
            selected_count = min(new_seeders_needed, len(candidates))
            self.logger.info(f"Labeling {selected_count} new seeders from {len(candidates)} candidates")
            
            for node in random.sample(candidates, selected_count):
                patch = {"metadata": {"labels": {"clyde-seeder": "true"}}}
                try:
                    self.core_v1.patch_node(node.metadata.name, patch)
                    healthy_seeders.append(node.metadata.name)
                    self.logger.info(f"Successfully labeled new seeder: {node.metadata.name}")
                except ApiException as e:
                    self.logger.error(f"Error labeling {node.metadata.name}: {e}")

        self._save_seeders(healthy_seeders)
        self.logger.info(f"Current seeders: {healthy_seeders}")
        return healthy_seeders
    
    def _read_image_file(self, image_file):
        """Read images from file with validation."""
        try:
            image_file = os.path.abspath(image_file)  # Ensure absolute path
            self.logger.debug(f"Attempting to read images from: {image_file}")
            
            with open(image_file, 'r') as f:
                images = [line.strip() for line in f if line.strip() and not line.startswith('#')]
                
            if not images:
                error_msg = f"No valid images found in {image_file}"
                self.logger.error(error_msg)
                raise ValueError(error_msg)
            
            self.logger.info(f"Loaded {len(images)} images from {image_file}")
            return images
            
        except FileNotFoundError:
            error_msg = (f"Image file not found: {image_file}\n"
                       f"Current working directory: {os.getcwd()}")
            self.logger.error(error_msg)
            raise RuntimeError(error_msg)
        except Exception as e:
            error_msg = f"Error reading image file: {e}"
            self.logger.error(error_msg)
            raise RuntimeError(error_msg)

    def _create_pull_daemonset(self, image, seeders):
        """Creates a DaemonSet with one pod per seeder node"""
        daemon_set_name = f"pull-{str(abs(hash(image)))[-6:]}"
        self.logger.info(f"Creating DaemonSet for image: {image} (name: {daemon_set_name})")

        labels = {"clyde-job": "image-pull"}

        daemonset = client.V1DaemonSet(
            metadata=client.V1ObjectMeta(
                name=daemon_set_name,
                labels=labels
            ),
            spec=client.V1DaemonSetSpec(
                selector=client.V1LabelSelector(match_labels=labels),
                template=client.V1PodTemplateSpec(
                    metadata=client.V1ObjectMeta(labels=labels),
                    spec=client.V1PodSpec(
                        node_selector={"clyde-seeder": "true"},
                        containers=[client.V1Container(
                            name="puller",
                            image=image,
                            command=["/bin/sh", "-c", f"echo 'Waiting for image {image} to be pulled'; sleep 3600"]
                        )],
                        restart_policy="Always",
                        termination_grace_period_seconds=0
                    )
                ),
                update_strategy=client.V1DaemonSetUpdateStrategy(type="OnDelete")
            )
        )

        try:
            self.apps_v1.create_namespaced_daemon_set(self.app_config['namespace'], daemonset)
            self.logger.info(f"Created Seeder DaemonSet {daemon_set_name} for image {image}")

            # Wait for desired pods to be scheduled
            for _ in range(self.app_config['pull_daemonset_schedule_timeout']):
                try:
                    ds = self.apps_v1.read_namespaced_daemon_set(
                        name=daemon_set_name, 
                        namespace=self.app_config['namespace']
                    )
                    desired_pods = ds.status.desired_number_scheduled

                    if desired_pods > 0:
                        self.logger.info(f"Seeder DaemonSet scheduled on {desired_pods} nodes")
                        break
                    time.sleep(1)
                except Exception as e:
                    self.logger.warning(f"Error checking Seeder DaemonSet status: {e}")
                    time.sleep(1)

            # Monitor until all pods are running
            while True:
                try:
                    pods = self.core_v1.list_namespaced_pod(
                        self.app_config['namespace'], 
                        label_selector="clyde-job=image-pull"
                    )
                    running = sum(1 for pod in pods.items if pod.status.phase == "Running")
                    self.logger.debug(f"Pods running: {running}/{desired_pods}")

                    if desired_pods > 0 and running == desired_pods:
                        self.logger.info("All pods reached 'Running' state")
                        break
                    time.sleep(3)
                except Exception as e:
                    self.logger.warning(f"Error checking pod status: {e}")
                    time.sleep(3)
                
            # Delete the DaemonSet once all pods are running
            self.apps_v1.delete_namespaced_daemon_set(
                daemon_set_name, 
                self.app_config['namespace']
            )
            self.logger.info(f"DaemonSet {daemon_set_name} removed after successful pod creation")
            
        except client.exceptions.ApiException as e:
            self.logger.error(f"Seeder DaemonSet creation failed: {e}")
            raise

    def seed_nodes(self, image_file=None):
        """Deploy image pull jobs to seeders"""
        if image_file is None:
            image_file = self.app_config['images_to_seed']
            
        self.logger.info(f"Starting node seeding with image file: {image_file}")
        
        try:
            images = self._read_image_file(image_file)
            seeders = self._load_current_seeders()
            
            if not seeders:
                error_msg = "No seeders available for seeding"
                self.logger.error(error_msg)
                raise RuntimeError(error_msg)

            self.logger.info(f"Seeding {len(images)} images to {len(seeders)} seeders")
            
            for image in images:
                self.logger.info(f"Processing image: {image}")
                self._create_pull_daemonset(image, seeders)
                
            self.logger.info("Node seeding completed successfully")
                
        except Exception as e:
            self.logger.error(f"Node seeding failed: {e}")
            raise


def main():
    parser = argparse.ArgumentParser(description="Clyde Seeder for Kubernetes")
    
    # Path-related arguments
    parser.add_argument('--base-dir', default=os.getcwd(),
                      help='Base directory for relative paths')
    parser.add_argument('--seeder-tracker-file', 
                      help='Path to seeder tracker file (relative to base-dir)')
    parser.add_argument('--images-to-seed', 
                      help='Path to images list file (relative to base-dir)')
    parser.add_argument('--log-file', 
                      help='Path to log file (relative to base-dir)')
    
    # Other arguments
    parser.add_argument('--namespace', help='Kubernetes namespace to use')
    parser.add_argument('--seeder-percentage', type=int, 
                      help='Percentage of nodes to use as seeders')
    parser.add_argument('--log-level', 
                      choices=['DEBUG', 'INFO', 'WARNING', 'ERROR', 'CRITICAL'],
                      help='Logging level')
    parser.add_argument('--no-stdout', action='store_true',
                      help='Disable logging to stdout')
    
    args = parser.parse_args()
    
    # Create config with resolved paths
    config = ClydeSeeder.create_config(
        base_dir=args.base_dir,
        seeder_tracker_file=args.seeder_tracker_file,
        images_to_seed=args.images_to_seed,
        namespace=args.namespace,
        seeder_percentage=args.seeder_percentage,
        log_file=args.log_file,
        log_level=args.log_level,
        log_to_stdout=not args.no_stdout
    )

    args = parser.parse_args()
    
    
    # Initialize logging
    setup_logging(config['log_file'], config['log_level'], config['log_to_stdout'])
    logger = logging.getLogger('main')
    
    try:
        logger.info("Starting Clyde Seeder")
        logger.debug(f"Configuration: {config}")
        
        # Create and run seeder
        seeder = ClydeSeeder(config)
        
        # Step 1: Create namespace
        seeder.create_namespace()
        
        # Step 2: Label and persist seeders
        seeder.label_seeders()
        
        # Step 3: Preload images
        seeder.seed_nodes()
        
        logger.info("Clyde Seeder completed successfully")
        
    except Exception as e:
        logger.critical(f"Fatal error in Clyde Seeder: {e}", exc_info=True)
        raise

if __name__ == "__main__":
    main()