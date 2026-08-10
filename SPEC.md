# InterCall Go Proof of Concept

This document is the frozen design for the Go proof of concept. `README.md` is
authoritative for the InterCall interface language and wire protocol; this
specification defines the Go mapping, generated artifacts, runtime behavior, and
tooling. A Go implementation must satisfy both documents. If they appear to
conflict, the protocol in `README.md` wins.

## Scope

The proof of concept includes:

- a reusable runtime over an established, reliable, ordered, full-duplex raw
  byte stream;
- a complete `.intercall` lexer, parser, validator, semantic comment model, and
  formatter;
- `intercall-go export`, using Go package discovery to emit an interface and a
  generated export binding;
- `intercall-go import`, using the exact bytes of an interface to emit a
  generated import binding;
- statically generated typed callers, dispatch, value codecs, and exception
  mappings; and
- every InterCall primitive, list, record, named-type, procedure, and exception
  form defined by `README.md`.

This is a trusted-peer proof of concept. It has no policy resource limits, but
it must still reject structurally invalid data, check integer arithmetic, and
check every `uint64` conversion to a native size before allocation or
iteration.

## Repository and Package Layout

The repository root is the Go module:

```text
module github.com/cerasos/intercall

go 1.26.5
```

The root Go package is `intercall` and contains the public shared runtime.
`cmd/intercall-go` contains the CLI. Interface syntax, Go discovery, and code
generation live under `internal/`; the intended divisions are
`internal/syntax`, `internal/godiscovery`, and `internal/generate`.

The runtime and interface-syntax implementation use only the standard library.
Discovery may depend on `golang.org/x/tools/go/packages`. Generated bindings
may import the standard library, provider packages in an export binding, and
the shared root runtime; they must not import generator internals or any other
third-party runtime.

Each invocation generates one binding into a dedicated, generator-owned Go
package. An import binding and an export binding are always separate packages
and cannot share an output directory. Generated export wrappers call provider
package functions directly. There is no runtime registration, reflection,
public application handler table, or generated client object.

## Interface Processing

### Parsing and validation

The parser accepts exactly the grammar and UTF-8 rules in `README.md`. It builds
an AST with source spans and validates every declaration, type reference,
identifier scope, reserved word, declaration key, and key collision. It accepts
an empty interface. It provides no syntax extensions.

Parsing, protocol validation, and Go projection are distinct stages. A file may
be a valid InterCall interface yet fail import because its names cannot be
projected to Go without an override. Go-specific runtime exception restrictions
are also import checks, not changes to the InterCall grammar.

### Semantic comments and formatting

Comment preservation is semantic, not a concrete-syntax round trip. The AST has
one optional documentation string on each declaration, procedure parameter,
record field, and type-specifier occurrence. Type occurrences include a declared
underlying type, exception payload, procedure return, parameter or field type,
list element, named reference, primitive, list, and inline record. An exception
without a payload and a procedure without a return have no corresponding type
occurrence.

For interface source, a documentation group is one or more consecutive block
comments in the syntactic trivia immediately before an eligible slot, with no
blank line between the comments or before the slot; a blank line contains only
spaces or tabs between line terminators. A same-line group after a semicolon
that completed another node is trailing and unassociated. A group before a
complete declaration, parameter, or field documents that node. A same-line
group between its name, `}`, or `list` keyword and the following type documents
the following type occurrence. These rules apply recursively. All other
comments are unassociated, and the formatter discards them.

Documentation is normalized by this exact algorithm:

1. Convert CRLF and CR line endings to LF and split into lines.
2. Remove trailing spaces and tabs from every line.
3. Remove leading and trailing lines containing only spaces or tabs.
4. Find the longest byte prefix consisting only of spaces and tabs that is
   present on every nonblank line, and remove it from every nonblank line.
5. Join the remaining lines with LF. An empty result means no documentation.

Each interface block-comment body is normalized separately. A documentation
group joins its nonempty normalized bodies, in source order, with exactly two LF
bytes. In handwritten Go source, documentation starts with the exact string
returned by `(*go/ast.CommentGroup).Text`. Recognized source-directive lines are
parsed and removed, `@param` and `@return` payloads are assigned to their slots,
and all retained text is normalized by the same algorithm. A preceding Go doc
group is eligible; a trailing Go comment is unassociated. Generated source owned
by `intercall-go` represents semantic documentation only with the metadata
format defined below.

A retained normalized string containing the bytes `*/` is a formatting or
export error at the source comment. The interface formatter does not escape or
rewrite documentation text. This rule guarantees that every emitted InterCall
block comment terminates at the intended location.

For a nonempty normalized string `D`, `doc(indent, D)` emits exactly:

- `indent + "/* " + D + " */\n"` when `D` has no LF; or
- `indent + "/*\n"`, then each nonempty line as
  `indent + "    " + line + "\n"` and each empty line as `"\n"`, then
  `indent + "*/\n"` when `D` contains LF.

`indent` is four ASCII spaces per enclosing record or procedure block. A
declaration, parameter, or field document is emitted with `doc` immediately
before that complete node. A documented type occurrence is emitted by ending
its prefix with LF, emitting `doc` at the type's indentation, and then emitting
the type at that indentation. An undocumented type follows its prefix with one
ASCII space. This applies recursively after `list`. A return document therefore
appears between a procedure's `}` and return type, and an exception-payload
document appears between the exception name and payload type.

The remaining formatter rules are:

- emit declarations in AST order and never wrap for a target line width;
- use one ASCII space between adjacent keywords and names on the same line and
  immediately before `{`, with no other horizontal whitespace outside
  indentation or documentation;
- write `type name`, `exception name`, procedure/field/parameter names, and type
  occurrences using the prefix rule above;
- write `record {}` and an empty procedure parameter block `{}` inline;
- write each nonempty record or procedure block with `{` on the current line,
  one field or parameter per line, and `}` at the parent indentation;
- indent record fields and procedure parameters by four additional spaces;
- emit each semicolon immediately after the preceding type or `}`;
- place one blank line between top-level declarations;
- end a nonempty file with one LF; and
- emit zero bytes for an empty interface.

Thus an undocumented return is `} T;`, while a documented return is `}`, LF,
`doc("", D)`, then `T;`. The same template applies after an exception name and
after `list`. These rules define bytes for every retained attachment slot. The
formatter handles every valid AST and is used for exported interfaces. Import
parses but never reformats its input.

Generated projected symbols retain conventional Go documentation. A symbol
projecting a declaration starts with this exact line, where `<kind>` is `type`,
`exception`, or `procedure`:

```go
// <GoName> is the Go projection of InterCall <kind> <wire-name>.
```

A projected field starts with the following line; `<field-selector>` is the
field path from the structural grammar below.

```go
// <GoName> is the Go projection of InterCall field <field-selector>.
```

Other generated exported symbols use fixed generator prose. If a declaration's
root has any semantic documentation, its projected symbol also gets this fixed
second paragraph:

```go
//
// InterCall semantic documentation is preserved in generated package metadata.
```

