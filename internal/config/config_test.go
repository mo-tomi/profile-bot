package config

import "testing"

func TestGetEnvOrDefault(t *testing.T) {
	t.Run("returns env value when set", func(t *testing.T) {
		t.Setenv("TEST_ENV_KEY", "custom-value")
		if got := getEnvOrDefault("TEST_ENV_KEY", "default"); got != "custom-value" {
			t.Errorf("got %q, want %q", got, "custom-value")
		}
	})

	t.Run("returns default when unset", func(t *testing.T) {
		if got := getEnvOrDefault("TEST_ENV_KEY_UNSET", "default"); got != "default" {
			t.Errorf("got %q, want %q", got, "default")
		}
	})

	t.Run("returns default when empty", func(t *testing.T) {
		t.Setenv("TEST_ENV_KEY_EMPTY", "")
		if got := getEnvOrDefault("TEST_ENV_KEY_EMPTY", "default"); got != "default" {
			t.Errorf("got %q, want %q", got, "default")
		}
	})
}

func TestGetEnvAsInt(t *testing.T) {
	t.Run("returns parsed int when set", func(t *testing.T) {
		t.Setenv("TEST_ENV_INT_KEY", "42")
		if got := getEnvAsInt("TEST_ENV_INT_KEY", 7); got != 42 {
			t.Errorf("got %d, want %d", got, 42)
		}
	})

	t.Run("returns default when unset", func(t *testing.T) {
		if got := getEnvAsInt("TEST_ENV_INT_KEY_UNSET", 7); got != 7 {
			t.Errorf("got %d, want %d", got, 7)
		}
	})

	t.Run("returns default when not a valid int", func(t *testing.T) {
		t.Setenv("TEST_ENV_INT_KEY_INVALID", "not-a-number")
		if got := getEnvAsInt("TEST_ENV_INT_KEY_INVALID", 7); got != 7 {
			t.Errorf("got %d, want %d", got, 7)
		}
	})
}

func TestGetEnvAsBool(t *testing.T) {
	t.Run("returns parsed true when set", func(t *testing.T) {
		t.Setenv("TEST_ENV_BOOL_KEY", "true")
		if got := getEnvAsBool("TEST_ENV_BOOL_KEY", false); got != true {
			t.Errorf("got %v, want %v", got, true)
		}
	})

	t.Run("returns parsed false when set", func(t *testing.T) {
		t.Setenv("TEST_ENV_BOOL_KEY", "false")
		if got := getEnvAsBool("TEST_ENV_BOOL_KEY", true); got != false {
			t.Errorf("got %v, want %v", got, false)
		}
	})

	t.Run("returns default when unset", func(t *testing.T) {
		if got := getEnvAsBool("TEST_ENV_BOOL_KEY_UNSET", true); got != true {
			t.Errorf("got %v, want %v", got, true)
		}
	})

	t.Run("returns default when not a valid bool", func(t *testing.T) {
		t.Setenv("TEST_ENV_BOOL_KEY_INVALID", "not-a-bool")
		if got := getEnvAsBool("TEST_ENV_BOOL_KEY_INVALID", true); got != true {
			t.Errorf("got %v, want %v", got, true)
		}
	})
}

func TestLoadConfig_MissingRequiredEnv(t *testing.T) {
	t.Run("missing DISCORD_TOKEN", func(t *testing.T) {
		t.Setenv("DISCORD_TOKEN", "")
		t.Setenv("DATABASE_URL", "postgresql://example")

		_, err := LoadConfig()
		if err == nil {
			t.Fatal("expected error when DISCORD_TOKEN is missing, got nil")
		}
	})

	t.Run("missing DATABASE_URL", func(t *testing.T) {
		t.Setenv("DISCORD_TOKEN", "token")
		t.Setenv("DATABASE_URL", "")

		_, err := LoadConfig()
		if err == nil {
			t.Fatal("expected error when DATABASE_URL is missing, got nil")
		}
	})
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgresql://example")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("PORT", "")
	t.Setenv("TARGET_VOICE_CHANNELS", "")
	t.Setenv("EXCLUDED_USER_IDS", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "json")
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if len(cfg.TargetVoiceChannels) == 0 {
		t.Error("expected default TargetVoiceChannels to be non-empty")
	}
	if len(cfg.ExcludedUserIDs) == 0 {
		t.Error("expected default ExcludedUserIDs to be non-empty")
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgresql://example")
	t.Setenv("TARGET_VOICE_CHANNELS", "111,222,333")
	t.Setenv("EXCLUDED_USER_IDS", "999,888")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantVC := []string{"111", "222", "333"}
	if len(cfg.TargetVoiceChannels) != len(wantVC) {
		t.Fatalf("TargetVoiceChannels = %v, want %v", cfg.TargetVoiceChannels, wantVC)
	}
	for i, v := range wantVC {
		if cfg.TargetVoiceChannels[i] != v {
			t.Errorf("TargetVoiceChannels[%d] = %q, want %q", i, cfg.TargetVoiceChannels[i], v)
		}
	}

	wantExcluded := []string{"999", "888"}
	if len(cfg.ExcludedUserIDs) != len(wantExcluded) {
		t.Fatalf("ExcludedUserIDs = %v, want %v", cfg.ExcludedUserIDs, wantExcluded)
	}
}
