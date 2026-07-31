package cmdoptions

import "testing"

func TestStatusRequestIsAnIndependentSnapshot(t *testing.T) {
	root := DefaultRoot()
	root.Explain = true
	root.Watch.Watch = true
	root.Output = "json"
	root.Simulate = []string{"replicas=4"}
	root.OutputTemplates = map[string]OutputTemplateConfig{
		"compact": {Type: "go-template", Template: "{{.Name}}"},
	}
	names := []string{"web", "api"}

	request := NewStatusRequest(root, names)

	root.Explain = false
	root.Watch.Watch = false
	root.Output = "yaml"
	root.Simulate[0] = "replicas=9"
	root.OutputTemplates["compact"] = OutputTemplateConfig{Template: "changed"}
	names[0] = "changed"

	if !request.IncludeInterpretation() {
		t.Fatal("interpretation decision should be captured when the request is created")
	}
	if !request.WatchEnabled() {
		t.Fatal("watch decision should be captured when the request is created")
	}
	if got := request.Names(); len(got) != 2 || got[0] != "web" || got[1] != "api" {
		t.Fatalf("unexpected captured names: %v", got)
	}

	options := request.Options()
	if options.Output != "json" || options.Simulate[0] != "replicas=4" {
		t.Fatalf("request observed later root mutation: %+v", options)
	}
	if got := options.OutputTemplates["compact"].Template; got != "{{.Name}}" {
		t.Fatalf("request observed later template mutation: %q", got)
	}

	options.Output = "table"
	options.Simulate[0] = "replicas=20"
	options.OutputTemplates["compact"] = OutputTemplateConfig{Template: "mutated execution copy"}
	gotAgain := request.Options()
	if gotAgain.Output != "json" ||
		gotAgain.Simulate[0] != "replicas=4" ||
		gotAgain.OutputTemplates["compact"].Template != "{{.Name}}" {
		t.Fatalf("execution copy mutated the immutable request: %+v", gotAgain)
	}

	capturedNames := request.Names()
	capturedNames[0] = "mutated"
	if got := request.Names()[0]; got != "web" {
		t.Fatalf("Names returned aliased storage: %q", got)
	}
}

func TestStatusRequestHonorsNoInterpret(t *testing.T) {
	root := DefaultRoot()
	root.Interpret = true
	root.NoInterpret = true

	if NewStatusRequest(root, []string{"web"}).IncludeInterpretation() {
		t.Fatal("--no-interpret must win in the captured request")
	}
}

func TestListAndScanRequestsAreIndependentSnapshots(t *testing.T) {
	root := DefaultRoot()
	root.Filter = "limited"
	root.Watch.Watch = true

	listRequest := NewListRequest(root)
	scanRequest := NewScanRequest(root)

	root.Filter = "error"
	root.Watch.Watch = false

	listOptions := listRequest.Options()
	if listOptions.Filter != "limited" || !listRequest.WatchEnabled() {
		t.Fatalf("list request observed later root mutation: %+v", listOptions)
	}

	scanOptions := scanRequest.Options()
	if !scanOptions.AllNamespaces || !scanOptions.Problem || !scanOptions.Wide {
		t.Fatalf("scan invariants were not captured: %+v", scanOptions)
	}
	if root.AllNamespaces || root.Problem || root.Wide {
		t.Fatalf("scan request mutated the source root: %+v", root)
	}

	scanOptions.Filter = "changed"
	if got := scanRequest.Options().Filter; got != "limited" {
		t.Fatalf("execution copy mutated the scan request: %q", got)
	}
}
