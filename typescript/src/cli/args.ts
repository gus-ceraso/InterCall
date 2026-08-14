export type CliCommand =
    | { readonly kind: "help" }
    | { readonly kind: "import"; readonly out: string; readonly interfacePath: string; readonly tsNames: readonly string[] }
    | { readonly kind: "export"; readonly project: string; readonly out: string; readonly interfacePath: string; readonly sources: readonly string[]; readonly include: readonly string[]; readonly exclude: readonly string[] };

export const HELP = `Usage: intercall-ts <import|export> [options]

Commands:
  import   Generate a TypeScript import binding
  export   Generate a TypeScript export binding

Import options:
  --out DIR              Output directory
  --ts-name SELECTOR=NAME  Override a generated TypeScript name (repeatable)
  INTERFACE FILE         InterCall interface file

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
    const tsNames: string[] = [];
    const positional: string[] = [];
    for (let index = 1; index < argv.length; index += 1) {
        const argument = argv[index]!;
        if (!argument.startsWith("--")) { positional.push(argument); continue; }
        const name = argument.slice(2);
        if (name === "help") return { kind: "help" };
        if (name === "include" || name === "exclude" || name === "ts-name") {
            const value = argv[++index];
            if (value === undefined || value.startsWith("--")) throw new Error(`option --${name} requires a value`);
            if (name === "include") include.push(value);
            else if (name === "exclude") exclude.push(value);
            else tsNames.push(value);
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
        return { kind: "import", out, interfacePath: positional[0]!, tsNames };
    }
    if (tsNames.length > 0) throw new Error("--ts-name is valid only for import");
    const project = required(values, "project");
    const interfacePath = required(values, "interface");
    if (positional.length === 0) throw new Error("export requires at least one source operand");
    return { kind: "export", project, out, interfacePath, sources: positional, include, exclude };
}

function required(values: ReadonlyMap<string, string>, name: string): string {
    const value = values.get(name);
    if (value === undefined) throw new Error(`missing required option --${name}`);
    return value;
}
