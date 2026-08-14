export function formatGeneratedSource(source: string): string {
    return source
        .replace(/\r\n?/gu, "\n")
        .split("\n")
        .map((line) => line.replace(/[ \t]+$/u, ""))
        .join("\n")
        .replace(/\n*$/u, "\n");
}
