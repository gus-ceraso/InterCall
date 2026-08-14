import { PayloadException, type HandlerContext, type Int32 } from "./runtime.js";

/** @intercall procedure */
export function add(_context: HandlerContext, value: Int32): Promise<Int32> { return Promise.resolve(value); }

/** @intercall exception denied */
export const Denied = new Error("denied");

/** @intercall exception failed */
export class Failed extends PayloadException<{ readonly code: Int32 }> {}

/** @intercall type alias */
export type Alias = Point;

/** @intercall type */
export interface Point { readonly x: Int32; }
export interface Recursive { readonly next: Recursive; }
