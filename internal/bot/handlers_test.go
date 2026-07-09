package bot

import (
	"strings"
	"testing"

	"github.com/tomim/profile-bot/internal/config"
	"github.com/tomim/profile-bot/internal/database"
)

func TestContains(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  bool
	}{
		{"found", []string{"a", "b", "c"}, "b", true},
		{"not found", []string{"a", "b", "c"}, "z", false},
		{"empty slice", []string{}, "a", false},
		{"nil slice", nil, "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(tt.slice, tt.item); got != tt.want {
				t.Errorf("contains(%v, %q) = %v, want %v", tt.slice, tt.item, got, tt.want)
			}
		})
	}
}

func TestCreateIntroductionEmbed(t *testing.T) {
	b := &Bot{
		RolesConfig: &config.RolesConfig{
			RoleCategories: []config.CategoryConfig{
				{Name: "activity", DisplayName: "🎮 アクティビティ"},
			},
		},
	}

	intro := &database.Introduction{
		UserID:    "123",
		ChannelID: "456",
		MessageID: "789",
	}

	roleInfo := map[string][]string{
		"🎮 アクティビティ": {"🎮 ゲーマー"},
	}

	embed := b.createIntroductionEmbed("テストユーザー", "https://example.com/avatar.png", "テストVC", "はじめまして", intro, roleInfo, "guild-1")

	if embed == nil {
		t.Fatal("expected non-nil embed")
	}
	if !strings.Contains(embed.Description, "テストユーザー") {
		t.Errorf("embed description missing username: %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "はじめまして") {
		t.Errorf("embed description missing intro content: %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "🎮 アクティビティ") {
		t.Errorf("embed description missing role category: %q", embed.Description)
	}
	if embed.Thumbnail == nil || embed.Thumbnail.URL != "https://example.com/avatar.png" {
		t.Errorf("unexpected thumbnail: %+v", embed.Thumbnail)
	}
}

func TestCreateIntroductionEmbed_NoRoles(t *testing.T) {
	b := &Bot{
		RolesConfig: &config.RolesConfig{},
	}

	intro := &database.Introduction{UserID: "123", ChannelID: "456", MessageID: "789"}

	embed := b.createIntroductionEmbed("ユーザー", "", "VC", "本文", intro, map[string][]string{}, "guild-1")

	if embed.Thumbnail != nil {
		t.Errorf("expected no thumbnail when avatarURL is empty, got %+v", embed.Thumbnail)
	}
	if !strings.Contains(embed.Description, "本文") {
		t.Errorf("embed description missing intro content: %q", embed.Description)
	}
}
