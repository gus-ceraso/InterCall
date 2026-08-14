# InterCall Compatibility Matrix

This matrix maps the normative protocol rules in `../README.md` and the Go
profile rules in `../go/SPEC.md` to the TypeScript implementation and its
planned durable tests. A row names a contract, not an implementation detail.
The README remains authoritative when a future implementation choice conflicts
with this table.

Test paths below are the intended paths. A row is complete only when the named
test exists and passes against the checked-out Go implementation where the row
concerns wire compatibility.

## 1. Interface language and semantic processing

| Source rule | TypeScript implementation | Test |
| --- | --- | --- |
| Interface is a UTF-8 file with declarations and no enclosing header | `src/syntax/scanner.ts`, `src/syntax/parser.ts` | `test/syntax/parse.test.ts` |
| Empty and comments-only interfaces are valid | `src/syntax/parser.ts` | `test/syntax/empty.test.ts` |
| Comments and whitespace may occur between all tokens | `src/syntax/scanner.ts` | `test/syntax/scanner.test.ts` |
| BOM is invalid; malformed UTF-8 is rejected at its first byte | `src/syntax/scanner.ts` | `test/syntax/invalid-utf8.test.ts` |
| Comments are nonnested block comments ending at the first `*/` | `src/syntax/scanner.ts` | `test/syntax/comments.test.ts` |
| Whitespace includes spaces, tabs, CR, LF, form feed, and vertical tab | `src/syntax/scanner.ts` | `test/syntax/whitespace.test.ts` |
| Identifiers are ASCII C-style identifiers and keywords are reserved | `src/syntax/scanner.ts` | `test/syntax/identifiers.test.ts` |
| Identifiers are case-sensitive | `src/syntax/validator.ts` | `test/syntax/case-sensitivity.test.ts` |
| Declarations are type, exception, or procedure declarations only | `src/syntax/parser.ts` | `test/syntax/parse.test.ts` |
| A type names any valid type specifier and ends with `;` | `src/syntax/parser.ts` | `test/syntax/types.test.ts` |
| An exception has an optional payload type | `src/syntax/parser.ts` | `test/syntax/exceptions.test.ts` |
| A procedure has ordered parameters and an optional return type | `src/syntax/parser.ts` | `test/syntax/procedures.test.ts` |
| A list consumes one complete type specifier | `src/syntax/parser.ts` | `test/syntax/nesting.test.ts` |
| Records are ordered, closed, and may be empty | `src/syntax/parser.ts`, `src/syntax/validator.ts` | `test/syntax/records.test.ts` |
| Named references resolve only to earlier type declarations | `src/syntax/validator.ts` | `test/syntax/references.test.ts` |
| Named types cannot be recursive, including through lists | `src/syntax/validator.ts` | `test/syntax/recursion.test.ts` |
| Global type, exception, and procedure names share one scope | `src/syntax/validator.ts` | `test/syntax/global-scope.test.ts` |
| Procedure parameters and records have independent local scopes | `src/syntax/validator.ts` | `test/syntax/local-scope.test.ts` |
| Every declaration is validated, including unreachable declarations | `src/syntax/validator.ts` | `test/syntax/unreachable.test.ts` |
| Duplicate names are rejected in their own scope | `src/syntax/validator.ts` | `test/syntax/duplicates.test.ts` |
| Canonical formatting is independent of input formatting | `src/syntax/formatter.ts` | `test/syntax/formatter.test.ts` |
| Documentation attachment is semantic and comments may be discarded | `src/syntax/docs.ts` | `test/syntax/documentation.test.ts` |
| Documentation normalizes CR/LF, indentation, and blank lines | `src/syntax/docs.ts` | `test/syntax/documentation-normalization.test.ts` |
| Canonical formatting preserves declaration and field order | `src/syntax/formatter.ts` | `test/syntax/formatter-golden.test.ts` |
| Syntax walks use explicit stacks for unrestricted nesting | `src/syntax/parser.ts`, `validator.ts`, `formatter.ts` | `test/syntax/deep-nesting.test.ts` |

