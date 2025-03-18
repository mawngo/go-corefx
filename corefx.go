package corefx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"go.uber.org/fx"
	"io/fs"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
)

const (
	ConfigFolder = "configs"
	ConfigFile   = "app.json"
)
const (
	ProfileProduction  = "production"
	ProfileDevelopment = "development"
	ProfileDebug       = "debug"
)

type CoreConfig interface {
	// AppNameValue application name.
	AppNameValue() string
	// AppVersionValue application version.
	AppVersionValue() string
	// AppConfigLocationValue base config file to load from.
	// Config file must in JSON format.
	// Return empty string to disable loading from a config file.
	// Default implementations read config from file:./configs/app.json.
	AppConfigLocationValue() (string, error)
	// AppAutomaticEnvValue enable read env variable into config struct automatically.
	AppAutomaticEnvValue() bool
	// ProfileValue application env profile (production,development,debug).
	ProfileValue() string
	// RequiredValues the list of field that must specify.
	// This method must return a list of pointers to specified field on the same object.
	RequiredValues() []any
	// LogLevelValue application log level.
	LogLevelValue() string
	// LogFormatValue the format of log, accept "text", "json"
	LogFormatValue() string
	// IsProd shorthand production profile checking.
	IsProd() bool
}

// nolint:staticcheck
type CoreEnv struct {
	AppName    string `json:"app_name" mapstructure:"app_name"`
	AppVersion string `json:"app_version" mapstructure:"app_version"`
	LogLevel   string `json:"log_level" mapstructure:"log_level"`
	Profile    string `json:"profile" mapstructure:"profile"`
	SentryEnv
}

func (e CoreEnv) AppAutomaticEnvValue() bool {
	return true
}

func (e CoreEnv) AppConfigLocationValue() (string, error) {
	path := filepath.Join(".", ConfigFolder, ConfigFile)
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return "file:" + path, nil
}

func (e CoreEnv) LogLevelValue() string {
	return e.LogLevel
}

func (e CoreEnv) LogFormatValue() string {
	return ""
}

func (e CoreEnv) AppNameValue() string {
	return e.AppName
}

func (e CoreEnv) AppVersionValue() string {
	return e.AppVersion
}

func (e CoreEnv) RequiredValues() []any {
	return nil
}

func (e CoreEnv) ProfileValue() string {
	return e.Profile
}

func (e CoreEnv) IsProd() bool {
	return e.ProfileValue() == ProfileProduction
}

func NewEnv() CoreEnv {
	return CoreEnv{
		SentryEnv: SentryEnv{},
	}
}

var _ CoreConfig = (*CoreEnv)(nil)

// NewModule register core module and enable health check support.
func NewModule() fx.Option {
	return fx.Options(
		NewCoreModule(),
		fx.Provide(NewHealth),
	)
}

// NewCoreModule Create a module that autoconfigure slog, sentry
// and populate configuration from file or environment.
// The env config object must implement CoreConfig, and registered using AsConfigFor to be autopopulated.
// The env config must also register as SentryConfig to enable sentry feature.
func NewCoreModule() fx.Option {
	return fx.Options(
		UseSlogLogger(),
		fx.Module("corefx",
			fx.Provide(NewGlobalSlogLogger),
			fx.Decorate(func(p LoadJSONConfigParams) (CoreConfig, error) {
				err := LoadJSONConfig(p)
				return p.Config, err
			}),
			fx.Invoke(func(_ *slog.Logger) {
				// force initialization of logger, which also initialize config.
			}),
		),
	)
}

// As register already registered type T under multiple interfaces.
// Useful if you need a single required object to provide multiple required types.
// This method allows you to inject the original object, and all type it registered by this function.
func As[T any](types ...any) any {
	annotations := make([]fx.Annotation, 0, len(types))
	for i := range types {
		annotations = append(annotations, fx.As(types[i]))
	}

	return fx.Annotate(
		func(t T) T { return t },
		annotations...,
	)
}

// From create a function that accepts and return self.
// This method can be used with other As... methods of multiple fx packages when you want to keep both the original type and annotated type
// after annotated.
// For example, fx.Provide(newMyService, AsInterface(From[*myService])).
func From[T any]() any {
	return func(t T) T { return t }
}

// Named indicate that this service has a name (which may not unique between services).
type Named interface {
	Name() string
}

// LoadJSONConfigInto load json config into cfg pointer.
func LoadJSONConfigInto(cfg any, automaticEnv bool, defaultCfgPath string) error {
	if reflect.ValueOf(cfg).Type().Kind() != reflect.Pointer {
		return errors.New("error LoadConfigInto require a pointer to config struct")
	}

	cfgJSONBytes, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	viper.SetConfigType("json")
	if automaticEnv {
		viper.SetEnvKeyReplacer(strings.NewReplacer(`.`, `__`))
		viper.AutomaticEnv()
	}

	// Load default required keys from struct.
	if err := viper.ReadConfig(bytes.NewReader(cfgJSONBytes)); err != nil {
		return err
	}

	// Handle the config file.
	if strings.HasPrefix(defaultCfgPath, "file:") {
		viper.SetConfigFile(defaultCfgPath[5:])
		// Merge required file into default required, ignore if not exist.
		if err := viper.MergeInConfig(); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return viper.Unmarshal(cfg, func(config *mapstructure.DecoderConfig) {
		config.TagName = "json"
		config.Squash = true
	})
}

type LoadJSONConfigParams struct {
	fx.In
	Config CoreConfig
}

// LoadJSONConfig load config into CoreConfig.
func LoadJSONConfig(p LoadJSONConfigParams) error {
	configLocation, err := p.Config.AppConfigLocationValue()
	if err != nil {
		return err
	}
	if err := LoadJSONConfigInto(p.Config, p.Config.AppAutomaticEnvValue(), configLocation); err != nil {
		return err
	}

	requireds := p.Config.RequiredValues()
	if len(requireds) == 0 {
		return nil
	}
	return checkRequired(p.Config, requireds...)
}

func checkRequired(s any, vals ...any) error {
	c := reflect.ValueOf(s).Elem()
	for i := range vals {
		ptr := vals[i]
		if reflect.ValueOf(ptr).Type().Kind() != reflect.Pointer {
			return errors.New("error requiredValues must return array of pointer")
		}
		f := reflect.ValueOf(ptr).Elem()

		for i := 0; i < c.NumField(); i++ {
			valueField := c.Field(i)

			// Nested struct
			if valueField.Type().Kind() == reflect.Struct {
				ptr := reflect.New(valueField.Type())
				ptr.Elem().Set(valueField.Addr().Elem())
				err := checkRequired(valueField.Addr().Interface(), vals...)
				if err != nil {
					return err
				}
			}

			// Find field belong to required
			if valueField.Addr().Interface() == f.Addr().Interface() {
				isUnset := reflect.ValueOf(ptr).Elem().IsZero()
				if !isUnset {
					continue
				}

				field := c.Type().Field(i)
				configName := field.Tag.Get("json")
				if configName == "" {
					configName = field.Tag.Get("mapstructure")
				}
				if configName == "" {
					configName = field.Name
				}
				return fmt.Errorf("[%s] is config, consider setting value: [%s] in config file or [%s] in env",
					field.Name, configName, strings.ToUpper(configName))
			}
		}
	}
	return nil
}
