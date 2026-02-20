package seed

import (
	"context"
	"fmt"
	"time"

	"clydectl/internal/kube"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Executor struct {
	client      *kube.Client
	seededNodes map[string]bool
	inFlight    map[string]bool
	podStart    map[string]time.Time
}

type Batch struct {
	Nodes    []string
	PodNames []string
}

type BatchProgress struct {
	Remaining          []string
	CompletedDurations []time.Duration
	FailedCount        int
}

func NewExecutor(c *kube.Client) *Executor {
	return &Executor{
		client:      c,
		seededNodes: make(map[string]bool),
		inFlight:    make(map[string]bool),
		podStart:    make(map[string]time.Time),
	}
}

func (e *Executor) Seed(ctx context.Context, image string, batchSize int) error {
	batch, err := e.StartSeedBatch(ctx, image, batchSize)
	if err != nil {
		return err
	}
	return e.WaitForBatch(ctx, batch)
}

// StartSeedBatch starts image pull pods pinned to unseeded nodes without waiting.
func (e *Executor) StartSeedBatch(ctx context.Context, image string, batchSize int) (Batch, error) {
	if batchSize <= 0 {
		return Batch{}, nil
	}

	targetNodes, err := e.selectUnseededNodes(ctx, batchSize)
	if err != nil {
		return Batch{}, err
	}
	return e.startBatchOnNodes(ctx, image, targetNodes, "clyde-seed", true)
}

// StartMonitorBatch starts pull-probe pods on specific nodes (no seeded-state mutation).
func (e *Executor) StartMonitorBatch(ctx context.Context, image string, nodes []string) (Batch, error) {
	return e.startBatchOnNodes(ctx, image, nodes, "clyde-monitor", false)
}

func (e *Executor) startBatchOnNodes(ctx context.Context, image string, nodes []string, prefix string, trackSeedState bool) (Batch, error) {
	targetNodes := append([]string(nil), nodes...)
	fmt.Printf("Targeting %d specific nodes: %v\n", len(targetNodes), targetNodes)

	var podNames []string
	for _, nodeName := range targetNodes {
		if trackSeedState {
			e.inFlight[nodeName] = true
		}
		labels := map[string]string{"clyde-seeding": "true"}
		if !trackSeedState {
			labels = map[string]string{"clyde-monitoring": "true"}
		}
		pod, err := e.client.CreatePullPod(ctx, image, nodeName, prefix, labels)
		if err != nil {
			if trackSeedState {
				delete(e.inFlight, nodeName)
			}
			return Batch{}, err
		}
		e.podStart[pod.Name] = time.Now()
		podNames = append(podNames, pod.Name)
	}

	return Batch{
		Nodes:    targetNodes,
		PodNames: podNames,
	}, nil
}

// WaitForBatch blocks until all pods in the batch complete successfully.
func (e *Executor) WaitForBatch(ctx context.Context, batch Batch) error {
	remaining := append([]string(nil), batch.PodNames...)
	for len(remaining) > 0 {
		progress, err := e.checkBatchProgressDetailed(ctx, remaining, true)
		if err != nil {
			return err
		}
		remaining = progress.Remaining
		if len(remaining) > 0 {
			time.Sleep(2 * time.Second)
		}
	}
	return nil
}

// CheckBatchProgress polls all provided pods and returns the remaining running/pending pods.
func (e *Executor) CheckBatchProgress(ctx context.Context, podNames []string) ([]string, error) {
	progress, err := e.checkBatchProgressDetailed(ctx, podNames, true)
	if err != nil {
		return nil, err
	}
	return progress.Remaining, nil
}

// CheckBatchProgressDetailed polls pod status and returns remaining pods + completed pull durations.
func (e *Executor) CheckBatchProgressDetailed(ctx context.Context, podNames []string) (BatchProgress, error) {
	return e.checkBatchProgressDetailed(ctx, podNames, true)
}

// CheckMonitorProgressDetailed polls monitor pod status and returns remaining/complete/failed.
func (e *Executor) CheckMonitorProgressDetailed(ctx context.Context, podNames []string) (BatchProgress, error) {
	return e.checkBatchProgressDetailed(ctx, podNames, false)
}

func (e *Executor) checkBatchProgressDetailed(ctx context.Context, podNames []string, markSeeded bool) (BatchProgress, error) {
	remaining := podNames[:0]
	completedDurations := make([]time.Duration, 0)
	failedCount := 0
	for _, name := range podNames {
		p, err := e.client.Clientset.CoreV1().Pods("default").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return BatchProgress{}, err
		}

		if p.Status.Phase == corev1.PodSucceeded {
			if markSeeded {
				fmt.Printf("Node %s seeded successfully.\n", p.Spec.NodeName)
				e.seededNodes[p.Spec.NodeName] = true
				delete(e.inFlight, p.Spec.NodeName)
			}
			completedDurations = append(completedDurations, e.pullDuration(name, p))
			delete(e.podStart, name)
		} else if p.Status.Phase == corev1.PodFailed {
			if markSeeded {
				delete(e.inFlight, p.Spec.NodeName)
			}
			delete(e.podStart, name)
			if markSeeded {
				return BatchProgress{}, fmt.Errorf("seeding failed on node %s", p.Spec.NodeName)
			}
			failedCount++
		} else {
			remaining = append(remaining, name)
		}
	}
	return BatchProgress{
		Remaining:          remaining,
		CompletedDurations: completedDurations,
		FailedCount:        failedCount,
	}, nil
}

func (e *Executor) selectUnseededNodes(ctx context.Context, batchSize int) ([]string, error) {
	allNodes, err := e.client.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	var targetNodes []string
	for _, node := range allNodes {
		if !e.seededNodes[node.Name] && !e.inFlight[node.Name] {
			targetNodes = append(targetNodes, node.Name)
			if len(targetNodes) == batchSize {
				break
			}
		}
	}
	return targetNodes, nil
}

func (e *Executor) pullDuration(podName string, pod *corev1.Pod) time.Duration {
	start, ok := e.podStart[podName]
	if !ok {
		start = time.Now()
	}

	if len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].State.Terminated != nil {
		finished := pod.Status.ContainerStatuses[0].State.Terminated.FinishedAt.Time
		if !finished.IsZero() && finished.After(start) {
			return finished.Sub(start)
		}
	}

	if pod.Status.StartTime != nil && pod.Status.StartTime.Time.After(start) {
		// Use kube start time as better lower-bound when available.
		start = pod.Status.StartTime.Time
	}

	return time.Since(start)
}
