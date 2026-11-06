package controller

import (
	"testing"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

func w(weight int32) *servingv1alpha1.CanarySpec {
	return &servingv1alpha1.CanarySpec{Weight: weight}
}

func TestSplitReplicas(t *testing.T) {
	tests := []struct {
		name                   string
		total                  int32
		canary                 *servingv1alpha1.CanarySpec
		wantStable, wantCanary int32
	}{
		{"no canary", 5, nil, 5, 0},
		{"weight 0", 5, w(0), 5, 0},
		{"weight 20", 5, w(20), 4, 1},
		{"weight 100", 5, w(100), 0, 5},
		{"round 0.6 up", 3, w(20), 2, 1},
		{"clamp min 1", 10, w(1), 9, 1},
		{"clamp max total-1", 10, w(99), 1, 9},
		{"single replica", 1, w(50), 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, c := splitReplicas(tt.total, tt.canary)
			if s != tt.wantStable || c != tt.wantCanary {
				t.Errorf("splitReplicas(%d, %v) = (%d, %d), want (%d, %d)",
					tt.total, tt.canary, s, c, tt.wantStable, tt.wantCanary)
			}
		})
	}
}
