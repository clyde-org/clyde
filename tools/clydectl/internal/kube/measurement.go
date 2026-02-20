package kube

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) CreateMeasurementPod(ctx context.Context, image, nodeName, url, authHeader string, durationSec int) (*corev1.Pod, error) {
	podName := measurementPodName(nodeName)

	authArg := ""
	if authHeader != "" {
		authArg = fmt.Sprintf(`-H "Authorization: %s"`, authHeader)
	}
	cmd := fmt.Sprintf(`probe() {
  curl -s -L -o /dev/null --max-time %d --write-out '%%{speed_download} %%{http_code} %%{size_download} %%{time_total}' "$@"
}
OUT=$(probe %s "%s" 2>/dev/null || true)
CODE=$(printf '%%s' "$OUT" | awk '{print $2}')
if [ "$CODE" = "401" ]; then
  CHAL=$(curl -sI --max-time 5 "%s" 2>/dev/null | tr -d '\r' | awk 'tolower($1) ~ /^www-authenticate:/ {sub(/^[^:]*: /,""); print; exit}')
  REALM=$(printf '%%s' "$CHAL" | sed -n 's/.*realm="\([^"]*\)".*/\1/p')
  SERVICE=$(printf '%%s' "$CHAL" | sed -n 's/.*service="\([^"]*\)".*/\1/p')
  SCOPE=$(printf '%%s' "$CHAL" | sed -n 's/.*scope="\([^"]*\)".*/\1/p')
  if [ -n "$REALM" ] && [ -n "$SERVICE" ] && [ -n "$SCOPE" ]; then
    TOKRESP=$(curl -s --max-time 5 "$REALM?service=$SERVICE&scope=$SCOPE" 2>/dev/null || true)
    TOK=$(printf '%%s' "$TOKRESP" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
    [ -n "$TOK" ] || TOK=$(printf '%%s' "$TOKRESP" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
    if [ -n "$TOK" ]; then
      OUT=$(probe -H "Authorization: Bearer $TOK" "%s" 2>/dev/null || true)
    fi
  fi
fi
[ -n "$OUT" ] || OUT='0 000 0 0'
printf '%%s\n' "$OUT"`, durationSec, authArg, url, url, url)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: map[string]string{"clyde-monitoring": "true"},
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "measurement",
					Image:   image,
					Command: []string{"sh", "-c", cmd},
				},
			},
		},
	}
	return c.Clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
}

func (c *Client) CreateMeasurementPods(ctx context.Context, image string, nodes []string, url, authHeader string, durationSec int) ([]*corev1.Pod, error) {
	pods := make([]*corev1.Pod, 0, len(nodes))
	for _, n := range nodes {
		p, err := c.CreateMeasurementPod(ctx, image, n, url, authHeader, durationSec)
		if err != nil {
			return pods, err
		}
		pods = append(pods, p)
	}
	return pods, nil
}

func (c *Client) GetPodLogs(ctx context.Context, podName string) (string, error) {
	val, err := c.Clientset.CoreV1().Pods("default").GetLogs(podName, &corev1.PodLogOptions{}).DoRaw(ctx)
	if err != nil {
		return "", err
	}
	return string(val), nil
}

func (c *Client) DeletePod(ctx context.Context, podName string) error {
	return c.Clientset.CoreV1().Pods("default").Delete(ctx, podName, metav1.DeleteOptions{})
}

func measurementPodName(nodeName string) string {
	sanitized := strings.ToLower(nodeName)
	sanitized = strings.NewReplacer(".", "-", "_", "-", ":", "-").Replace(sanitized)
	const prefix = "clyde-measure-"
	const maxLen = 63
	if len(prefix)+len(sanitized) <= maxLen {
		return prefix + sanitized
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(nodeName))
	suffix := fmt.Sprintf("-%08x", h.Sum32())
	allowed := maxLen - len(prefix) - len(suffix)
	if allowed < 1 {
		allowed = 1
	}
	return prefix + sanitized[:allowed] + suffix
}

