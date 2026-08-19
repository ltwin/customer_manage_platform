// Command migrate runs forward database migrations without opening the HTTP
// server. It is used by the destructive restore state machine while writers
// remain stopped.
package main

import (
	"fmt"
	"os"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "migrate error: DATABASE_URL is required")
		os.Exit(1)
	}
	if err := store.MigrateUp(databaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "migrate error: %v\n", err)
		os.Exit(1)
	}
}
