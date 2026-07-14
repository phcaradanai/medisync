// Package catalog owns drug master data (code, name, form, strength,
// sticker template fields). Every mutation is audited.
package catalog

import "time"

// Drug is the domain model for a medication in the catalog.
// It mirrors the proto medisync.catalog.v1.Drug fields and decouples
// the store from proto types.
type Drug struct {
	ID          string
	Code        string
	Name        string
	GenericName string
	Form        string
	Strength    string
	Unit        string
	StickerNote string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
