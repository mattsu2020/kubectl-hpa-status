package cmdoptions

// StatusRequest is an immutable snapshot of one status command invocation.
//
// Cobra binds flags to a long-lived, mutable Root. Capturing that Root at the
// RunE boundary prevents lower layers from observing later flag/config
// mutations. The fields stay private and accessors return fresh copies so
// downstream normalization cannot mutate the request itself.
type StatusRequest struct {
	options               Root
	names                 []string
	includeInterpretation bool
}

// NewStatusRequest snapshots the effective options and positional HPA names
// after Cobra's PersistentPreRunE normalization has completed.
func NewStatusRequest(root Root, names []string) StatusRequest {
	options := root.Copy()
	return StatusRequest{
		options:               options,
		names:                 cloneStrings(names),
		includeInterpretation: (options.Interpret || options.Explain || options.Suggest) && !options.NoInterpret,
	}
}

// Options returns an independent execution copy of the captured options.
func (r StatusRequest) Options() Root {
	return r.options.Copy()
}

// Names returns an independent copy of the captured positional arguments.
func (r StatusRequest) Names() []string {
	return cloneStrings(r.names)
}

// IncludeInterpretation reports the interpretation decision captured at the
// command boundary.
func (r StatusRequest) IncludeInterpretation() bool {
	return r.includeInterpretation
}

// WatchEnabled reports whether this invocation selected watch mode.
func (r StatusRequest) WatchEnabled() bool {
	return r.options.Watch.Watch
}

// ListRequest is an immutable snapshot of one list or scan invocation.
type ListRequest struct {
	options Root
}

// NewListRequest snapshots the effective list options at the RunE boundary.
func NewListRequest(root Root) ListRequest {
	return ListRequest{options: root.Copy()}
}

// NewScanRequest snapshots the effective options and applies the fixed scan
// semantics without mutating Cobra's shared Root.
func NewScanRequest(root Root) ListRequest {
	options := root.Copy()
	options.AllNamespaces = true
	options.Problem = true
	options.Wide = true
	return ListRequest{options: options}
}

// Options returns an independent execution copy of the captured options.
func (r ListRequest) Options() Root {
	return r.options.Copy()
}

// WatchEnabled reports whether this list invocation selected watch mode.
func (r ListRequest) WatchEnabled() bool {
	return r.options.Watch.Watch
}
