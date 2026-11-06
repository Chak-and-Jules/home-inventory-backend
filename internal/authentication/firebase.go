package authentication

import (
	"context"
	"fmt"
	"os"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

// Client exposes only the Firebase operations used by the API.
type Client interface {
	VerifyIDTokenAndCheckRevoked(context.Context, string) (*auth.Token, error)
	DeleteUser(context.Context, string) error
}

// NewClient initializes Firebase once using Application Default Credentials.
// On Cloud Run these belong to the service's runtime service account.
func NewClient(ctx context.Context) (Client, error) {
	return newClient(ctx, os.Getenv("FIREBASE_PROJECT_ID"))
}

func newClient(ctx context.Context, projectID string, opts ...option.ClientOption) (*auth.Client, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("FIREBASE_PROJECT_ID is required")
	}
	// The emulator accepts unsigned tokens. Never enable that in a deployed API.
	if os.Getenv("FIREBASE_AUTH_EMULATOR_HOST") != "" && (os.Getenv("APP_ENV") != "local" || os.Getenv("K_SERVICE") != "") {
		return nil, fmt.Errorf("Firebase Auth emulator requires APP_ENV=local outside Cloud Run")
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, opts...)
	if err != nil {
		return nil, fmt.Errorf("initialize Firebase app: %w", err)
	}
	return app.Auth(ctx)
}
