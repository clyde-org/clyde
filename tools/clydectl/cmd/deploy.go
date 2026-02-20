package cmd

import (
	"context"
	"fmt"
	"time"

	"clydectl/internal/bandwidth"
	"clydectl/internal/kube"
	"clydectl/internal/registry"
	"clydectl/internal/seed"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
)

var deployCmd = &cobra.Command{
	Use:   "daemonset",
	Short: "Seed image before deploying a DaemonSet",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		image, _ := cmd.Flags().GetString("image")
		name, _ := cmd.Flags().GetString("name")
		namespace, _ := cmd.Flags().GetString("namespace")

		initialSeeds, _ := cmd.Flags().GetInt("initial-seeds")
		publicSeeds, _ := cmd.Flags().GetInt("public-seeds")
		disableBandwidthAware, _ := cmd.Flags().GetBool("disable-bandwidth-aware")
		monitorInterval, _ := cmd.Flags().GetInt("monitor-interval")
		monitorWindow, _ := cmd.Flags().GetInt("monitor-window")
		monitorBandwidth, _ := cmd.Flags().GetFloat64("monitor-bandwidth-threshold")
		monitorJitter, _ := cmd.Flags().GetFloat64("monitor-jitter-threshold")
		monitorDrop, _ := cmd.Flags().GetFloat64("monitor-drop-threshold")
		monitorImage, _ := cmd.Flags().GetString("monitor-image")

		client, err := kube.New()
		if err != nil {
			return err
		}

		nodes, err := client.ListNodes(ctx)
		if err != nil {
			return err
		}

		totalNodes := len(nodes)
		if totalNodes == 0 {
			return fmt.Errorf("no nodes found in cluster")
		}
		seedTargetNodes := (totalNodes + 1) / 2 // at least half of cluster nodes

		executor := seed.NewExecutor(client)
		if disableBandwidthAware {
			fmt.Printf("Bandwidth-aware mode disabled. Running classic doubling seeding to %d/%d nodes.\n", seedTargetNodes, totalNodes)
			planner := seed.NewDoublingPlanner(seedTargetNodes, initialSeeds)
			wave := 1
			for planner.HasNext() {
				batchSize := planner.NextBatch()
				fmt.Printf("--- Wave %d: Seeding %d nodes ---\n", wave, batchSize)
				if err := executor.Seed(ctx, image, batchSize); err != nil {
					return fmt.Errorf("failed during wave %d: %w", wave, err)
				}
				wave++
			}
			fmt.Printf("Success: Seeded %d/%d nodes. Deploying final DaemonSet...\n", seedTargetNodes, totalNodes)
			return client.DeployDaemonSet(ctx, name, namespace, image)
		}

		if kube.ClusterHasOnlyPublicNodes(nodes) {
			fmt.Println("Detected public-capable cluster network. Using simplified source-pull path.")
			if publicSeeds > 0 {
				fmt.Printf("Seeding %d public nodes from source before deploy...\n", publicSeeds)
				if err := executor.Seed(ctx, image, publicSeeds); err != nil {
					return fmt.Errorf("public seed phase failed: %w", err)
				}
			}
			fmt.Println("Deploying DaemonSet...")
			return client.DeployDaemonSet(ctx, name, namespace, image)
		}

		fmt.Println("Detected private/NAT cluster network. Starting monitored seed strategy...")

		regClient := registry.NewClient()
		imageSizeBytes, err := regClient.ResolveImageSize(ctx, image)
		if err != nil {
			return fmt.Errorf("failed to resolve image size for monitoring: %w", err)
		}
		fmt.Printf("Target image size: %.2f MB\n", float64(imageSizeBytes)/1024.0/1024.0)

		if monitorImage == "" {
			monitorImage = image
		}
		probeURL, authHeader, layerSizeBytes, err := regClient.ResolveLayer(ctx, monitorImage)
		if err != nil {
			fmt.Printf("Warning: failed to resolve monitor image '%s' (%v). Falling back to target image.\n", monitorImage, err)
			monitorImage = image
			probeURL, authHeader, layerSizeBytes, err = regClient.ResolveLayer(ctx, monitorImage)
			if err != nil {
				return fmt.Errorf("failed to resolve fallback monitor target: %w", err)
			}
		}
		fmt.Printf("Monitor image: %s (probe layer: %.2f MB)\n", monitorImage, float64(layerSizeBytes)/1024.0/1024.0)
		fmt.Printf("Monitor probe target: %s\n", probeURL)
		if authHeader != "" {
			fmt.Println("Monitor probe auth header: present")
		} else {
			fmt.Println("Monitor probe auth header: absent")
		}

		fmt.Printf("Seeding target: %d/%d nodes (half-cluster rollout)\n", seedTargetNodes, totalNodes)
		planner := seed.NewDoublingPlanner(seedTargetNodes, initialSeeds)

		initialBatchSize := planner.NextBatch()
		fmt.Printf("Initial seed batch: %d nodes\n", initialBatchSize)
		initialBatch, err := executor.StartSeedBatch(ctx, image, initialBatchSize)
		if err != nil {
			return fmt.Errorf("initial seed phase failed: %w", err)
		}

		remainingInitial := append([]string(nil), initialBatch.PodNames...)
		monitorNodes := selectMonitorNodes(nodes, initialBatch.Nodes, len(initialBatch.Nodes))
		if len(monitorNodes) == 0 {
			monitorNodes = append([]string(nil), initialBatch.Nodes...)
			fmt.Println("Monitor node fallback: no non-seed nodes available, using initial seed nodes for monitoring.")
		}
		fmt.Printf("Monitor nodes: %v\n", monitorNodes)
		monitorCurlImage := "curlimages/curl:latest"
		agg := monitorAggregate{}
		monitorStart := time.Now()
		monitoringStopped := false
		decisionAttempts := 0
		nextDecisionAt := monitorStart.Add(time.Duration(monitorWindow) * time.Second)
		remainingWave := append([]string(nil), remainingInitial...)
		wave := 1
		runMonitorTick := func(remainingSeedPods int) error {
			if !planner.HasNext() && remainingSeedPods == 0 {
				monitoringStopped = true
				return nil
			}
			if monitoringStopped {
				return nil
			}
			sample, sampleErr := bandwidth.SampleNodesOnce(
				ctx,
				client,
				monitorNodes,
				monitorCurlImage,
				probeURL,
				authHeader,
				monitorInterval,
			)
			if sampleErr != nil {
				agg.addFailureBatch(len(monitorNodes))
				fmt.Printf("Monitor tick => elapsed=%ds sample_error=%v total_samples=%d total_failures=%d remaining_wave=%d\n",
					int(time.Since(monitorStart).Seconds()), sampleErr, agg.totalSamples, agg.failedSamples, remainingSeedPods)
			} else {
				agg.add(sample)
				fmt.Printf("Monitor tick => elapsed=%ds new_samples=%d new_failures=%d total_samples=%d total_failures=%d remaining_wave=%d\n",
					int(time.Since(monitorStart).Seconds()),
					sample.TotalSamples-sample.FailedSamples,
					sample.FailedSamples,
					agg.totalSamples,
					agg.failedSamples,
					remainingSeedPods,
				)
				metrics := agg.snapshot()
				fmt.Printf("Monitor sample => bandwidth=%.2f MB/s jitter=%.2f ms drop=%.2f%%\n",
					metrics.AvgBandwidthMBps, metrics.JitterMS, metrics.DropRatePct)
				fmt.Printf("Monitor details => layer=%.2f MB samples=%d\n",
					float64(layerSizeBytes)/1024.0/1024.0, metrics.TotalSamples)
			}
			fmt.Println()

			now := time.Now()
			if now.After(nextDecisionAt) || now.Equal(nextDecisionAt) {
				metrics := agg.snapshot()
				decisionAttempts++
				fmt.Printf("Monitor decision %d at +%ds => bandwidth=%.2f MB/s jitter=%.2f ms drop=%.2f%% samples=%d\n",
					decisionAttempts, int(now.Sub(monitorStart).Seconds()),
					metrics.AvgBandwidthMBps, metrics.JitterMS, metrics.DropRatePct, metrics.TotalSamples)
				thresholds := bandwidth.Thresholds{
					MinBandwidthMBps: monitorBandwidth,
					MaxJitterMS:      monitorJitter,
					MaxDropRatePct:   monitorDrop,
				}
				if !bandwidth.IsHealthy(metrics, thresholds) {
					fmt.Println("Decision: quality not healthy. Stopping monitoring and continuing classic doubling seeding.")
					monitoringStopped = true
				} else {
						if planner.HasNext() {
							nextBatch := planner.NextBatch()
							fmt.Printf("Decision: quality healthy. Starting monitored doubling batch of %d nodes (remaining=%d)\n", nextBatch, planner.Remaining())
							batch, err := executor.StartSeedBatch(ctx, image, nextBatch)
							if err != nil {
								return fmt.Errorf("failed to start monitored doubling batch: %w", err)
							}
							remainingWave = append(remainingWave, batch.PodNames...)
						} else {
							fmt.Println("Decision: quality healthy. Seeding target already reached.")
							monitoringStopped = true
						}
				}
				fmt.Println()
				nextDecisionAt = nextDecisionAt.Add(time.Duration(monitorWindow) * time.Second)
			}
			return nil
		}

		for len(remainingWave) > 0 {
			progress, err := executor.CheckBatchProgressDetailed(ctx, remainingWave)
			if err != nil {
				return err
			}
			remainingWave = progress.Remaining
			if err := runMonitorTick(len(remainingWave)); err != nil {
				return err
			}
			if len(remainingWave) > 0 {
				time.Sleep(time.Duration(monitorInterval) * time.Second)
			}
		}

		wave++
		for planner.HasNext() {
			batchSize := planner.NextBatch()
			fmt.Printf("--- Wave %d: Seeding %d nodes ---\n", wave, batchSize)
			batch, err := executor.StartSeedBatch(ctx, image, batchSize)
			if err != nil {
				return fmt.Errorf("failed starting wave %d: %w", wave, err)
			}
			remainingWave = append([]string(nil), batch.PodNames...)
			for len(remainingWave) > 0 {
				progress, err := executor.CheckBatchProgressDetailed(ctx, remainingWave)
				if err != nil {
					return fmt.Errorf("failed during wave %d: %w", wave, err)
				}
				remainingWave = progress.Remaining
				if err := runMonitorTick(len(remainingWave)); err != nil {
					return err
				}
				if len(remainingWave) > 0 {
					time.Sleep(time.Duration(monitorInterval) * time.Second)
				}
			}
			wave++
		}

		fmt.Printf("Seeding target reached (%d/%d nodes). Deploying final DaemonSet...\n", seedTargetNodes, totalNodes)
		return client.DeployDaemonSet(ctx, name, namespace, image)
	},
}

