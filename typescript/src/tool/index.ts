export {
    interfaceID,
    interfaceIDHex,
    sha256,
} from "./interface-id.js";
export type { InterfaceID } from "./interface-id.js";
export { MAX_PROJECTION_DEPTH, validateProjectionDepth } from "./depth.js";
export { compileCodecProgram, compileCodecPrograms } from "./codec.js";
export { buildImportGeneration } from "./import.js";
export { buildValidatedImportGeneration } from "./import-validation.js";
export { emitImportTypes, emitTypeExpression } from "./import-emitter.js";
export { emitImportExceptions } from "./exception-emitter.js";
export type {
    ImportDeclarationRecord,
    ImportExceptionRecord,
    ImportFieldRecord,
    ImportGenerationRecord,
    ImportNamedTypeRecord,
    ImportParameterRecord,
    ImportProcedureRecord,
} from "./import.js";
export { manglePrivate, PublicNameScope } from "./mangle.js";
export type {
    CodecRootFact,
    ExportExceptionFact,
    ExportGeneration,
    ExportProcedureFact,
    ExportTypeFact,
    ImportDeclarationFact,
    ImportFieldFact,
    ImportGeneration,
    ImportParameterFact,
} from "./model.js";
export {
    parseOverride,
    parseOverrides,
    parseSelector,
    resolveOverride,
    resolveSelector,
    selectorToString,
} from "./selector.js";
export type {
    Override,
    Selector,
    SelectorKind,
    SelectorTarget,
    Step,
} from "./selector.js";
export {
    initialisms,
    isCanonicalWireName,
    isInitialism,
    isTypeScriptKeyword,
    isValidTypeScriptIdentifier,
    isValidWireName,
    longestInitialism,
    requireTypeScriptIdentifier,
    typeScriptToWire,
    wireToTypeScript,
} from "./name.js";