Imported text is not copied into readable Go prose. Instead, each import output
contains exactly one unexported constant whose Go doc is the authoritative
semantic-documentation carrier. Its collision-free private name is selected by
the ordinary deterministic private-name rules, and its shape is:

```go
// <metadataName> preserves InterCall semantic documentation.
//
// @intercall-go doc <doc-selector> <payload>
const <metadataName> = ""
```

There is one metadata line for each nonempty documentation slot and no metadata
line for an empty slot. Multiple metadata lines are consecutive, with no blank
comment lines, in the order defined below. If there are no nonempty slots, the
carrier contains only its conventional lead. The imported text never appears
literally elsewhere in Go source.

A documentation selector is an ASCII structural path. Its grammar is defined by
these path categories, where every `<name>` is an exact InterCall identifier:

```text
declaration-path = type:<name> | exception:<name> | procedure:<name>
parameter-path   = procedure:<name>/param:<name>
type-root        = type:<name>/underlying
                 | exception:<name>/payload
                 | procedure:<name>/return
type-path        = type-root
                 | parameter-path/type
                 | field-path/type
                 | type-path/element
field-path       = type-path/field:<name>
doc-selector     = declaration-path/doc
                 | parameter-path/doc
                 | field-path/doc
                 | type-path/doc
```

The grammar needs no escaping because InterCall identifiers contain neither `:`
nor `/`. A type path names one type-specifier occurrence, not the declaration of
a referenced named type. `/element` enters the element occurrence of a list, and
`/field:<name>` enters a field node of an inline record. `/type` then enters that
parameter's or field's type occurrence. A step is valid only for the indicated
AST node. For example:

```text
type:user/doc
type:user/underlying/doc
type:user/underlying/field:name/doc
type:user/underlying/field:name/type/doc
procedure:get_user/param:name/doc
procedure:get_user/param:name/type/doc
procedure:get_user/return/element/doc
exception:failure/payload/field:code/type/doc
```

Metadata lines use exactly `// @intercall-go doc `, the selector, one ASCII
space, and the payload. The payload is the RFC 4648 base64url encoding of the
normalized UTF-8 bytes, without `=` padding. It must be nonempty, contain only
`A-Z`, `a-z`, `0-9`, `_`, and `-`, decode as valid UTF-8, decode to a nonempty
normalized string, and reproduce the same payload when re-encoded. Thus NUL,
U+FEFF, line terminators, comment markers, and directive-like text all become
ASCII comment bytes and always produce valid Go source.

The importer emits metadata in interface AST preorder. For a declaration it
visits the declaration document first. It then visits a type declaration's
underlying type, an exception's payload, or a procedure's parameters in order
followed by its return. For a parameter it visits the parameter document and
then its type. For a type occurrence it visits the type document and then its
list element or its record fields in order. For a field it visits the field
document and then its type. Slots without documentation emit nothing but do not
change this order.

Selectors and payloads are validated when generated and when consumed. A
generated import package must contain exactly one carrier, every metadata line
must match the exact template, and no selector may occur twice. Each emitted
selector must resolve to exactly one eligible slot in the imported AST. On later
export, metadata for each consumed declaration root must resolve against that
projected declaration's reconstructed InterCall AST. Unresolved, structurally
invalid, duplicate, noncanonical, or conflicting metadata is an error. The
exporter assigns the decoded string directly to the selected slot and never
scans it as Go prose or as a source directive.

Generated named types retain their generator-owned type line. After the lead and
optional fixed metadata note, their doc ends with exactly:

```go
//
// @intercall type <wire-name>
```

In a file whose first line is the exact generated marker, the exporter validates
generated comment templates and extracts only generator-owned type and
documentation metadata. Conventional leads, fixed notes, and machine lines are
not semantic documentation.

The generated marker is the classification boundary. In an unmarked handwritten
file, an `@intercall-go doc`-like line is ordinary Go prose and is never
metadata. A handwritten file that copies the exact marker is treated as
generated: a fully valid carrier is metadata, and any template violation is an
error. Therefore ordinary unmarked prose, including text equal to any
`@intercall`, `@param`, `@return`, or `@intercall-go` line, cannot become a
semantic directive after import or later export. Comment text never determines
an exception's `Error` string.

### Interface digests

Every generated binding embeds the SHA3-256 digest of the exact interface-file
bytes represented by that binding. Export computes the digest after canonical
formatting and embeds the bytes that it writes through `--interface`. Import
computes the digest from its input before parsing or normalization. Import-only
Go naming overrides do not affect it.

The digest is descriptor identity metadata and a diagnostic aid only. The proof
of concept has no handshake, digest exchange, or remote digest verification.

## Go Export Model

### Package discovery

`intercall-go export` operands are standard Go package patterns. File operands
and an automatic module-wide scan are not supported. Discovery uses the active
module or workspace, build tags, `GOOS`, `GOARCH`, and other build settings used
by the Go command. Tests and test variants are excluded.

A package directly matched by an operand is an **explicitly scanned package**;
dependencies loaded only for type checking are not explicitly scanned. Pattern
overlap is deduplicated by canonical import path. A pattern that matches no
package is an error.

Generated Go files, as recognized by Go's standard generated-file marker, are
ignored when selecting procedures and application exceptions. If a selected
signature reaches a type declared in a generated file, the exporter still
inspects that type's directives, field tags, documentation, and type graph.

Every explicitly scanned package must type-check and be an importable non-`main`
package. The export output directory must resolve to an importable package path
in the active module or workspace. That output package must be able to import
each provider and each package that supplies an application exception or
reachable named type. Go `internal` visibility, module or workspace visibility,
and import-cycle failures are fatal. Cross-module discovery is permitted when
the active workspace makes the packages importable.

### Procedure selection and filtering

Only package-level functions carrying an `@intercall procedure` directive are
eligible. With no `--include`, every eligible function in every explicitly
scanned package is selected. Repeatable `--include` flags restrict that set;
repeatable `--exclude` flags then remove functions, so exclusion wins.

Each filter value has the exact form `full/import/path.Symbol`. It must resolve
to an exported package-level function in an explicitly scanned package, and
that function must carry a valid procedure directive. Unknown, unexported,
untagged, duplicate, malformed, method, or generic selectors are errors. A
symbol named by both filters is valid and excluded.

Filtering affects procedures only. Every valid tagged application exception in
a non-generated file of every explicitly scanned package remains in the global
interface. Reachable ordinary types are computed after procedure filtering but
include types reached through all retained application exception payloads.

### Source directives

Recognized directives are line-oriented within Go documentation comments. After
comment markers and surrounding whitespace are removed, their forms are:

```text
@intercall procedure [wire-name]
@intercall exception [wire-name]
@intercall type [wire-name]
@intercall param GoName wire_name
@param GoName text
@return text
```

Square brackets above mean an optional operand; they are not literal syntax.
The optional wire name overrides the default source-to-wire projection.

