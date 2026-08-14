import type { HandlerContext } from "./runtime.js";

/** @intercall procedure */
export function badContext(context: string): number { return context.length; }

/** @intercall procedure */
export function badResult(_context: HandlerContext): number { return 1; }
