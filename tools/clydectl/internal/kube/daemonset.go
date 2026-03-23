package kube

import (
	"context"
	"fmt"
	"os"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func (c *Client) DeployDaemonSet(ctx context.Context, name, ns, image string) error {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
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

	_, err := c.Clientset.AppsV1().DaemonSets(ns).Create(ctx, ds, metav1.CreateOptions{})
	return err
}

func (c *Client) DeployDaemonSetFromFile(ctx context.Context, filePath, fallbackNamespace string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read daemonset file %q: %w", filePath, err)
	}

	var ds appsv1.DaemonSet
	if err := yaml.Unmarshal(content, &ds); err != nil {
		return fmt.Errorf("failed to parse daemonset YAML %q: %w", filePath, err)
	}
	if ds.Name == "" {
		return fmt.Errorf("daemonset file %q is missing metadata.name", filePath)
	}
	if ds.Namespace == "" {
		ds.Namespace = fallbackNamespace
	}

	_, err = c.Clientset.AppsV1().DaemonSets(ds.Namespace).Create(ctx, &ds, metav1.CreateOptions{})
	return err
}

func (c *Client) DeployHFModelDaemonSet(ctx context.Context, name, ns, image, cacheDir string, useLocalProxy bool) error {
	if cacheDir == "" {
		cacheDir = "/data/cache/hf/model"
	}

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{
							Key:      "node-role.kubernetes.io/control-plane",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           image,
							ImagePullPolicy: corev1.PullAlways,
							Env: []corev1.EnvVar{
								{
									Name: "NODE_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "status.hostIP",
										},
									},
								},
								{
									Name:  "USE_LOCAL_PROXY",
									Value: strconv.FormatBool(useLocalProxy),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "hf-cache",
									MountPath: cacheDir,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "hf-cache",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: cacheDir,
									Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := c.Clientset.AppsV1().DaemonSets(ns).Create(ctx, ds, metav1.CreateOptions{})
	return err
}
