export {
    interfaceID,
    interfaceIDHex,
    sha256,
} from "./interface-id.js";
export type { InterfaceID } from "./interface-id.js";
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
    selectorToString,
} from "./selector.js";
export type {
    Override,
    Selector,
    SelectorKind,
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
