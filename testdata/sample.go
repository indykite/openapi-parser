package testdata

import "github.com/indykite/openapi-parser/testdata/model"

// @title        Example API
// @version      1.0
// @description  A sample API exercising the parser.
// @contact.name API Support
// @contact.email support@example.com
// @license.name Apache 2.0
// @host         api.example.com
// @BasePath     /v1
// @tag.name     accounts
// @tag.summary  Account management
// @tag.kind     nav
// @tag.name     accounts.reports
// @tag.summary  Analytics reports
// @tag.parent   accounts
// @tag.kind     nav
// @securityDefinitions.apikey ApiKeyAuth
// @in           header
// @name         Authorization

// Account is a user account.
// @Description User account information.
type Account struct {
	ID    int    `json:"id" example:"1"`
	Name  string `json:"name" validate:"required"`
	Email string `json:"email,omitempty"`
	Role  string `json:"role" enums:"admin,user,guest"`
}

// ShowAccount godoc
// @Summary  Show an account
// @Description get account by ID
// @Tags     accounts
// @Produce  json
// @Param    id   path      int  true  "Account ID"  minimum(1)
// @Success  200  {object}  Account  "the account"
// @Failure  404  {object}  Account  "not found"
// @Security ApiKeyAuth
// @Router   /accounts/{id} [get]
func ShowAccount() {}

// SearchAccounts uses the 3.2 QUERY method natively — no x- hack needed.
// @Summary  Search accounts
// @Tags     accounts
// @Produce  json
// @Param    filter body Account true "search filter"
// @Success  200 {array} Account "matches"
// @Router   /accounts/search [query]
func SearchAccounts() {}

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
