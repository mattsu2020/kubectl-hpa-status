package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/observation"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

var (
	informerDebounce    = 100 * time.Millisecond
	informerRewatchWait = 250 * time.Millisecond
)

func applyWatchTimeout(ctx context.Context, opts *options) (context.Context, context.CancelFunc) {
	if opts.WatchTimeout > 0 {
		return context.WithTimeout(ctx, opts.WatchTimeout)
	}
	return context.WithCancel(ctx)
}

func runWatchInformer(
	ctx context.Context,
	out io.Writer,
	opts *options,
	client *kube.Client,
	ec *enrichmentContext,
	name string,
	includeInterpretation bool,
	theme style.Theme,
) error {
	ctx, cancel := applyWatchTimeout(ctx, opts)
	defer cancel()

	humanOutput := watchUsesHumanOutput(opts)
	var previous *hpaanalysis.Analysis

	return watchHPACacheLoop(ctx, client, client.Namespace, metav1.ListOptions{FieldSelector: "metadata.name=" + name}, opts.ChunkSize, func(items []autoscalingv2.HorizontalPodAutoscaler) (bool, error) {
		target := findHPAInSlice(items, client.Namespace, name)
		if target == nil {
			return false, wrapHPALookupError(client.Namespace, name, apierrors.NewNotFound(autoscalingv2.Resource("horizontalpodautoscalers"), name))
		}
		if err := prepareWatchFrame(out, opts, theme, humanOutput); err != nil {
			return false, err
		}
		obs := observation.New(client.Interface, target.DeepCopy())
		report, err := buildStatusReportFromObservation(ctx, opts, client, target.DeepCopy(), includeInterpretation, ec.NewObservation(), obs)
		if err != nil {
			return false, err
		}
		if err := writeWatchReport(out, opts, report, previous); err != nil {
			return false, err
		}
		previous = &report.Analysis
		if humanOutput {
			writeStabilizationCountdown(out, &report.Analysis)
		}
		if opts.UntilCondition != "" && reportHasCondition(report, opts.UntilCondition) {
			_, err := fmt.Fprintf(watchDiagnosticWriter(opts, out), "\nStopped: condition %q is present.\n", opts.UntilCondition)
			return true, err
		}
		return false, nil
	})
}

func runWatchListInformer(ctx context.Context, out io.Writer, opts *options, client *kube.Client, theme style.Theme) error {
	ctx, cancel := applyWatchTimeout(ctx, opts)
	defer cancel()

	namespace := client.Namespace
	if opts.AllNamespaces {
		namespace = metav1.NamespaceAll
	}
	filter := opts.Filter
	if opts.Problem && filter == "" {
		filter = "issue"
	}
	humanOutput := watchUsesHumanOutput(opts)

	return watchHPACacheLoop(ctx, client, namespace, metav1.ListOptions{LabelSelector: opts.Selector}, opts.ChunkSize, func(items []autoscalingv2.HorizontalPodAutoscaler) (bool, error) {
		if err := prepareWatchFrame(out, opts, theme, humanOutput); err != nil {
			return false, err
		}
		if err := renderAccumulatedList(ctx, out, opts, filter, items); err != nil {
			return false, err
		}
		return false, nil
	})
}

func watchHPACacheLoop(
	ctx context.Context,
	client *kube.Client,
	namespace string,
	baseListOpts metav1.ListOptions,
	chunkSize int64,
	onSnapshot func([]autoscalingv2.HorizontalPodAutoscaler) (bool, error),
) error {
	for {
		list, err := kube.ListHPAsFromInterface(ctx, client.Interface, namespace, baseListOpts, chunkSize)
		if err != nil {
			return err
		}
		cacheMap := make(map[string]*autoscalingv2.HorizontalPodAutoscaler, len(list.Items))
		for i := range list.Items {
			hpa := list.Items[i].DeepCopy()
			cacheMap[hpa.Namespace+"/"+hpa.Name] = hpa
		}
		if stop, err := onSnapshot(sortedHPACache(cacheMap)); err != nil || stop {
			return err
		}

		rv := list.ResourceVersion
		resync, err := runWatchStream(ctx, client, namespace, baseListOpts, rv, cacheMap, onSnapshot)
		if err != nil {
			return err
		}
		if !resync {
			return nil
		}
		if err := waitWatchBackoff(ctx, informerRewatchWait); err != nil {
			return err
		}
	}
}

