package main

import (
	"encoding/json"
	"fmt"
	"os"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func LoadConfig() bool {
	file, err := os.ReadFile("config.json")
	if err != nil {
		return false
	}
	json.Unmarshal(file, &GlobalConfig)

	themePath := "themes/" + GlobalConfig.Theme
	if GlobalConfig.Theme == "" {
		themePath = "themes/default"
	}

	themeFile, err := os.ReadFile(themePath + "/config.json")
	if err == nil {
		json.Unmarshal(themeFile, &CurrentThemeConfig)
	} else {
		if err == nil {
			json.Unmarshal(themeFile, &CurrentThemeConfig)
		} else {
			// 读取失败时的默认值，注意结构体字段已变
			CurrentThemeConfig = ThemeConfig{
				Name:        "Default",
				Author:      "Admin",
				Description: "Fallback theme",
				// 初始化一个空的 map 防止空指针报错
				Config: make(map[string]string),
			}
		}
	}

	return GlobalConfig.Installed
}

func ConnectDB() error {
	var dialector gorm.Dialector

	switch GlobalConfig.DBType {
	case "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			GlobalConfig.DBUser, GlobalConfig.DBPass, GlobalConfig.DBHost, GlobalConfig.DBPort, GlobalConfig.DBName)
		dialector = mysql.Open(dsn)
	case "postgres":
		dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
			GlobalConfig.DBHost, GlobalConfig.DBUser, GlobalConfig.DBPass, GlobalConfig.DBName, GlobalConfig.DBPort)
		dialector = postgres.Open(dsn)
	default:
		dbFile := GlobalConfig.DBName
		if dbFile == "" {
			dbFile = "gopress.db"
		}
		dialector = sqlite.Open(dbFile)
	}

	var err error
	DB, err = gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Error),
	})

	if err != nil {
		return err
	}

	err = DB.AutoMigrate(&Post{}, &User{}, &Option{})
	if err != nil {
		return err
	}

	return nil
}

func SaveConfig(cfg Config) error {
	cfg.Installed = true
	if cfg.Theme == "" {
		cfg.Theme = "default"
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile("config.json", data, 0644)
}
