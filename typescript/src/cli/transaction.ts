import { mkdir } from "node:fs/promises";

export async function validateBeforeOutput<T>(outDirectory: string, validate: () => T | Promise<T>, write: (value: T, outDirectory: string) => void | Promise<void>): Promise<void> {
    const value = await validate();
    await mkdir(outDirectory, { recursive: true });
    await write(value, outDirectory);
}