func init() {
	deployCmd.Flags().String("image", "", "Container image to deploy")
	deployCmd.Flags().String("name", "", "DaemonSet name")
	deployCmd.Flags().String("namespace", "default", "Namespace")

	deployCmd.Flags().Int("initial-seeds", 0, "Number of initial seed nodes (default: 10% of cluster)")
	deployCmd.Flags().Int("public-seeds", 0, "When nodes have public IPs, pre-seed this many nodes before direct deployment")
	deployCmd.Flags().Bool("disable-bandwidth-aware", false, "Disable monitoring and run classic doubling seeding strategy")
	deployCmd.Flags().Int("monitor-interval", 2, "Private-path monitoring interval in seconds while initial seeds are pulling")
	deployCmd.Flags().Int("monitor-window", 20, "Minimum monitoring window in seconds before first expansion decision")
	deployCmd.Flags().Float64("monitor-bandwidth-threshold", 50.0, "Minimum monitored bandwidth (MB/s) required to expand seed count")
	deployCmd.Flags().Float64("monitor-jitter-threshold", 20.0, "Maximum monitored jitter (ms) allowed to expand seed count")
	deployCmd.Flags().Float64("monitor-drop-threshold", 1.0, "Maximum monitored drop rate (%) allowed to expand seed count")
	deployCmd.Flags().String("monitor-image", "", "Optional image used for progress-based monitor probing (default: --image)")

	deployCmd.MarkFlagRequired("image")
	deployCmd.MarkFlagRequired("name")
}

