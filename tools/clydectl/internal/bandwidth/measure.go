package bandwidth

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clydectl/internal/kube"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Metrics struct {
	AvgBandwidthMBps float64
	JitterMS         float64
	DropRatePct      float64
	TotalSamples     int
	FailedSamples    int
}

type Thresholds struct {
	MinBandwidthMBps float64
	MaxJitterMS      float64
	MaxDropRatePct   float64
}

func IsHealthy(metrics Metrics, thresholds Thresholds) bool {
	return metrics.AvgBandwidthMBps >= thresholds.MinBandwidthMBps &&
		metrics.JitterMS <= thresholds.MaxJitterMS &&
		metrics.DropRatePct <= thresholds.MaxDropRatePct
}

// SampleNodesOnce runs one timed transfer sample per node.
func SampleNodesOnce(ctx context.Context, client *kube.Client, nodes []string, image, url, authHeader string, durationSec int) (Metrics, error) {
	if len(nodes) == 0 {
		return Metrics{}, fmt.Errorf("no nodes provided for sampling")
	}

	pods, err := client.CreateMeasurementPods(ctx, image, nodes, url, authHeader, durationSec)
	if err != nil {
		for _, p := range pods {
			_ = client.DeletePod(ctx, p.Name)
		}
		return Metrics{}, fmt.Errorf("failed to create measurement pods: %w", err)
	}

	type sample struct {
		bw      float64
		elapsed time.Duration
		err     error
	}
	ch := make(chan sample, len(pods))
	var wg sync.WaitGroup
	for _, pod := range pods {
		wg.Add(1)
		go func(p *corev1.Pod) {
			defer wg.Done()
			defer client.DeletePod(ctx, p.Name)
			bw, elapsed, err := waitForResult(ctx, client, p.Name)
			ch <- sample{bw: bw, elapsed: elapsed, err: err}
		}(pod)
	}
	wg.Wait()
	close(ch)

	var (
		totalBW      float64
		successCount int
		failCount    int
		elapsedMS    []float64
		failReasons  = make(map[string]int)
	)
	for s := range ch {
		if s.err != nil {
			failCount++
			failReasons[s.err.Error()]++
			continue
		}
		successCount++
		totalBW += s.bw
		elapsedMS = append(elapsedMS, float64(s.elapsed.Milliseconds()))
	}
	total := successCount + failCount
	if total == 0 {
		return Metrics{}, fmt.Errorf("no samples collected")
	}
	if successCount == 0 {
		return Metrics{}, fmt.Errorf("all samples failed: %s", summarizeReasons(failReasons))
	}
	return Metrics{
		AvgBandwidthMBps: totalBW / float64(successCount),
		JitterMS:         stdDev(elapsedMS),
		DropRatePct:      (float64(failCount) / float64(total)) * 100,
		TotalSamples:     total,
		FailedSamples:    failCount,
	}, nil
}

func waitForResult(ctx context.Context, client *kube.Client, podName string) (float64, time.Duration, error) {
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		case <-time.After(1 * time.Second):
			p, err := client.Clientset.CoreV1().Pods("default").Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				return 0, 0, err
			}
			if p.Status.Phase == corev1.PodSucceeded {
				logs, err := client.GetPodLogs(ctx, podName)
				if err != nil {
					return 0, 0, fmt.Errorf("failed to get logs: %w", err)
				}
				fields := strings.Fields(strings.TrimSpace(logs))
				if len(fields) < 4 {
					return 0, 0, fmt.Errorf("invalid measurement output '%s'", strings.TrimSpace(logs))
				}
				bwBytes, err := strconv.ParseFloat(fields[0], 64)
				if err != nil {
					return 0, 0, fmt.Errorf("failed to parse bandwidth '%s': %w", fields[0], err)
				}
				httpCode, err := strconv.Atoi(fields[1])
				if err != nil {
					return 0, 0, fmt.Errorf("failed to parse http_code '%s': %w", fields[1], err)
				}
				sizeDownloaded, err := strconv.ParseFloat(fields[2], 64)
				if err != nil {
					return 0, 0, fmt.Errorf("failed to parse size_download '%s': %w", fields[2], err)
				}
				if httpCode < 200 || httpCode >= 400 {
					return 0, 0, fmt.Errorf("measurement request failed with http_code=%d", httpCode)
				}
				if sizeDownloaded <= 0 {
					return 0, 0, fmt.Errorf("measurement transferred zero bytes")
				}
				return bwBytes / 1024 / 1024, time.Since(start), nil
			}
			if p.Status.Phase == corev1.PodFailed {
				return 0, 0, fmt.Errorf("measurement pod failed")
			}
		}
	}
}

func stdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	var variance float64
	for _, v := range values {
		d := v - mean
		variance += d * d
	}
	variance /= float64(len(values))
	return math.Sqrt(variance)
}

func summarizeReasons(reasons map[string]int) string {
	if len(reasons) == 0 {
		return "unknown error"
	}
	type pair struct {
		msg   string
		count int
	}
	items := make([]pair, 0, len(reasons))
	for m, c := range reasons {
		items = append(items, pair{msg: m, count: c})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].msg < items[j].msg
		}
		return items[i].count > items[j].count
	})
	limit := 3
	if len(items) < limit {
		limit = len(items)
	}
	parts := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		parts = append(parts, fmt.Sprintf("%dx %s", items[i].count, items[i].msg))
	}
	return strings.Join(parts, "; ")
}

