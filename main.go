// Copyright (c) 2026 IndyKite
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Command oasgen parses Go files carrying swag-style annotations and emits an
// OpenAPI 3.2 (or 3.1) document. It does not depend on swaggo/swag.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/indykite/openapi-parser/gen"
)

func main() {
	var (
		dirs    = flag.String("d", ".", "comma-separated directories to parse")
		out     = flag.String("o", "openapi.json", "output file")
		version = flag.String("oas", "3.2.0", "target OpenAPI version: 3.2.0 or 3.1.0")
	)
	flag.Parse()

	dirList := strings.Split(*dirs, ",")
	for i := range dirList {
		dirList[i] = strings.TrimSpace(dirList[i])
	}

	api, err := gen.Parse(dirList)
	if err != nil {
		fmt.Fprintln(os.Stderr, "oasgen: parse:", err)
		os.Exit(1)
	}

	data, err := api.EmitJSON(gen.EmitOptions{Version: *version})
	if err != nil {
		fmt.Fprintln(os.Stderr, "oasgen: emit:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, data, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "oasgen: write:", err)
		os.Exit(1)
	}
	_, _ = fmt.Fprintf(os.Stdout, "wrote %s (openapi %s, %d operations, %d schemas)\n",
		*out, *version, len(api.Operations), len(api.Schemas))
}
