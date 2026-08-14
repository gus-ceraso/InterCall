import { PayloadException } from "../../../src/index.js";
import type {
    EmptyRecord,
    HandlerContext,
    Int32,
} from "../../../src/index.js";

type OneAlias = Int32;
type TwoAliases = OneAlias;
type EmptyAlias = EmptyRecord;

export interface Point {
    readonly x: Int32;
    readonly y: Int32;
}

export class Failed extends PayloadException<Point> {}
export class Blank extends PayloadException<EmptyAlias> {}

/**
 * Adds one.
 * @intercall procedure add_one
 * @param value Input value.
 */
export function addOne(
    context: HandlerContext,
    value: TwoAliases,
): Promise<TwoAliases> {
    return Promise.resolve(value + 1);
}

/**
 * A documented provider.
 * @intercall procedure report
 */
export function report(
    context: HandlerContext,
): Promise<void> {
    return Promise.resolve();
}
