export const CODEC_NODE_BUDGET = 1_048_576;

export { CodecResourceError } from "./codec-errors.js";
import { CodecResourceError } from "./codec-errors.js";

export class CodecBudget {
    private usedValue = 0;

    constructor(readonly limit = CODEC_NODE_BUDGET) {
        if (!Number.isSafeInteger(limit) || limit < 0) throw new RangeError("invalid codec node budget");
    }

    get used(): number {
        return this.usedValue;
    }

    get remaining(): number {
        return this.limit - this.usedValue;
    }

    charge(nodes = 1): void {
        if (!Number.isSafeInteger(nodes) || nodes < 0 || nodes > this.remaining) {
            throw new CodecResourceError("codec node budget exceeded");
        }
        this.usedValue += nodes;
    }
}
