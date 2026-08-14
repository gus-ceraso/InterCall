import { type HandlerContext, type Int32 } from "../../../src/index.js";

/** @intercall type two */
export interface Payload { readonly right: Int32; }

/** @intercall procedure second */
export function second(_context: HandlerContext, value: Payload): Promise<void> { return Promise.resolve(); }
