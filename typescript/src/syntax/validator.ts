import type {
    Declaration,
    InterfaceFile,
    ProcDecl,
    RecordType,
    TypeExpr,
} from "./ast.js";
import { SyntaxDiagnostic } from "./source.js";
import { declarationKey } from "./key.js";
import type { Ident, NamedType } from "./ast.js";

interface KeyOwner {
    readonly kind: "procedure" | "exception";
    readonly name: string;
}

export function validateInterface(file: InterfaceFile): void {
    const validator = new Validator(file);
    validator.validate();
}

class Validator {
    private readonly global = new Map<string, Ident>();
    private readonly types = new Set<string>();
    private readonly keys = new Map<bigint, KeyOwner>();

    constructor(private readonly file: InterfaceFile) {}

    validate(): void {
        for (const declaration of this.file.declarations) {
            this.validateDeclaration(declaration);
        }
    }

    private validateDeclaration(declaration: Declaration): void {
        switch (declaration.kind) {
            case "type-decl":
                this.declareGlobal(declaration.name, "type");
                this.validateType(declaration.type, `type ${declaration.name.name}`);
                this.types.add(declaration.name.name);
                return;
            case "exception-decl":
                this.declareGlobal(declaration.name, "exception");
                if (declaration.type !== undefined) {
                    this.validateType(declaration.type, `exception ${declaration.name.name}`);
                }
                this.validateKey(declaration.name, "exception");
                return;
            case "procedure-decl":
                this.declareGlobal(declaration.name, "procedure");
                this.validateProcedure(declaration);
                this.validateKey(declaration.name, "procedure");
                return;
        }
    }

    private validateProcedure(declaration: ProcDecl): void {
        const seen = new Set<string>();
        const where = `procedure ${declaration.name.name}`;
        for (const parameter of declaration.params) {
            if (seen.has(parameter.name.name)) {
                throw this.error(parameter.name, `duplicate parameter ${JSON.stringify(parameter.name.name)} in ${where}`);
            }
            seen.add(parameter.name.name);
            this.validateType(parameter.type, `parameter ${JSON.stringify(parameter.name.name)} of ${where}`);
        }
        if (declaration.result !== undefined) {
            this.validateType(declaration.result, `return type of ${where}`);
        }
    }

    private declareGlobal(identifier: Ident, kind: string): void {
        const previous = this.global.get(identifier.name);
        if (previous !== undefined) {
            const position = this.file.source.position(previous.span.start);
            throw this.error(
                identifier,
                `duplicate ${kind} name ${JSON.stringify(identifier.name)} (first declared at line ${position.line})`,
            );
        }
        this.global.set(identifier.name, identifier);
    }

    private validateKey(identifier: Ident, kind: "procedure" | "exception"): void {
        const key = declarationKey(kind, identifier.name);
        if (key === 0n) {
            throw this.error(identifier, `key of ${kind} ${JSON.stringify(identifier.name)} is 0, which is invalid`);
        }
        const previous = this.keys.get(key);
        if (previous !== undefined) {
            throw this.error(
                identifier,
                `key collision: ${kind} ${JSON.stringify(identifier.name)} collides with ${previous.kind} ${JSON.stringify(previous.name)}`,
            );
        }
        this.keys.set(key, { kind, name: identifier.name });
    }

    private validateType(root: TypeExpr, where: string): void {
        type Step =
            | { readonly kind: "type"; readonly type: TypeExpr; readonly where: string }
            | { readonly kind: "record"; readonly record: RecordType; readonly where: string; next: number; readonly seen: Set<string> };
        const stack: Step[] = [{ kind: "type", type: root, where }];
        while (stack.length > 0) {
            const step = stack.at(-1)!;
            if (step.kind === "type") {
                stack.pop();
                switch (step.type.kind) {
                    case "named":
                        this.validateNamed(step.type, step.where);
                        break;
                    case "list":
                        stack.push({ kind: "type", type: step.type.elem, where: step.where });
                        break;
                    case "primitive":
                        break;
                    case "record":
                        stack.push({
                            kind: "record",
                            record: step.type,
                            where: step.where,
                            next: 0,
                            seen: new Set(),
                        });
                        break;
                }
                continue;
            }

            if (step.next >= step.record.fields.length) {
                stack.pop();
                continue;
            }
            const field = step.record.fields[step.next]!;
            step.next += 1;
            if (step.seen.has(field.name.name)) {
                throw this.error(field.name, `duplicate field ${JSON.stringify(field.name.name)} in ${step.where}`);
            }
            step.seen.add(field.name.name);
            stack.push({
                kind: "type",
                type: field.type,
                where: `field ${JSON.stringify(field.name.name)} of ${step.where}`,
            });
        }
    }

    private validateNamed(type: NamedType, where: string): void {
        if (!this.types.has(type.name.name)) {
            throw this.error(type.name, `unresolved type reference ${JSON.stringify(type.name.name)} in ${where}`);
        }
    }

    private error(identifier: Ident, message: string): SyntaxDiagnostic;
    private error(span: { readonly span: { readonly start: number; readonly end: number } }, message: string): SyntaxDiagnostic;
    private error(value: Ident | { readonly span: { readonly start: number; readonly end: number } }, message: string): SyntaxDiagnostic {
        const span = value.span;
        return new SyntaxDiagnostic(
            this.file.source.name,
            this.file.source.position(span.start),
            span,
            message,
        );
    }
}