func runWatchStream(
	ctx context.Context,
	client *kube.Client,
	namespace string,
	baseListOpts metav1.ListOptions,
	rv string,
	cacheMap map[string]*autoscalingv2.HorizontalPodAutoscaler,
	onSnapshot func([]autoscalingv2.HorizontalPodAutoscaler) (bool, error),
) (bool, error) {
	for {
		watchOpts := baseListOpts
		watchOpts.ResourceVersion = rv
		watchOpts.AllowWatchBookmarks = true
		watcher, err := client.Interface.AutoscalingV2().HorizontalPodAutoscalers(namespace).Watch(ctx, watchOpts)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return true, nil
		}

		resync, nextRV, stop, err := consumeWatchEvents(ctx, watcher, rv, cacheMap, onSnapshot)
		watcher.Stop()
		if err != nil {
			return false, err
		}
		if stop {
			return false, nil
		}
		if resync {
			return true, nil
		}
		rv = nextRV
		if err := waitWatchBackoff(ctx, informerRewatchWait); err != nil {
			return false, err
		}
	}
}

func consumeWatchEvents(
	ctx context.Context,
	watcher watch.Interface,
	rv string,
	cacheMap map[string]*autoscalingv2.HorizontalPodAutoscaler,
	onSnapshot func([]autoscalingv2.HorizontalPodAutoscaler) (bool, error),
) (resync bool, nextRV string, stop bool, err error) {
	ch := watcher.ResultChan()
	var debounceTimer *time.Timer
	var debounceCh <-chan time.Time
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return false, rv, false, ctx.Err()
		case <-debounceCh:
			debounceCh = nil
			debounceTimer = nil
			stop, err := onSnapshot(sortedHPACache(cacheMap))
			if err != nil || stop {
				return false, rv, stop, err
			}
		case ev, ok := <-ch:
			if !ok {
				return false, rv, false, nil
			}
			resync, nextRV, emit := handleWatchEvent(ev, rv, cacheMap)
			rv = nextRV
			if resync {
				return true, rv, false, nil
			}
			if !emit {
				continue
			}
			if informerDebounce <= 0 {
				stop, err := onSnapshot(sortedHPACache(cacheMap))
				if err != nil || stop {
					return false, rv, stop, err
				}
				continue
			}
			if debounceTimer == nil {
				debounceTimer = time.NewTimer(informerDebounce)
				debounceCh = debounceTimer.C
			}
		}
	}
}

func handleWatchEvent(ev watch.Event, rv string, cacheMap map[string]*autoscalingv2.HorizontalPodAutoscaler) (resync bool, nextRV string, emit bool) {
	if ev.Type == watch.Error {
		return true, rv, false
	}
	hpa, isHPA := ev.Object.(*autoscalingv2.HorizontalPodAutoscaler)
	if !isHPA || hpa == nil {
		return false, rv, false
	}
	if hpa.ResourceVersion != "" {
		rv = hpa.ResourceVersion
	}
	if ev.Type == watch.Bookmark {
		return false, rv, false
	}
	applyWatchEventToCache(cacheMap, ev.Type, hpa)
	return false, rv, true
}

func applyWatchEventToCache(cacheMap map[string]*autoscalingv2.HorizontalPodAutoscaler, evType watch.EventType, hpa *autoscalingv2.HorizontalPodAutoscaler) {
	key := hpa.Namespace + "/" + hpa.Name
	switch evType {
	case watch.Added, watch.Modified:
		cacheMap[key] = hpa.DeepCopy()
	case watch.Deleted:
		delete(cacheMap, key)
	}
}

func sortedHPACache(cacheMap map[string]*autoscalingv2.HorizontalPodAutoscaler) []autoscalingv2.HorizontalPodAutoscaler {
	keys := make([]string, 0, len(cacheMap))
	for k := range cacheMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]autoscalingv2.HorizontalPodAutoscaler, 0, len(keys))
	for _, k := range keys {
		out = append(out, *cacheMap[k].DeepCopy())
	}
	return out
}

func findHPAInSlice(items []autoscalingv2.HorizontalPodAutoscaler, namespace, name string) *autoscalingv2.HorizontalPodAutoscaler {
	for i := range items {
		if items[i].Namespace == namespace && items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}

func prepareWatchFrame(out io.Writer, opts *options, theme style.Theme, humanOutput bool) error {
	if humanOutput {
		return clearWatchScreen(out, theme, opts.CurrentTime())
	}
	return writeWatchDocumentSeparator(out, opts)
}

func waitWatchBackoff(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
