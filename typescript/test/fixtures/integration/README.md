# Interoperability fixtures

`backend.intercall` models the Go-exported backend imported by TypeScript;
`browser.intercall` models the browser-exported interface imported by Go.

Regenerate TypeScript artifacts from the package root with:

```sh
npm run build:cli
node dist/cli/main.js import --out /tmp/intercall-backend backend.intercall
node dist/cli/main.js export --project test/fixtures/compiler/tsconfig-discovery.json \
  --out /tmp/intercall-browser --interface /tmp/browser.intercall \
  test/fixtures/compiler/discovery.ts
```

Ordinary tests compare checked-in fixture inputs and never rewrite them.
