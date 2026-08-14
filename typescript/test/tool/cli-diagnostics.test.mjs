import assert from "node:assert/strict";
import test from "node:test";
import { formatDiagnostics, sortDiagnostics } from "../../dist/cli/diagnostics.js";

test("sorts and formats logical diagnostics deterministically", () => {
    const diagnostics = [
        { path: "b.ts", line: 1, column: 1, message: "z" },
        { path: "a.ts", line: 2, column: 1, message: "b" },
        { path: "a.ts", line: 1, column: 4, message: "a" },
    ];
    assert.deepEqual(sortDiagnostics(diagnostics).map((item) => item.path + item.line), ["a.ts1", "a.ts2", "b.ts1"]);
    assert.equal(formatDiagnostics(diagnostics), "a.ts:1:4: a\na.ts:2:1: b\nb.ts:1:1: z");
});
