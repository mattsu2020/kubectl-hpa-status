package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	ctx := context.Background()

	var namespace string
	var contextName string
	var showEvents bool

	flag.StringVar(&namespace, "namespace", "", "namespace")
	flag.StringVar(&namespace, "n", "", "namespace")
	flag.StringVar(&contextName, "context", "", "kubeconfig context")
	flag.BoolVar(&showEvents, "events", true, "show recent HPA events")
	if err := flag.CommandLine.Parse(normalizeArgs(os.Args[1:])); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: kubectl hpa status <hpa-name> [-n namespace]")
		os.Exit(2)
	}
	hpaName := flag.Arg(0)

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if loadingRules.ExplicitPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			loadingRules.ExplicitPath = filepath.Join(home, ".kube", "config")
		}
	}

	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		overrides,
	)

	if namespace == "" {
		ns, _, err := clientConfig.Namespace()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve namespace: %v\n", err)
			os.Exit(1)
		}
		namespace = ns
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load kubeconfig: %v\n", err)
		os.Exit(1)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	hpa, err := client.AutoscalingV2().
		HorizontalPodAutoscalers(namespace).
		Get(ctx, hpaName, metav1.GetOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get HPA %s/%s: %v\n", namespace, hpaName, err)
		os.Exit(1)
	}

	printHPAStatus(hpa)

	if showEvents {
		printRecentEvents(ctx, client, hpa)
	}

	fmt.Println()
	fmt.Println("Limitations:")
	fmt.Println("  - This plugin does not know the controller's internal intermediate calculations.")
	fmt.Println("  - It reports what can be inferred from existing HPA status, metrics, conditions, and events.")
}

func normalizeArgs(args []string) []string {
	var flags []string
	var positionals []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}

		flags = append(flags, arg)
		if strings.Contains(arg, "=") || arg == "--events" {
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flags = append(flags, args[i+1])
			i++
		}
	}

	return append(flags, positionals...)
}

func printHPAStatus(hpa *autoscalingv2.HorizontalPodAutoscaler) {
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	fmt.Printf("HPA %s/%s\n", hpa.Namespace, hpa.Name)
	fmt.Printf("Target: %s/%s\n", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name)
	fmt.Printf("Replicas: current=%d desired=%d min=%d max=%d\n",
		hpa.Status.CurrentReplicas,
		hpa.Status.DesiredReplicas,
		minReplicas,
		hpa.Spec.MaxReplicas,
	)

	fmt.Println()
	fmt.Printf("Summary: %s\n", summarizeDirection(hpa, minReplicas))

	fmt.Println()
	fmt.Println("Conditions:")
	if len(hpa.Status.Conditions) == 0 {
		fmt.Println("  No conditions reported.")
	} else {
		for _, condition := range hpa.Status.Conditions {
			fmt.Printf("  %-15s %-7s %-24s %s\n",
				condition.Type,
				condition.Status,
				condition.Reason,
				condition.Message,
			)
		}
	}

	fmt.Println()
	fmt.Println("Metrics:")
	if len(hpa.Status.CurrentMetrics) == 0 {
		fmt.Println("  No current metrics reported.")
	} else {
		for _, metric := range hpa.Status.CurrentMetrics {
			fmt.Printf("  - %s\n", formatMetricStatus(hpa, metric))
		}
	}

	fmt.Println()
	fmt.Println("Interpretation:")
	for _, line := range interpret(hpa, minReplicas) {
		fmt.Printf("  - %s\n", line)
	}
}

func summarizeDirection(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) string {
	if condition := findCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		return "HPA cannot currently compute a scaling recommendation from metrics."
	}
	if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
		return "HPA has no visible desired replica recommendation in status."
	}

	current := hpa.Status.CurrentReplicas
	desired := hpa.Status.DesiredReplicas

	switch {
	case desired > current:
		return "HPA currently wants to scale up."
	case desired < current:
		return "HPA currently wants to scale down."
	case desired == hpa.Spec.MaxReplicas:
		return "HPA is at maxReplicas."
	case desired == minReplicas:
		return "HPA is at minReplicas."
	default:
		return "HPA currently keeps the replica count unchanged."
	}
}

func interpret(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []string {
	var lines []string

	if condition := findCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		lines = append(lines,
			fmt.Sprintf("ScalingActive is %s: %s - %s", condition.Status, condition.Reason, condition.Message))
		lines = append(lines, "The HPA is not reporting a reliable scale direction while metric evaluation is inactive.")
		lines = append(lines, "This plugin avoids treating desiredReplicas=0 as a scale-down recommendation in this state.")
		lines = append(lines, "This plugin uses existing HPA status, conditions, metrics, and events. It does not expose internal controller calculations.")
		return lines
	}

	if condition := findCondition(hpa, "AbleToScale"); condition != nil && condition.Status != corev1.ConditionTrue {
		lines = append(lines,
			fmt.Sprintf("AbleToScale is %s: %s - %s", condition.Status, condition.Reason, condition.Message))
	} else if condition := findCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		lines = append(lines,
			fmt.Sprintf("Scale down appears stabilized: %s", condition.Message))
	}

	if condition := findCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		if hpa.Status.DesiredReplicas == hpa.Spec.MaxReplicas {
			lines = append(lines, "ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.")
		} else if hpa.Status.DesiredReplicas == minReplicas {
			lines = append(lines, "ScalingLimited reports that the visible desired replica count is constrained by minReplicas.")
		} else {
			lines = append(lines, "The recommendation is reported as limited.")
		}
	}

	if hpa.Status.DesiredReplicas > hpa.Status.CurrentReplicas {
		lines = append(lines, "desiredReplicas is greater than currentReplicas, so the HPA is recommending scale up.")
	} else if hpa.Status.DesiredReplicas < hpa.Status.CurrentReplicas {
		lines = append(lines, "desiredReplicas is less than currentReplicas, so the HPA is recommending scale down.")
	} else {
		lines = append(lines, "desiredReplicas equals currentReplicas, so no immediate replica change is visible from status.")
		if hpa.Status.DesiredReplicas != hpa.Spec.MaxReplicas && hpa.Status.DesiredReplicas != minReplicas && hasMetricOutsideTarget(hpa) {
			lines = append(lines, "A metric is outside its target while desiredReplicas is unchanged; this may be due to tolerance, rounding, stabilization, or conservative handling of missing metrics.")
			lines = append(lines, "Existing HPA status does not expose the exact internal reason for this no-scale decision.")
		}
	}

	if len(hpa.Status.CurrentMetrics) > 1 {
		lines = append(lines, "Multiple current metrics are reported, but the API does not expose per-metric replica recommendations as structured status.")
		lines = append(lines, "Events and condition messages can hint at the contributing metric, but they are not a stable decision record.")
	}

	lines = append(lines, "This plugin uses existing HPA status, conditions, metrics, and events. It does not expose internal controller calculations.")

	return lines
}

