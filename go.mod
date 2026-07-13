module github.com/indykite/openapi-parser

go 1.26.4

// Stdlib only by design. Parsing uses go/parser + go/ast; emission uses
// encoding/json plus a minimal internal YAML encoder (gen/yaml.go). No
// third-party dependency, no swag dependency.
