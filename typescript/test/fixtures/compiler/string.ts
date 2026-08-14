import type { HandlerContext } from "../../../src/index.js";

/** @intercall procedure echo */
export function echo(_context: HandlerContext, value: string): Promise<string> {
    return Promise.resolve(value);
}
