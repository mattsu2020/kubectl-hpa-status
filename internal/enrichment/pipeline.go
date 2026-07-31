package enrichment

import (
	"context"
	"fmt"
)

// PipelineTask is one already-bound application enrichment step. Domain
// packages bind their typed input in Run; the engine only owns ordering,
// enablement, warning policy, and fail-fast behavior.
type PipelineTask struct {
	Name         string
	Enabled      bool
	AbortOnError bool
	Run          func(context.Context) error
}

// RunPipeline executes enabled tasks in order. Every task failure is reported
// to onError. Fail-fast tasks return their original error after reporting it;
// best-effort tasks allow the remaining pipeline to continue.
func RunPipeline(ctx context.Context, tasks []PipelineTask, onError func(name string, err error)) error {
	for _, task := range tasks {
		if !task.Enabled {
			continue
		}
		if task.Run == nil {
			err := fmt.Errorf("enrichment task %q has no runner", task.Name)
			if onError != nil {
				onError(task.Name, err)
			}
			if task.AbortOnError {
				return err
			}
			continue
		}
		if err := task.Run(ctx); err != nil {
			if onError != nil {
				onError(task.Name, err)
			}
			if task.AbortOnError {
				return err
			}
		}
	}
	return nil
}
