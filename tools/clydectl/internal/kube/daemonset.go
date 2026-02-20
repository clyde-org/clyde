package kube

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1" // Add this
)

func (c *Client) DeployDaemonSet(ctx context.Context, name, ns, image string) error {
	fmt.Print()
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{ // Fixed: metav1
			Name:      name,
			Namespace: ns,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{ // Fixed: metav1
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{ // Fixed: metav1
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{ // Fixed: corev1
					Containers: []corev1.Container{
						{
							Name:  name,
							Image: image,
						},
					},
				},
			},
		},
	}

	// Fixed: metav1.CreateOptions
	_, err := c.Clientset.AppsV1().DaemonSets(ns).Create(ctx, ds, metav1.CreateOptions{})
	return err
}
