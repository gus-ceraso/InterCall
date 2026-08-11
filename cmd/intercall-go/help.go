package main

// usageText is the command help. It documents the exact import and
// export grammar of SPEC.md "Commands", the repeatable flags, the
// package default, the one-file ownership rule, and the exit status.
const usageText = `intercall-go generates InterCall Go bindings.

Usage:

    intercall-go export --out DIR --interface FILE [--package NAME]
        [--include full/import/path.Symbol]...
        [--exclude full/import/path.Symbol]...
        PACKAGE_PATTERN...

    intercall-go import --out DIR [--package NAME]
        [--go-name SELECTOR=GoIdentifier]...
        INTERFACE_FILE

Commands:

    export    Generate an export binding and its owned interface from
              the tagged Go procedures of the package patterns, in the
              active module or workspace. Requires at least one package
              pattern and distinct --out and --interface targets.
    import    Generate an import binding from one exact interface file.
              Requires exactly one file operand; stdin is not
              supported.

Options:

    --out DIR          Generated artifact directory; created if needed.
    --interface FILE   Owned interface target of an export binding
                       (export only).
    --package NAME     Generated package name, matching
                       [A-Za-z_][A-Za-z0-9_]* and not "_", "main", or a
                       Go keyword; never sanitized. Defaults to an
                       existing owned binding's package clause, or to
                       the output directory's base name for a new
                       output. An explicit name must equal an existing
                       owned binding's package name.
    --include PATH.SYM Restrict export selection to the named eligible
                       function; repeatable.
    --exclude PATH.SYM Remove the named eligible function from the
                       selection; repeatable; wins over --include.
    --go-name SEL=ID   Override one generated Go identifier (import
                       only); repeatable.
    --help, -h         Show this help.

Each binding is one file named binding_gen.go in DIR, stamped with the
SHA-256 digest of the canonical interface body; an export binding also
owns the interface file. Owned targets are replaced safely by rename;
an unowned collision is an error, nothing is created or modified, and
no path is ever deleted. Diagnostics use path:line:column: message.
Exit status is 0 on success and 1 on any error.
`