`@intercall procedure` applies only to a package-level function.
`@intercall exception` applies only to one exported sentinel variable or one
exported named struct type. `@intercall type` applies only to an ordinary
defined type; it is mandatory for every reachable ordinary defined type.
`@intercall param` renames a wire parameter and is valid only in the selected
function's documentation. `@param` fills that parameter's declaration-document
slot, and `@return` fills the optional result type's documentation slot. The
context parameter and final `error` result are not wire values and cannot be
targets.

A sentinel declaration containing more than one variable is ambiguous and is
rejected. Every recognized directive must occupy its complete logical line,
except for the documented trailing operands. In ordinary handwritten files,
`@intercall-go` is not a directive prefix and is retained as prose. In a
validated generated file, the exporter extracts only the generator-owned
complete machine lines defined above and does not apply handwritten-comment
parsing. Duplicate, malformed, unknown, or contradictory `@intercall`
directives are errors, as are unknown parameter names, duplicate parameter or
return documentation, and directives on the wrong Go object. A bare
`@intercall` is invalid. Other doc tags, such as `@deprecated`, are ordinary
prose and are retained.

The exporter diagnoses malformed InterCall directives on package-level objects
in explicitly scanned source, and on every reachable type even when it is in a
dependency or generated file. CLI flags cannot rename wire declarations;
source directives and struct tags own all export-side wire names.

### Implementation function signatures

A selected implementation must be an exported, non-generic, non-variadic,
package-level function with no receiver. Its signature is exactly one of:

```go
func(context.Context, P1, ..., Pn) error
func(context.Context, P1, ..., Pn) (T, error)
```

The first parameter's type must be exactly `context.Context` by Go type
identity. The final result must be the predeclared `error` type. Aliases that
resolve to those exact types are acceptable; a defined lookalike is not. There
is at most one data result, and it precedes `error`. Every wire parameter must
have a nonblank Go name. The context and results may be unnamed.

Methods, generic functions, generic reachable types, variadics, and any other
result form are rejected. A generated wrapper always passes its handler context
as the first argument.

### Value and type mapping

The mapping is exact:

| Go source type | InterCall type |
| --- | --- |
| `int8`, `int16`, `int32`, `int64` | same name |
| `uint8`, `uint16`, `uint32`, `uint64` | same name |
| `float32`, `float64` | same name |
| `string` | `string` |
| source `[]byte` | `bytes` |
| source `[]uint8` | `list uint8` |
| `[]T` otherwise | `list T` |
| anonymous `struct { ... }` | inline `record { ... }` |
| tagged ordinary defined type | named InterCall type declaration |
| Go alias | its resolved target, with no declaration |

The exporter preserves the source distinction between `byte` and `uint8`
through alias RHSs. A slice maps to `bytes` only when its source/alias expansion
uses the predeclared spelling `byte` for that slice element; an expansion using
`uint8` maps to `list uint8`. For example, `type B = []byte` maps to `bytes`,
while `type U = []uint8` maps to `list uint8`. A defined `type B []byte` requires
an `@intercall type` directive and declares a named type whose underlying wire
type is `bytes`.

Ordinary defined types are never flattened. Every reachable defined type must
have exactly one `@intercall type [wire-name]` directive, and it produces one
named InterCall declaration. An untagged reachable defined type is an error.
Aliases produce no declaration and eventually resolve to a primitive, slice,
anonymous struct, or tagged defined type. Every reachable defined type from
another package must be exported; in practice, every defined type that the
separate generated binding must name must be exported and importable.

Recursive type graphs are rejected, including recursion through slices or
anonymous records. Any reachable generic declaration or instantiation is
rejected.

The following are unsupported as wire values, even when their element or
underlying types would otherwise be supported:

- `int`, `uint`, `uintptr`, `bool`, and complex numbers;
- arrays, pointers, maps, interfaces, channels, and functions; and
- `unsafe.Pointer` and all other unsafe forms.

The predeclared `error` interface is permitted only as the mandatory final
function result.

### Records

Go struct fields map in source order. Every field is required and must be
exported. Blank fields and embedded fields are rejected. The only recognized
wire tag is:

```go
Field T `intercall:"wire_name"`
```

Its value must be exactly one valid, nonreserved InterCall identifier. Empty
values, `-`, comma options, and duplicate or malformed `intercall` keys are
errors. Other struct-tag keys and unrelated tag options are ignored. There is no
omission, optionality, or default-value behavior.

Anonymous structs always remain inline records, including in nested records,
lists, parameters, returns, and exception fields. `struct{}` maps to
`record {}`.

A nil Go slice encodes as an empty InterCall list or `bytes` value. Decoding an
empty list or `bytes` value produces a non-nil, zero-length Go slice. Codecs for
lists of zero-width elements process the count and native length but do not loop
over the elements.

### Naming and collisions

The default source-to-wire projection is lower snake case. The default
wire-to-Go projection is Pascal case for declarations and fields and camel case
for parameters. The fixed initialism list is:

```text
ACL API ASCII CPU CSS DNS EOF GUID HTML HTTP HTTPS ID IP JSON QPS RAM RPC
SLA SMTP SQL SSH TCP TLS TTL UDP UI UID URI URL UTF8 UUID VM XML XMPP XSRF XSS
```

Default projection operates on ASCII only. A canonical wire name matches
`[a-z][a-z0-9]*(_[a-z][a-z0-9]*)*`. Any other valid InterCall spelling,
including uppercase letters, a leading or trailing underscore, or consecutive
underscores, requires an import `--go-name` override.

Wire-to-Go projection splits a canonical wire name at underscores. A word is an
initialism only when its uppercase spelling equals a complete entry in the fixed
list; initialisms are never recognized as substrings. An ordinary word is
projected by uppercasing its first letter and leaving its lowercase letters and
digits unchanged. Pascal case concatenates those projected words. Camel case
uses the lowercase wire spelling for the first word and Pascal projection for
later words. Thus:

```text
user_id       -> UserID / userID
http_url      -> HTTPURL / httpURL
utf8_value    -> UTF8Value / utf8Value
api2_client   -> Api2Client / api2Client
sha_value     -> ShaValue / shaValue
```

The first result on each line is Pascal and the second is camel. `api2` is not
the complete initialism `API`, and `sha` is not in the fixed list.

Source-to-wire projection first rejects a source identifier containing a
non-ASCII byte or underscore. It then tokenizes the identifier with this ASCII
scanner:

1. Scan left to right and place a boundary before an uppercase letter whose
   predecessor is lowercase or a digit.
2. Place a boundary before the last uppercase letter of an uppercase run when
   that letter is followed by lowercase. This is the usual `HTTPServer` split
   into `HTTP` and `Server`.
3. For each resulting token made only of uppercase letters and digits, accept a
   single uppercase letter optionally followed by digits, or segment the entire
   token as fixed initialisms using longest match first. Reject the identifier
   if any bytes remain. Initialisms containing digits, such as `UTF8`, match as
   complete list entries; digits never form a word by themselves.
4. Every other token must be one leading letter followed only by lowercase
   letters or digits. Lowercase is permitted on the first token only for a
   parameter's camel-case name.
