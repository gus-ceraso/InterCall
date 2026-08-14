import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const ts = require(process.env.TS_MODULE ?? "typescript");
const fixtureDir = path.dirname(fileURLToPath(import.meta.url));

function loadConfig(name) {
    const configPath = path.join(fixtureDir, name);
    const read = ts.readConfigFile(configPath, ts.sys.readFile);
    assert.equal(read.error, undefined, ts.flattenDiagnosticMessageText(read.error?.messageText ?? "", "\n"));
    return ts.parseJsonConfigFileContent(
        read.config,
        ts.sys,
        fixtureDir,
        undefined,
        configPath,
    );
}

function makeProgram(config) {
    return ts.createProgram({ rootNames: config.fileNames, options: config.options });
}

function source(program, name) {
    const file = program.getSourceFile(path.join(fixtureDir, name));
    assert.ok(file, `missing source ${name}`);
    return file;
}

function findFirst(node, predicate) {
    let result;
    function visit(current) {
        if (result) return;
        if (predicate(current)) {
            result = current;
            return;
        }
        ts.forEachChild(current, visit);
    }
    visit(node);
    assert.ok(result, "expected AST node was not found");
    return result;
}

function diagnostics(program) {
    return ts.getPreEmitDiagnostics(program).map((diagnostic) =>
        ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"),
    );
}

function exportedSymbol(checker, file, name) {
    const moduleSymbol = checker.getSymbolAtLocation(file);
    assert.ok(moduleSymbol?.exports, `missing module exports for ${file.fileName}`);
    const symbol = moduleSymbol.exports.get(ts.escapeLeadingUnderscores(name));
    assert.ok(symbol, `missing export ${name}`);
    return symbol;
}

function aliasTarget(checker, symbol) {
    let current = symbol;
    while (current.flags & ts.SymbolFlags.Alias) {
        current = checker.getAliasedSymbol(current);
    }
    return current;
}

function localAliasDeclarations(file) {
    const aliases = new Map();
    file.forEachChild((node) => {
        if (ts.isTypeAliasDeclaration(node)) aliases.set(node.name.text, node);
    });
    return aliases;
}

function resolvesToMarker(checker, aliases, typeNode, markerSymbol) {
    assert.ok(ts.isTypeReferenceNode(typeNode), "alias chain must use type references");
    const name = typeNode.typeName.getText();
    const declaration = aliases.get(name);
    if (declaration) {
        return resolvesToMarker(checker, aliases, declaration.type, markerSymbol);
    }
    const symbol = checker.getSymbolAtLocation(typeNode.typeName);
    assert.ok(symbol, `missing symbol for ${name}`);
    return aliasTarget(checker, symbol) === markerSymbol;
}

function testMarkersAndDocs() {
    const config = loadConfig("tsconfig.json");
    const program = makeProgram(config);
    const errors = diagnostics(program);
    assert.deepEqual(errors, [], `compiler diagnostics: ${errors.join("; ")}`);
    const checker = program.getTypeChecker();
    const runtime = source(program, "runtime.ts");
    const provider = source(program, "provider.ts");
    const marker = exportedSymbol(checker, runtime, "Int32");
    const aliases = localAliasDeclarations(provider);

    const twoAliases = aliases.get("TwoAliases");
    assert.ok(twoAliases);
    assert.equal(resolvesToMarker(checker, aliases, twoAliases.type, marker), true);

    const emptyAlias = aliases.get("EmptyAlias");
    assert.ok(emptyAlias);
    const emptyMarker = exportedSymbol(checker, runtime, "EmptyRecord");
    assert.equal(resolvesToMarker(checker, aliases, emptyAlias.type, emptyMarker), true);

    const procedure = findFirst(
        provider,
        (node) => ts.isFunctionDeclaration(node) && node.name?.text === "addOne",
    );
    const tags = ts.getJSDocTags(procedure).map((tag) => tag.tagName.text);
    assert.deepEqual(tags, ["intercall", "param"]);
    const fullText = procedure.getSourceFile().text;
    const procedureOffset = fullText.indexOf("@intercall procedure add_one");
    assert.ok(procedureOffset >= 0);
    const position = procedure.getSourceFile().getLineAndCharacterOfPosition(procedureOffset);
    assert.equal(position.line, 21);
    assert.equal(position.character, 3);
}

function testModuleResolution() {
    const cases = [
        ["tsconfig.json", "generated.ts", "./provider.js", "provider.ts"],
        ["tsconfig-tsx-transform.json", "generated-tsx-transform.ts", "./provider-tsx.js", "provider-tsx.tsx"],
        ["tsconfig-preserve.json", "generated-tsx.ts", "./provider-tsx.jsx", "provider-tsx.tsx"],
    ];
    for (const [configName, generatedName, specifier, providerName] of cases) {
        const config = loadConfig(configName);
        const generatedPath = path.join(fixtureDir, generatedName);
        const resolution = ts.resolveModuleName(
            specifier,
            generatedPath,
            config.options,
            ts.sys,
        ).resolvedModule;
        assert.ok(resolution, `${configName}: module was not resolved`);
        assert.equal(
            path.normalize(resolution.resolvedFileName),
            path.join(fixtureDir, providerName),
        );
    }
}

function testDeepAliases() {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "intercall-ts-deep-"));
    try {
        const runtime = path.join(tempDir, "runtime.ts");
        const deep = path.join(tempDir, "deep.ts");
        fs.writeFileSync(runtime, "export type Int32 = number;\n");
        const lines = ["import type { Int32 } from './runtime.js';", "type T0 = Int32;"];
        for (let i = 1; i <= 4096; i += 1) lines.push(`type T${i} = T${i - 1};`);
        lines.push("export type Final = T4096;", "export const value: Final = 1;", "");
        fs.writeFileSync(deep, lines.join("\n"));
        const options = {
            target: ts.ScriptTarget.ES2022,
            module: ts.ModuleKind.NodeNext,
            moduleResolution: ts.ModuleResolutionKind.NodeNext,
            strict: true,
            noEmit: true,
        };
        const program = ts.createProgram({ rootNames: [runtime, deep], options });
        const errors = diagnostics(program);
        assert.deepEqual(errors, [], `deep alias diagnostics: ${errors.join("; ")}`);
    } finally {
        fs.rmSync(tempDir, { recursive: true, force: true });
    }
}

testMarkersAndDocs();
testModuleResolution();
testDeepAliases();
console.log("compiler experiments passed");