## 2. Keys and canonical interface identity

| Source rule | TypeScript implementation | Test |
| --- | --- | --- |
| FNV-0 starts at zero and uses the 64-bit FNV prime modulo 2^64 | `src/syntax/key.ts` | `test/syntax/keys.test.ts` |
| Procedure input is ASCII `procedure ` plus exact name | `src/syntax/key.ts` | `test/syntax/keys.test.ts` |
| Exception input is ASCII `exception ` plus exact name | `src/syntax/key.ts` | `test/syntax/keys.test.ts` |
| Key zero is invalid | `src/syntax/validator.ts` | `test/syntax/key-validation.test.ts` |
| Procedure/exception key collisions are rejected across kinds | `src/syntax/validator.ts` | `test/syntax/key-validation.test.ts` |
| Types have no keys | `src/syntax/key.ts` | `test/syntax/keys.test.ts` |
| Key calculation ignores contracts, docs, and unrelated declarations | `src/syntax/key.ts` | `test/syntax/key-stability.test.ts` |
| Canonical body identity is SHA-256 of canonical interface bytes | `src/tool/interface-id.ts` | `test/tool/interface-id.test.ts` |
| Interface IDs are metadata, not credentials | `src/runtime/binding.ts` | `test/runtime/interface-id.test.ts` |
| Empty profile ID is SHA-256 of the exact three-exception body | `src/runtime/empty.ts` | `test/runtime/empty.test.ts` |

## 3. Value encoding

| Source rule | TypeScript implementation | Test |
| --- | --- | --- |
| All multibyte integers, lengths, counts, IDs, keys, and float bits are little-endian | `src/runtime/codec.ts` | `test/codec/endian.test.ts` |
| Values have no implicit alignment or padding | `src/runtime/codec.ts` | `test/codec/layout.test.ts` |
| `int8`/`uint8` occupy one byte | `src/runtime/codec.ts` | `test/codec/primitives.test.ts` |
| `int16`/`uint16` occupy two bytes | `src/runtime/codec.ts` | `test/codec/primitives.test.ts` |
| `int32`/`uint32` occupy four bytes | `src/runtime/codec.ts` | `test/codec/primitives.test.ts` |
| `int64`/`uint64` occupy eight bytes | `src/runtime/codec.ts` | `test/codec/primitives.test.ts` |
| Signed integers use two's-complement representation | `src/runtime/codec.ts` | `test/codec/signed-boundaries.test.ts` |
| float32 and float64 use IEEE 754 bit patterns | `src/runtime/codec.ts` | `test/codec/floats.test.ts` |
| Finite floats, infinities, and signed zero retain their bit patterns | `src/runtime/codec.ts` | `test/codec/floats.test.ts` |
| Encoders emit canonical quiet NaNs only | `src/runtime/codec.ts` | `test/codec/nan.test.ts` |
| Decoders reject every noncanonical NaN pattern | `src/runtime/codec.ts` | `test/codec/nan.test.ts` |
| Strings carry a uint64 UTF-8 byte length followed by UTF-8 bytes | `src/runtime/codec.ts` | `test/codec/strings.test.ts` |
| Strings contain Unicode scalar values and are not normalized | `src/runtime/codec.ts` | `test/codec/strings.test.ts` |
| Invalid UTF-8, surrogate code points, overlong encodings, and truncation fail | `src/runtime/codec.ts` | `test/codec/utf8-invalid.test.ts` |
| Bytes carry a uint64 byte length followed by raw bytes | `src/runtime/codec.ts` | `test/codec/bytes.test.ts` |
| Lists carry a uint64 element count followed by consecutive elements | `src/runtime/codec.ts` | `test/codec/lists.test.ts` |
| Lists of zero-width values still carry their count | `src/runtime/codec.ts` | `test/codec/zero-width.test.ts` |
| Records encode fields in declaration order without names or counts | `src/runtime/codec.ts` | `test/codec/records.test.ts` |
| Empty records occupy zero bytes | `src/runtime/codec.ts` | `test/codec/zero-width.test.ts` |
| Named types use the underlying type representation | `src/runtime/codec.ts` | `test/codec/named-types.test.ts` |
| Values consume exactly the selected payload | `src/runtime/codec.ts` | `test/codec/exhaustion.test.ts` |
| Wire lengths and counts are checked before conversion/allocation/iteration | `src/runtime/codec.ts` | `test/codec/bounds.test.ts` |
| TypeScript safety limits reject excessive nodes before allocation | `src/runtime/codec.ts` | `test/codec/resource-limits.test.ts` |
| `bytes` remains distinct from `list uint8` | `src/tool/import-emitter.ts`, `src/tool/export-mapper.ts` | `test/codec/bytes-vs-list.test.ts` |