5. Lowercase every token, join them with one underscore, project that candidate
   back to Pascal or camel case as appropriate, and accept it only if the result
   is byte-for-byte equal to the source identifier.

Fixed initialisms are considered only in step 3, so they never split an ordinary
mixed-case word such as `Identity`. Unknown uppercase runs are errors rather than
sequences of guessed one-letter words. Representative results are:

```text
HTTPServer     -> http_server
HTTPURL        -> http_url
UserID         -> user_id
Version42ID    -> version42_id
UTF8Value      -> utf8_value
ShaValue       -> sha_value
SHAValue       -> error: unknown uppercase run SHA
API2Client     -> error: API2 is not a complete initialism sequence
User_ID        -> error: underscore in source identifier
```

An explicit procedure, exception, or type wire-name directive,
`@intercall param`, or field tag bypasses source projection for that name. A
noncanonical wire name or any source name rejected above therefore remains
usable only through its corresponding explicit override.

All projected identifiers must be valid, non-keyword Go identifiers with the
required visibility. The generator rejects any lossy projection, Go keyword,
unexportable declaration or field name, or collision in a Go package or local
scope. Wire names and their FNV-0 keys must likewise be unique under the rules in
`README.md`. The generator never resolves a public name collision by appending a
number or silently escaping a name.

The default exception wire name is the ordinary lower-snake projection of the
exact Go sentinel or type identifier. The exporter does not strip `Err`,
`Error`, or any other prefix or suffix.

### Application exceptions

A no-payload application exception is an exported package-level variable,
assignable to `error`, tagged with `@intercall exception [wire-name]`. It is a
sentinel. Generated dispatch converts its current value to the `error` interface
and compares `err == error(provider.Sentinel)`. The interface-to-interface
comparison compiles for every declared type assignable to `error`. If a dynamic
sentinel value is not comparable, the comparison panics; the dispatch recovery
boundary converts that outcome to `internal_exception`.

A payload application exception is an exported tagged named struct type `T` for
which `*T` implements `error`. Its fields map as an inline exception-payload
record under the ordinary record rules. Generated dispatch uses a direct
`err.(*T)` assertion. The tagged type is exception metadata, not an ordinary
named wire type; it cannot also carry `@intercall type`, and no procedure value
or ordinary type declaration may reach it. Ordinary tagged types reached by its
fields remain normal named declarations.

Every tagged exception selected from a non-generated file in every explicitly
scanned package is available to every generated handler. Dependencies that were
not explicitly matched do not contribute exceptions merely because they
contain tagged errors.

For each non-nil error returned by a provider, generated code evaluates every
direct sentinel comparison and payload type assertion. Exactly one match sends
that application exception. Zero matches, multiple matches, wrapped errors, a
nil `*T` payload held in a non-nil error interface, or a panic during matching
send `internal_exception`. A comparison panic may stop the remaining tests
because its outcome is already fixed. Dispatch never uses `errors.Is` or
`errors.As`. Provider data results are ignored when the error is non-nil.

A provider panic sends `internal_exception`; panic values and stacks are not
logged or exposed. If a successful result or a matched application payload
cannot be encoded, dispatch instead sends the no-payload
`internal_exception`.

### Export declaration order

An exported interface is deterministic. Build a dependency graph whose nodes are
reachable ordinary type declarations and whose edges point from each type to
every named type it references. Repeatedly choose the lexicographically smallest
wire name among nodes whose dependencies have all been emitted, emit it, and
remove it from the graph. If nodes remain but none is ready, report the recursive
type graph instead of emitting output.

After all types, emit exceptions by exact wire-name byte order and then
procedures by exact wire-name byte order. This ready-set topological algorithm is
total for every accepted acyclic graph and ensures that each named reference
points backward, as required by `README.md`.

## Go Import Model

### Generated callers and values

Each procedure becomes an exported package function. Its first argument is
`context.Context`, followed by the procedure parameters. Its result form mirrors
the export subset:

```go
func P(context.Context, P1, ..., Pn) error
func P(context.Context, P1, ..., Pn) (T, error)
```

The function finds its connection through the shared root runtime context key,
validates the generated import descriptor's exact identity, encodes the request,
and calls the runtime. Missing context binding returns `intercall.ErrNoConnection`.
There is no generated client or runtime registration.

Primitive and list mappings are the inverse of export: `bytes` is `[]byte`, and
`list uint8` is `[]uint8`. Every InterCall type declaration becomes a defined,
exported Go type; named references remain references to that generated type
rather than being flattened. Anonymous records remain anonymous Go structs in
every position, including nested and list positions. The generator does not
create public helper types for inline records.

Generated named types carry the generator-owned `@intercall type` machine line
with the exact wire name, and generated struct fields carry exact `intercall`
tags. The package metadata carrier stores every imported semantic-documentation
slot by structural selector and canonical base64url payload. This metadata lets
a later export reconstruct a reachable generated type without selecting
procedures or exceptions from the generated file.

The generated decoder validates strings, canonical NaNs, lengths, counts,
selected response types, and exact payload exhaustion. It returns non-nil empty
slices for empty lists and bytes.

### Imported exceptions

A no-payload application exception becomes one generated exported sentinel
whose `Error` string is exactly the wire name. Its Go variable name is the
normal Pascal projection of the declaration name unless overridden.

An exception with an inline record payload becomes a generated exported named
error struct with those record fields directly. This includes an explicit
`record {}`, which remains a distinct zero-field error type. Any other payload
becomes a generated exported named error struct with one exported field:

```go
Payload <mapped-payload-type>
```

A named payload remains the corresponding named generated Go type. Payload
exception values are returned as unwrapped pointers to their generated error
struct. Their `Error` method returns exactly the exception's wire name,
independent of documentation or payload values. The generated error type's name
is the normal Pascal projection of the exception declaration unless overridden.

The three fixed Go runtime exceptions, when declared with their required
no-payload shape, map to shared root-runtime sentinels rather than generated
ones. An interface may omit a runtime exception. Import rejects a reserved
runtime name used for any other declaration kind or with a payload.

### Import-only Go name overrides

Repeatable `--go-name selector=GoIdentifier` flags resolve an otherwise lossy,
unexportable, keyword, or colliding native projection. They change generated Go
identifiers only; they never change wire names, declaration keys, encoded bytes,
or the interface digest.

Selectors use exact wire names and these forms:

```text
type:<name>
exception:<name>
procedure:<name>
procedure:<name>/param:<name>
<root>/field:<name>
<root>/element/field:<name>
procedure:<name>/return/field:<name>
```

`<root>` is a type, exception, or procedure selector. `/param:<name>` selects a
procedure parameter. `/return` enters a return type, `/element` enters a list
element, and repeated `/field:<name>` steps enter inline record fields. For a
procedure parameter's inline type, further steps follow the `/param:<name>`
step. A named type reference is not traversed; its fields are selected from its
own `type:<name>` root.

A root override names the generated function, type, or exception symbol. A
parameter override may be unexported; declaration and field overrides must be
exported. Every override must resolve exactly once and produce a valid
non-keyword Go identifier. Duplicate selectors, unresolved paths, and remaining
collisions are errors. These Go-name selectors select native identifiers only;
they are distinct from documentation selectors and never identify declaration
or type-occurrence documentation slots.

