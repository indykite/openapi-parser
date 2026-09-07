# Contributing

Thanks for your interest in improving `openapi-parser`. When contributing to this repository, please first discuss any non-trivial change via an issue before opening a pull request — a 30-second sanity check from a maintainer can save an afternoon of work.

## Before you start

- Read the [README](./README.md) to understand the pipeline (`extract.go` → `lexer.go` → `parse.go` → `schema.go` → `emit.go`) and the version-agnostic model in the middle.
- Check open issues and PRs to make sure the change isn't already in flight.
- Read [`responsible_disclosure.md`](./responsible_disclosure.md) if your contribution touches anything security-sensitive. Do **not** open a public issue for security reports.

## Ground rules

- **Stdlib only, by design.** The module has no third-party dependencies and no swag dependency: parsing uses `go/parser` + `go/ast`, emission uses `encoding/json` plus the minimal internal
  YAML encoder (`gen/yaml.go`). PRs that add a dependency to `go.mod` will be rejected unless discussed and agreed in an issue first.
- **swag compatibility matters.** Existing swag-style annotations must keep working. If a change alters how an annotation is interpreted, call that out explicitly in the PR.
- **Spec-version separation.** Annotation syntax and output spec version evolve independently. Parser/model changes belong in `parse.go`/`schema.go`; version-specific output belongs in the emitter.

## Development setup

You need Go 1.27 or later.

```sh
git clone https://github.com/indykite/openapi-parser
cd openapi-parser

make install-tools   # gci + golangci-lint
go build ./...
make test
```

Optionally install the pre-commit hooks (`pre-commit install`) so formatting and lint run automatically.

## Making changes

1. Fork the repository and branch off `master`.
2. Make your change, keeping it focused: one fix or feature per PR.
3. Add or update tests. Parser and emitter behavior is covered by tests under `gen/` with fixtures in `testdata/`; new directives or type-resolution rules need both a positive case and an edge case.
4. Run the checks locally before pushing:

   ```sh
   make fmt      # gofmt
   make gci      # import ordering
   make lint     # golangci-lint
   make test     # go test ./...
   ```

5. If your change affects generated output, run the parity gate against a repo you have access to and note the result in the PR:

   ```sh
   make parity REPO=path/to/annotated-repo
   ```

   Any repo with swag-style annotations and a checked-in baseline works: the tool speaks of "services", but that just means any directory with its own
   `docs/swagger.yaml`; a single-binary repo counts as one service at its root (see [README § Parity gate](./README.md#parity-gate-cmdoasparity)).

## Pull requests

- Keep one fix or feature per PR; reviewers should not have to evaluate unrelated changes together.
- Use a descriptive title following the existing convention, e.g. `feat: add @tag.parent support` or `fix: nullable pointer emission for 3.1`.
- The body should answer: what does this change do, why is it needed, and how was it tested?
- Breaking changes to annotation interpretation or emitted output must be called out explicitly in the PR title and body.
- By submitting a PR you confirm you have the right to contribute the content under this repo's [Apache-2.0 LICENSE](./LICENSE).

## Reporting bugs

A good parser bug report contains:

1. A minimal annotated Go snippet that reproduces the issue.
2. The `oasgen` invocation used (flags, target OpenAPI version).
3. The output you got and the output you expected (ideally referencing the [OpenAPI specification](https://spec.openapis.org/) or swag's behavior).

## Code of conduct

Be kind. We follow the spirit of the [Contributor Covenant](https://www.contributor-covenant.org/) v2.1:

- Assume good faith; critique the work, not the person.
- Be specific and actionable in reviews: vague negativity wastes everyone's time.
- Welcome newcomers; point them at this guide rather than dismissing the PR.
- Harassment, discriminatory language, doxxing, or sustained bad-faith argument are not tolerated and are grounds for a ban.

To report a conduct issue privately, use the contact address in [`responsible_disclosure.md`](./responsible_disclosure.md); reports are kept confidential among the maintainers handling the case, and good-faith reports will never be held against you.
