package hpa

import (
	"bytes"
	"testing"
)

func TestListTextAndStreamingAreIdentical(t *testing.T) {
	report := ListReport{Items: []ListItem{{
		Namespace: "日本語-ns", Name: "web", Target: "Deployment/web",
		Current: 2, Desired: 3, Min: 1, Max: 5, Health: "OK", Summary: "steady",
	}}}
	for _, wide := range []bool{false, true} {
		var regular, streaming bytes.Buffer
		opts := ListTextOptions{Wide: wide}
		if err := WriteListText(&regular, report, opts); err != nil {
			t.Fatal(err)
		}
		if err := WriteListTextStreaming(&streaming, report, opts, true); err != nil {
			t.Fatal(err)
		}
		if regular.String() != streaming.String() {
			t.Fatalf("wide=%v: regular and streaming output differ", wide)
		}
	}
}
