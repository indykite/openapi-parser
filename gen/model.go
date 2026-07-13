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

// This file defines the internal model: a parser-agnostic, emitter-agnostic
// representation of an API. Annotations are parsed INTO this model; OpenAPI
// documents are emitted FROM it. Keeping a model in the middle means the
// annotation syntax and the output spec version can evolve independently.

// API is the whole parsed surface.
type API struct {
	SecuritySchemes map[string]SecurityScheme
	ExternalDocs    *ExternalDocs
	Schemas         map[string]*Schema
	Extensions      map[string]any
	Info            Info
	Self            string // 3.2 $self: this document's URI
	Servers         []Server
	Schemes         []string // from @schemes; applied to host-derived servers
	Tags            []Tag
	Security        []map[string][]string
	Operations      []Operation
}

// Info is the OpenAPI info object (title, version, contact, license).
type Info struct {
	Title          string
	Summary        string // 3.1+
	Version        string
	Description    string
	TermsOfService string
	Contact        Contact
	License        License
}

// Contact is the API contact information.
type Contact struct{ Name, URL, Email string }

// License is the API license name, SPDX identifier (3.1+), and URL.
type License struct{ Name, Identifier, URL string }

// Server is a single server entry.
type Server struct {
	Variables   map[string]ServerVariable
	URL         string
	Name        string // 3.2
	Description string
}

// ServerVariable is one URL-template variable of a server.
type ServerVariable struct {
	Default     string
	Description string
	Enum        []string
}

// Tag carries the native 3.2 hierarchical fields directly — no x- smuggling,
// because we own the parser now.
type Tag struct {
	ExternalDocs *ExternalDocs
	Name         string
	Summary      string
	Description  string
	Parent       string
	Kind         string
}

// ExternalDocs is an external documentation reference (description + URL).
type ExternalDocs struct{ Description, URL string }

// SecurityScheme describes one security scheme (apiKey, http, oauth2, ...).
type SecurityScheme struct {
	Type              string // apiKey, http, oauth2, openIdConnect, mutualTLS
	Scheme            string // for http: basic, bearer
	In                string // for apiKey: header, query, cookie
	Name              string // for apiKey
	BearerFormat      string
	Description       string
	Flows             map[string]OAuthFlow // oauth2 flows
	OpenIDConnectURL  string
	OAuth2MetadataURL string // 3.2
	Deprecated        bool   // 3.2
}

// OAuthFlow describes a single OAuth2 flow's URLs and scopes.
type OAuthFlow struct {
	Scopes                 map[string]string
	AuthorizationURL       string
	TokenURL               string
	RefreshURL             string
	DeviceAuthorizationURL string
}

// Operation is one path+method — or one webhook+method when Webhook is set
// (webhooks live under the top-level `webhooks` map, not `paths`).
type Operation struct {
	Extensions   map[string]any
	Body         *Param
	Form         []Param // formData params, emitted as one form request body
	ExternalDocs *ExternalDocs
	Path         string
	Webhook      string // webhook name; mutually exclusive with Path
	Method       string
	ID           string
	Summary      string
	Description  string
	Params       []Param
	Produces     []string
	Consumes     []string
	Responses    []Response
	Security     []map[string][]string
	Tags         []string
	Deprecated   bool
}

// Param is a request parameter or body (its location given by In).
type Param struct {
	Schema      *Schema
	Attributes  map[string]string
	Name        string
	In          string
	Type        string
	Description string
	Required    bool
}

// Response is one response entry keyed by status Code.
type Response struct {
	Schema      *Schema
	Headers     map[string]Header
	Code        string
	Summary     string // 3.2
	Description string
	Kind        string
	DataType    string
}

// Header is a response header's type and description.
type Header struct {
	Type        string
	Description string
}

// Schema is a minimal JSON-Schema-ish node sufficient for the common cases.
// Ref, when set, wins (emitted as $ref). Otherwise Type/Items/Properties.
type Schema struct {
	Example              any
	Default              any
	Extensions           map[string]any // x-... from extensions struct tags
	Items                *Schema
	Properties           map[string]*Schema
	AdditionalProperties *Schema
	Minimum              *float64
	Maximum              *float64
	ExclusiveMinimum     *float64
	ExclusiveMaximum     *float64
	MultipleOf           *float64
	MinLength            *int
	MaxLength            *int
	MinItems             *int
	MaxItems             *int
	Ref                  string
	Format               string
	Pattern              string
	Description          string
	Type                 []string
	Required             []string
	Enum                 []any
	AnyOf                []*Schema
}
