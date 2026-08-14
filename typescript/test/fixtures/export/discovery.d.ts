export function add(context: unknown, value: number): Promise<number>;
export const Denied: Error;
export class Failed extends Error { readonly payload: { readonly code: number }; }
