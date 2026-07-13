package testdata

import "github.com/indykite/openapi-parser/testdata/model"

// @title        Example API
// @summary      Accounts and things, for parser exercise.
// @version      1.0
// @description  A sample API exercising the parser.
// @contact.name API Support
// @contact.email support@example.com
// @license.name Apache 2.0
// @license.identifier Apache-2.0
// @host         api.example.com
// @BasePath     /v1
// @schemes      https http
// @self         https://api.example.com/v1/openapi.json
// @server.url   https://{region}.api.example.com/v1
// @server.name  regional
// @server.description Regional endpoint
// @server.variable region eu "Region code" enums(eu,us)
// @security     ApiKeyAuth
// @externalDocs.url https://docs.example.com
// @externalDocs.description Platform documentation
// @tag.name     accounts
// @tag.summary  Account management
// @tag.kind     nav
// @tag.docs.url https://docs.example.com/tags/accounts
// @tag.docs.description Accounts guide
// @tag.name     accounts.reports
// @tag.summary  Analytics reports
// @tag.parent   accounts
// @tag.kind     nav
// @securityDefinitions.apikey ApiKeyAuth
// @in           header
// @name         Authorization
// @securityDefinitions.apikey Bearer Token
// @in           header
// @name         Authorization
// @securityDefinitions.oauth2.accesscode OAuth2Code
// @authorizationUrl https://auth.example.com/authorize
// @tokenUrl     https://auth.example.com/token
// @scope.admin  full access
// @securityDefinitions.bearerauth BearerJWT
// @bearerFormat JWT
// @description  Bearer token for machine clients.
// @securityDefinitions.openidconnect OIDC
// @openIdConnectUrl https://auth.example.com/.well-known/openid-configuration
// @securityDefinitions.mutualtls MTLS
// @securityDefinitions.oauth2.device OAuth2Device
// @tokenUrl     https://auth.example.com/token
// @deviceAuthorizationUrl https://auth.example.com/device
// @oauth2MetadataUrl https://auth.example.com/.well-known/oauth-authorization-server
// @scope.read   read access

// Account is a user account.
// @Description User account information.
type Account struct {
	ID       int                    `json:"id" example:"1"`
	Name     string                 `json:"name" validate:"required"`
	Email    string                 `json:"email,omitempty"`
	Role     string                 `json:"role" enums:"admin,user,guest"`
	Priority int                    `json:"priority" example:"2" enums:"1,2,3" minimum:"1" maximum:"3"`
	Status   string                 `json:"status" binding:"required,oneof=active inactive"`
	Weight   float32                `json:"weight" binding:"gte=0,lte=1" extensions:"x-nullable,x-unit=score,!x-indexed"`
	Meta     map[string]string      `json:"meta"`
	Attrs    map[string]interface{} `json:"attrs"`
	Raw      []byte                 `json:"raw" swaggertype:"string" format:"base64"`
	// Config has an escaped-JSON example; tags after it must still parse.
	Config string `json:"config" example:"{\"key\":\"value\"}" binding:"required,min=1,max=100"`
	// Code is required even though json omits it when empty (swag semantics).
	Code string `json:"code,omitempty" validate:"required"`
	// Hosts: validator dive scopes min=8 to each element, not the array.
	Hosts  []string `json:"hosts" binding:"min=1,dive,min=8"`
	hidden string   // unexported: never marshaled, must not appear
}

// ShowAccount godoc
// @Summary  Show an account
// @Description get account by ID
// @Tags     accounts
// @Produce  json
// @externalDocs.url https://docs.example.com/accounts
// @externalDocs.description Account API guide
// @Param    id   path      int  true  "Account ID"  minimum(1)
// @Success  200  {object}  Account  "the account"
// @Failure  404  {object}  Account  "not found"
// @Failure  401,403 {object} Account "denied"
// @Header   200  {string}  Etag "concurrency control version"
// @Header   all  {string}  X-Request-Id "trace identifier"
// @Security ApiKeyAuth
// @Router   /accounts/{id} [get]
func ShowAccount() {}

