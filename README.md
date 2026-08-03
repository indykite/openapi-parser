# oasgen

Parse Go files carrying **swag-style annotations** and emit **OpenAPI 3.2**
(or 3.1, for backward compatibility) **without depending on swaggo/swag**.

**You write** (unchanged swag-style annotations):

```go
package accounts

// @title   Accounts API
// @version 1.0.0

type Account struct {
    ID    int64   `json:"id"`
    Email string  `json:"email" example:"ada@corp.io"`
    Plan  string  `json:"plan" binding:"oneof=free pro"`
    Trial *string `json:"trial_ends"`
}

// GetAccount godoc
// @Summary  Fetch one account
// @Produce  json
// @Param    id   path      int  true  "account id"  minimum(1)
// @Success  200  {object}  Account
// @Router   /accounts/{id} [get]
func GetAccount(w http.ResponseWriter, r *http.Request) {}
```

**You get** (OpenAPI 3.2):

```yaml
openapi: "3.2.0"
info:
  title: "Accounts API"
  version: "1.0.0"
paths:
  "/accounts/{id}":
    get:
      operationId: GetAccount
      summary: "Fetch one account"
      parameters:
        - name: id
          in: path
          required: true
          description: "account id"
          schema:
            type: integer
            format: int32
            minimum: 1
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                "$ref": "#/components/schemas/accounts.Account"
components:
  schemas:
    accounts.Account:
      type: object
      properties:
        id: {type: integer, format: int64}
        email:
          type: string
          examples: ["ada@corp.io"]
        plan:
          type: string
          enum: [free, pro]
        trial_ends:
          type: [string, "null"]
```

## Why

Tired of swaggo/swag forcing you into Swagger 2.0? Generate OpenAPI 3.2
documents directly from your existing annotations - zero intermediate
conversion steps.

`oasgen` is a from-scratch replacement for swag's front end: existing
annotations keep working, but the parser, schema resolver, and emitter are
all here. Because we own the parser, 3.2 constructs are parsed natively
(`@Router /x [query]`, `@tag.parent`, webhooks, streaming `itemSchema`, …)
instead of being smuggled through `x-` hacks in a post-processor. And there
is no conversion pipeline to babysit: annotations go in, a valid 3.2 (or
3.1) document comes out.

## Quick start

```sh
go install github.com/indykite/openapi-parser/cmd/oasgen@latest

oasgen -d . -o openapi.yaml
```

`-d` selects the directories to scan for annotations (recursively), `-o` the
output file - format inferred from the extension. Add `-oas 3.1.0` if you
need 3.1 output.

> **Naming:** the repository and Go module are `openapi-parser`
> (`github.com/indykite/openapi-parser`); `oasgen` is the name of the command
> it builds (**OAS** generator), living at `cmd/oasgen` so
> `go install github.com/indykite/openapi-parser/cmd/oasgen@latest` installs
> a binary with the right name.

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
go build -o oasgen ./cmd/oasgen
# or: go install github.com/indykite/openapi-parser/cmd/oasgen@latest

# Emit 3.2 from the current directory (and everything under it):
./oasgen -d . -o openapi.json

# YAML output (inferred from the extension, or force with -format yaml):
./oasgen -d ./internal/rest -o openapi.yaml

# Multiple dirs, 3.1 output:
./oasgen -d ./cmd/api,./internal/handlers -o openapi.json -oas 3.1.0
```

How `-d` is scoped:

- **Annotations** are harvested recursively from each `-d` dir (like swag).
  Generate one document per service: if several annotated services live under
  one `-d` root, they merge into a single document and their general-info
  blocks overwrite each other.
- **Types** resolve against the whole Go module enclosing the first `-d` dir,
  so cross-package references (`model.Account`) work without extra flags: no
  `--parseDependency`/`--parseInternal` equivalents needed. Outside a module,
  only the given dirs are indexed. Types from other modules aren't resolved
  and degrade to a permissive `object` schema.

Library use:

```go
import "github.com/indykite/openapi-parser/gen"

