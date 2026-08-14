import { requireTypeScriptIdentifier, isValidWireName } from "./name.js";
import type {
    Declaration,
    Field,
    InterfaceFile,
    Param,
    RecordType,
    TypeExpr,
} from "../syntax/index.js";

export type SelectorKind = "type" | "exception" | "procedure";
export type Step =
    | { readonly kind: "element" }
    | { readonly kind: "field"; readonly field: string };

export interface Selector {
    readonly kind: SelectorKind;
    readonly name: string;
    readonly param?: string;
    readonly return?: boolean;
    readonly steps: readonly Step[];
}

export interface Override {
    readonly selector: Selector;
    readonly name: string;
    readonly text: string;
}

export interface SelectorTarget {
    readonly selector: Selector;
    readonly declaration: Declaration;
    readonly parameter?: Param;
    readonly field?: Field;
    readonly record?: RecordType;
}

export function selectorToString(selector: Selector): string {
    let result = `${selector.kind}:${selector.name}`;
    if (selector.param !== undefined) result += `/param:${selector.param}`;
    if (selector.return === true) result += "/return";
    for (const step of selector.steps) {
        result += step.kind === "element" ? "/element" : `/field:${step.field}`;
    }
    return result;
}

export function parseSelector(text: string): Selector {
    const prefix = ["type", "exception", "procedure"].find((kind) => text.startsWith(`${kind}:`));
    if (prefix === undefined) {
        throw new Error(`invalid selector ${JSON.stringify(text)}: must start with type:, exception:, or procedure:`);
    }
    const kind = prefix as SelectorKind;
    let rest = text.slice(prefix.length + 1);
    const scanned = scanWireName(rest);
    if (scanned.name === "") throw new Error(`invalid selector ${JSON.stringify(text)}: missing declaration name`);
    rest = scanned.rest;
    const selector = { kind, name: scanned.name, steps: [] } as {
        kind: SelectorKind;
        name: string;
        param?: string;
        return?: boolean;
        steps: Step[];
    };

    if (kind !== "procedure") {
        if (rest !== "") selector.steps = parseFieldPath(text, rest);
        return selector;
    }
    if (rest === "") return selector;
    if (rest.startsWith("/param:")) {
        const parameter = scanWireName(rest.slice("/param:".length));
        if (parameter.name === "") throw new Error(`invalid selector ${JSON.stringify(text)}: missing parameter name`);
        selector.param = parameter.name;
        if (parameter.rest !== "") selector.steps = parseFieldPath(text, parameter.rest);
        return selector;
    }
    if (rest.startsWith("/return")) {
        selector.return = true;
        selector.steps = parseFieldPath(text, rest.slice("/return".length));
        return selector;
    }
    throw new Error(`invalid selector ${JSON.stringify(text)}: expected /param:<name> or /return after the procedure name`);
}

export function parseOverride(text: string): Override {
    const equals = text.indexOf("=");
    if (equals < 0) throw new Error(`invalid --ts-name override ${JSON.stringify(text)}: expected SELECTOR=TypeScriptIdentifier`);
    const selector = parseSelector(text.slice(0, equals));
    const name = text.slice(equals + 1);
    requireTypeScriptIdentifier(name);
    return { selector, name, text };
}

export function parseOverrides(texts: readonly string[]): Override[] {
    const overrides: Override[] = [];
    const seen = new Set<string>();
    for (const text of texts) {
        const override = parseOverride(text);
        const selector = selectorToString(override.selector);
        if (seen.has(selector)) throw new Error(`duplicate --ts-name override for ${selector}`);
        seen.add(selector);
        overrides.push(override);
    }
    return overrides;
}

export function resolveSelector(file: InterfaceFile, selector: Selector): SelectorTarget {
    const declaration = file.declarations.find((candidate) => declarationName(candidate) === selector.name);
    if (declaration === undefined) throw new Error(`selector ${selectorToString(selector)}: no declaration named ${JSON.stringify(selector.name)}`);
    if (selector.kind === "type") {
        if (declaration.kind !== "type-decl") throw new Error(`selector ${selectorToString(selector)}: ${JSON.stringify(selector.name)} is not a type`);
        return resolveTypeTarget(selector, declaration, declaration.type);
    }
    if (selector.kind === "exception") {
        if (isFixedException(selector.name)) throw new Error(`selector ${selectorToString(selector)}: fixed runtime exception cannot be overridden`);
        if (declaration.kind !== "exception-decl") throw new Error(`selector ${selectorToString(selector)}: ${JSON.stringify(selector.name)} is not an exception`);
        if (declaration.type === undefined && selector.steps.length > 0) throw new Error(`selector ${selectorToString(selector)}: exception has no payload`);
        return resolveTypeTarget(selector, declaration, declaration.type);
    }
    if (declaration.kind !== "procedure-decl") throw new Error(`selector ${selectorToString(selector)}: ${JSON.stringify(selector.name)} is not a procedure`);
    return resolveProcedureTarget(selector, declaration);
}

