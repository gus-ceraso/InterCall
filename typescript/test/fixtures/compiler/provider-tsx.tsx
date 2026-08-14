import type { HandlerContext, Int32 } from "./runtime.js";

/**
 * A TSX provider.
 * @intercall procedure render_value
 * @param value Rendered value.
 */
export function renderValue(
    context: HandlerContext,
    value: Int32,
): Promise<void> {
    return Promise.resolve();
}
