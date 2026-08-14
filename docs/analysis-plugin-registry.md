# Analysis plugin registry decision

## Decision

Do not introduce a dynamic `AnalysisPlugin` registry while the public analysis
payload remains a statically typed contract. Command construction and its
shared-flag capabilities are consolidated in `cmd.commandGroups`, but analysis
domains continue to use explicit typed wiring.

This is a deliberate deferral, not a rejection of extensibility. A registry is
appropriate only after the output contract has an extension boundary with an
explicit compatibility policy.

## Why a registry is unsafe today

An analysis domain currently crosses several boundaries that intentionally
provide compile-time checks:

- `pkg/hpa.Analysis` and grouped reports are public Go types.
- JSON schema v1 and v2 describe their serialized representation.
- Enrichment phases have different Kubernetes clients, inputs, and ordering
  requirements.
- Text rendering consumes typed results, labels, themes, and health-impacting
  decisions.
- Presets and feature flags decide both collection cost and visible output.

A `name + enrich + render` interface would hide those differences without
removing them. Adding a plugin would still require changing the typed payload
and schemas, or it would move type and compatibility errors from compilation
to runtime. It could also make enrichment ordering and health-score effects
implicit.

## Safe reduction made now

Each root command now has one `commandSpec` entry containing its constructor
and shared-flag capabilities. The former name-keyed capability map has been
removed. A registry consistency test rejects duplicate command names, duplicate
groups, nil builders, and invalid watch/workflow capability combinations.

This removes one drift-prone registration point while retaining Cobra's typed
command constructors and the existing output contracts.

## Preconditions for a future registry

A future major schema version may add a registry after all of the following are
defined:

1. A versioned extension envelope in `Analysis`, with deterministic JSON and
   schema representation.
2. Explicit plugin dependencies, execution phase, ordering, required clients,
   and health-score contribution.
3. Separate typed collection and rendering contracts rather than one broad
   interface.
4. Output-format registration and collision rules for text, JSON, YAML, and
   templates.
5. Schema generation and compatibility tests driven from the same registry.
6. Unknown-plugin behavior and a deprecation/versioning policy.

Until those conditions are met, explicit wiring is safer and easier to audit.