## 4. Frames and request/response semantics

| Source rule | TypeScript implementation | Test |
| --- | --- | --- |
| Request header is request ID, procedure key, payload length, payload | `src/runtime/frame.ts` | `test/runtime/frames.test.ts` |
| Response header is response ID, exception key, payload length, payload | `src/runtime/frame.ts` | `test/runtime/frames.test.ts` |
| Headers are exactly 24 bytes with fields at offsets 0, 8, and 16 | `src/runtime/frame.ts` | `test/runtime/frame-layout.test.ts` |
| Response bit is the most significant request-ID bit | `src/runtime/frame.ts` | `test/runtime/frames.test.ts` |
| Request IDs occupy the lower 63 bits and zero is valid | `src/runtime/connection.ts` | `test/runtime/request-ids.test.ts` |
| Opposing peers allocate IDs independently | `src/runtime/connection.ts` | `test/runtime/bidirectional.test.ts` |
| Outgoing IDs increase monotonically and are never reused | `src/runtime/connection.ts` | `test/runtime/request-ids.test.ts` |
| Responses may arrive out of order | `src/runtime/connection.ts` | `test/runtime/out-of-order.test.ts` |
| Multiple outstanding requests are supported | `src/runtime/connection.ts` | `test/runtime/concurrent-calls.test.ts` |
| An outgoing request receives one response unless the connection closes | `src/runtime/connection.ts` | `test/runtime/lifecycle.test.ts` |
| Request parameters encode in declaration order without names/counts | generated codecs | `test/integration/request-order.test.ts` |
| Success uses exception key zero and the procedure return type | generated codecs | `test/integration/success.test.ts` |
| Nonzero exception keys select declared exception payloads | generated codecs | `test/integration/exceptions.test.ts` |
| Omitted returns and zero-width values require empty payloads | generated codecs | `test/integration/zero-width.test.ts` |
| Unknown request procedures are fully framed before dispatch selection | generated export dispatch | `test/integration/unknown-procedure.test.ts` |
| Malformed request arguments do not invoke the handler | generated export dispatch | `test/integration/invalid-arguments.test.ts` |
| Unknown matched response exceptions are terminal protocol errors | `src/runtime/connection.ts` | `test/runtime/matched-response-errors.test.ts` |
| Unmatched responses are consumed and ignored opaquely | `src/runtime/connection.ts` | `test/runtime/unmatched-responses.test.ts` |
| Trailing response bytes are terminal protocol errors | `src/runtime/connection.ts` | `test/runtime/matched-response-errors.test.ts` |
| Payload above 64 MiB is rejected before payload allocation/read | `src/runtime/frame.ts` | `test/runtime/frame-limits.test.ts` |
| Incomplete headers and payloads terminate as transport failures | `src/runtime/frame-reader.ts` | `test/runtime/transport-errors.test.ts` |
| Requests and responses share one noninterleaving write gate | `src/runtime/connection.ts` | `test/runtime/write-gate.test.ts` |
| A handler may call the peer while handling an incoming request | `src/runtime/connection.ts` | `test/integration/nested-calls.test.ts` |
| Independent calls have no execution-order guarantee | `src/runtime/connection.ts` | `test/integration/concurrency.test.ts` |

## 5. Go-profile runtime behavior

