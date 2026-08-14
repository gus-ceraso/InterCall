export type CliCommand =
    | { readonly kind: "help" }
    | { readonly kind: "import"; readonly out: string; readonly interfacePath: string }
    | { readonly kind: "export"; readonly project: string; readonly out: string; readonly interfacePath: string; readonly sources: readonly string[]; readonly include: readonly string[]; readonly exclude: readonly string[] };

export const HELP = `Usage: intercall-ts <import|export> [options]

Commands:
  import   Generate a TypeScript import binding
  export   Generate a TypeScript export binding

Import options:
  --out DIR              Output directory
  --interface FILE       InterCall interface file

Export options:
  --project FILE         TypeScript project file
  --out DIR              Output directory
  --interface FILE       InterCall interface file
  --include NAME         Include a repeatable selector
  --exclude NAME         Exclude a repeatable selector
`;

export function parseCliArguments(argv: readonly string[]): CliCommand {
    if (argv.length === 0 || argv[0] === "--help" || argv[0] === "-h") return { kind: "help" };
    const command = argv[0];
    if (command !== "import" && command !== "export") throw new Error(`unknown command ${JSON.stringify(command)}`);
    const values = new Map<string, string>();
    const include: string[] = [];
    const exclude: string[] = [];
    const positional: string[] = [];
    for (let index = 1; index < argv.length; index += 1) {
        const argument = argv[index]!;
        if (!argument.startsWith("--")) { positional.push(argument); continue; }
        const name = argument.slice(2);
        if (name === "help") return { kind: "help" };
        if (name === "include" || name === "exclude") {
            const value = argv[++index];
            if (value === undefined || value.startsWith("--")) throw new Error(`option --${name} requires a value`);
            (name === "include" ? include : exclude).push(value);
            continue;
        }
        if (name !== "out" && name !== "interface" && name !== "project") throw new Error(`unknown option --${name}`);
        const value = argv[++index];
        if (value === undefined || value.startsWith("--")) throw new Error(`option --${name} requires a value`);
        if (values.has(name)) throw new Error(`duplicate option --${name}`);
        values.set(name, value);
    }
    const out = required(values, "out");
    if (command === "import") {
        if (values.has("project") || include.length > 0 || exclude.length > 0 || positional.length !== 1 || values.has("interface")) throw new Error("invalid import arguments");
        return { kind: "import", out, interfacePath: positional[0]! };
    }
    return { kind: "export", project: required(values, "project"), out, interfacePath: required(values, "interface"), sources: positional, include, exclude };
}

function required(values: ReadonlyMap<string, string>, name: string): string {
    const value = values.get(name);
    if (value === undefined) throw new Error(`missing required option --${name}`);
    return value;
}
