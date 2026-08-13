// Command check-deprecated-facades prevents repository-internal code from
// taking new dependencies on public compatibility facades that are scheduled
// for removal in v3. External users may continue to use those APIs until then.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const modulePath = "github.com/mattsu2020/kubectl-hpa-status"

type facadeSpec struct {
	symbols     map[string]struct{}
	replacement string
}

var deprecatedFacades = map[string]facadeSpec{
	modulePath + "/pkg/hpa": {
		symbols: symbolSet(
			"ReadinessImpact",
			"ReadinessDoctorReport",
			"ReadinessPodAgeDistribution",
			"ReadinessProbeAnalysis",
			"ReadinessInitImpact",
			"ReadinessExclusionEstimate",
			"ReadinessDoctorInput",
			"ReadinessDoctorPod",
			"AnalyzeReadinessDoctor",
			"HealthSnapshot",
			"HealthTrendResult",
			"AnalyzeHealthTrend",
			"DetectFlapping",
			"ComputeHealthVariance",
			"FormatHealthSparkline",
			"DetectAnomalies",
			"RenderHealthTrendASCII",
			"FormatTrendText",
			"FormatTrendAnomalyText",
			"FormatTrendAnomalyGraph",
			"FormatTrendListRow",
			"KEDAAnalysis",
			"KEDATriggerSummary",
			"KEDAFallbackInfo",
			"AnalyzeKEDA",
			"ChurnLevel",
			"ChurnAnalysis",
			"ChurnRecommendation",
			"ChurnLow",
			"ChurnMedium",
			"ChurnHigh",
			"ChurnCritical",
			"AnalyzeChurnFromEvents",
			"VPARecommendationInfo",
			"VPAInfo",
			"VPAContainerPolicy",
			"VPAConflictInfo",
			"VPARecommendation",
			"VPAConflictLevel",
			"VPAAdvisory",
			"VPAConflictNone",
			"VPAConflictWarning",
			"VPAConflictError",
			"AnalyzeVPA",
			"NewVPAConflictInfo",
			"AnalyzeVPAAdvisory",
			"WriteMarkdownListReport",
			"WriteHTMLListReport",
		),
		replacement: "the canonical pkg/hpa domain subpackage",
	},
	modulePath + "/pkg/hpa/healthtrend": {
		symbols:     symbolSet("HealthTrendResult"),
		replacement: "healthtrend.Result",
	},
}

type violation struct {
	file        string
	line        int
	column      int
	symbol      string
	replacement string
	dotImport   bool
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deprecated facade check: determine working directory: %v\n", err)
		os.Exit(1)
	}

	violations, err := scanRepository(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deprecated facade check: %v\n", err)
		os.Exit(1)
	}
	if len(violations) == 0 {
		fmt.Println("deprecated facade check passed")
		return
	}

	fmt.Fprintln(os.Stderr, "repository code must not add new uses of deprecated compatibility facades:")
	for _, item := range violations {
		if item.dotImport {
			fmt.Fprintf(os.Stderr, "  %s:%d:%d: dot import of deprecated facade package; use %s\n",
				item.file, item.line, item.column, item.replacement)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %s:%d:%d: %s; use %s\n",
			item.file, item.line, item.column, item.symbol, item.replacement)
	}
	os.Exit(1)
}

func scanRepository(root string) ([]violation, error) {
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open repository root: %w", err)
	}
	defer func() { _ = rootFS.Close() }()

	var violations []violation
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "node_modules":
				return fs.SkipDir
			default:
				return nil
			}
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		source, err := rootFS.ReadFile(relative)
		if err != nil {
			return err
		}
		found, err := scanSource(filepath.ToSlash(relative), source)
		if err != nil {
			return err
		}
		violations = append(violations, found...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].file != violations[j].file {
			return violations[i].file < violations[j].file
		}
		if violations[i].line != violations[j].line {
			return violations[i].line < violations[j].line
		}
		return violations[i].column < violations[j].column
	})
	return violations, nil
}

func scanSource(filename string, source []byte) ([]violation, error) {
	fileset := token.NewFileSet()
	file, err := parser.ParseFile(fileset, filename, source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	aliases, violations, err := collectDeprecatedImports(fileset, file, filename)
	if err != nil {
		return nil, err
	}
	violations = appendSelectorViolations(fileset, file, filename, aliases, violations)
	return appendBoundaryTypeViolations(fileset, file, filename, violations), nil
}

func collectDeprecatedImports(fileset *token.FileSet, file *ast.File, filename string) (map[string]facadeSpec, []violation, error) {
	aliases := map[string]facadeSpec{}
	var violations []violation
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return nil, nil, fmt.Errorf("parse import in %s: %w", filename, err)
		}
		spec, tracked := deprecatedFacades[importPath]
		if !tracked {
			continue
		}

		alias := filepath.Base(importPath)
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		switch alias {
		case "_":
			continue
		case ".":
			position := fileset.Position(imported.Pos())
			violations = append(violations, violation{
				file:        filename,
				line:        position.Line,
				column:      position.Column,
				replacement: spec.replacement,
				dotImport:   true,
			})
		default:
			aliases[alias] = spec
		}
	}
	return aliases, violations, nil
}

func appendSelectorViolations(fileset *token.FileSet, file *ast.File, filename string, aliases map[string]facadeSpec, violations []violation) []violation {
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		qualifier, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		spec, tracked := aliases[qualifier.Name]
		if !tracked {
			return true
		}
		if _, deprecated := spec.symbols[selector.Sel.Name]; !deprecated {
			return true
		}
		position := fileset.Position(selector.Sel.Pos())
		violations = append(violations, violation{
			file:        filename,
			line:        position.Line,
			column:      position.Column,
			symbol:      qualifier.Name + "." + selector.Sel.Name,
			replacement: spec.replacement,
		})
		return true
	})
	return violations
}

func appendBoundaryTypeViolations(fileset *token.FileSet, file *ast.File, filename string, violations []violation) []violation {
	// The primary storage and grouped schema live in package hpa itself, where
	// import-selector checks cannot see unqualified compatibility aliases.
	// Inspect field type expressions in those boundary files while deliberately
	// skipping qualified canonical selectors such as churn.ChurnAnalysis.
	base := filepath.Base(filename)
	if file.Name.Name == "hpa" && (base == "types.go" || base == "analysis_groups.go") {
		rootSpec := deprecatedFacades[modulePath+"/pkg/hpa"]
		ast.Inspect(file, func(node ast.Node) bool {
			field, ok := node.(*ast.Field)
			if !ok {
				return true
			}
			ast.Inspect(field.Type, func(typeNode ast.Node) bool {
				if _, qualified := typeNode.(*ast.SelectorExpr); qualified {
					return false
				}
				ident, ok := typeNode.(*ast.Ident)
				if !ok {
					return true
				}
				if _, deprecated := rootSpec.symbols[ident.Name]; !deprecated {
					return true
				}
				position := fileset.Position(ident.Pos())
				violations = append(violations, violation{file: filename, line: position.Line, column: position.Column, symbol: ident.Name, replacement: rootSpec.replacement})
				return true
			})
			return false
		})
	}
	return violations
}

func symbolSet(symbols ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		set[symbol] = struct{}{}
	}
	return set
}