| Go profile rule | TypeScript implementation | Test |
| --- | --- | --- |
| Browser runtime has no Node production dependency | package export boundaries and browser build | `test/tool/browser-import-boundary.test.ts` |
| Generated bindings are static and thin | import/export emitters | `test/tool/generated-shape.test.ts` |
| No reflection, runtime registration, or handler registry | generated dispatch and codecs | `test/tool/generated-shape.test.ts` |
| Import generates typed positional clients | `src/tool/import-emitter.ts` | `test/tool/import-golden.test.ts` |
| Export discovers tagged providers from a compiler Program | `src/tool/discovery.ts` | `test/tool/export-discovery.test.ts` |
| Export and import use exact wire names and keys | `src/tool/mapping.ts` | `test/tool/name-and-key.test.ts` |
| TypeScript markers are recognized by exact compiler identity | `src/tool/type-mapping.ts` | `test/tool/type-markers.test.ts` |
| `EmptyRecord` is the only special empty-record marker | `src/tool/type-mapping.ts` | `test/tool/empty-record.test.ts` |
| `.ts` and `.tsx` providers are supported | `src/tool/discovery.ts` | `test/tool/tsx-discovery.test.ts` |
| JSX transform and preserve imports are deterministic | `src/tool/module-specifiers.ts` | `test/tool/tsx-specifiers.test.ts` |
| Strict projection depth is exactly 4,096 occurrences | `src/tool/depth.ts` | `test/tool/depth.test.ts` |
| Generated output is deterministic | all emitters | `test/tool/determinism.test.ts` |
| Generated files have exact ownership markers | `src/tool/artifacts.ts` | `test/tool/artifacts.test.ts` |
| Validation precedes output mutation | `src/tool/command.ts` | `test/tool/validation-order.test.ts` |
| Handwritten and symlink targets are never overwritten | `src/tool/artifacts.ts` | `test/tool/artifacts-safety.test.ts` |
| Owned replacements use staging and rename | `src/tool/artifacts.ts` | `test/tool/artifacts-replacement.test.ts` |
| Stale files are never deleted | `src/tool/artifacts.ts` | `test/tool/artifacts-safety.test.ts` |
| Canonical semantic metadata uses base64url chunks of at most 4,096 bytes | `src/tool/metadata.ts` | `test/tool/metadata.test.ts` |
| Generated metadata is trusted only behind the exact marker | `src/tool/metadata.ts` | `test/tool/metadata-security.test.ts` |
| Metadata preserves nested documentation and wire structure | `src/tool/metadata.ts` | `test/tool/metadata-roundtrip.test.ts` |
| Export includes fixed runtime exceptions | `src/tool/export-model.ts` | `test/tool/fixed-exceptions.test.ts` |
| Application exception matching requires exactly one direct match | generated export dispatch | `test/tool/exception-dispatch.test.ts` |
| Provider panic/rejection and encoding failure select internal exception | generated export dispatch | `test/integration/internal-exception.test.ts` |
| No-payload exceptions use process-wide generated/import mappings | `src/runtime/exceptions.ts` | `test/runtime/exceptions.test.ts` |
| Payload exceptions are returned with typed payloads | generated import emitter | `test/tool/payload-exceptions.test.ts` |
| Per-call cancellation retires only the local request | `src/runtime/connection.ts` | `test/runtime/cancellation.test.ts` |
| Late responses to canceled calls remain opaque | `src/runtime/connection.ts` | `test/runtime/cancellation.test.ts` |
| Terminal first cause is permanent | `src/runtime/connection.ts` | `test/runtime/terminal-cause.test.ts` |
| Close publishes promptly; closed waits for cleanup | `src/runtime/connection.ts` | `test/runtime/lifecycle.test.ts` |
| Handler cancellation does not require handler cooperation for teardown | `src/runtime/connection.ts` | `test/runtime/lifecycle.test.ts` |
| Context/handler state supports nested calls | `src/runtime/connection.ts` | `test/integration/nested-calls.test.ts` |
| Browser safety limits are checked before unsafe allocation/buffering | runtime limit accounting | `test/runtime/resource-limits.test.ts` |

