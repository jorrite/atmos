# Example: Scaffold Computed `template:`

Derive a complex data structure once, from an external template rendered with explicit arguments,
instead of every file in a template re-implementing the same derivation logic.

Learn more in the [Scaffold Command Documentation](https://atmos.tools/cli/commands/scaffold/generate#computed-fields).

## What You'll See

- A `type: computed` field whose value comes from `template: {source, args}` instead of a `value:`
  expression: `lib/sizing.json.tmpl` is rendered with the selected `environments` and decoded into
  a real structured value at `.Config.sizing`
- `lib/sizing.json.tmpl` uses plain default Go-template delimiters even though `scaffold.yaml`
  declares a custom delimiter pair for every other file — a shared, reusable template file always
  renders with fixed default delimiters, never the calling template's own choice
- `lib/sizing.json.tmpl` — despite being declared in `spec.files[]` like any other file — never
  appearing in generated output, since it exists solely to be rendered as data
- The `.json.tmpl` naming convention: a trailing `.tmpl` is stripped before deciding how to decode
  the rendered output, so `sizing.json.tmpl` decodes as JSON the same way `sizing.json` would

## Try It

```shell
# List available scaffold templates
atmos scaffold list

# Generate with a few environments selected
atmos scaffold generate example ./my-project --set environments=dev,staging,prod
```

This generates `sizing.tf` (and `atmos.yaml`, this whole directory's own config) — but never
`lib/sizing.json.tmpl` itself. `sizing.tf` reads:

```hcl
locals {
  sizing = {"environments":{"dev":{"memory":"512Mi","replicas":1},"prod":{"memory":"2Gi","replicas":3},"staging":{"memory":"512Mi","replicas":1}}}
}
```

`prod` derives a larger replica count and memory allocation than `dev`/`staging` — computed once in
`lib/sizing.json.tmpl`, reusable from any file in the template via `.Config.sizing`, instead of
every file that needs a sizing plan re-implementing the same per-environment rule.

## Key Files

| File | Purpose |
|------|---------|
| `scaffold.yaml` | Template configuration: an `environments` multiselect field, a `sizing` computed field sourced from `template: {source, args}` |
| `lib/sizing.json.tmpl` | The external template deriving the sizing plan — never copied into generated output |
| `sizing.tf` | Discovered template file, reads `.Config.sizing` (already decoded, no `fromJson`/`toJson` needed to read it) |
| `atmos.yaml` | Registers this directory as the `example` template, and is copied verbatim into generated output for the same reason `README.md` is in `examples/scaffolding` |
| `README.md` | This file, also copied verbatim into generated output |

## Learn More

`template: {source, args}`'s `source` accepts the same forms [`!include`](https://atmos.tools/functions/yaml/include)
does — a local path, or a remote `git::`/`oci://`/`https://` reference — see the
[`atmos scaffold generate`](https://atmos.tools/cli/commands/scaffold/generate#computed-fields)
docs for the full reference, including how `args:` values are resolved (the same
`answers.<path>`-dot-path-or-expression dispatch `options:` already uses).