// PurgeAccount uses a non-standard verb, which 3.2 places under
// additionalOperations (and 3.1 downgrades to post).
// @Summary  Purge an account
// @ID       purgeAccount
// @Tags     accounts
// @Param    id path int true "Account ID"
// @Success  202 {string} string "accepted"
// @Router   /accounts/{id} [purge]
func PurgeAccount() {}

// UploadAvatar exercises formData params: they assemble into one multipart
// request body instead of overwriting each other.
// @Summary  Upload an avatar
// @Tags     accounts
// @Accept   mpfd
// @Param    avatar formData file true "image file"
// @Param    label formData string false "display label"
// @Success  204 "stored"
// @Router   /accounts/{id}/avatar [post]
func UploadAvatar() {}

// SearchAccounts uses the 3.2 QUERY method natively - no x- hack needed.
// @Summary  Search accounts
// @Tags     accounts
// @Accept   xml,json
// @Produce  json
// @Param    filter body Account true "search filter"
// @Success  200 {array} Account "matches"
// @Router   /accounts/search [query]
func SearchAccounts() {}

// baseResponse carries fields shared by all responses; embedding promotes them.
type baseResponse struct {
	ID   string `json:"id" validate:"required"`
	Etag string `json:"etag"`
}

// Labels is a named non-struct type; fields using it resolve to its
// underlying type inline.
type Labels []*Account

// AccountResponse embeds baseResponse through a pointer - its fields promote.
type AccountResponse struct {
	*baseResponse
	Name   string `json:"name"`
	Labels Labels `json:"labels"`
}

// listResponse is a generic page wrapper, referenced with swaggo's bracket
// syntax: listResponse[AccountResponse].
type listResponse[D any] struct {
	Data []D `json:"data" validate:"required"`
}

// ListAccounts exercises generics, embedded promotion, and a multi-word
// security scheme name.
// @Summary  List accounts
// @Tags     accounts
// @Produce  json
// @Param    limit query int false "page size" minimum(1) maximum(100) default(20)
// @Param    page query int false "page number (required when using cursor)"
// @Param    state query string false "State filter" Enums(active, suspended)
// @Param    sort query string false "sort field" deprecated(true) style(form) explode(false)
// @Success  200 {object} listResponse[AccountResponse] "one page"
// @Security Bearer Token
// @Router   /accounts [get]
func ListAccounts() {
	_ = listResponse[AccountResponse]{} // real use so the types aren't unused
}

// WatchAccounts streams account events over SSE; in 3.2 the response uses
// itemSchema (each event's shape) instead of schema.
// @Summary  Watch account events
// @Tags     accounts
// @Produce  event-stream
// @Success  200 {object} Account "one event per change"
// @ResponseSummary 200 Account change feed
// @Security OAuth2Code[read, admin]
// @Router   /accounts/watch [get]
func WatchAccounts() {}

// AccountCreated documents an outgoing webhook (3.1+ top-level webhooks map).
// @Webhook  account.created
// @Summary  Account created notification
// @Param    payload body Account true "the new account"
// @Success  204 "acknowledged"
func AccountCreated() {}

type alphaHandlers struct{}

// Stats on alphaHandlers shares its name with betaHandlers.Stats: neither
// operation may claim the ambiguous name as operationId.
// @Summary  Alpha stats
// @Tags     accounts
// @Success  200 {object} Account "stats"
// @Router   /alpha/stats [get]
func (alphaHandlers) Stats() {}

type betaHandlers struct{}

// Stats (beta) — see alphaHandlers.Stats.
// @Summary  Beta stats
// @Tags     accounts
// @Success  200 {object} Account "stats"
// @Router   /beta/stats [get]
func (betaHandlers) Stats() {}

// Envelope is a generic wrapper whose Data field gets overridden via composition.
type Envelope struct {
	Code int         `json:"code"`
	Data interface{} `json:"data"`
}

// Wrapped exercises model composition + cross-package resolution:
// Envelope{data=model.Thing} overrides Data with a $ref to the other package.
// @Summary  Wrapped response
// @Tags     accounts
// @Produce  json
// @Success  200 {object} Envelope{data=model.Thing} "wrapped thing"
// @Success  201 {object} Envelope{data=[]model.Thing} "wrapped list"
// @Router   /wrapped [get]
func Wrapped() {
	_ = model.Thing{} // real use so the import isn't unused
}
