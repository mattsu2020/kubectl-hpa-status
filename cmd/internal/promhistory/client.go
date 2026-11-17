package promhistory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
)

const (
	defaultStep          = time.Minute
	defaultWindow        = 6 * time.Hour
	defaultHTTPTimeout   = 15 * time.Second
	maxResponseBytes     = 16 * 1024 * 1024
	provenancePrometheus = "prometheus-query-range"
	ksmCurrentReplicas   = "kube_horizontalpodautoscaler_status_current_replicas"
	ksmDesiredReplicas   = "kube_horizontalpodautoscaler_status_desired_replicas"
	ksmConditionMetric   = "kube_horizontalpodautoscaler_status_condition"
	prometheusDateLayout = time.RFC3339
	defaultMetricName    = "custom"
	defaultMetricType    = "External"
)

type queryRangeResponse struct {
	Status    string         `json:"status"`
	ErrorType string         `json:"errorType,omitempty"`
	Error     string         `json:"error,omitempty"`
	Data      queryRangeData `json:"data"`
}

type queryRangeData struct {
	ResultType string       `json:"resultType"`
	Result     []matrixItem `json:"result"`
}

type matrixItem struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}

// LoadTrace queries Prometheus for HPA replica and optional metric history and
// synthesizes a TimelineTrace. Queries that match multiple ambiguous series
// fail closed instead of guessing.
func LoadTrace(ctx context.Context, cfg QueryConfig) (*ImportResult, error) {
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	currentSamples, err := querySingleSeries(ctx, normalized, replicaQuery(ksmCurrentReplicas, normalized.Namespace, normalized.HPAName))
	if err != nil {
		return nil, fmt.Errorf("query current replicas: %w", err)
	}
	desiredSamples, err := querySingleSeries(ctx, normalized, replicaQuery(ksmDesiredReplicas, normalized.Namespace, normalized.HPAName))
	if err != nil {
		return nil, fmt.Errorf("query desired replicas: %w", err)
	}
	if len(currentSamples) == 0 && len(desiredSamples) == 0 {
		return nil, fmt.Errorf("%w for %s/%s", ErrNoPrometheusSeries, normalized.Namespace, normalized.HPAName)
	}

	var warnings []string
	limitedSamples, limitedErr := queryOptionalSeries(ctx, normalized, conditionQuery(normalized.Namespace, normalized.HPAName, "ScalingLimited"))
	if limitedErr != nil {
		warnings = append(warnings, fmt.Sprintf("ScalingLimited condition series skipped: %v", limitedErr))
	}

	var metricSamples map[int64]float64
	if normalized.MetricQuery != "" {
		metricSamples, err = querySingleSeries(ctx, normalized, normalized.MetricQuery)
		if err != nil {
			return nil, fmt.Errorf("query metric series: %w", err)
		}
		if len(metricSamples) == 0 {
			warnings = append(warnings, "metric query returned no samples; MetricValues omitted")
		}
	}

	trace, hasMetrics := buildTimelineTrace(normalized, currentSamples, desiredSamples, limitedSamples, metricSamples)
	confidence := ConfidenceMedium
	if !hasMetrics {
		confidence = ConfidenceLow
	}

	return &ImportResult{
		Trace:      trace,
		Provenance: provenancePrometheus,
		Confidence: confidence,
		Warnings:   warnings,
	}, nil
}

