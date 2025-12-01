package main

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	Url string `mapstructure:"url"`
}

var (
	Global *Config
)

func Init(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "configs"
	}
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configPath)
	viper.AddConfigPath(".")
	viper.AddConfigPath("./configs")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Printf("Config file not found, using defaults and environment variables")
		} else {
			return nil, fmt.Errorf("failed to read config file: %v", err)
		}
	}
	// 设置默认值
	setDefaults()

	// 解析配置到结构体
	if err := viper.Unmarshal(&Global); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	return Global, nil
}

func setDefaults() {
	viper.SetDefault("url", "unknow")
}
