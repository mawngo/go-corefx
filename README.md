# Core FX

Integrate core feature like configuration slog and sentry into FX.

## Install

```
go get -u github.com/mawngo/go-corefx
```

## Usage

By default, core fx module will load configuration from `.configs/app.json` then `enviroment variables`

```go
package main

import (
	"encoding/json"
	"github.com/mawngo/go-corefx"
	"go.uber.org/fx"
)

func main() {
	fx.New(
		configModule,
		corefx.NewModule(),
		fx.Invoke(func(c *myConfig, s fx.Shutdowner) {
			b, _ := json.Marshal(c)
			// app.json: {"app_version": "1.1.1", "log_level": "warn"}
			println(string(b)) // {"app_name":"example","app_version":"1.1.1","log_level":"warn","profile":"","sentry_dsn":"","sentry_log_level":""}
			_ = s.Shutdown()
		}),
	).Run()
}

// requiredConfigModule create a required module that provide corefx.CoreConfig.
var configModule = fx.Module("config",
	fx.Provide(
		newConfig,
		corefx.As[*myConfig](
			new(corefx.CoreConfig),
			new(corefx.SentryConfig), // Optional.
		),
	),
)

// myConfig Create a custom required struct that implement corefx.CoreConfig.
type myConfig struct {
	corefx.CoreEnv
}

func newConfig() *myConfig {
	return &myConfig{
		CoreEnv: corefx.CoreEnv{
			AppName:  "example",
			LogLevel: "info",
		},
	}
}

func (c *myConfig) ProfileValue() string {
	return c.Profile
}
```

Required: all config implementer should support UnmarshalJSON and MarshalJSON.