## 6. Browser WebSocket binding

| Browser rule | TypeScript implementation | Test |
| --- | --- | --- |
| Browser is the WebSocket client | `src/browser/websocket.ts` | `test/browser/connect.test.ts` |
| Relative HTTP(S) URL becomes WS(S) | `src/browser/url.ts` | `test/browser/url.test.ts` |
| Raw connection begins at first frame | `src/browser/connect.ts` | `test/browser/raw-connect.test.ts` |
| Negotiated client sends expected-server ID first | `src/browser/negotiation.ts` | `test/browser/negotiation.test.ts` |
| Client compares server ID to its export ID | `src/browser/negotiation.ts` | `test/browser/negotiation.test.ts` |
| Negotiation uses exactly 32-byte IDs and preserves residual bytes | `src/browser/negotiation.ts` | `test/browser/negotiation.test.ts` |
| Open and negotiation have separate ten-second default timers | `src/browser/connect.ts` | `test/browser/timeouts.test.ts` |
| Caller cancellation closes setup and rejects setup | `src/browser/connect.ts` | `test/browser/cancellation.test.ts` |
| Binary messages use `ArrayBuffer` | `src/browser/websocket.ts` | `test/browser/messages.test.ts` |
| Text messages are rejected | `src/browser/websocket.ts` | `test/browser/messages.test.ts` |
| Messages may contain fragments or multiple frames | `src/runtime/chunks.ts` | `test/browser/chunking.test.ts` |
| Native WebSocket buffering is handled as a residual browser risk | `src/browser/websocket.ts` | `test/browser/backpressure.test.ts` |
| `bufferedAmount` controls send admission | `src/runtime/write-gate.ts` | `test/browser/backpressure.test.ts` |
| Browser close returns before close-event cleanup | `src/browser/websocket.ts` | `test/browser/close.test.ts` |
| Same-origin/authentication policy remains outside InterCall | Go handler/deployment | `test/integration/auth-boundary.test.ts` |

## 7. Cross-language acceptance matrix

| Scenario | Expected result | Test |
| --- | --- | --- |
| Go export → TypeScript import, string echo | identical result and frames | `test/integration/go-to-ts-echo.test.ts` |
| Go export → TypeScript import, every primitive | identical codec bytes | `test/integration/go-to-ts-primitives.test.ts` |
| Go export → TypeScript import, records/lists | identical values and bytes | `test/integration/go-to-ts-structures.test.ts` |
| Go export → TypeScript import, exceptions | typed remote exceptions | `test/integration/go-to-ts-exceptions.test.ts` |
| TypeScript export → Go import, string echo | identical result and frames | `test/integration/ts-to-go-echo.test.ts` |
| TypeScript export → Go import, every primitive | identical codec bytes | `test/integration/ts-to-go-primitives.test.ts` |
| TypeScript export → Go import, records/lists | identical values and bytes | `test/integration/ts-to-go-structures.test.ts` |
| TypeScript export → Go import, exceptions | Go application exceptions | `test/integration/ts-to-go-exceptions.test.ts` |
| Simultaneous calls in both directions | all calls complete | `test/integration/bidirectional.test.ts` |
| Nested calls in both directions | all nested calls complete | `test/integration/nested-calls.test.ts` |
| Interface-ID agreement | connection becomes usable | `test/integration/negotiation.test.ts` |
| Interface-ID mismatch | setup fails and socket closes | `test/integration/negotiation.test.ts` |
| Late canceled response | ignored without connection failure | `test/integration/cancellation.test.ts` |
| Malformed matched response | connection terminates with protocol error | `test/integration/malformed.test.ts` |
| Unmatched malformed response | payload remains opaque | `test/integration/malformed.test.ts` |
| Split/coalesced WebSocket messages | same frames as a byte stream | `test/integration/websocket-chunking.test.ts` |
| Browser Chromium, Firefox, and WebKit | full matrix passes | `test/integration/browser-matrix.test.ts` |
