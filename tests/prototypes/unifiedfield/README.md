# Unified field configuration: Gate 1 prototype

This **isolated nested Go module** proves public authoring contracts and defensive
ownership. It is not a second supported field API. Production packages do not
import it, the root Go module does not include it in `./...`, and no production
declarations or runtime wiring change. The relative `replace` uses this checkout's
real `store`, `schema` and `query` contracts; it is only for this repository probe.

From the repository root:

```sh
GOWORK=off go -C tests/prototypes/unifiedfield test -mod=readonly ./...
GOWORK=off go -C tests/prototypes/unifiedfield vet -mod=readonly ./...
```

Use `test -v -run TestActualPackageDependencies ./compile` in this module to print
the dependency edges derived from `go list`. Negative fixtures are `probe.go.txt`
files copied to temporary `.go` files and passed to the installed Go compiler.
Each must fail with its specific expected diagnostic. They are text fixtures so
deliberately invalid Go syntax cannot break repository-wide formatting checks.

| Package | Proof responsibility |
| --- | --- |
| `operation` | Narrow callback identities/views, typed value/change carriers, issues, reference output and one bound read contract; no executor. |
| `field` | Text/Number/Relationship/Group facades, representative policy ownership, symbolic references and a bounded public edit surface. |
| `core` | A single configuration struct proving `core -> field`; no coordination or resolver. |
| `consumer` | Application/plugin code in an external package, using public types only. |
| `compile` | Real compiler rejection/acceptance probes and actual import-boundary assertions. |

`consumer/consumer.go` is the representative application. Read it before the
implementation. Ownership tests directly call sampled callbacks to observe which
attachments survived; they do not implement or test engine lifecycle dispatch.

The probe intentionally samples only two write phases and one read phase, four
field facades, group children, and one equality declaration. Other existing phases
and shapes are not removed or declared unsupported by these samples. There is no
expression evaluator, configuration validator, runtime value conversion guard,
occurrence binding, canonical field graph, lowering, plugin scheduler, adapter or
admin integration. Constructors assume valid, non-nil nodes for this probe.

`Fields` is an ordinary caller-owned slice. Group construction and edit publication
snapshot it; immutable child values may share inaccessible backing containers.
Pointer-to-facade inputs are frozen to concrete values at these boundaries. Policy
inputs, returned policy snapshots, and retained edit drafts cannot mutate fields.
Generic carriers do not claim to clone arbitrary `T`, and callbacks retain their
author-owned closure captures. The internal attachment sentinel verifies
preservation without introducing a public opaque-state metadata framework.

The supported field configuration API is documented in the public
[fields guide](https://riducms.com/docs/fields/). This module remains a
small compiler/ownership probe; maintained production fixtures cover the full field runtime.
