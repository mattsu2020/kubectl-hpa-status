package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/mattsu2020/kubectl-hpa-status/pkg/style"
)

// listStreamer emits list items incrementally as pages arrive, instead of
// buffering the whole cluster. It is constructed by runListStreaming and
// dispatches on the output format:
//   - table/wide: a single continuous table whose header is written once and
//     whose rows are appended per page (no enclosing array);
//   - jsonl: one compact JSON object per line.
//
// The streamer deliberately does not support json/yaml: those formats wrap the
// result in an array/object and so cannot be produced incrementally without
// buffering. Callers needing those formats use the accumulated path.
type listStreamer struct {
	out   io.Writer
	opts  *options
	wide  bool
	first bool // whether any page has been emitted yet (for table header)
}

func newListStreamer(out io.Writer, opts *options) *listStreamer {
	return &listStreamer{
		out:   out,
		opts:  opts,
		wide:  opts.Wide || normalizeOutputFormat(opts.Output) == "wide",
		first: true,
	}
}

func (s *listStreamer) begin() error { return nil }

// writePage analyzes and emits a single page's items. For the table form it
// writes the header on the first page; for jsonl it writes one object per line
// with no framing. An empty page writes nothing.
func (s *listStreamer) writePage(items []hpaanalysis.ListItem) error {
	if len(items) == 0 {
		return nil
	}
	format := normalizeOutputFormat(s.opts.Output)
	if format == "jsonl" {
		return s.writeJSONL(items)
	}
	return s.writeTable(items)
}

func (s *listStreamer) writeTable(items []hpaanalysis.ListItem) error {
	// Reuse the canonical list text renderer with streaming=true so the header
	// is emitted on the first page only and subsequent pages append rows.
	textOpts := hpaanalysis.ListTextOptions{
		Wide:              s.wide,
		Color:             shouldColorize(s.opts.Color, s.out),
		Theme:             style.NewTheme(shouldColorize(s.opts.Color, s.out)),
		Lang:              outputLang(s.opts.Lang, s.opts.Output),
		Labels:            labelProviderForLang(s.opts.Lang, s.opts.Output),
		SummaryTranslator: summaryTranslatorForLang(s.opts.Lang, s.opts.Output),
	}
	return hpaanalysis.WriteListTextStreaming(s.out, hpaanalysis.ListReport{
		APIVersion: hpaanalysis.SchemaVersion,
		Items:      items,
	}, textOpts, s.first)
}

func (s *listStreamer) writeJSONL(items []hpaanalysis.ListItem) error {
	encoder := json.NewEncoder(s.out)
	encoder.SetEscapeHTML(false)
	for i := range items {
		if err := encoder.Encode(items[i]); err != nil {
			return fmt.Errorf("jsonl stream: encode item %d: %w", i, err)
		}
	}
	return nil
}

func (s *listStreamer) end() error { return nil }