func findCondition(hpa *autoscalingv2.HorizontalPodAutoscaler, conditionType string) *autoscalingv2.HorizontalPodAutoscalerCondition {
	for i := range hpa.Status.Conditions {
		if string(hpa.Status.Conditions[i].Type) == conditionType {
			return &hpa.Status.Conditions[i]
		}
	}
	return nil
}

func formatMetricStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) string {
	switch metric.Type {
	case "":
		return "Metric status is present, but details are unavailable"
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource == nil {
			return "Resource metric: <missing status>"
		}
		target := findResourceTarget(hpa, string(metric.Resource.Name))
		current := formatMetricValue(metric.Resource.Current.AverageUtilization, metric.Resource.Current.AverageValue)
		note := compareMetricToTarget(metric.Resource.Current.AverageUtilization, target)
		if note == "" {
			return fmt.Sprintf("Resource %s current=%s target=%s", metric.Resource.Name, current, target)
		}
		return fmt.Sprintf("Resource %s current=%s target=%s note=%q", metric.Resource.Name, current, target, note)
	default:
		return fmt.Sprintf("%s metric is present, but this POC only formats Resource metrics in detail", metric.Type)
	}
}

func findResourceTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType &&
			spec.Resource != nil &&
			string(spec.Resource.Name) == name {
			target := spec.Resource.Target
			switch target.Type {
			case autoscalingv2.UtilizationMetricType:
				if target.AverageUtilization != nil {
					return fmt.Sprintf("%d%%", *target.AverageUtilization)
				}
			case autoscalingv2.AverageValueMetricType:
				if target.AverageValue != nil {
					return target.AverageValue.String()
				}
			case autoscalingv2.ValueMetricType:
				if target.Value != nil {
					return target.Value.String()
				}
			}
		}
	}
	return "<unknown>"
}

func formatMetricValue(utilization *int32, averageValue *resource.Quantity) string {
	if utilization != nil {
		return fmt.Sprintf("%d%%", *utilization)
	}
	if averageValue != nil && !averageValue.IsZero() {
		return averageValue.String()
	}
	return "<unknown>"
}

func compareMetricToTarget(utilization *int32, target string) string {
	if utilization == nil || !strings.HasSuffix(target, "%") {
		return ""
	}

	targetValue := strings.TrimSuffix(target, "%")
	var targetUtilization int32
	if _, err := fmt.Sscanf(targetValue, "%d", &targetUtilization); err != nil {
		return ""
	}

	switch {
	case *utilization > targetUtilization:
		return "current value is above target"
	case *utilization < targetUtilization:
		return "current value is below target"
	default:
		return "current value equals target"
	}
}

func hasMetricOutsideTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil {
			continue
		}
		utilization := metric.Resource.Current.AverageUtilization
		if utilization == nil {
			continue
		}
		target := findResourceTarget(hpa, string(metric.Resource.Name))
		if !strings.HasSuffix(target, "%") {
			continue
		}

		targetValue := strings.TrimSuffix(target, "%")
		var targetUtilization int32
		if _, err := fmt.Sscanf(targetValue, "%d", &targetUtilization); err != nil {
			continue
		}
		if *utilization != targetUtilization {
			return true
		}
	}

	return false
}

func printRecentEvents(ctx context.Context, client *kubernetes.Clientset, hpa *autoscalingv2.HorizontalPodAutoscaler) {
	selector := fields.AndSelectors(
		fields.OneTermEqualSelector("involvedObject.kind", "HorizontalPodAutoscaler"),
		fields.OneTermEqualSelector("involvedObject.name", hpa.Name),
		fields.OneTermEqualSelector("involvedObject.namespace", hpa.Namespace),
	)

	events, err := client.CoreV1().
		Events(hpa.Namespace).
		List(ctx, metav1.ListOptions{
			FieldSelector: selector.String(),
			Limit:         10,
		})
	if err != nil {
		fmt.Printf("\nRecent events:\n  failed to list events: %v\n", err)
		return
	}

	sort.Slice(events.Items, func(i, j int) bool {
		return events.Items[i].LastTimestamp.After(events.Items[j].LastTimestamp.Time)
	})

	fmt.Println()
	fmt.Println("Recent events:")
	if len(events.Items) == 0 {
		fmt.Println("  No recent events found.")
		return
	}

	limit := len(events.Items)
	if limit > 5 {
		limit = 5
	}

	for _, event := range events.Items[:limit] {
		message := strings.ReplaceAll(event.Message, "\n", " ")
		fmt.Printf("  - %s: %s\n", event.Reason, message)
	}
}
