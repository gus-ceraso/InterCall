import type { HandlerContext } from "../../../src/index.js";

/** @intercall procedure */
export function badContext(context: string): number { return context.length; }

/** @intercall procedure */
export function badResult(_context: HandlerContext): number { return 1; }

/** @intercall exception bad_sentinel */
export const badSentinel = "not an Error";
