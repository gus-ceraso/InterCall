export const initialisms = [
    "ACL", "API", "ASCII", "CPU", "CSS", "DNS", "EOF", "GUID", "HTML",
    "HTTP", "HTTPS", "ID", "IP", "JSON", "QPS", "RAM", "RPC", "SLA",
    "SMTP", "SQL", "SSH", "TCP", "TLS", "TTL", "UDP", "UI", "UID", "URI",
    "URL", "UTF8", "UUID", "VM", "XML", "XMPP", "XSRF", "XSS",
] as const;

const initialismSet = new Set<string>(initialisms);
const tsKeywords = new Set([
    "break", "case", "catch", "class", "const", "continue", "debugger", "default",
    "delete", "do", "else", "enum", "export", "extends", "false", "finally", "for",
    "function", "if", "import", "in", "instanceof", "new", "null", "return", "super",
    "switch", "this", "throw", "true", "try", "typeof", "var", "void", "while", "with",
    "as", "any", "boolean", "constructor", "declare", "get", "implements", "infer",
    "is", "keyof", "module", "namespace", "never", "of", "readonly", "require", "set",
    "static", "string", "symbol", "type", "undefined", "unique", "unknown", "from",
    "globalThis", "bigint", "number", "object", "package", "private", "protected", "public",
    "interface", "abstract", "asserts", "async", "await",
]);

type NameCase = "pascal" | "camel";

export function isInitialism(value: string): boolean {
    return initialismSet.has(value);
}

export function longestInitialism(value: string): string {
    let best = "";
    for (const initialism of initialisms) {
        if (initialism.length > best.length && value.startsWith(initialism)) best = initialism;
    }
    return best;
}

export function isValidWireName(name: string): boolean {
    if (name.length === 0 || (!isAsciiLetter(name.charCodeAt(0)) && name[0] !== "_")) return false;
    for (let i = 1; i < name.length; i += 1) {
        const code = name.charCodeAt(i);
        if (!isAsciiLetter(code) && !isDigit(code) && code !== 0x5f) return false;
    }
    return true;
}

export function isCanonicalWireName(name: string): boolean {
    if (name.length === 0 || !isLower(name.charCodeAt(0))) return false;
    let offset = 1;
    while (offset < name.length && (isLower(name.charCodeAt(offset)) || isDigit(name.charCodeAt(offset)))) offset += 1;
    while (offset < name.length) {
        if (name.charCodeAt(offset) !== 0x5f) return false;
        offset += 1;
        if (offset >= name.length || !isLower(name.charCodeAt(offset))) return false;
        offset += 1;
        while (offset < name.length && (isLower(name.charCodeAt(offset)) || isDigit(name.charCodeAt(offset)))) offset += 1;
    }
    return true;
}

export function isTypeScriptKeyword(name: string): boolean {
    return tsKeywords.has(name);
}

export function isValidTypeScriptIdentifier(name: string): boolean {
    if (name.length === 0 || isTypeScriptKeyword(name)) return false;
    if (!/^[\p{ID_Start}$_]/u.test(name)) return false;
    if (!/^[\p{ID_Start}$_][\p{ID_Continue}\u200c\u200d$]*$/u.test(name)) return false;
    return true;
}

export function requireTypeScriptIdentifier(name: string): string {
    if (!isValidTypeScriptIdentifier(name)) {
        throw new Error(`invalid TypeScript identifier ${JSON.stringify(name)}`);
    }
    return name;
}

export function wireToTypeScript(wire: string, nameCase: NameCase): string {
    if (!isCanonicalWireName(wire)) {
        throw new Error(`wire name ${JSON.stringify(wire)} is not canonical`);
    }
    const words = wire.split("_");
    return words.map((word, index) => {
        if (nameCase === "camel" && index === 0) return word;
        const upper = word.toUpperCase();
        if (isInitialism(upper)) return upper;
        return upper[0] + word.slice(1);
    }).join("");
}

export function typeScriptToWire(name: string, nameCase: NameCase): string {
    if (!isValidTypeScriptIdentifier(name)) throw new Error(`invalid TypeScript identifier ${JSON.stringify(name)}`);
    if (!isAsciiIdentifier(name) || name.includes("_")) {
        throw new Error(`TypeScript identifier ${JSON.stringify(name)} requires an explicit wire-name override`);
    }
    const words = splitSourceWords(name).flatMap(sourceWordToWire);
    const wire = words.join("_");
    if (!isCanonicalWireName(wire) || wireToTypeScript(wire, nameCase) !== name) {
        throw new Error(`TypeScript identifier ${JSON.stringify(name)} does not survive the ${nameCase} round trip`);
    }
    return wire;
}

function splitSourceWords(name: string): string[] {
    const words: string[] = [];
    let start = 0;
    for (let i = 1; i < name.length; i += 1) {
        if (!isUpper(name.charCodeAt(i))) continue;
        const previous = name.charCodeAt(i - 1);
        if (isLower(previous) || isDigit(previous) ||
            (isUpper(previous) && i + 1 < name.length && isLower(name.charCodeAt(i + 1)))) {
            words.push(name.slice(start, i));
            start = i;
        }
    }
    words.push(name.slice(start));
    return words;
}

function sourceWordToWire(word: string): string[] {
    let allUpperOrDigit = true;
    for (let i = 0; i < word.length; i += 1) {
        const code = word.charCodeAt(i);
        if (!isUpper(code) && !isDigit(code)) {
            allUpperOrDigit = false;
            break;
        }
    }
    if (!allUpperOrDigit) return [lowerAscii(word)];
    if (isUpper(word.charCodeAt(0)) && allDigits(word.slice(1))) return [lowerAscii(word)];

    const words: string[] = [];
    let rest = word;
    while (rest !== "") {
        const initialism = longestInitialism(rest);
        if (initialism === "") throw new Error(`uppercase run ${JSON.stringify(word)} is not a fixed-initialism sequence`);
        words.push(lowerAscii(initialism));
        rest = rest.slice(initialism.length);
    }
    return words;
}

function isAsciiIdentifier(name: string): boolean {
    if (name.length === 0 || (!isAsciiLetter(name.charCodeAt(0)) && name[0] !== "_")) return false;
    for (let i = 1; i < name.length; i += 1) {
        const code = name.charCodeAt(i);
        if (!isAsciiLetter(code) && !isDigit(code) && code !== 0x5f) return false;
    }
    return true;
}

function isAsciiLetter(code: number): boolean {
    return isLower(code) || isUpper(code);
}

function isLower(code: number): boolean {
    return code >= 0x61 && code <= 0x7a;
}

function isUpper(code: number): boolean {
    return code >= 0x41 && code <= 0x5a;
}

function isDigit(code: number): boolean {
    return code >= 0x30 && code <= 0x39;
}

function allDigits(value: string): boolean {
    for (let i = 0; i < value.length; i += 1) if (!isDigit(value.charCodeAt(i))) return false;
    return true;
}

function lowerAscii(value: string): string {
    let result = "";
    for (const character of value) {
        const code = character.charCodeAt(0);
        result += isUpper(code) ? String.fromCharCode(code + 0x20) : character;
    }
    return result;
}