## Runtime Bindings and Public API

### Byte stream

The runtime boundary is:

```go
type ByteStream interface {
	io.Reader
	io.Writer
	io.Closer
}
```

In addition to those method sets, a stream must permit one read and one write to
run concurrently, make `Close` unblock both, deliver bytes reliably and in
order, and begin at the first InterCall frame. A connection has no initiator or
acceptor role. EOF and either read or write half-closure terminate the whole
connection.

### Binding descriptors

Every connection has exactly one immutable local `ExportBinding` and one
immutable remote `ImportBinding`. Generated packages expose singleton accessors:

```go
// In a generated export package.
func ExportBinding() *intercall.ExportBinding
func Run(context.Context, *intercall.Connection) error

// In a generated import package.
func ImportBinding() *intercall.ImportBinding
```

Each accessor returns the same descriptor pointer on every call, and any number
of connections may share it concurrently. Descriptor fields are unexported;
their public digest accessor returns a copy. Pointer identity, not equal digest
contents, is the binding identity. A copied, zero, or independently constructed
descriptor is not identical.

The public runtime and generated-code bridge is:

```go
type InterfaceDigest [32]byte

type DispatchFunc func(
	ctx context.Context,
	procedureKey uint64,
	payload []byte,
) (exceptionKey uint64, responsePayload []byte)

type RequestEncoder func() ([]byte, error)
type ResponseDecoder func(exceptionKey uint64, payload []byte) error

func NewExportBinding(InterfaceDigest, DispatchFunc) (*ExportBinding, error)
func NewImportBinding(InterfaceDigest) *ImportBinding
func (b *ExportBinding) Digest() InterfaceDigest
func (b *ImportBinding) Digest() InterfaceDigest

func NewConnection(
	stream ByteStream,
	local *ExportBinding,
	remote *ImportBinding,
) (*Connection, error)

func (c *Connection) Run(context.Context, *ExportBinding) error
func (c *Connection) WaitRunning(context.Context) error
func (c *Connection) Close() error
func (c *Connection) Err() error
func (c *Connection) CheckExportBinding(*ExportBinding) error
func (c *Connection) CheckImportBinding(*ImportBinding) error
func (c *Connection) Call(
	ctx context.Context,
	binding *ImportBinding,
	procedureKey uint64,
	encode RequestEncoder,
	decode ResponseDecoder,
) error

func WithConnection(context.Context, *Connection) context.Context
func ConnectionFromContext(context.Context) (*Connection, error)
```

`DispatchFunc`, `RequestEncoder`, `ResponseDecoder`, and `Connection.Call` are a
low-level ABI for generated code, not an application handler registry.
`DispatchFunc` receives a complete, solely owned request payload. A request
encoder is called at most once and returns a complete owned payload. A response
decoder runs in the receive goroutine; returning nil means it validated and
consumed the selected payload and stored any typed result or remote exception in
its generated closure. Returning an error means the matched response is
malformed and terminates the connection.

`NewExportBinding` rejects a nil dispatcher. `NewConnection` validates its
arguments from left to right: the stream interface, local export descriptor, and
remote import descriptor. It rejects a nil stream interface, nil descriptors,
zero descriptors, copied descriptors, and descriptors not produced by their
runtime constructors. These failures wrap `ErrInvalidByteStream` or
`ErrInvalidBinding`. An all-zero digest value is data, not by itself an invalid
descriptor.

Public runtime validation uses this precedence:

1. A method with a nil `*Connection` receiver returns `ErrInvalidConnection`
   before inspecting any argument. `Err` on a nil receiver returns that same
   sentinel.
2. A non-nil receiver method that takes a context, and
   `ConnectionFromContext`, returns `ErrInvalidContext` for a nil context.
3. Other arguments are checked in signature order. Invalid descriptors return
   `ErrInvalidBinding`; a valid descriptor with wrong identity returns a
   `BindingError` wrapping `ErrBindingMismatch`; a zero procedure key or nil
   codec callback returns `ErrInvalidCall`.
4. Lifecycle state is checked next. `Run` checks and consumes its one-shot claim
   as described below; `Call` returns `ErrNotRunning` or the terminal cause as
   appropriate; `WaitRunning` observes running or terminal state.
5. For a non-nil context, an already available lifecycle result wins; otherwise,
   the exact `ctx.Err()` wins when cancellation is observed.

Thus `((*Connection)(nil)).WaitRunning(nil)` returns `ErrInvalidConnection`, a
non-nil connection's `WaitRunning(nil)` returns `ErrInvalidContext`, and an
otherwise valid call on a new connection returns `ErrNotRunning` even if its
non-nil context is already canceled. Binding-check methods do not inspect
lifecycle state. `WithConnection` follows `context.WithValue`: it panics first
for a nil parent and otherwise panics for a nil connection. All generated paths
pass valid values.

The generated export `Run` calls `Connection.Run` with its singleton descriptor.
The method validates exact export identity before claiming `Run` or performing
frame I/O. Each generated imported call obtains the context connection and uses
`Connection.Call` with its singleton import descriptor; `Call` validates exact
import identity before invoking the encoder, allocating an ID, or performing
frame I/O. A wrong descriptor is a local binding error and does not alter the
connection.

`WithConnection` uses one unexported key owned by the root runtime and always
replaces an earlier binding in the derived context. It stores the connection
without starting it or validating its state or descriptors; generated `Run` and
call paths perform those checks. Handler contexts use this same function.
`ConnectionFromContext` and generated callers return `ErrNoConnection` if no
non-nil connection is bound.

### Lifecycle state machine

A connection has four states and a separate one-shot `Run` claim:

```text
new -> running -> stopping -> stopped
  \----------------> stopping -> stopped
```

- **new:** Construction succeeded, `Run` is unclaimed, and no receive loop is
  active.
- **running:** A correctly bound `Run` has atomically claimed the connection,
  installed its handler-root context, and is about to read or is reading frames.
- **stopping:** The first terminal cause has been selected and teardown has
  started.
- **stopped:** Teardown and the owning receive loop have completed. Handlers may
  still exist if they ignored cancellation, but they cannot start a write.

After the common receiver and context checks, descriptor validation precedes
the one-shot claim. The first correctly bound `Run` attempt consumes the claim
even if the connection was explicitly closed while new. A later correctly
bound attempt returns `ErrRunAlreadyCalled`; a wrongly bound attempt returns its
binding error without consuming the claim.

If no terminal cause already exists and its context is already canceled, the
first `Run` claims the connection, selects the exact `ctx.Err()` as the terminal
cause, and never enters running. Otherwise, a nonterminal `Run` transitions new
to running before its first blocking read. There is exactly one receive loop,
executed by the goroutine calling `Run`.

