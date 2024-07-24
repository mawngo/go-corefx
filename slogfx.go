package corefx

import (
	"context"
	"github.com/getsentry/sentry-go"
	"github.com/phsym/console-slog"
	slogmulti "github.com/samber/slog-multi"
	slogsentry "github.com/samber/slog-sentry/v2"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"log/slog"
	"os"
	"strings"
	"time"
)

// nolint:gochecknoinits
// Set default level to Warn to avoid pre-setup logging.
// Client can always set this value in main().
func init() {
	slog.SetLogLoggerLevel(slog.LevelWarn)
}

// UseSlogLogger configure fx to use slog.Default logger.
func UseSlogLogger() fx.Option {
	return fx.WithLogger(func() fxevent.Logger {
		return &fxevent.SlogLogger{Logger: slog.Default()}
	})
}

// newSlogLogger create a logger instance.
func newSlogLogger(p SlogLoggerParams) (*slog.Logger, error) {
	level := parseLogLevel(p.Config.LogLevelValue())
	if p.Config.ProfileValue() == ProfileDebug {
		level = slog.LevelDebug
	}

	logFormat := p.Config.LogFormatValue()
	if logFormat == "" && p.Config.ProfileValue() == ProfileProduction {
		logFormat = "json"
	}
	var handler slog.Handler
	if logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = console.NewHandler(os.Stderr, &console.HandlerOptions{Level: level})
	}
	if p.LogConfig == nil || p.LogConfig.SentryDsnValue() == "" {
		return slog.New(handler), nil
	}
	// Setup sentry.
	environment := ProfileDevelopment
	if p.Config.ProfileValue() == ProfileProduction {
		environment = ProfileProduction
	}
	release := p.Config.AppNameValue()
	if release == "" {
		release = "unknown"
	}
	if p.Config.AppVersionValue() != "" {
		release += "@" + p.Config.AppVersionValue()
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:           p.LogConfig.SentryDsnValue(),
		EnableTracing: false,
		Environment:   environment,
		Release:       release,
	})
	if err != nil {
		return nil, err
	}
	p.Lifecycle.Append(fx.Hook{
		OnStop: func(_ context.Context) error {
			sentry.Flush(5 * time.Second)
			return nil
		},
	})

	sentryLogLevel := slog.LevelWarn
	if p.LogConfig.SentryLogLevelValue() != "" {
		sentryLogLevel = parseLogLevel(p.LogConfig.SentryLogLevelValue())
	}
	return slog.New(
		slogmulti.Fanout(
			handler,
			slogsentry.Option{Level: sentryLogLevel}.NewSentryHandler(),
		),
	), nil
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type SlogLoggerParams struct {
	fx.In
	Config    CoreConfig
	LogConfig SentryConfig `optional:"true"`
	Lifecycle fx.Lifecycle
}

// NewGlobalSlogLogger create a logger instance and register it globally.
func NewGlobalSlogLogger(p SlogLoggerParams) (*slog.Logger, error) {
	logger, err := newSlogLogger(p)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	return logger, nil
}

type SentryConfig interface {
	SentryDsnValue() string
	SentryLogLevelValue() string
}

type SentryEnv struct {
	SentryDsn      string `json:"sentry_dsn" mapstructure:"sentry_dsn"`
	SentryLogLevel string `json:"sentry_log_level" mapstructure:"sentry_log_level"`
}

func (e SentryEnv) SentryDsnValue() string {
	return e.SentryDsn
}

func (e SentryEnv) SentryLogLevelValue() string {
	return e.SentryLogLevel
}

var _ SentryConfig = (*SentryEnv)(nil)
