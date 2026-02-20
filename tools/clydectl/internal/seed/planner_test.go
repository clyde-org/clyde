package seed

import (
	"testing"
)

func TestNewDoublingPlanner(t *testing.T) {
	tests := []struct {
		desc         string
		total        int
		initialCount int
		wantInitial  int
	}{
		{
			desc:         "Default 10%",
			total:        100,
			initialCount: 0,
			wantInitial:  10,
		},
		{
			desc:         "Small cluster default min 2",
			total:        5,
			initialCount: 0,
			wantInitial:  2,
		},
		{
			desc:         "Custom initial count",
			total:        100,
			initialCount: 50,
			wantInitial:  50,
		},
		{
			desc:         "Custom initial count overflow",
			total:        10,
			initialCount: 20,
			wantInitial:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			p := NewDoublingPlanner(tt.total, tt.initialCount)
			if p.initial != tt.wantInitial {
				t.Errorf("NewDoublingPlanner(%d, %d).initial = %d, want %d", tt.total, tt.initialCount, p.initial, tt.wantInitial)
			}
		})
	}
}

func TestRemaining(t *testing.T) {
	p := NewDoublingPlanner(10, 2)

	if got := p.Remaining(); got != 10 {
		t.Fatalf("Remaining() before start = %d, want 10", got)
	}

	if got := p.NextBatch(); got != 2 {
		t.Fatalf("first NextBatch() = %d, want 2", got)
	}
	if got := p.Remaining(); got != 8 {
		t.Fatalf("Remaining() after first batch = %d, want 8", got)
	}

	if got := p.NextBatch(); got != 2 {
		t.Fatalf("second NextBatch() = %d, want 2", got)
	}
	if got := p.Remaining(); got != 6 {
		t.Fatalf("Remaining() after second batch = %d, want 6", got)
	}
}
