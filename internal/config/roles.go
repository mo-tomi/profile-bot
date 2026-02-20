package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// RoleConfig はロール設定を表す構造体
type RoleConfig struct {
	Name  string `yaml:"name"`
	Emoji string `yaml:"emoji"`
}

// CategoryConfig はロールカテゴリ設定を表す構造体
type CategoryConfig struct {
	Name        string       `yaml:"name"`
	DisplayName string       `yaml:"display_name"`
	EmojiPrefix bool         `yaml:"emoji_prefix"`
	Roles       []RoleConfig `yaml:"roles"`
}

// SpecialFilter は特殊フィルター設定を表す構造体
type SpecialFilter struct {
	Pattern string `yaml:"pattern"`
	Action  string `yaml:"action"`
}

// RolesConfig はロール設定全体を表す構造体
type RolesConfig struct {
	RoleCategories   []CategoryConfig `yaml:"role_categories"`
	ExcludedRoles    []string         `yaml:"excluded_roles"`
	ExcludedSuffixes []string         `yaml:"excluded_suffixes"`
	SpecialFilters   []SpecialFilter  `yaml:"special_filters"`
}

// LoadRolesConfig はYAMLファイルからロール設定を読み込む
func LoadRolesConfig(filepath string) (*RolesConfig, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read roles config file: %w", err)
	}

	var config RolesConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse roles config YAML: %w", err)
	}

	return &config, nil
}

// IsExcludedRole は指定されたロールが除外対象かチェックする
func (rc *RolesConfig) IsExcludedRole(roleName string, managed bool) bool {
	// bot管理フラグがtrueの場合は除外
	if managed {
		return true
	}

	// 除外リストに含まれているかチェック
	for _, excluded := range rc.ExcludedRoles {
		if roleName == excluded {
			return true
		}
	}

	// サフィックスチェック
	for _, suffix := range rc.ExcludedSuffixes {
		if strings.HasSuffix(roleName, suffix) {
			return true
		}
	}

	return false
}

// ApplySpecialFilters は特殊フィルターを適用する
func (rc *RolesConfig) ApplySpecialFilters(roleName string) string {
	result := roleName
	for _, filter := range rc.SpecialFilters {
		if filter.Action == "remove" {
			result = strings.ReplaceAll(result, filter.Pattern, "")
		}
	}
	return strings.TrimSpace(result)
}

// GetCategoryAndEmoji は指定されたロール名に対応するカテゴリと絵文字を取得する
func (rc *RolesConfig) GetCategoryAndEmoji(roleName string) (string, string, string) {
	// 特殊フィルター適用
	filteredName := rc.ApplySpecialFilters(roleName)

	// カテゴリから検索
	for _, category := range rc.RoleCategories {
		for _, role := range category.Roles {
			if role.Name == filteredName {
				return category.Name, category.DisplayName, role.Emoji
			}
		}
	}

	return "", "", ""
}

// CategorizedRole はカテゴリ別に整理されたロール情報
type CategorizedRole struct {
	Name  string
	Emoji string
}

// CategoryWithRoles はカテゴリとそのロールリスト
type CategoryWithRoles struct {
	Name        string
	DisplayName string
	Roles       []CategorizedRole
}

// CategorizeRoles はロール名のリストをカテゴリ別に整理する
func (rc *RolesConfig) CategorizeRoles(roleNames []string) []CategoryWithRoles {
	// カテゴリごとのロールマップ
	categoryMap := make(map[string]*CategoryWithRoles)

	// カテゴリを初期化
	for _, category := range rc.RoleCategories {
		categoryMap[category.Name] = &CategoryWithRoles{
			Name:        category.Name,
			DisplayName: category.DisplayName,
			Roles:       []CategorizedRole{},
		}
	}

	// ロールを分類
	for _, roleName := range roleNames {
		categoryName, displayName, emoji := rc.GetCategoryAndEmoji(roleName)
		if categoryName != "" {
			if cat, exists := categoryMap[categoryName]; exists {
				cat.Roles = append(cat.Roles, CategorizedRole{
					Name:  rc.ApplySpecialFilters(roleName),
					Emoji: emoji,
				})
				cat.DisplayName = displayName
			}
		}
	}

	// カテゴリリストを作成（元の順序を維持）
	var result []CategoryWithRoles
	for _, category := range rc.RoleCategories {
		if cat, exists := categoryMap[category.Name]; exists && len(cat.Roles) > 0 {
			result = append(result, *cat)
		}
	}

	return result
}