api, err := gen.Parse([]string{"./handlers"})
data, err := api.EmitJSON(gen.EmitOptions{Version: "3.2.0"}) // or api.EmitYAML(...)
```

## Type resolution

Beyond swag's annotation set, the resolver understands:

- **Generics**: swaggo bracket syntax `listResponse[AccountResponse]` in
  annotations and Go generic types in struct fields; instantiations are
  registered as `Base-Arg` components.
- **Embedded structs**: anonymous fields (including pointer embeds) promote
  their properties and required lists into the parent schema, like Go does.
- **Named non-struct types**: `type Labels []*Account` resolves inline to its
  underlying type.
- **Cross-package references**: the whole module is indexed, so
  `model.Account` resolves through the referencing file's imports.
- **Multi-word security scheme names**: `@securityDefinitions.apikey Bearer
  Token` and `@Security Bearer Token` keep the full name.
- **`@schemes`**: expands host-derived servers into one entry per scheme.
- **Constraints**: `@Param` attributes (`minimum(1) maximum(100) default(20)
  pattern(...)`) and struct tags (`minimum`, `maximum`, `minLength`,
  `maxLength`, `multipleOf`, `default`) emit real JSON Schema keywords, and
  `binding:`/`validate:` rules map too: `oneof` → `enum`, `gte`/`lte`/`min`/
  `max`/`len` → `minimum`/`maxLength`/`minItems`/… by type, `gt`/`lt` →
  `exclusiveMinimum`/`exclusiveMaximum`.
- **Typed scalars**: examples, defaults, and enums are coerced to the field's
  type (`example:"5"` on an int emits `5`, not `"5"`).
- **Nullable pointers**: `*T` adds `"null"` to the type array; pointer struct
  refs wrap as `anyOf: [$ref, null]` ($ref allows no sibling keywords).
- **Request body content type**: derived from `@Accept` (one entry per MIME
  type) instead of hardcoding `application/json`.
- **Response descriptions**: default to the HTTP status text when the
  annotation carries none (matches swag).
- **Schema `x-` extensions**: swag's `extensions:"x-nullable,x-unit=kg,!x-hidden"`
  struct tag (bare = true, `!` = false, `x-` prefix added if missing).

## Beyond swag's directive set

- **Info & license**: `@summary` (info summary, 3.1+), `@license.identifier`
  (SPDX, 3.1+).
- **Document security**: `@security <scheme>` in the general block emits
  top-level `security` (operation `@Security` still overrides).
- **More security schemes**: `@securityDefinitions.bearerauth` (+
  `@bearerFormat`), `@securityDefinitions.openidconnect` (+
  `@openIdConnectUrl`), `@securityDefinitions.mutualtls` (3.1+).
- **Servers**: `@server.url`, `@server.name` (3.2), `@server.description`, and
  `@server.variable name default "desc" enums(a,b)` for URL templates.
- **Operation docs**: `@externalDocs.url` / `@externalDocs.description` inside
  an operation block.
- **Parameter attributes**: `deprecated(true)`, `style(form)`,
  `explode(false)`, `allowReserved(true)` alongside the schema constraints.
- **Response summary** (3.2): `@ResponseSummary <code> <text>` after the
  matching `@Success`/`@Failure` line.
- **operationId by default**: when no `@ID` is given, the documented handler
  function's name becomes the `operationId` (`@ID` always wins).
- **Scheme descriptions**: a `@description` following a
  `@securityDefinitions.*` block attaches to that scheme, not the info block
  (swag's positional rule).

## Native 3.2 constructs

- **Streaming**: a `@Produce` of a sequential media type (`event-stream`/
  `sse`, `jsonl`, `ndjson`, `json-seq`) emits the response schema as 3.2
  `itemSchema` (each event's shape); 3.1 output falls back to `schema`.
- **Webhooks**: `@Webhook account.created [post]` in place of `@Router`
  documents an outgoing event under the top-level `webhooks` map (3.1+);
  the method defaults to `post`.
- **`$self`**: `@self https://.../openapi.json` sets the document's own URI
  (3.2 only).
- **OAuth2 device flow**: `@securityDefinitions.oauth2.device` with
  `@deviceAuthorizationUrl`/`@tokenUrl`; plus `@refreshUrl`,
  `@oauth2MetadataUrl`, and `@deprecated` on any scheme. 3.2-only pieces are
  dropped cleanly from 3.1 output.
- Plus the earlier ones: `@Router /x [query]` first-class, non-standard verbs
  via `additionalOperations`, hierarchical tags (`@tag.parent`, `@tag.kind`).

## Parity gate (`cmd/oasparity`)

`oasparity` is a spec-parity regression gate: it parses each service's
annotations with this module and compares the operation and schema inventories
against the checked-in baseline (`docs/swagger.yaml`), without needing swag
or a YAML dependency. "Service" is just the word for a check unit so any
directory with annotated Go code and its own baseline document counts as one,
whatever it is in your repo: a monorepo has many, a single-binary repo has
exactly one (its root). The baseline may be swag's Swagger 2.0 output (the
migration case) or a previously generated OpenAPI 3.x document (the regression
case). Missing/extra operations or missing schemas fail with exit 1; extra
oasgen components are allowed (unreferenced components are valid so swag
doesn't register embedded base types, we do).

Against a 2.0 baseline, constructs swag cannot express (webhooks, the `query`
method, `additionalOperations` verbs) are excluded from the comparison; against
a 3.x baseline they are compared too. 3.x baselines should be 3.2 output:
3.1 downgrades those methods to `post`, which would diff against the
annotations.

```sh
# Services are auto-discovered by the presence of docs/swagger.yaml:
make parity REPO=../my-platform

# Or directly, with an explicit service list and verbose extras:
go run ./cmd/oasparity -repo ../my-platform -services services/a,services/b -v

# A different swag output location:
go run ./cmd/oasparity -repo ../my-platform -docs api/swagger.yaml

# A single-binary repo: the repo root is the one "service":
go run ./cmd/oasparity -repo ../my-api
```

Run it against any repo you're migrating before releasing a parser change;
when it's green, that repo's `swag init` invocations can be pointed at
`oasgen` instead.
