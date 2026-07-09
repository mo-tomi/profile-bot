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

func TestVCMessageKey(t *testing.T) {
	got := vcMessageKey("guild-1", "user-1")
	want := "guild-1:user-1"
	if got != want {
		t.Errorf("vcMessageKey() = %q, want %q", got, want)
	}
}

func TestTrackVCMessage(t *testing.T) {
	t.Run("records message when DeleteOnLeave is enabled", func(t *testing.T) {
		b := &Bot{
			Config:     &config.Config{DeleteOnLeave: true},
			vcMessages: make(map[string][]sentMessage),
		}

		b.trackVCMessage("guild-1", "user-1", "channel-1", "message-1")
		b.trackVCMessage("guild-1", "user-1", "channel-2", "message-2")

		key := vcMessageKey("guild-1", "user-1")
		got := b.vcMessages[key]
		if len(got) != 2 {
			t.Fatalf("expected 2 tracked messages, got %d: %+v", len(got), got)
		}
		if got[0] != (sentMessage{ChannelID: "channel-1", MessageID: "message-1"}) {
			t.Errorf("unexpected first tracked message: %+v", got[0])
		}
		if got[1] != (sentMessage{ChannelID: "channel-2", MessageID: "message-2"}) {
			t.Errorf("unexpected second tracked message: %+v", got[1])
		}
	})

	t.Run("does nothing when DeleteOnLeave is disabled", func(t *testing.T) {
		b := &Bot{
			Config:     &config.Config{DeleteOnLeave: false},
			vcMessages: make(map[string][]sentMessage),
		}

		b.trackVCMessage("guild-1", "user-1", "channel-1", "message-1")

		if len(b.vcMessages) != 0 {
			t.Errorf("expected no tracked messages, got %+v", b.vcMessages)
		}
	})
}
