module github.com/indykite/openapi-parser

go 1.26.4

// Stdlib only by design. Parsing uses go/parser + go/ast; emission uses
// encoding/json. No third-party dependency, no swag dependency. (YAML output
// would need a dep — JSON is emitted natively; convert with any tool if needed.)
