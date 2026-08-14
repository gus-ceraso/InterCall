# Compiler experiments

These fixtures are run with the pinned TypeScript compiler version `5.9.3`:

```sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
npm install --prefix "$tmp" --no-save --no-package-lock --ignore-scripts \
    typescript@5.9.3
TS_MODULE="$tmp/node_modules/typescript" node run.mjs
```

The experiment checks marker-symbol resolution through aliases, JSDoc tag
positions, `.js` and `.jsx` module resolution for `.ts` and `.tsx` providers
under transformed and preserved JSX configurations, `EmptyRecord`,
`PayloadException<T>`, and a 4,096-edge alias chain.
