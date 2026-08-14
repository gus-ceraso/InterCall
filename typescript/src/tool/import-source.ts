import { attachDocumentation, type InterfaceFile } from "../syntax/index.js";
import { validateGeneratedSource } from "./generated-check.js";
import { emitImportBinding } from "./binding-emitter.js";
import { emitImportClient } from "./client-emitter.js";
import { emitImportCodecPrograms } from "./codec-emitter.js";
import { emitImportExceptions } from "./exception-emitter.js";
import { emitImportMetadata } from "./metadata-emitter.js";
import { emitImportTypes } from "./import-emitter.js";
import { buildImportGeneration } from "./import.js";
import { buildValidatedImportGeneration } from "./import-validation.js";

export function buildValidatedImportSource(file: InterfaceFile, overrideTexts: readonly string[] = []): string {
    attachDocumentation(file);
    const generation = buildValidatedImportGeneration(file, buildImportGeneration(file), overrideTexts);
    const source = [emitImportTypes(generation), emitImportExceptions(generation), emitImportCodecPrograms(file, generation), emitImportBinding(file), emitImportMetadata(file, generation), emitImportClient(generation)].join("\n");
    validateGeneratedSource(source);
    return source;
}
