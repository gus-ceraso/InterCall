import { type HandlerContext, type Int32 } from "../../../src/index.js";

/** @intercall type one */
export interface Payload { readonly left: Int32; }

/** @intercall procedure first */
export function first(_context: HandlerContext, value: Payload): Promise<void> { return Promise.resolve(); }

/** @intercall type _private */
export interface PrivatePayload { readonly value: Int32; }

/** @intercall procedure MixedCase */
export function mixed(_context: HandlerContext, value: PrivatePayload): Promise<void> { return Promise.resolve(); }
