package controller

import (
	"testing"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

func TestLabelsFor(t *testing.T) {
	md := &servingv1alpha1.ModelDeployment{}
	md.Name = "foo"

	got := labelsFor(md, trackCanary)

	if got[labelInstance] != "foo" {
		t.Errorf("instance label = %q, want %q", got[labelInstance], "foo")
	}
	if got[labelTrack] != trackCanary {
		t.Errorf("track label = %q, want %q", got[labelTrack], trackCanary)
	}
	if len(got) != 2 {
		t.Errorf("got %d labels, want 2: %v", len(got), got)
	}
}
