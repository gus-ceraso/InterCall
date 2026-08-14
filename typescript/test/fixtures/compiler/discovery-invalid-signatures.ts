import type { HandlerContext } from "../../../src/index.js";

/** @intercall procedure */
export declare function ambient(_context: HandlerContext): Promise<void>;

/** @intercall procedure */
export function generic<T>(_context: HandlerContext): Promise<void> { return Promise.resolve(); }