A call in new returns `ErrNotRunning` without invoking its encoder or emitting a
frame. A call in running may proceed. A call in stopping or stopped returns the
stored terminal cause. `WaitRunning` returns nil when it observes running, the
stored cause if termination wins first, or its own exact `ctx.Err()` if its wait
is canceled first. It is the startup synchronization mechanism; callers must
not use sleeps or assume that starting a `Run` goroutine makes the connection
immediately callable.

The following events terminate the connection:

- cancellation of the `Run` context;
- explicit `Connection.Close`;
- any stream read, write, EOF, or half-close failure; and
- a terminal structural or response protocol error.

One lock-protected selection chooses the first terminal cause. That exact error
wins permanently. Teardown closes the byte stream exactly once to unblock I/O,
cancels the handler-root context, and wakes every unclaimed pending caller with
the same cause. A stream `Close` cleanup error never replaces or joins the
selected cause.

`Close` selects `ErrClosed` only if no earlier terminal cause exists and is
otherwise idempotent. For a valid connection it returns nil after invoking the
one-time stream close, canceling the handler root, and waking pending calls. It
does not wait for `Run` or handlers to exit. `Err` returns nil before selection
and the exact first terminal cause afterward.

`Run` closes the stream before returning, returns the first terminal cause, and
never returns nil. It does not wait for provider goroutines. A provider that
ignores cancellation may outlive `Run`, but its handler observes terminal state
instead of writing a late response.

Every handler context derives from the `Run` context, is bound to the current
connection, and is canceled when its handler completes or the connection
terminates.

### Local errors

The root package exports stable sentinels suitable for direct comparison and
`errors.Is`:

```go
ErrNoConnection
ErrInvalidByteStream
ErrInvalidBinding
ErrBindingMismatch
ErrInvalidContext
ErrInvalidConnection
ErrInvalidCall
ErrNotRunning
ErrRunAlreadyCalled
ErrClosed
ErrRequestIDsExhausted
```

Binding mismatches use this exported structured error:

```go
type BindingError struct {
	Direction string // "export" or "import"
	Expected  InterfaceDigest
	Actual    InterfaceDigest
	Err       error // wraps ErrBindingMismatch
}
```

It implements `error` and `Unwrap() error`. `Expected` is the descriptor passed
by generated code, and `Actual` is the descriptor stored on the connection.
Invalid descriptors wrap `ErrInvalidBinding` instead.

Transport and terminal protocol failures use exported structured types:

```go
type TransportError struct {
	Operation    string
	RequestID    uint64
	HasRequestID bool
	Err          error
}

type ProtocolError struct {
	Operation    string
	RequestID    uint64
	HasRequestID bool
	Err          error
}
```

Both implement `error` and `Unwrap() error`; the wrapped transport or validation
cause remains discoverable with `errors.Is` and `errors.As`. `Operation` is one
of `read_header`, `read_payload`, `write_request`, `write_response`,
`validate_frame`, `decode_response`, or `validate_incoming_id`. Request metadata
is present whenever a complete header or an outgoing allocation supplied an ID.
Local error strings other than wire exception sentinel strings are not
compatibility promises; sentinel identity, structured fields, unwrapping, and
first-cause behavior are promises.

A per-call context cancellation returns the exact `context.Canceled` or
`context.DeadlineExceeded` from that context when cancellation wins. It does not
terminate the connection. The runtime is silent: it has no logger, panic hook,
or stack-reporting hook.

## Runtime Wire and Concurrency Behavior

### Frame reading and ownership

`Run` is the sole reader. It reads each 24-byte header and then the complete
payload. Before converting or allocating, it checks `payload_length` against
`maxInt`, checks all additions and multiplications for overflow, and checks
available payload bounds during value decoding. An incomplete header or payload
is a terminal `TransportError`. An impossible native size or other structural
frame violation is a terminal `ProtocolError`.

The read loop allocates one owned payload buffer per frame after the checked
conversion. It never lets a decoder read beyond that buffer. A request transfers
sole ownership of its complete raw payload to one handler goroutine. The runtime
treats it as immutable until generated decoding; generated codecs may transfer
solely owned byte subslices into decoded Go values because the runtime never
reuses the frame buffer.

A response is fully buffered before matching. A response whose ID is unknown,
was canceled, or names a pending entry still in `registered` state is consumed
and ignored as opaque bytes, without checking its exception key or payload. A
registered entry remains registered. Only an entry in `writing` or `waiting`
state is eligible for a response claim. An eligible response is claimed under
the pending-call lock and decoded in the receive goroutine with that call's
generated decoder. The runtime signals the caller only after that decoder
returns, and it never reuses response storage retained by successfully decoded
Go values. Unknown exception keys, invalid values, noncanonical NaNs, wrong
empty/nonempty payloads, and trailing bytes are terminal response protocol
errors. The runtime recovers a decoder panic and treats it as a terminal response
protocol error.

### Incoming requests

Incoming and outgoing ID spaces are independent. The runtime tracks every
incoming request ID from admission until its complete response write succeeds.
A request reusing an active incoming ID is a terminal protocol violation. The
runtime may stop immediately after that duplicate header because it is closing
the connection. Reuse after the earlier response write completes is allowed.

After buffering a request, the runtime atomically rejects an ID already in the
active set or reserves the new ID before starting one unbounded handler
goroutine. The handler uses generated static switch dispatch and codecs; it does
not use reflection. A runtime recovery boundary surrounds the complete
`DispatchFunc`, so any panic that escapes generated dispatch becomes
`internal_exception`. The handler remains active until its response write
succeeds or the connection becomes terminal.

After consuming an entire request payload, an unknown procedure key receives
`procedure_not_found`. A known procedure whose arguments are malformed or do
not consume the payload exactly receives `invalid_arguments`, and the provider
is not invoked. A provider panic, undeclared, wrapped, ambiguous, or typed-nil
application error, or result/exception encoding failure receives
`internal_exception`.

### Outgoing calls and request IDs

Outgoing request IDs are allocated monotonically from `0` through
`0x7fffffffffffffff`. They are never reused within a connection, even after a
response or cancellation. Once the final ID has been allocated, the next call
returns `ErrRequestIDsExhausted` without emitting a frame. Both peers allocate
this range independently.

Under the public validation precedence above, `Connection.Call` rejects a nil or
invalid binding with `ErrInvalidBinding`, a valid wrong binding with a
`BindingError` wrapping `ErrBindingMismatch`, and procedure key zero or a nil
encoder or decoder with `ErrInvalidCall`. A nil payload returned by an encoder
is a valid empty payload. After receiver, context, argument, and binding
validation, it performs these phases:

1. check running or terminal state, then check for a pre-canceled context;
2. invoke the generated encoder to produce a complete owned request payload;
3. recheck terminal state and context, allocate and permanently retire an ID,
   and register its pending entry;
4. acquire the connection-wide write gate in a context-aware wait;
5. claim write start under the pending/state lock;
6. write the complete frame without allowing the per-call context to interrupt
   that write; and
7. resolve the pending result under the same lock arbitration.

