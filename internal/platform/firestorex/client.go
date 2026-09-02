package firestorex

import (
	"context"
	"fmt"
	"os"

	"cloud.google.com/go/firestore"
)

// NewClient creates a Firestore client for the given GCP project.
// When FIRESTORE_EMULATOR_HOST is set, the official client talks to the emulator
// (no real GCP credentials required).
func NewClient(ctx context.Context, projectID string) (*firestore.Client, error) {
	if projectID == "" {
		return nil, fmt.Errorf("GCP project ID is required")
	}
	if host := os.Getenv("FIRESTORE_EMULATOR_HOST"); host != "" {
		// Documented for operators; the SDK picks this up automatically.
		_ = host
	}
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("firestore client: %w", err)
	}
	return client, nil
}
