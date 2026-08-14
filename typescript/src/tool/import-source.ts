import type { InterfaceFile } from "../syntax/index.js";
import { validateGeneratedSource } from "./generated-check.js";
import { emitImportBinding } from "./binding-emitter.js";
import { emitImportClient } from "./client-emitter.js";
import { emitImportCodecPrograms } from "./codec-emitter.js";
import { emitImportExceptions } from "./exception-emitter.js";
import { emitImportMetadata } from "./metadata-emitter.js";
import { emitImportTypes } from "./import-emitter.js";
import { buildImportGeneration } from "./import.js";

export function buildValidatedImportSource(file: InterfaceFile): string {
    const generation = buildImportGeneration(file);
    const source = [emitImportTypes(generation), emitImportExceptions(generation), emitImportCodecPrograms(file, generation), emitImportBinding(file), emitImportMetadata(file, generation), emitImportClient(generation)].join("\n");
    validateGeneratedSource(source);
    return source;
}