An encoding failure occurs before ID allocation, header construction, or gate
acquisition and returns the encoder's exact error. A pending entry is in exactly
one of `registered`, `writing`, `waiting`, or `claimed` state. While registered,
only write start, per-call cancellation, and terminal teardown are eligible to
claim or transition it. The lock winner determines the outcome. A cancellation
claim removes the entry, returns the exact context error, emits no frame, and
retires the allocated ID. A terminal claim also prevents the request write. A
response received while registered is unmatched and opaque as described above;
it neither completes nor changes the pending entry.

A successful write-start claim changes `registered` to `writing`. From that
atomic point until `Write` returns, response delivery and terminal teardown
remain eligible, but per-call cancellation is ineligible. Cancellation may be
observed and remembered only by the waiting call; it cannot claim the entry,
interrupt `Write`, invoke stream `Close`, or select a connection terminal cause.
The call does not return while its `Write` remains in progress.

If `Write` returns the full byte count and no error, the writer records success
under the lock. If the entry was already claimed by a response or terminal
cause, that claim remains unchanged. Otherwise, the writer checks `ctx.Err()`
while still holding the lock: a non-nil result must claim cancellation
immediately, and a nil result transitions the entry to `waiting`. After a
`waiting` transition, response, cancellation, and terminal teardown race
normally; the first lock claim determines the result. A cancellation claim
removes the entry, so it survives every later connection failure and makes a
later response unmatched and opaque.

If `Write` is partial or returns an error, the write failure terminates the
connection. If the entry is still unclaimed, that terminal cause claims it. A
cancellation merely observed during `writing` cannot survive the write failure
because it was not eligible to claim. A response or an earlier terminal cause
that already claimed the entry remains that call's result; in particular, a
response may claim during full-duplex writing and survives a later write
failure, although the write failure still terminates the connection for all
other work.

There is no cancellation frame. Retiring an ID or canceling a local wait does
not imply that the remote handler stops.

### Frame writing

Generated request and handler response values are append-encoded completely
into owned `[]byte` payloads. The runtime then builds one owned contiguous slice
containing the 24-byte header and payload. Encoding therefore cannot fail after
any header byte has been written, and mutable provider values are observed in
one encoding pass rather than a size pass followed by an encoding pass.

One connection-wide write gate serializes requests and responses. After its
final terminal-state check, the runtime makes one `Write` call for the complete
frame. Any returned error or byte count other than the full slice length is a
terminal `TransportError`; it does not retry a partial frame. A handler does not
complete until this write succeeds or terminal state prevents or interrupts it.
A handler that finishes after terminal selection cannot begin a write.

### Generated codecs

Codecs are static generated code with checked, append-style encoding and
bounded decoding. They implement the exact little-endian representations in
`README.md`, including:

- two's-complement exact-width integers;
- canonical output NaNs and rejection of every noncanonical input NaN;
- UTF-8 validation on both Go string encoding and wire string decoding;
- checked `uint64` lengths and list counts before `int` conversion, arithmetic,
  allocation, or iteration;
- declaration-order record fields with no padding;
- exact payload exhaustion; and
- no per-element work for a list whose resolved element encoding is zero-width.

Nil slices encode with count or length zero. Empty decoded slices are allocated
as non-nil empty slices. The proof of concept imposes no smaller policy limit
than checked native representability and available input bytes.

## Fixed Go Runtime Exceptions

The Go runtime wire exceptions are frozen for the proof of concept:

| Name | Key | Payload |
| --- | --- | --- |
| `procedure_not_found` | `0x970e76fcc5e2dacb` | none |
| `invalid_arguments` | `0x3f5fc972f8477b07` | none |
| `internal_exception` | `0x1aaec22e85996f50` | none |

Export inserts all three declarations into every interface. Their names are
reserved across the entire global InterCall namespace, so an application type,
procedure, or exception cannot use them. Their no-payload shapes and keys do not
change during the proof of concept.

Import maps recognized declarations to shared root sentinels:

```go
ErrProcedureNotFound
ErrInvalidArguments
ErrInternalException
```

Each sentinel's `Error` string is exactly its wire name. Runtime conditions, not
provider error matching, select these fixed export responses; a provider's
non-nil error must directly match exactly one declared application exception or
it becomes `internal_exception`.

A malformed request frame whose payload cannot safely be framed is terminal. A
fully framed request with unknown procedure or malformed/trailing arguments gets
the runtime response described above. Any malformed matched response, including
an undeclared exception key or trailing payload, terminates the connection.

## CLI and Generated Output

### Commands

The CLI forms are:

```text
intercall-go export --out DIR --interface FILE [--package NAME]
    [--include full/import/path.Symbol]...
    [--exclude full/import/path.Symbol]...
    PACKAGE_PATTERN...

intercall-go import --out DIR [--package NAME]
    [--go-name selector=GoIdentifier]...
    INTERFACE_FILE
```

`--out` and `--interface` are distinct export destinations. Export requires at
least one package pattern. Import requires exactly one file and reads its exact
bytes; stdin is not an input form. Export's include/exclude and import's Go-name
flags are repeatable.

The generated package name is `--package` when supplied. Otherwise, it is an
existing generator manifest's package name or, for a new output, the output
directory's base name. Explicit and inferred names must match the ASCII pattern
`[A-Za-z_][A-Za-z0-9_]*`, must not be `_` or `main`, and must not be a Go
keyword. The CLI does not sanitize a name. This restriction matches the default
ASCII native-name projection and keeps manifest package strings canonical
without a Unicode encoding policy.

### Output ownership and updates

Every generated `.go` file begins with:

```go
// Code generated by intercall-go; DO NOT EDIT.
```

The manifest is the exact file `.intercall-go.json`. Manifest schema version 1
is a JSON object with exactly these keys and types:

```json
{
  "version": 1,
  "mode": "import",
  "package": "example",
  "files": [
    "binding_gen.go"
  ]
}
```

`mode` is exactly `import` or `export`; `package` is the generated ASCII Go
package identifier; and `files` is the nonempty, complete owned Go-file set
sorted by bytewise filename order. Every string value in this schema is ASCII.
Canonical manifest bytes use the key order above, two-space JSON indentation,
literal string bytes without optional escapes, and one final LF. They contain no
timestamp, absolute path, temporary path, source path, or host-specific data. A
prior manifest must have this canonical encoding. Duplicate JSON keys, missing
or unknown keys, a noninteger or unsupported version, invalid UTF-8, and a mode
or package mismatch are errors; there is no implicit manifest migration.

Every `files` entry must equal its `path.Clean` result and be a direct-child
ASCII basename matching `[a-z][a-z0-9_]*_gen[.]go`; no normalization is applied
to an entry. Entries containing `/` or `\\`, absolute names, `.` or `..`,
duplicates, directories, the manifest name, and every other reserved or
unsupported name are invalid. Restricting ownership to these normalized
generated-Go basenames means a manifest can never claim an unknown non-Go file.

Before any containment, collision, replacement, or stale-deletion decision, the
CLI resolves every relevant path into a filesystem coordinate. Relevant paths
are `--out`, `--interface`, the manifest, every current or prior generated
target, and every replacement destination. A coordinate is computed as follows:

