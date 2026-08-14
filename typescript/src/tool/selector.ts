import { requireTypeScriptIdentifier, isValidWireName } from "./name.js";

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
        steps: readonly Step[];
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
    return texts.map(parseOverride);
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
