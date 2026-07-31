package enrichment

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRunPipelineOrderingAndPolicies(t *testing.T) {
	sentinel := errors.New("failed")
	var ran []string
	var warned []string
	tasks := []PipelineTask{
		{Name: "disabled", Enabled: false, Run: func(context.Context) error {
			ran = append(ran, "disabled")
			return nil
		}},
		{Name: "best-effort", Enabled: true, Run: func(context.Context) error {
			ran = append(ran, "best-effort")
			return sentinel
		}},
		{Name: "after", Enabled: true, Run: func(context.Context) error {
			ran = append(ran, "after")
			return nil
		}},
	}
	err := RunPipeline(context.Background(), tasks, func(name string, err error) {
		if !errors.Is(err, sentinel) {
			t.Fatalf("warning error = %v, want sentinel", err)
		}
		warned = append(warned, name)
	})
	if err != nil {
		t.Fatalf("RunPipeline() error = %v", err)
	}
	if !reflect.DeepEqual(ran, []string{"best-effort", "after"}) {
		t.Fatalf("run order = %v", ran)
	}
	if !reflect.DeepEqual(warned, []string{"best-effort"}) {
		t.Fatalf("warnings = %v", warned)
	}
}

func TestRunPipelineAbortOnError(t *testing.T) {
	sentinel := errors.New("fatal")
	afterRan := false
	err := RunPipeline(context.Background(), []PipelineTask{
		{Name: "fatal", Enabled: true, AbortOnError: true, Run: func(context.Context) error { return sentinel }},
		{Name: "after", Enabled: true, Run: func(context.Context) error {
			afterRan = true
			return nil
		}},
	}, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunPipeline() error = %v, want sentinel", err)
	}
	if afterRan {
		t.Fatal("pipeline continued after fatal task")
	}
}
