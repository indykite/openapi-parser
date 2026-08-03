---
name: Bug report
about: A parsing or emission bug — wrong, missing, or invalid OpenAPI output
title: 'bug: '
labels: bug
---

<!--
Security-sensitive reports (crashes on crafted input, dropped security schemes,
path traversal) go to the private channel in responsible_disclosure.md —
please do NOT open a public issue for those.
-->

## Minimal reproduction

<!-- The smallest annotated Go snippet that reproduces the issue.
     A single handler with its comment block is usually enough. -->

```go
// ShowAccount godoc
//
// @Summary  ...
// @Router   /accounts/{id} [get]
func ShowAccount(w http.ResponseWriter, r *http.Request) {}
```

## Command

<!-- The exact oasgen invocation, including flags and target OpenAPI version. -->

```sh
oasgen -d . -o openapi.json -oas 3.2.0
```

## Actual output

<!-- The relevant fragment of the emitted document (or the error/panic). -->

```json
```

## Expected output

<!-- What you expected instead, ideally with a reference: a section of the
     OpenAPI specification (https://spec.openapis.org/) or swag's behavior
     for the same annotations. -->

```json
```

## Environment

- `openapi-parser` version / commit:
- Go version (`go version`):
- OS:
