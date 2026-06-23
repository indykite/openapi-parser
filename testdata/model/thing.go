package model

// Thing lives in a SEPARATE package to exercise cross-package resolution.
type Thing struct {
	ID    int    `json:"id" example:"7"`
	Label string `json:"label" validate:"required"`
}
