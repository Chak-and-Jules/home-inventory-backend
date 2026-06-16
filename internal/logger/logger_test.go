package logger

import (
	"os"
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestInitLogger_FallbackLocal(t *testing.T) {
	// Ensure axiom envs are unset
	os.Unsetenv("AXIOM_DATASET")
	os.Unsetenv("AXIOM_TOKEN")
	os.Setenv("APP_ENV", "local")

	InitLogger()
	assert.NotNil(t, Log)

	Sync()
}

func TestInitLogger_FallbackProduction(t *testing.T) {
	// Ensure axiom envs are unset
	os.Unsetenv("AXIOM_DATASET")
	os.Unsetenv("AXIOM_TOKEN")
	os.Setenv("APP_ENV", "production")

	InitLogger()
	assert.NotNil(t, Log)

	Sync()
}

func TestInitLogger_Axiom_InvalidToken(t *testing.T) {
	// Should gracefully fallback
	os.Setenv("AXIOM_DATASET", "test_dataset")
	os.Setenv("AXIOM_TOKEN", "test_token")
	defer func() {
		os.Unsetenv("AXIOM_DATASET")
		os.Unsetenv("AXIOM_TOKEN")
	}()

	InitLogger()
	assert.NotNil(t, Log)

	Sync()
}
