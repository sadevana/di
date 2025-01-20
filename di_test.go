package di

import (
	"errors"
	"strings"
	"testing"
)

func TestNewProvider(t *testing.T) {
	t.Run("non-function provider", func(t *testing.T) {
		_, err := NewProvider("not a function")
		requireError(t, err)
		requireEqual(t, "failed to parse providers: 0th provider is not a function, got string", err.Error())
	})

	t.Run("provider with no output", func(t *testing.T) {
		_, err := NewProvider(func() {})
		requireError(t, err)
		requireEqual(t, "failed to parse providers: 0th provider 1 has no output", err.Error())
	})

	t.Run("provider with more than two outputs", func(t *testing.T) {
		providerFunc := func() (string, string, int) { return "", "", 0 }
		_, err := NewProvider(providerFunc)
		requireError(t, err)
		requireEqual(t, "failed to parse providers: 0th provider 1 has more than two outputs. Provider must return a single value or a value and an error", err.Error())
	})

	t.Run("provider with wrong second output type", func(t *testing.T) {
		providerFunc := func() (string, string) { return "", "" }
		_, err := NewProvider(providerFunc)
		requireError(t, err)
		requireEqual(t, "failed to parse providers: 0th provider 1 has two outputs, but the second one is not an error", err.Error())
	})

	t.Run("duplicate provider", func(t *testing.T) {
		providerFunc1 := func() string { return "" }
		providerFunc2 := func() string { return "" }
		_, err := NewProvider(providerFunc1, providerFunc2)
		requireError(t, err)
		requireEqual(t, "failed to parse providers: 1th provider 2 returns the same type string as provider 1", err.Error())
	})
	t.Run("missing dependency", func(t *testing.T) {
		providerFunc := func(int) string { return "" }
		_, err := NewProvider(providerFunc)
		requireError(t, err)
		requireEqual(t, "all deps must be provided: dependency int is not provided", err.Error())
	})
	t.Run("cyclic dependency", func(t *testing.T) {
		_, err := NewProvider(
			func(i int) string { return "" },
			func(s string) int { return 0 },
		)
		requireError(t, err)
		requireContains(t, err.Error(), "should not have cyclic dependencies")
	})
	t.Run("valid providers", func(t *testing.T) {
		providerFunc1 := func() string { return "hello" }
		providerFunc2 := func(s string) int { return len(s) }
		_, err := NewProvider(providerFunc1, providerFunc2)
		requireNoError(t, err)
	})
}

func TestProvider_Provide(t *testing.T) {
	t.Run("non-pointer destination", func(t *testing.T) {
		provider, err := NewProvider(func() string { return "hello" })
		requireNoError(t, err)

		err = provider.Provide(struct{}{})
		requireError(t, err)
	})

	t.Run("non-struct pointer destination", func(t *testing.T) {
		provider, err := NewProvider(func() string { return "hello" })
		requireNoError(t, err)

		err = provider.Provide(new(string))
		requireError(t, err)
	})

	t.Run("provider returns error", func(t *testing.T) {
		providerError := errors.New("provider error")
		provider, err := NewProvider(func() (string, error) {
			return "", providerError
		})
		requireNoError(t, err)

		err = provider.Provide(&struct{ S string }{})
		requireErrorIs(t, err, providerError)
	})
	t.Run("provider dependency returns error", func(t *testing.T) {
		depProviderError := errors.New("provider dependency error")
		depProvider := func() (string, error) {
			return "", depProviderError
		}
		mainProvider := func(s string) int {
			return len(s)
		}
		provider, err := NewProvider(depProvider, mainProvider)
		requireNoError(t, err)

		err = provider.Provide(&struct {
			I int
		}{})
		requireErrorIs(t, err, depProviderError)
	})

	t.Run("missing field provider", func(t *testing.T) {
		provider, err := NewProvider(func() string { return "hello" })
		requireNoError(t, err)

		err = provider.Provide(&struct {
			S string
			I int
		}{})
		requireError(t, err)
		requireEqual(t, "failed to resolve field I: no provider found for type int", err.Error())
	})

	t.Run("successful injection", func(t *testing.T) {
		provider, err := NewProvider(
			func() string { return "hello" },
			func(s string) int { return len(s) },
		)
		requireNoError(t, err)

		dst := &struct {
			S string
			I int
		}{}
		err = provider.Provide(dst)
		requireNoError(t, err)

		requireEqual(t, "hello", dst.S)
		requireEqual(t, 5, dst.I)
	})
}

func TestComplexDependencies(t *testing.T) {
	type Config struct {
		DBHost string
		DBPort int
	}
	type Database struct {
		Config Config
		URL    string
	}
	type Service struct {
		DB     Database
		Active bool
	}

	provider, err := NewProvider(
		func() Config {
			return Config{
				DBHost: "localhost",
				DBPort: 5432,
			}
		},
		func(cfg Config) Database {
			return Database{
				Config: cfg,
				URL:    "postgresql://" + cfg.DBHost,
			}
		},
		func(db Database) Service {
			return Service{
				DB:     db,
				Active: true,
			}
		},
	)
	requireNoError(t, err)

	dst := &struct {
		Cfg Config
		DB  Database
		Svc Service
	}{}

	err = provider.Provide(dst)
	requireNoError(t, err)

	requireEqual(t, "localhost", dst.Cfg.DBHost)
	requireEqual(t, "postgresql://localhost", dst.DB.URL)
	requireTrue(t, dst.Svc.Active)
}

func requireEqual[T comparable](t *testing.T, a, b T) {
	if a != b {
		t.Fatalf("expected %v, got %v", a, b)
	}
}

func requireError(t *testing.T, err error) {
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func requireErrorIs(t *testing.T, err error, target error) {
	if !errors.Is(err, target) {
		t.Fatalf("expected error %v, got %v", target, err)
	}
}

func requireNoError(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func requireTrue(t *testing.T, b bool) {
	if !b {
		t.Fatalf("expected true, got false")
	}
}

func requireContains(t *testing.T, s, substr string) {
	if !strings.Contains(s, substr) {
		t.Fatalf("expected %s to contain %s", s, substr)
	}
}
