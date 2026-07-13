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

package gen

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// EmitYAML renders the API model to a block-style YAML document.
//
// The encoder is deliberately minimal so the module stays stdlib-only: map
// keys are sorted (matching encoding/json's map behavior), and any string
// that isn't a safe plain scalar is emitted double-quoted with JSON escaping
// — JSON string escapes are a subset of YAML double-quoted-scalar escapes,
// so the output is always valid YAML.
func (api *API) EmitYAML(opt EmitOptions) ([]byte, error) {
	var b strings.Builder
	writeYAMLMap(&b, api.Emit(opt), 0, false)
	return []byte(b.String()), nil
}

// writeYAMLValue writes v after a `key:` or `-` already on the current line:
// scalars and empty collections inline, non-empty collections on new lines.
func writeYAMLValue(b *strings.Builder, v any, indent int) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			b.WriteString(" {}\n")
			return
		}
		b.WriteByte('\n')
		writeYAMLMap(b, t, indent, false)
	case []any:
		if len(t) == 0 {
			b.WriteString(" []\n")
			return
		}
		b.WriteByte('\n')
		writeYAMLSeq(b, t, indent)
	default:
		b.WriteByte(' ')
		b.WriteString(yamlScalar(t))
		b.WriteByte('\n')
	}
}

// writeYAMLMap writes a map's entries at the given indent. When inline is
// true the first key continues the current line (after a sequence dash).
func writeYAMLMap(b *strings.Builder, m map[string]any, indent int, inline bool) {
	for i, k := range slices.Sorted(maps.Keys(m)) {
		if !inline || i > 0 {
			b.WriteString(strings.Repeat("  ", indent))
		}
		b.WriteString(yamlString(k))
		b.WriteByte(':')
		writeYAMLValue(b, m[k], indent+1)
	}
}

func writeYAMLSeq(b *strings.Builder, s []any, indent int) {
	for _, v := range s {
		b.WriteString(strings.Repeat("  ", indent))
		b.WriteByte('-')
		if m, ok := v.(map[string]any); ok && len(m) > 0 {
			// compact form: first key shares the dash's line
			b.WriteByte(' ')
			writeYAMLMap(b, m, indent+1, true)
			continue
		}
		writeYAMLValue(b, v, indent+1)
	}
}

func yamlScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return yamlString(t)
	default:
		// bools, numbers, and any leftover structured value: JSON is valid
		// YAML flow style, and encoding/json handles every scalar correctly.
		data, err := json.Marshal(t)
		if err != nil {
			return "null"
		}
		return string(data)
	}
}

// yamlPlain matches strings that are unambiguous unquoted: they can't be
// mistaken for numbers, and contain no YAML indicator characters.
var yamlPlain = regexp.MustCompile(`^[A-Za-z_/][A-Za-z0-9_./-]*$`)

// yamlReserved are plain words YAML 1.1/1.2 loaders may coerce to bool/null.
var yamlReserved = map[string]bool{
	"true": true, "false": true, "null": true, "yes": true, "no": true,
	"on": true, "off": true, "y": true, "n": true,
}

func yamlString(s string) string {
	if yamlPlain.MatchString(s) && !yamlReserved[strings.ToLower(s)] {
		return s
	}
	data, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(data)
}
