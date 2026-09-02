// Command local runs the ReceiptUploaded pipeline offline against in-memory
// fakes so a reader can see the parse → store → publish flow end to end with
// `go run ./cmd/local` — no GCP project, no network.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/events"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/service"
	"github.com/anshulsao/demo-apps/expense-tracker/backend/internal/store"
)

func main() {
	ctx := context.Background()

	st := store.NewFake()
	pub := events.NewFake()
	svc := service.New(st, pub)

	obj := service.GCSObject{
		Bucket:      "receipts-bucket",
		Name:        "receipts/2026-09-01_starbucks_4.50_a1b2c3.jpg",
		ContentType: "image/jpeg",
		Size:        2048,
	}

	fmt.Printf("Handling GCS object: %s\n\n", obj.Name)
	if err := svc.Handle(ctx, obj); err != nil {
		log.Fatalf("Handle failed: %v", err)
	}

	saved := st.Saved[0]
	fmt.Println("Stored expense:")
	printJSON(saved)

	fmt.Println("\nPublished expense.created event:")
	printJSON(pub.Published[0])
}

func printJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	fmt.Println(string(b))
}