1. Make the requested path absolute and lexically clean it under the host
   platform's filepath rules.
2. Walk from its volume root until the path ends or the first missing component
   is reached. Resolve each existing component that acts as a directory through
   the filesystem, reject dangling links, loops, and lookup errors, and require
   the result to be a directory. A symlink naming `--out` itself may resolve to
   its directory. An existing file-target leaf is inspected without following
   it; the ownership rules below reject a leaf symlink.
3. Record the stable identity and OS-reported canonical spelling of every
   existing directory and any existing file leaf. If a component is missing,
   record the deepest existing directory identity and append that component and
   the remaining, necessarily unresolved, suffix.
4. Compare component names with the containing filesystem's native filename
   equivalence when the platform exposes it. This includes case-insensitive and
   normalization-insensitive aliases. On a filesystem that exposes only
   byte-sensitive names, comparison is bytewise.

Two nonexistent coordinates are equal when they have the same existing ancestor
identity and equivalent remaining components. One coordinate contains another
when those coordinates have the same canonical prefix by this rule. Existing
entries are also compared by native stable file identity, regardless of
spelling or hard links. `os.SameFile` is sufficient only where it exposes that
identity; an implementation uses a stronger platform file-ID facility when
needed. Any resolution ambiguity or failure is an error. Cleaned path strings
alone never establish separation, containment, or ownership.

The output directory must resolve to, or be created as, one actual directory.
After creation, the CLI records its identity. Manifest and generated-file access
is relative to that resolved directory and does not follow a leaf symlink. The
CLI then validates existing ownership:

- the manifest, when present, must be a regular non-symlink file;
- every listed file that exists must be a regular non-symlink direct child whose
  first line is the exact generated marker;
- every direct child whose name has a `.go` suffix under native filename
  equivalence must be listed by the valid manifest, so a handwritten, unowned,
  directory, or symlink Go entry is an error;
- distinct manifest, generated, and selected interface targets must have
  distinct canonical coordinates and, when they exist, distinct filesystem
  identities; and
- every staged generated filename must satisfy the same basename rule and be
  unique under the output directory's native filename equivalence.

Unknown non-Go entries are preserved and cannot appear in `files`. The manifest
itself is owned specially and is never listed or deleted as a stale file.

The interface target is checked only after coordinate resolution. If it equals
or is contained by the resolved output directory, it must not equal the manifest
or any current or prior generated target. Its final component must not have a
`.go` suffix under the containing directory's native filename equivalence. Thus
`alias/binding_gen.go` is inside `out` when `alias` resolves to `out`, and a name
such as `SCHEMA.GO` is a Go target on a case-insensitive filesystem. These rules
also cover nested and not-yet-created targets. If the target exists, it must be
a regular non-symlink file and must not share filesystem identity with any
manifest or current or prior generated file, even when reached outside `--out`.
An explicitly selected, otherwise valid non-Go interface target may be replaced
and is the only exception to preserving an unknown non-Go entry.

The generator stages every new file, formats Go source with the Go formatter,
and validates the complete staged set before replacing destinations. Immediately
before its first mutation and before each later deletion or replacement, it
re-resolves the destination, verifies the recorded output-directory identity,
and repeats the applicable containment, type, identity, ownership-marker, and
collision checks. An entry that existed during validation must still have the
same identity. An entry that was absent must still be absent.

A stale file is eligible for deletion only if its name came from a fully
validated prior manifest. If it existed during validation, deletion requires the
same regular non-symlink identity and generated marker immediately before the
operation. If it was absent, it remains untouched; its unexpected appearance
aborts generation. Operations use the resolved output directory and direct-child
basenames rather than reinterpreting an unresolved input path. The generator
deletes no other path, writes the new manifest last, and applies the same checks
to the separately staged export interface. A target whose bytes are unchanged
is not rewritten.

This is a trusted local CLI contract, not a sandbox against an attacker mutating
the filesystem concurrently. Implementations use directory-relative,
non-symlink operations or equivalent platform facilities where available and
abort on detectable changes, but they need not promise protection from an
unavoidable hostile mutation between the final check and operation. In the
absence of such concurrent mutation, symlink, hard-link, case, and normalization
aliases cannot cause an interface, manifest, generated file, or stale file to be
mistaken for a distinct target.

Generated file contents and ordering are deterministic for the same exact
inputs and Go build configuration. They contain no timestamps or absolute source
or temporary paths. Private helper names, import aliases, and file splitting may
use deterministic collision-free mangling within the owned filename rule, but
they do not alter public Go names or wire data. Generated artifacts are intended
to be checked into version control.

### Diagnostics

CLI errors use `path:line:column: message`. Lines and columns are one-based. A
source position starts at the zero-based byte offset of the first offending byte
and is converted as follows:

- line is one plus the number of LF bytes before the offset;
- column is one plus the number of bytes since the byte after the preceding LF,
  or since offset zero on the first line;
- UTF-8 multibyte code points therefore advance subsequent columns by their
  encoded byte length, and each tab advances by one column without tab-stop
  expansion;
- in CRLF, only LF advances the line and CR counts as one byte on the preceding
  line; a bare CR counts as one column and does not advance the line; and
- EOF uses offset `len(input)`, so EOF after a final LF is on the next line at
  column 1, while other EOF positions are one byte-column past the final
  byte. Invalid UTF-8 points at its first invalid byte.

These are the `go/token` byte-column rules and apply to Go and interface input.
Go diagnostics use unadjusted physical positions equivalent to
`token.File.PositionFor(pos, false)`; `//line` directives do not alter diagnostic
paths or coordinates. A diagnostic for a span uses the span's starting byte. Go
source paths use `import/path` plus the file path relative to that package;
interface diagnostics use the cleaned input path supplied to the command. Errors
without a source span use line 1, column 1 of the relevant operand.

Diagnostics are sorted by logical path, line, column, and message and are stable
for identical inputs. Diagnostics and generated files never include
staging-directory paths. Generation reports all independent discovery,
directive, projection, and interface-validation errors that can be determined
safely in one run; it emits no output when any such error exists.

## Non-Contractual Implementation Details

Only details that do not affect interoperability, public APIs, generated public
names, deterministic bytes, lifecycle outcomes, or diagnostics are left to the
implementation. These include the private runtime map/channel types, buffer
allocation strategy without pooling-visible aliasing, number of generated `.go`
files, private helper identifiers, and private import aliases. Such choices must
still obey the ownership, race, ordering, and output rules above.

## Deferred Features

The following features are outside the Go proof of concept:

- WebSocket and WebTransport adapters;
- TypeScript and all other non-Go bindings;
- handshake, protocol-version, or interface exchange and remote digest
  verification;
- authentication, authorization, policy, and procedure whitelists;
- configurable or fixed policy resource limits;
- transport dialing, listening, TLS, encryption, and other transport setup;
- transport-level or wire-level cancellation;
- streaming values or streaming procedure parameters and results;
- one transport stream per call or any independent per-call stream binding; and
- compatibility guarantees for Go toolchains older than Go 1.26.5.
