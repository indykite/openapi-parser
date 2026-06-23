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

import "strings"

// directive is one parsed annotation line: the @name and its argument string.
type directive struct {
	name string // lowercased, without the leading '@'
	args string // everything after the name, trimmed
	raw  string // original line for diagnostics
}

// parseCommentGroup turns a block of comment text (already stripped of // or
// /* */ markers by go/ast) into directives. Non-@ lines are treated as
// continuation of the previous directive's args when that directive supports
// multi-line text (description), else ignored.
func parseCommentGroup(text string) []directive {
	var out []directive
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "@") {
			continue
		}
		line = strings.TrimPrefix(line, "@")
		name, args := splitFirst(line)
		out = append(out, directive{
			name: strings.ToLower(name),
			args: strings.TrimSpace(args),
			raw:  line,
		})
	}
	return out
}

// splitFirst splits on the first run of whitespace.
func splitFirst(s string) (head, rest string) {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if r == ' ' || r == '\t' {
			return s[:i], strings.TrimSpace(s[i:])
		}
	}
	return s, ""
}

// fields splits an argument string into whitespace-separated tokens, but keeps
// double-quoted runs together (quotes stripped). swag's @Param / @Success lines
// rely on this: `id path int true "Account ID" minimum(1)`.
func fields(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// parseAttributes pulls swag-style trailing attributes like
// `minimum(1) maxLength(10) enums(a,b,c) default(x)` out of a token list,
// returning the leading non-attribute tokens and the attribute map.
func parseAttributes(toks []string) (lead []string, attrs map[string]string) {
	attrs = map[string]string{}
	for _, t := range toks {
		open := strings.IndexByte(t, '(')
		if open > 0 && strings.HasSuffix(t, ")") {
			key := strings.ToLower(t[:open])
			val := t[open+1 : len(t)-1]
			attrs[key] = val
			continue
		}
		lead = append(lead, t)
	}
	return lead, attrs
}

// isTruthy interprets swag's required flag.
func isTruthy(s string) bool {
	switch strings.ToLower(s) {
	case "true", "required", "1", "yes":
		return true
	}
	return false
}
