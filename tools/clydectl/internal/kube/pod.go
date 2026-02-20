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
	podName := pullPodName(prefix, nodeName)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName, // Force pod to this specific node.
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "puller",
					Image:   image,
					Command: []string{"sh", "-c", "echo Pulled"},
				},
			},
		},
	}

	return c.Clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
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