export function resolveOverride(file: InterfaceFile, text: string): { readonly override: Override; readonly target: SelectorTarget } {
    const override = parseOverride(text);
    return { override, target: resolveSelector(file, override.selector) };
}

function resolveTypeTarget(selector: Selector, declaration: Declaration, root: TypeExpr | undefined): SelectorTarget {
    if (selector.steps.length === 0) return { selector, declaration };
    if (root === undefined) throw new Error(`selector ${selectorToString(selector)}: selected declaration has no payload type`);
    const result = walkFieldPath(selector, root);
    return { selector, declaration, field: result.field, record: result.record };
}

function resolveProcedureTarget(selector: Selector, declaration: Extract<Declaration, { readonly kind: "procedure-decl" }>): SelectorTarget {
    if (selector.param !== undefined) {
        const parameter = declaration.params.find((candidate) => candidate.name.name === selector.param);
        if (parameter === undefined) throw new Error(`selector ${selectorToString(selector)}: procedure has no parameter named ${JSON.stringify(selector.param)}`);
        if (selector.steps.length === 0) return { selector, declaration, parameter };
        const result = walkFieldPath(selector, parameter.type);
        return { selector, declaration, parameter, field: result.field, record: result.record };
    }
    if (selector.return === true) {
        if (declaration.result === undefined) throw new Error(`selector ${selectorToString(selector)}: procedure has no return value`);
        const result = walkFieldPath(selector, declaration.result);
        return { selector, declaration, field: result.field, record: result.record };
    }
    if (selector.steps.length > 0) throw new Error(`selector ${selectorToString(selector)}: procedure root cannot have a field path`);
    return { selector, declaration };
}

function walkFieldPath(selector: Selector, root: TypeExpr): { readonly field: Field; readonly record: RecordType } {
    let current = root;
    for (let index = 0; index < selector.steps.length; index += 1) {
        const step = selector.steps[index]!;
        if (current.kind === "named") {
            throw new Error(`selector ${selectorToString(selector)}: named reference ${JSON.stringify(current.name.name)} is not traversed`);
        }
        if (step.kind === "element") {
            if (current.kind !== "list") throw new Error(`selector ${selectorToString(selector)}: /element requires a list`);
            current = current.elem;
            continue;
        }
        if (current.kind !== "record") throw new Error(`selector ${selectorToString(selector)}: /field:${step.field} requires an inline record`);
        const field = current.fields.find((candidate) => candidate.name.name === step.field);
        if (field === undefined) throw new Error(`selector ${selectorToString(selector)}: no field ${JSON.stringify(step.field)}`);
        if (index === selector.steps.length - 1) return { field, record: current };
        current = field.type;
    }
    throw new Error(`selector ${selectorToString(selector)}: field path has no final field`);
}

function declarationName(declaration: Declaration): string {
    return declaration.name.name;
}

function isFixedException(name: string): boolean {
    return name === "internal_exception" || name === "invalid_arguments" || name === "procedure_not_found";
}

function parseFieldPath(text: string, rest: string): Step[] {
    if (rest === "") throw new Error(`invalid selector ${JSON.stringify(text)}: missing field path (a field path must end with /field:<name>)`);
    const steps: Step[] = [];
    while (rest !== "") {
        if (!rest.startsWith("/")) throw new Error(`invalid selector ${JSON.stringify(text)}: unexpected ${JSON.stringify(rest)}`);
        rest = rest.slice(1);
        if (rest === "") throw new Error(`invalid selector ${JSON.stringify(text)}: empty step after '/'`);
        if (rest.startsWith("element") && (rest.length === "element".length || rest["element".length] === "/")) {
            steps.push({ kind: "element" });
            rest = rest.slice("element".length);
            continue;
        }
        if (rest.startsWith("field:")) {
            const field = scanWireName(rest.slice("field:".length));
            if (field.name === "") throw new Error(`invalid selector ${JSON.stringify(text)}: missing field name after /field:`);
            steps.push({ kind: "field", field: field.name });
            rest = field.rest;
            continue;
        }
        throw new Error(`invalid selector ${JSON.stringify(text)}: expected /element or /field:<name>`);
    }
    if (steps.length === 0 || steps.at(-1)!.kind !== "field") {
        throw new Error(`invalid selector ${JSON.stringify(text)}: field path must end with /field:<name>`);
    }
    return steps;
}

function scanWireName(value: string): { readonly name: string; readonly rest: string } {
    if (value === "" || (!isValidWireName(value[0]!) && value[0] !== "_")) return { name: "", rest: value };
    let offset = 1;
    while (offset < value.length && isWirePart(value[offset]!)) offset += 1;
    return { name: value.slice(0, offset), rest: value.slice(offset) };
}

function isWirePart(character: string): boolean {
    return /^[A-Za-z0-9_]$/u.test(character);
}
