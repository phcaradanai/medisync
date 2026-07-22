package printing

import "time"

// PrescriptionData is the minimal prescription info needed by the printing module.
// Used by the consumer to fetch data from the lookup layer without importing
// the full dispensing domain.
type PrescriptionData struct {
	PrescriptionID string
	HN             string
	PatientName    string
	WardID         string
	SourceSystem   string
	IssuedAt       *time.Time
	Items          []PrescriptionItemData
}

// PrescriptionItemData is a minimal item for sticker building.
type PrescriptionItemData struct {
	DrugName   string
	Quantity   int32
	DosageText string
}