func normalizeConfig(cfg QueryConfig) (QueryConfig, error) {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	if cfg.BaseURL == "" {
		return cfg, fmt.Errorf("prometheus URL is required")
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return cfg, fmt.Errorf("invalid prometheus URL %q (expected http:// or https://)", cfg.BaseURL)
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	cfg.Namespace = strings.TrimSpace(cfg.Namespace)
	cfg.HPAName = strings.TrimSpace(cfg.HPAName)
	if cfg.HPAName == "" {
		return cfg, fmt.Errorf("HPA name is required")
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.End.IsZero() {
		cfg.End = time.Now().UTC()
	}
	if cfg.Start.IsZero() {
		cfg.Start = cfg.End.Add(-defaultWindow)
	}
	if !cfg.End.After(cfg.Start) {
		return cfg, fmt.Errorf("end time (%s) must be after start time (%s)", cfg.End.Format(prometheusDateLayout), cfg.Start.Format(prometheusDateLayout))
	}
	if cfg.Step <= 0 {
		cfg.Step = defaultStep
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	if cfg.MetricName == "" {
		cfg.MetricName = defaultMetricName
	}
	if cfg.MetricType == "" {
		cfg.MetricType = defaultMetricType
	}
	return cfg, nil
}

func replicaQuery(metric, namespace, name string) string {
	return fmt.Sprintf(`%s{namespace=%q,horizontalpodautoscaler=%q}`, metric, namespace, name)
}

func conditionQuery(namespace, name, condition string) string {
	return fmt.Sprintf(`%s{namespace=%q,horizontalpodautoscaler=%q,condition=%q,status="true"}`, ksmConditionMetric, namespace, name, condition)
}

func queryOptionalSeries(ctx context.Context, cfg QueryConfig, promQL string) (map[int64]float64, error) {
	return querySingleSeries(ctx, cfg, promQL)
}

func querySingleSeries(ctx context.Context, cfg QueryConfig, promQL string) (map[int64]float64, error) {
	endpoint := fmt.Sprintf("%s/api/v1/query_range", cfg.BaseURL)
	params := url.Values{}
	params.Set("query", promQL)
	params.Set("start", strconv.FormatInt(cfg.Start.Unix(), 10))
	params.Set("end", strconv.FormatInt(cfg.End.Unix(), 10))
	params.Set("step", strconv.Itoa(max(1, int(cfg.Step.Seconds()))))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
	}

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prometheus HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded queryRangeResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}
	if decoded.Status != "success" {
		return nil, fmt.Errorf("prometheus error (%s): %s", decoded.ErrorType, decoded.Error)
	}
	if len(decoded.Data.Result) == 0 {
		return nil, nil //nolint:nilnil // empty series result is a valid non-error signal
	}
	if len(decoded.Data.Result) > 1 {
		return nil, fmt.Errorf("%w: query %q matched %d series; narrow label filters", ErrAmbiguousSeries, promQL, len(decoded.Data.Result))
	}

	return parseMatrixSamples(decoded.Data.Result[0].Values), nil
}

func parseMatrixSamples(rawValues [][]any) map[int64]float64 {
	samples := make(map[int64]float64, len(rawValues))
	for _, pair := range rawValues {
		if len(pair) != 2 {
			continue
		}
		tsFloat, ok := pair[0].(float64)
		if !ok || math.IsNaN(tsFloat) || math.IsInf(tsFloat, 0) {
			continue
		}
		valStr, ok := pair[1].(string)
		if !ok {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
			continue
		}
		samples[int64(math.Round(tsFloat))] = val
	}
	return samples
}

func buildTimelineTrace(
	cfg QueryConfig,
	currentSamples, desiredSamples, limitedSamples, metricSamples map[int64]float64,
) (hpaanalysis.TimelineTrace, bool) {
	tsSet := make(map[int64]struct{}, len(currentSamples)+len(desiredSamples))
	for ts := range currentSamples {
		tsSet[ts] = struct{}{}
	}
	for ts := range desiredSamples {
		tsSet[ts] = struct{}{}
	}

	timestamps := make([]int64, 0, len(tsSet))
	for ts := range tsSet {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	snapshots := make([]hpaanalysis.TimelineSnapshot, 0, len(timestamps))
	hasMetrics := false
	for _, ts := range timestamps {
		currVal, hasCurr := currentSamples[ts]
		desVal, hasDes := desiredSamples[ts]
		if !hasCurr && !hasDes {
			continue
		}
		curr := clampToInt32(currVal)
		des := clampToInt32(desVal)
		if !hasCurr {
			curr = des
		}
		if !hasDes {
			des = curr
		}

		snap := synthesizeSnapshot(cfg, ts, curr, des, limitedSamples[ts] >= 0.5, metricSamples)
		if len(snap.MetricValues) > 0 {
			hasMetrics = true
		}
		snapshots = append(snapshots, snap)
	}

	trace := hpaanalysis.TimelineTrace{
		HPAName:   cfg.HPAName,
		Namespace: cfg.Namespace,
		Start:     cfg.Start,
		End:       cfg.End,
		Interval:  cfg.Step,
		Snapshots: snapshots,
	}
	if len(snapshots) > 0 {
		trace.Start = snapshots[0].Timestamp
		trace.End = snapshots[len(snapshots)-1].Timestamp
	}
	return trace, hasMetrics
}

func synthesizeSnapshot(
	cfg QueryConfig,
	ts int64,
	curr, des int32,
	limited bool,
	metricSamples map[int64]float64,
) hpaanalysis.TimelineSnapshot {
	health := string(hpaanalysis.HealthOK)
	score := 100
	var conditions []hpaanalysis.Condition
	if limited {
		health = string(hpaanalysis.HealthLimited)
		score = 75
		conditions = append(conditions, hpaanalysis.Condition{
			Type:   hpaanalysis.ConditionScalingLimited,
			Status: "True",
			Reason: "PrometheusObserved",
		})
	}

	var metricValues []hpaanalysis.MetricReading
	topMetric := "[prometheus] replica history only"
	if val, ok := metricSamples[ts]; ok {
		reading := hpaanalysis.MetricReading{
			Type:   cfg.MetricType,
			Name:   cfg.MetricName,
			Value:  val,
			Target: cfg.MetricTarget,
			Unit:   cfg.MetricUnit,
		}
		metricValues = append(metricValues, reading)
		topMetric = fmt.Sprintf("%s=%.2f%s", cfg.MetricName, val, cfg.MetricUnit)
	}

	return hpaanalysis.TimelineSnapshot{
		Timestamp:    time.Unix(ts, 0).UTC(),
		Current:      curr,
		Desired:      des,
		Health:       health,
		HealthScore:  score,
		TopMetric:    topMetric,
		MetricValues: metricValues,
		Conditions:   conditions,
		Summary:      fmt.Sprintf("[%s] imported from Prometheus (%s/%s)", provenancePrometheus, cfg.Namespace, cfg.HPAName),
	}
}

func clampToInt32(v float64) int32 {
	if v <= 0 {
		return 0
	}
	rounded := math.Round(v)
	if rounded >= float64(math.MaxInt32) {
		return math.MaxInt32
	}
	return int32(rounded)
}
