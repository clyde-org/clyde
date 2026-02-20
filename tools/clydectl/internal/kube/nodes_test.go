package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestNodeHasPublicExternalIP(t *testing.T) {
	publicNode := corev1.Node{
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.10"},
				{Type: corev1.NodeExternalIP, Address: "34.86.1.2"},
			},
		},
	}
	privateOnlyNode := corev1.Node{
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.11"},
				{Type: corev1.NodeExternalIP, Address: "192.168.1.40"},
			},
		},
	}

	if !NodeHasPublicExternalIP(publicNode) {
		t.Fatalf("expected public node to be public-capable")
	}
	if NodeHasPublicExternalIP(privateOnlyNode) {
		t.Fatalf("expected private-only node to not be public-capable")
	}
}

func TestClusterHasOnlyPublicNodes(t *testing.T) {
	publicNode := corev1.Node{
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeExternalIP, Address: "34.100.1.1"},
			},
		},
	}
	privateNode := corev1.Node{
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
			},
		},
	}

	if !ClusterHasOnlyPublicNodes([]corev1.Node{publicNode}) {
		t.Fatalf("single public node cluster should be public")
	}
	if ClusterHasOnlyPublicNodes([]corev1.Node{publicNode, privateNode}) {
		t.Fatalf("mixed cluster should not be considered fully public")
	}
}

