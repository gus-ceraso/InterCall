import assert from "node:assert/strict";
import ts from "typescript";
import test from "node:test";
import { scanTypeScriptDirectives, sourceDocumentation, sourceParameterDocumentation, sourceReturnDocumentation } from "../../dist/tool/index.js";

test("rejects malformed InterCall directives", () => {
    const file = ts.createSourceFile("bad.ts", "/** @intercall unknown thing */\nexport const value = 1;",  ts.ScriptTarget.ES2022, true, ts.ScriptKind.TS);
    assert.throws(() => scanTypeScriptDirectives(file), /malformed/);
});

test("scans supported directives with physical source positions", () => {
    const source = `/**\n * Adds one.\n * @intercall procedure report_progress\n * @param completed Work completed.\n * @returns Result text.\n */\nexport function report(completed: number): string { return String(completed); }\n/** @intercall field label */\nconst label = "x";\n`;
    const file = ts.createSourceFile("provider.ts", source, ts.ScriptTarget.ES2022, true, ts.ScriptKind.TS);
    const declaration = file.statements.find((statement) => statement.name?.text === "report");
    assert.equal(sourceDocumentation(declaration), "Adds one.");
    assert.equal(sourceParameterDocumentation(declaration, "completed"), "Work completed.");
    assert.equal(sourceReturnDocumentation(declaration), "Result text.");
    const directives = scanTypeScriptDirectives(file);
    assert.deepEqual(directives.map((directive) => [directive.kind, directive.arguments]), [
        ["procedure", "report_progress"],
        ["param", "completed Work completed."],
        ["returns", "Result text."],
        ["field", "label"],
    ]);
    assert.equal(directives[0].line, 3);
    assert.equal(directives[0].character > 1, true);
});
