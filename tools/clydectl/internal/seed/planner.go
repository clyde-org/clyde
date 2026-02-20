package seed

import "math"

type DoublingPlanner struct {
	current int  // Total nodes that have been seeded so far
	total   int  // Total nodes in the cluster
	initial int  // Size of the first wave
	started bool // Flag to track if we've moved past the first wave
}

// NewDoublingPlanner calculates an initial seed density (10% or min 2)
// If initialCount is > 0, it overrides the default 10% logic.
func NewDoublingPlanner(total int, initialCount int) *DoublingPlanner {
	var initialSeeds int
	if initialCount > 0 {
		initialSeeds = initialCount
	} else {
		// Dynamically calculate: 10% of cluster, but at least 2 nodes
		initialSeeds = int(math.Max(2, math.Floor(float64(total)*0.1)))
	}

	// Ensure initial doesn't exceed total for very small clusters
	// or if user requested more than total
	if initialSeeds > total {
		initialSeeds = total
	}

	return &DoublingPlanner{
		current: 0,
		total:   total,
		initial: initialSeeds,
		started: false,
	}
}

func (p *DoublingPlanner) HasNext() bool {
	return p.current < p.total
}

func (p *DoublingPlanner) Remaining() int {
	if p.current >= p.total {
		return 0
	}
	return p.total - p.current
}

func (p *DoublingPlanner) NextBatch() int {
	var batch int

	if !p.started {
		// First Wave: The "Initial Density" phase
		batch = p.initial
		p.started = true
	} else {
		// Subsequent Waves: Double the existing seeder capacity
		batch = p.current
	}

	// Safety: Ensure the batch doesn't exceed remaining nodes
	if p.current+batch > p.total {
		batch = p.total - p.current
	}

	p.current += batch
	return batch
}
