package logger

import (
	"log"
	"os"

	adapter "github.com/axiomhq/axiom-go/adapters/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger

func InitLogger() {

	// Determine if we should use Axiom logging
	axiomDataset := os.Getenv("AXIOM_DATASET")
	axiomToken := os.Getenv("AXIOM_TOKEN")

	var core zapcore.Core

	if axiomDataset != "" && axiomToken != "" {
		// Initialize the Axiom adapter for zap
		axiomCore, err := adapter.New(
			adapter.SetDataset(axiomDataset),
		)
		if err != nil {
			log.Printf("Failed to initialize Axiom logger adapter: %v, falling back to standard console logging", err)
			core = getFallbackCore()
		} else {
			// Also log to console in production for standard output tracking
			consoleEncoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
			consoleCore := zapcore.NewCore(
				consoleEncoder,
				zapcore.Lock(os.Stdout),
				zapcore.InfoLevel,
			)

			// Combine both cores
			core = zapcore.NewTee(consoleCore, axiomCore)
		}
	} else {
		core = getFallbackCore()
	}

	Log = zap.New(core, zap.AddCaller())

	// Replace global zap logger
	zap.ReplaceGlobals(Log)
}

func getFallbackCore() zapcore.Core {
	// Fallback to standard console logging if Axiom isn't configured
	config := zap.NewProductionConfig()
	if os.Getenv("APP_ENV") == "local" || os.Getenv("APP_ENV") == "" {
		config = zap.NewDevelopmentConfig()
	}

	coreLogger, err := config.Build()
	if err != nil {
		log.Fatalf("Failed to build fallback zap logger: %v", err)
	}
	return coreLogger.Core()
}

func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}
