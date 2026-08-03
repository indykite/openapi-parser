# Responsible Disclosure

Thank you for taking the time to help keep this project — and the toolchains that consume its output — safe.

## Why a parser needs a disclosure process

`oasgen` parses untrusted input: Go source files and their comment annotations. Its output (OpenAPI documents) is consumed by code generators, gateways, and validation middleware.
A parsing bug can therefore be more than a crash: a crafted annotation or source file could produce a document that misleads downstream security tooling
(e.g. dropping a declared security scheme, mangling a constraint) or hang/exhaust the parser in CI.

## In scope

- Crafted Go source or annotations that crash the parser, cause unbounded memory/CPU use, or hang it.
- Inputs that make the emitter silently drop or alter security-relevant output (security schemes, `security` requirements, constraints, enum restrictions).
- Path handling issues: `-d`/`-o` flags reading or writing files outside the intended directories.
- Bugs in the internal YAML encoder (`gen/yaml.go`) that produce output parsed differently by downstream YAML consumers than intended.

## Out of scope

- Vulnerabilities in Go itself, `golangci-lint`, or other development tooling: report those upstream.
- Issues that require an already-compromised machine or a malicious operator.
- Theoretical concerns without a demonstrable trigger.
- Incorrect-but-honest output for exotic annotations (that's a regular bug so open an issue).

## How to report

**Contact:** <responsible-disclosure@indykite.com>

When reporting, include:

1. The affected component (file or package) and commit hash.
2. A minimal reproduction: ideally a small annotated Go file plus the `oasgen` invocation.
3. The impact: what an attacker could achieve, and under what assumptions.
4. Any suggested mitigation, if you have one.

Please **do not** open a public GitHub issue for security-sensitive reports.

## What to expect

- **Acknowledgement** within 5 business days.
- **Triage and severity assessment** within 10 business days.
- **Fix or mitigation** on a timeline proportional to severity.
- **Credit** in the changelog or release notes if you would like it. Let us know your preferred name/handle.

## Safe harbor

We will not pursue legal action against researchers who:

- Make a good-faith effort to avoid privacy violations, data destruction, and service disruption while testing.
- Report findings privately and give a reasonable window for remediation before public disclosure.
- Do not exploit a finding beyond what is necessary to demonstrate it.

If in doubt about whether your testing falls within these guidelines, ask first.
