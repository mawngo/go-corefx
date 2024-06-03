package main

import (
	"github.com/mawngo/go-corefx"
	"go.uber.org/fx"
)

func main() {
	fx.New(
		requiredConfigModule,
		corefx.NewModule(),
		fx.Invoke(func(_ *myRequiredConfig) {
			// Fail as required field not configured.
		}),
	).Run()
}

// requiredConfigModule create a required module that provide corefx.CoreConfig.
var requiredConfigModule = fx.Module("required",
	fx.Provide(
		newRequiredConfig,
		corefx.AsConfigFor[*myRequiredConfig](
			new(corefx.CoreConfig),
			new(corefx.SentryConfig), // optional, enable sentry configuration.
		),
	),
)

// myConfig Create a custom required struct that implement corefx.CoreConfig.
type myRequiredConfig struct {
	corefx.CoreEnv
	NestedConfig
	Other string
}

type NestedConfig struct {
	Required string `json:"required"`
}

func newRequiredConfig() *myRequiredConfig {
	return &myRequiredConfig{
		CoreEnv: corefx.NewEnv(),
	}
}

func (c *myRequiredConfig) ProfileValue() string {
	return c.Profile
}

func (c *myRequiredConfig) RequiredValues() []any {
	return []any{&c.Required}
}
