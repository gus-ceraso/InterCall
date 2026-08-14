import { PayloadException } from "./runtime.js";

/** @intercall procedure add */
export function add(_context: unknown, value: number): Promise<number> { return Promise.resolve(value); }

/** @intercall exception denied */
export const Denied = new Error("denied");

/** @intercall exception failed */
export class Failed extends PayloadException<{ readonly code: number }> {}

/** @intercall type point */
export interface Point { readonly x: number; }
