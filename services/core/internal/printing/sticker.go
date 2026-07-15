package printing

import (
	"fmt"
	"time"

	"github.com/adm-chura3inter/medisync/services/core/internal/dispensing"
)

// StickerPayload is the structured content for a prescription sticker
// sent inside the print_ops job payload.
type StickerPayload struct {
	PrescriptionID string   `json:"prescription_id"`
	PatientName    string   `json:"patient_name"`
	HN             string   `json:"hn"`
	WardID         string   `json:"ward_id"`
	Items          []string `json:"items"` // human-readable drug lines
	IssuedAt       string   `json:"issued_at"`
	GeneratedAt    string   `json:"generated_at"`
}

// BuildStickerPayload constructs a StickerPayload from prescription row data.
// This is the print_ops anti-corruption layer: it translates domain objects
// into the sticker rendering format expected by the print_ops template.
func BuildStickerPayload(prescription *dispensing.PrescriptionRow) StickerPayload {
	items := make([]string, len(prescription.Items))
	for i, it := range prescription.Items {
		items[i] = formatDispensingItem(it)
	}

	issuedAt := ""
	if prescription.IssuedAt != nil {
		issuedAt = prescription.IssuedAt.Format(time.RFC3339)
	}

	return StickerPayload{
		PrescriptionID: prescription.PrescriptionID,
		PatientName:    prescription.PatientName,
		HN:             prescription.HN,
		WardID:         prescription.WardID,
		Items:          items,
		IssuedAt:       issuedAt,
		GeneratedAt:    time.Now().Format(time.RFC3339),
	}
}

// BuildStickerPayloadFromData constructs a StickerPayload from PrescriptionData
// (the consumer-side view, before the full dispensing domain model).
func BuildStickerPayloadFromData(data PrescriptionData) StickerPayload {
	itemLines := make([]string, len(data.Items))
	for i, it := range data.Items {
		itemLines[i] = fmt.Sprintf("%s x%d", it.DrugName, it.Quantity)
	}

	issuedAtStr := ""
	if data.IssuedAt != nil {
		issuedAtStr = data.IssuedAt.Format(time.RFC3339)
	}

	return StickerPayload{
		PrescriptionID: data.PrescriptionID,
		PatientName:    data.PatientName,
		HN:             data.HN,
		WardID:         data.WardID,
		Items:          itemLines,
		IssuedAt:       issuedAtStr,
		GeneratedAt:    time.Now().Format(time.RFC3339),
	}
}

func formatDispensingItem(it dispensing.Item) string {
	return fmt.Sprintf("%s x%d", it.DrugName, it.Quantity)
}
