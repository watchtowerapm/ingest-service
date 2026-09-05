package config

import "testing"

func TestEnvOr(t *testing.T) {
	t.Setenv("WATCHTOWER_TEST_ENV_OR", "set")
	if got := EnvOr("WATCHTOWER_TEST_ENV_OR", "fallback"); got != "set" {
		t.Fatalf("EnvOr = %q, want set", got)
	}
	if got := EnvOr("WATCHTOWER_TEST_ENV_OR_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("EnvOr missing = %q, want fallback", got)
	}
}

func TestEnvInt64(t *testing.T) {
	t.Setenv("WATCHTOWER_TEST_ENV_INT", "42")
	if got := EnvInt64("WATCHTOWER_TEST_ENV_INT", 7); got != 42 {
		t.Fatalf("EnvInt64 = %d, want 42", got)
	}
	t.Setenv("WATCHTOWER_TEST_ENV_INT", "nope")
	if got := EnvInt64("WATCHTOWER_TEST_ENV_INT", 7); got != 7 {
		t.Fatalf("EnvInt64 invalid = %d, want 7", got)
	}
	t.Setenv("WATCHTOWER_TEST_ENV_INT", "0")
	if got := EnvInt64("WATCHTOWER_TEST_ENV_INT", 7); got != 7 {
		t.Fatalf("EnvInt64 zero = %d, want 7", got)
	}
}