type monitorAggregate struct {
	successBWTotal float64
	successSamples int
	failedSamples  int
	jitterValues   []float64
	totalSamples   int
}

func (a *monitorAggregate) add(m bandwidth.Metrics) {
	success := m.TotalSamples - m.FailedSamples
	if success > 0 {
		a.successBWTotal += m.AvgBandwidthMBps * float64(success)
		a.successSamples += success
		a.jitterValues = append(a.jitterValues, m.JitterMS)
	}
	a.failedSamples += m.FailedSamples
	a.totalSamples += m.TotalSamples
}

func (a *monitorAggregate) addFailureBatch(size int) {
	if size <= 0 {
		return
	}
	a.failedSamples += size
	a.totalSamples += size
}

func (a *monitorAggregate) snapshot() bandwidth.Metrics {
	var avgBW float64
	if a.successSamples > 0 {
		avgBW = a.successBWTotal / float64(a.successSamples)
	}
	var avgJitter float64
	if len(a.jitterValues) > 0 {
		var sum float64
		for _, v := range a.jitterValues {
			sum += v
		}
		avgJitter = sum / float64(len(a.jitterValues))
	}
	drop := 0.0
	if a.totalSamples > 0 {
		drop = (float64(a.failedSamples) / float64(a.totalSamples)) * 100
	}
	return bandwidth.Metrics{
		AvgBandwidthMBps: avgBW,
		JitterMS:         avgJitter,
		DropRatePct:      drop,
		TotalSamples:     a.totalSamples,
		FailedSamples:    a.failedSamples,
	}
}

func selectMonitorNodes(nodes []corev1.Node, excluded []string, target int) []string {
	if target <= 0 {
		return nil
	}
	excludeSet := make(map[string]bool, len(excluded))
	for _, n := range excluded {
		excludeSet[n] = true
	}
	selected := make([]string, 0, target)
	for _, n := range nodes {
		if excludeSet[n.Name] {
			continue
		}
		selected = append(selected, n.Name)
		if len(selected) == target {
			break
		}
	}
	return selected
}
