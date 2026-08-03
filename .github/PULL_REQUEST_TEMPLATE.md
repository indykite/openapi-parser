# Pull request

<!--
Thanks for opening a PR. Please fill in the sections below — reviewers use them
to triage and to write the eventual changelog entry. Empty PRs without context
get bounced.

If anything here doesn't apply (e.g. doc-only change), say so explicitly rather
than deleting the section.
-->

## What this PR does

<!-- One sentence. Lead with the verb: "Add ...", "Fix ...", "Tighten ...".
     Name the affected stage where relevant (lexer, parser, schema resolver, emitter). -->

## Effect on annotations and output

<!-- Required for any change to parsing or emission. Which annotations or Go
     constructs are affected, and how does the emitted OpenAPI document change?
     Call out explicitly:
     - any change to how an existing swag annotation is interpreted
     - any change to emitted output for existing inputs (breaking vs additive)
     - which OpenAPI versions are affected (3.1, 3.2, or both) -->

## How it was tested

<!-- Be specific. List the tests added or updated, and paste relevant output.
     If the change affects generated output, run the parity gate against an
     annotated repo and note the result: `make parity REPO=path/to/repo`. -->

- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] Tests added/updated for the new behavior (positive case + edge case)
- [ ] `make parity` run (if output-affecting) — repo and result:

## Known gaps or follow-ups

<!-- Anything intentionally out of scope: missing edge cases, unsupported
     annotation forms, features deferred to a follow-up PR.
     Better to surface than to surprise. -->

## Checklist

- [ ] One fix or feature per PR — no unrelated changes bundled in.
- [ ] PR title follows the existing convention (e.g. `feat: add @tag.parent support`, `fix: nullable pointer emission for 3.1`).
- [ ] No new entries in `go.mod` — the module is stdlib-only by design (see [`CONTRIBUTING.md`](../CONTRIBUTING.md#ground-rules)); if a dependency is unavoidable, it was agreed in an issue first.
- [ ] Breaking changes to annotation interpretation or emitted output are called out in the title and body.
- [ ] No secrets, internal URLs, or personal data committed.
- [ ] I have the right to contribute this content under the repo's [LICENSE](../LICENSE).
