# oasgen

Parse Go files carrying **swag-style annotations** and emit **OpenAPI 3.2**
(or 3.1, for backward compatibility) — **without depending on swaggo/swag**.

It's a from-scratch replacement for swag's front end: existing annotations
keep working, but the parser, schema resolver, and emitter are all here.
Because we own the parser, 3.2 constructs are parsed
natively (`@Router /x [query]`, `@tag.parent`, …) instead of being smuggled
through `x-` hacks in a post-processor.

> **Naming:** the repository and Go module are `openapi-parser`
> (`github.com/indykite/openapi-parser`); `oasgen` is the name of the command
> it builds (**OAS** generator).

## How it works

```text
annotated .go files
        │  gen.Parse(dirs)
        ▼
   *gen.API          version-agnostic model
        │  api.Emit(EmitOptions{Version})
        ▼
   OpenAPI 3.2.0 (or 3.1.0)
```

The pipeline is one file per stage: `extract.go` (go/ast → comment groups +
struct defs), `lexer.go` (lines → directives), `parse.go` (directives → model),
`schema.go` (Go types → schemas), `emit.go` (model → OpenAPI). The model in the
middle is the key design choice: annotation syntax and output spec version
evolve independently, so targeting 3.3 later means touching only the emitter.

## Usage

```sh
go build -o oasgen .

# Emit 3.2 from the current directory:
./oasgen -d . -o openapi.json

# Multiple dirs, 3.1 output:
./oasgen -d ./cmd/api,./internal/handlers -o openapi.json -oas 3.1.0
```

Library use:

```go
import "github.com/indykite/openapi-parser/gen"

api, err := gen.Parse([]string{"./handlers"})
data, err := api.EmitJSON(gen.EmitOptions{Version: "3.2.0"})
```
