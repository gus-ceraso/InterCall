import { type HandlerContext, type Int32 } from "../../../src/index.js";

type Bag = { readonly x: Int32 };
type Bags = readonly Bag[];

/** @intercall procedure use */
export function use(_context: HandlerContext, bag: Bag, bags: Bags): Promise<void> { return Promise.resolve(); }
