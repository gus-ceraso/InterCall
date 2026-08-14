import { PayloadException, type HandlerContext } from "./runtime.js";

/** @intercall procedure */
export function add(_context: HandlerContext, value: number): Promise<number> { return Promise.resolve(value); }

/** @intercall exception denied */
export const Denied = new Error("denied");

/** @intercall exception failed */
export class Failed extends PayloadException<{ readonly code: number }> {}

/** @intercall type point */
/** @intercall type alias */
export type Alias = Point;

/** @intercall type */
export interface Point { readonly x: number; }
export interface Recursive { readonly next: Recursive; }
