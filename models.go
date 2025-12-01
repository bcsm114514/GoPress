package main

import (
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Config 系统配置
type Config struct {
	Installed bool   `json:"installed"`
	DBType    string `json:"db_type"`
	DBHost    string `json:"db_host"`
	DBPort    string `json:"db_port"`
	DBUser    string `json:"db_user"`
	DBPass    string `json:"db_password"`
	DBName    string `json:"db_name"`
	Theme     string `json:"theme"`
}

// ThemeSetting 定义单个配置项
type ThemeSetting struct {
	Key         string   `json:"key"`         // 字段名 (如 site_name)
	Label       string   `json:"label"`       // 显示名 (如 站点名称)
	Type        string   `json:"type"`        // 类型: text, textarea, radio, select, checkbox
	Value       string   `json:"value"`       // 当前值
	Default     string   `json:"default"`     // 默认值
	Options     []string `json:"options"`     // 选项 (用于 radio/select)，格式如 ["开启", "关闭"]
	Description string   `json:"description"` // 底部说明文字
}

// ThemeConfig 主题配置
type ThemeConfig struct {
	// 元数据
	Name        string `json:"name"`
	Author      string `json:"author"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Screenshot  string `json:"screenshot"`

	// 设置定义列表 (用于生成后台表单)
	Settings []ThemeSetting `json:"settings"`

	// === 扁平化配置 (用于前台模板调用) ===
	// 这是一个辅助字段，不存入 JSON，只在内存中使用
	Config map[string]string `json:"-"`
}

var (
	GlobalConfig       Config
	CurrentThemeConfig ThemeConfig
)

// Option 系统设置表 (Key-Value)
type Option struct {
	Name  string `gorm:"primaryKey"` // e.g. "site_title", "site_description"
	Value string `gorm:"type:text"`
}

// Post 文章/页面模型
type Post struct {
	gorm.Model
	Title   string
	Slug    string `gorm:"uniqueIndex;size:200"`
	Content string `gorm:"type:text"`
	Status  string
	Type    string `gorm:"default:'post';index"` // 'post' or 'page'
}

// User 用户模型
type User struct {
	gorm.Model
	Username string `gorm:"uniqueIndex;size:100"`
	Password string
	Nickname string
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
