package kube

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) CreateSeedPod(ctx context.Context, image string, nodeName string) (*corev1.Pod, error) {
	return c.CreatePullPod(ctx, image, nodeName, "clyde-seed", map[string]string{"clyde-seeding": "true"})
}

func (c *Client) CreatePullPod(ctx context.Context, image string, nodeName string, prefix string, labels map[string]string) (*corev1.Pod, error) {
	container := corev1.Container{
		Name:    "puller",
		Image:   image,
		Command: []string{"sh", "-c", "echo Pulled"},
	}
	return c.createNodePinnedPod(ctx, nodeName, prefix, labels, container, nil)
}

func (c *Client) CreateHFModelSeedPod(ctx context.Context, modelID, cacheDir, nodeName, prefix string, labels map[string]string) (*corev1.Pod, error) {
	if cacheDir == "" {
		cacheDir = "/data/cache/hf/model"
	}
	container := corev1.Container{
		Name:  "hf-model-seeder",
		Image: "python:3.10-slim",
		Command: []string{
			"sh",
			"-c",
			"python3 -m pip install --no-cache-dir huggingface_hub && python3 - <<'PY'\nfrom huggingface_hub import snapshot_download\nimport os\nrepo_id = os.environ['HF_MODEL_ID']\ncache_dir = os.environ['HF_CACHE_DIR']\nsnapshot_download(repo_id=repo_id, cache_dir=cache_dir, force_download=True)\nprint(f'Seeded {repo_id} into {cache_dir}')\nPY",
		},
		Env: []corev1.EnvVar{
			{Name: "HF_MODEL_ID", Value: modelID},
			{Name: "HF_CACHE_DIR", Value: cacheDir},
			{Name: "HF_HUB_CACHE", Value: cacheDir},
			{Name: "HF_HUB_DISABLE_XET", Value: "1"},
			{Name: "HF_HUB_DOWNLOAD_TIMEOUT", Value: "600"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "hf-cache",
				MountPath: cacheDir,
			},
		},
	}
	volumes := []corev1.Volume{
		{
			Name: "hf-cache",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: cacheDir,
					Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
	}
	return c.createNodePinnedPod(ctx, nodeName, prefix, labels, container, volumes)
}

func (c *Client) createNodePinnedPod(ctx context.Context, nodeName string, prefix string, labels map[string]string, container corev1.Container, volumes []corev1.Volume) (*corev1.Pod, error) {
	podName := pullPodName(prefix, nodeName)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName, // Force pod to this specific node.
			RestartPolicy: corev1.RestartPolicyNever,
			Containers:    []corev1.Container{container},
			Volumes:       volumes,
		},
	}

	return c.Clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
}

func hostPathTypePtr(t corev1.HostPathType) *corev1.HostPathType {
	return &t
}

func pullPodName(prefix string, nodeName string) string {
	sanitized := strings.ToLower(nodeName)
	sanitized = strings.NewReplacer(".", "-", "_", "-", ":", "-").Replace(sanitized)
	const maxLen = 63

	if len(prefix)+1+len(sanitized) <= maxLen {
		return fmt.Sprintf("%s-%s", prefix, sanitized)
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(nodeName))
	suffix := fmt.Sprintf("-%08x", h.Sum32())
	allowed := maxLen - len(prefix) - 1 - len(suffix)
	if allowed < 1 {
		allowed = 1
	}
	return fmt.Sprintf("%s-%s%s", prefix, sanitized[:allowed], suffix)
}
