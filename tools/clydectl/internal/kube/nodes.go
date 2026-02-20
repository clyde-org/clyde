package kube

import (
	"context"
	"net"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1" // Required for ListOptions
)

func (c *Client) ListNodes(ctx context.Context) ([]corev1.Node, error) {
	// Fixed: Use metav1.ListOptions{} instead of v1.ListOptions{}
	list, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// NodeExternalIPs returns all ExternalIP addresses attached to a node.
func NodeExternalIPs(node corev1.Node) []string {
	externalIPs := make([]string, 0)
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeExternalIP && addr.Address != "" {
			externalIPs = append(externalIPs, addr.Address)
		}
	}
	return externalIPs
}

// IsPrivateIP returns true if an IP is loopback/link-local/private.
func IsPrivateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}

	return parsed.IsLoopback() ||
		parsed.IsLinkLocalMulticast() ||
		parsed.IsLinkLocalUnicast() ||
		parsed.IsPrivate()
}

// NodeHasPublicExternalIP returns true when at least one ExternalIP is public.
func NodeHasPublicExternalIP(node corev1.Node) bool {
	for _, ip := range NodeExternalIPs(node) {
		if !IsPrivateIP(ip) {
			return true
		}
	}
	return false
}

// ClusterHasOnlyPublicNodes returns true when every node has a public ExternalIP.
func ClusterHasOnlyPublicNodes(nodes []corev1.Node) bool {
	if len(nodes) == 0 {
		return false
	}

	for _, node := range nodes {
		if !NodeHasPublicExternalIP(node) {
			return false
		}
	}
	return true
}
