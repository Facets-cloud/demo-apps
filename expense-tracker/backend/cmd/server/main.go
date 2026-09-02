// Command server is the single container entrypoint for all three functions.
// It blank-imports the function package so its declarative init()
// registrations (CreateUploadURL / ReceiptUploaded / SummaryConsumer) run,
// then starts the functions-framework HTTP server. The framework serves the
// one function named by the FUNCTION_TARGET env var, so the same image is
// deployed as three Cloud Run services that differ only in FUNCTION_TARGET.
package main

import (
	"log"
	"os"

	"github.com/GoogleCloudPlatform/functions-framework-go/funcframework"

	// Registers the functions via the package's init().
	_ "github.com/anshulsao/demo-apps/expense-tracker/backend"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Cloud Run overrides this via PORT.
	}
	if err := funcframework.Start(port); err != nil {
		log.Fatalf("funcframework.Start: %v", err)
	}
}
