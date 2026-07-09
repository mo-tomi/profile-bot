package config

import (
	"path/filepath"
	"testing"
)

func testRolesConfig() *RolesConfig {
	return &RolesConfig{
		RoleCategories: []CategoryConfig{
			{
				Name:        "activity",
				DisplayName: "🎮 アクティビティ",
				Roles: []RoleConfig{
					{Name: "ゲーマー", Emoji: "🎮"},
					{Name: "エンジニア", Emoji: "💻"},
				},
			},
			{
				Name:        "region",
				DisplayName: "📍 地域",
				Roles: []RoleConfig{
					{Name: "関東", Emoji: "📍"},
				},
			},
		},
		ExcludedRoles:    []string{"@everyone"},
		ExcludedSuffixes: []string{"Bot", "BOT"},
		SpecialFilters: []SpecialFilter{
			{Pattern: "999", Action: "remove"},
		},
	}
}

func TestLoadRolesConfig(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "roles.yaml")

	cfg, err := LoadRolesConfig(path)
	if err != nil {
		t.Fatalf("unexpected error loading %s: %v", path, err)
	}
	if len(cfg.RoleCategories) == 0 {
		t.Error("expected at least one role category")
	}
}

func TestLoadRolesConfig_MissingFile(t *testing.T) {
	_, err := LoadRolesConfig("does-not-exist.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestIsExcludedRole(t *testing.T) {
	rc := testRolesConfig()

	tests := []struct {
		name    string
		role    string
		managed bool
		want    bool
	}{
		{"managed role is excluded", "AnyRole", true, true},
		{"explicitly excluded role", "@everyone", false, true},
		{"suffix-excluded role", "MusicBot", false, true},
		{"regular role is not excluded", "ゲーマー", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rc.IsExcludedRole(tt.role, tt.managed); got != tt.want {
				t.Errorf("IsExcludedRole(%q, %v) = %v, want %v", tt.role, tt.managed, got, tt.want)
			}
		})
	}
}

func TestApplySpecialFilters(t *testing.T) {
	rc := testRolesConfig()

	got := rc.ApplySpecialFilters("ゲーマー999")
	want := "ゲーマー"
	if got != want {
		t.Errorf("ApplySpecialFilters = %q, want %q", got, want)
	}

	// フィルターに該当しない場合は変化しない
	got = rc.ApplySpecialFilters("エンジニア")
	want = "エンジニア"
	if got != want {
		t.Errorf("ApplySpecialFilters = %q, want %q", got, want)
	}
}

func TestGetCategoryAndEmoji(t *testing.T) {
	rc := testRolesConfig()

	name, displayName, emoji := rc.GetCategoryAndEmoji("ゲーマー")
	if name != "activity" || displayName != "🎮 アクティビティ" || emoji != "🎮" {
		t.Errorf("got (%q, %q, %q)", name, displayName, emoji)
	}

	name, displayName, emoji = rc.GetCategoryAndEmoji("存在しないロール")
	if name != "" || displayName != "" || emoji != "" {
		t.Errorf("expected empty results for unknown role, got (%q, %q, %q)", name, displayName, emoji)
	}
}

func TestCategorizeRoles(t *testing.T) {
	rc := testRolesConfig()

	result := rc.CategorizeRoles([]string{"ゲーマー", "関東", "存在しないロール"})

	if len(result) != 2 {
		t.Fatalf("expected 2 categories, got %d: %+v", len(result), result)
	}

	if result[0].Name != "activity" || len(result[0].Roles) != 1 || result[0].Roles[0].Name != "ゲーマー" {
		t.Errorf("unexpected first category: %+v", result[0])
	}
	if result[1].Name != "region" || len(result[1].Roles) != 1 || result[1].Roles[0].Name != "関東" {
		t.Errorf("unexpected second category: %+v", result[1])
	}
}

func TestCategorizeRoles_Empty(t *testing.T) {
	rc := testRolesConfig()

	result := rc.CategorizeRoles([]string{"存在しないロール"})
	if len(result) != 0 {
		t.Errorf("expected no categories, got %+v", result)
	}
}
