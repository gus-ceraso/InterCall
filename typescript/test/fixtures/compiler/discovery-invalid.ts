/** @intercall procedure */
export function badContext(context: string): number { return context.length; }

/** @intercall procedure */
export function badResult(_context: import("./runtime.js").HandlerContext): number { return 1; }
