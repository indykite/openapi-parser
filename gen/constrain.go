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

// Schema constraint helpers: typed scalar coercion and the mapping from
// go-playground/validator rules (`binding:"..."` / `validate:"..."` tags)
// to JSON Schema keywords.

import (
	"strconv"
	"strings"
)

// coerceScalar converts a raw annotation/tag string to the schema's scalar
// type, so `example:"5"` on an int field emits 5, not "5". On array schemas
// the value is comma-separated per swag semantics (`example:"a,b"` on a
// []string field emits ["a", "b"]), each item coerced to the item type.
// Unparseable values stay strings.
func coerceScalar(v string, s *Schema) any {
	switch primaryType(s) {
	case "integer":
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	case "number":
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	case "boolean":
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	case "array":
		item := s.Items
		if item == nil {
			item = &Schema{}
		}
		parts := strings.Split(v, ",")
		arr := make([]any, 0, len(parts))
		for _, p := range parts {
			arr = append(arr, coerceScalar(strings.TrimSpace(p), item))
		}
		return arr
	}
	return v
}

// primaryType is the schema's first non-null type, or "".
func primaryType(s *Schema) string {
	if s == nil {
		return ""
	}
	for _, t := range s.Type {
		if t != "null" {
			return t
		}
	}
	return ""
}

// enumTarget picks where enum values belong: the items schema for arrays,
// the schema itself otherwise.
func enumTarget(s *Schema) *Schema {
	if primaryType(s) == "array" && s.Items != nil {
		return s.Items
	}
	return s
}

// isRequiredRule reports whether a validator rule list makes the field itself
// mandatory. Only rules before the first `dive` apply to the field: everything
// after it constrains the elements (or, between `keys` and `endkeys`, the map
// keys), so `omitempty,dive,keys,required,endkeys,required` on a map means
// "if present, every key and value must be non-empty", not "field required".
func isRequiredRule(rules string) bool {
	for rule := range strings.SplitSeq(rules, ",") {
		switch strings.TrimSpace(rule) {
		case "dive":
			return false
		case "required":
			return true
		}
	}
	return false
}

// applyValidationRules maps the widely-used go-playground/validator rules to
// schema constraints. `required` is handled by the caller (it belongs to the
// parent object); unknown/custom validators are ignored. `dive` redirects the
// remaining rules to the element schema, matching the validator's semantics
// (`min=1,dive,min=8` = at least one element, each at least 8 long); on a map
// the elements are the values (additionalProperties). Rules between `keys`
// and `endkeys` constrain map keys and are skipped.
func applyValidationRules(s *Schema, rules string) {
	if s == nil || rules == "" {
		return
	}
	inKeys := false
	for rule := range strings.SplitSeq(rules, ",") {
		name, val, _ := strings.Cut(strings.TrimSpace(rule), "=")
		switch name {
		case "dive":
			if s = elementSchema(s); s == nil {
				return
			}
			continue
		case "keys":
			inKeys = true
			continue
		case "endkeys":
			inKeys = false
			continue
		}
		if inKeys {
			continue
		}
		switch name {
		case "oneof":
			target := enumTarget(s)
			target.Enum = target.Enum[:0]
			for _, v := range splitOneOf(val) {
				target.Enum = append(target.Enum, coerceScalar(v, target))
			}
		case "gte":
			applySizeRule(s, val, sizeMin)
		case "lte":
			applySizeRule(s, val, sizeMax)
		case "min":
			applySizeRule(s, val, sizeMin)
		case "max":
			applySizeRule(s, val, sizeMax)
		case "len":
			applySizeRule(s, val, sizeMin)
			applySizeRule(s, val, sizeMax)
		case "gt":
			if primaryType(s) == "integer" || primaryType(s) == "number" {
				s.ExclusiveMinimum = parseFloatPtr(val)
			}
		case "lt":
			if primaryType(s) == "integer" || primaryType(s) == "number" {
				s.ExclusiveMaximum = parseFloatPtr(val)
			}
		}
	}
}

// elementSchema is what `dive` descends into: array items, or map values.
func elementSchema(s *Schema) *Schema {
	if s.Items != nil {
		return s.Items
	}
	return s.AdditionalProperties
}

type sizeBound bool

const (
	sizeMin sizeBound = false
	sizeMax sizeBound = true
)

// applySizeRule maps a numeric bound onto the keyword matching the schema's
// type: minimum/maximum for numbers, minLength/maxLength for strings,
// minItems/maxItems for arrays (validator's min/max semantics).
func applySizeRule(s *Schema, val string, bound sizeBound) {
	switch primaryType(s) {
	case "integer", "number":
		if bound == sizeMax {
			s.Maximum = parseFloatPtr(val)
		} else {
			s.Minimum = parseFloatPtr(val)
		}
	case "string":
		if bound == sizeMax {
			s.MaxLength = parseIntPtr(val)
		} else {
			s.MinLength = parseIntPtr(val)
		}
	case "array":
		if bound == sizeMax {
			s.MaxItems = parseIntPtr(val)
		} else {
			s.MinItems = parseIntPtr(val)
		}
	}
}

// splitOneOf splits a oneof value list on spaces, honoring validator's
// single-quoted values ('New York' Boston).
func splitOneOf(v string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range v {
		switch {
		case r == '\'':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func parseFloatPtr(v string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return nil
	}
	return &f
}

func parseIntPtr(v string) *int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return nil
	}
	return &n
}
