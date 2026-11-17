package promhistory

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestLoadTrace_SuccessWithMetricsAndConditions(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)

	var seenAuth string
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			seenAuth = req.Header.Get("Authorization")
			q := req.URL.Query().Get("query")
			switch {
			case strings.Contains(q, ksmCurrentReplicas):
				return jsonResponse(http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"namespace":"prod","horizontalpodautoscaler":"web"},"values":[[1777629660,"3"],[1777629600,"2"]]}]}}`), nil
			case strings.Contains(q, ksmDesiredReplicas):
				return jsonResponse(http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"namespace":"prod","horizontalpodautoscaler":"web"},"values":[[1777629600,"2"],[1777629660,"5"]]}]}}`), nil
			case strings.Contains(q, ksmConditionMetric):
				return jsonResponse(http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1777629600,"0"],[1777629660,"1"]]}]}}`), nil
			case strings.Contains(q, "rate(cpu"):
				return jsonResponse(http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1777629600,"45.5"],[1777629660,"85.0"],[1777629720,"NaN"]]}]}}`), nil
			default:
				t.Fatalf("unexpected PromQL query: %s", q)
				return nil, context.Canceled
			}
		}),
	}

	res, err := LoadTrace(context.Background(), QueryConfig{
		BaseURL:      "https://prom.example.com/",
		BearerToken:  "secret-token",
		HTTPClient:   client,
		Namespace:    "prod",
		HPAName:      "web",
		Start:        t0,
		End:          t1.Add(time.Minute),
		Step:         time.Minute,
		MetricQuery:  `rate(cpu[5m])`,
		MetricName:   "cpu",
		MetricType:   "Resource",
		MetricUnit:   "%",
		MetricTarget: 70,
	})
	if err != nil {
		t.Fatalf("LoadTrace failed: %v", err)
	}
	if seenAuth != "Bearer secret-token" {
		t.Errorf("expected Bearer token header, got %q", seenAuth)
	}
	if res.Provenance != "prometheus-query-range" {
		t.Errorf("unexpected provenance: %q", res.Provenance)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("expected confidence %q when metrics present, got %q", ConfidenceMedium, res.Confidence)
	}
	if len(res.Trace.Snapshots) != 2 {
		t.Fatalf("expected 2 sorted snapshots, got %d", len(res.Trace.Snapshots))
	}

	s0, s1 := res.Trace.Snapshots[0], res.Trace.Snapshots[1]
	if !s0.Timestamp.Equal(t0) || !s1.Timestamp.Equal(t1) {
		t.Fatalf("expected sorted timestamps (%v, %v), got (%v, %v)", t0, t1, s0.Timestamp, s1.Timestamp)
	}
	if s0.Current != 2 || s0.Desired != 2 || s0.Health != "OK" {
		t.Errorf("unexpected s0: %+v", s0)
	}
	if s1.Current != 3 || s1.Desired != 5 || s1.Health != "LIMITED" || len(s1.Conditions) != 1 {
		t.Errorf("unexpected s1: %+v", s1)
	}
	if len(s1.MetricValues) != 1 || s1.MetricValues[0].Value != 85.0 || s1.MetricValues[0].Target != 70 {
		t.Errorf("unexpected s1 metric values: %+v", s1.MetricValues)
	}
}

func TestLoadTrace_FailClosedOnAmbiguousSeries(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"cluster":"a"},"values":[[1777629600,"2"]]},{"metric":{"cluster":"b"},"values":[[1777629600,"4"]]}]}}`), nil
		}),
	}

	_, err := LoadTrace(context.Background(), QueryConfig{
		BaseURL:    "http://localhost:9090",
		HTTPClient: client,
		Namespace:  "default",
		HPAName:    "web",
		Start:      time.Unix(1777629500, 0),
		End:        time.Unix(1777629700, 0),
	})
	if !errors.Is(err, ErrAmbiguousSeries) {
		t.Fatalf("expected ErrAmbiguousSeries, got %v", err)
	}
}

func TestLoadTrace_NoSeriesReturnsErrNoPrometheusSeries(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[]}}`), nil
		}),
	}

	_, err := LoadTrace(context.Background(), QueryConfig{
		BaseURL:    "http://localhost:9090",
		HTTPClient: client,
		Namespace:  "default",
		HPAName:    "missing",
		Start:      time.Unix(1777629500, 0),
		End:        time.Unix(1777629700, 0),
	})
	if !errors.Is(err, ErrNoPrometheusSeries) {
		t.Fatalf("expected ErrNoPrometheusSeries, got %v", err)
	}
}

func TestLoadTrace_ValidationAndErrorHandling(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	cases := []struct {
		name string
		cfg  QueryConfig
	}{
		{
			name: "missing URL",
			cfg:  QueryConfig{HPAName: "web"},
		},
		{
			name: "non-http URL",
			cfg:  QueryConfig{BaseURL: "file:///etc/passwd", HPAName: "web"},
		},
		{
			name: "missing HPA name",
			cfg:  QueryConfig{BaseURL: "http://localhost:9090"},
		},
		{
			name: "end before start",
			cfg:  QueryConfig{BaseURL: "http://localhost:9090", HPAName: "web", Start: now, End: now.Add(-time.Minute)},
		},
		{
			name: "prometheus error status",
			cfg: QueryConfig{
				BaseURL: "http://localhost:9090",
				HPAName: "web",
				HTTPClient: &http.Client{
					Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
						return jsonResponse(http.StatusOK, `{"status":"error","errorType":"bad_data","error":"parse error"}`), nil
					}),
				},
			},
		},
		{
			name: "http 503 response",
			cfg: QueryConfig{
				BaseURL: "http://localhost:9090",
				HPAName: "web",
				HTTPClient: &http.Client{
					Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
						return jsonResponse(http.StatusServiceUnavailable, "unavailable"), nil
					}),
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := LoadTrace(context.Background(), tc.cfg); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}
