// Command feeder is the mock hospital prescription feeder. It publishes
// rx.prescription.created events in the exact wire format (protojson of
// medisync.events.v1.PrescriptionCreated) the external team's real feeder
// must produce. Dev/testing only.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
)

func main() {
	url := flag.String("url", "nats://localhost:4222", "NATS server URL")
	count := flag.Int("count", 1, "number of prescriptions to publish")
	ward := flag.String("ward", "WARD-3A", "ward id stamped on events")
	projectCode := flag.String("project-code", "0001", "immutable 4-digit destination project code")
	source := flag.String("source", "mock-his", "source_system identifier")
	fixedID := flag.String("id", "", "fixed prescription_id (for idempotency testing); default is time-based")
	flag.Parse()

	if err := run(*url, *count, *ward, *projectCode, *source, *fixedID); err != nil {
		log.Fatal(err)
	}
}

func run(url string, count int, ward, projectCode, source, fixedID string) error {
	nc, err := nats.Connect(url, nats.Name("medisync-mock-feeder"))
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Drain()

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < count; i++ {
		ev := sample(ward, source, i)
		ev.ProjectCode = projectCode
		if fixedID != "" {
			ev.PrescriptionId = fixedID
		}
		data, err := protojson.Marshal(ev)
		if err != nil {
			return err
		}

		// Nats-Msg-Id gives JetStream-level dedupe on top of DB idempotency.
		msg := nats.NewMsg(natsx.SubjectPrescriptionCreated)
		msg.Data = data
		msg.Header.Set("Nats-Msg-Id", source+"/"+ev.GetPrescriptionId())

		ack, err := js.PublishMsg(ctx, msg)
		if err != nil {
			return fmt.Errorf("publish: %w", err)
		}
		fmt.Printf("published %s (stream=%s seq=%d)\n", ev.GetPrescriptionId(), ack.Stream, ack.Sequence)
	}
	return nil
}

func sample(ward, source string, i int) *eventsv1.PrescriptionCreated {
	now := time.Now()
	id := fmt.Sprintf("RX-%s-%03d", now.Format("20060102-150405"), i+1)
	return &eventsv1.PrescriptionCreated{
		PrescriptionId: id,
		SourceSystem:   source,
		ProjectCode:    "0001",
		Hn:             fmt.Sprintf("HN%06d", 100000+i),
		PatientName:    fmt.Sprintf("Test Patient %02d", i+1),
		WardId:         ward,
		IssuedAt:       timestamppb.New(now),
		TraceId:        "feeder-" + id,
		Items: []*eventsv1.PrescriptionItem{
			{
				DrugCode:   "PARA-500",
				DrugName:   "Paracetamol 500 mg",
				Quantity:   10,
				DosageText: "รับประทานครั้งละ 1 เม็ด ทุก 6 ชั่วโมง เวลาปวดหรือมีไข้",
			},
			{
				DrugCode:   "CM-AMOX",
				DrugName:   "Amoxicillin 500 mg",
				Quantity:   21,
				DosageText: "รับประทานครั้งละ 1 แคปซูล วันละ 3 ครั้ง หลังอาหาร",
			},
		},
	}
}
