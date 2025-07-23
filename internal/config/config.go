package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

var DefaultConfig = Config{
	EwwDefaultNotificationKey: nil,
	EwwWindow:                 nil,
	MaxNotifications:          0,
	NotificationOrientation:   Vertical,
	Timeout: Timeout{
		ByUrgency: TimeoutByUrgency{
			Low:      5,
			Normal:   10,
			Critical: 0,
		},
	},
}

type ConfigFile struct {
	Config Config `toml:"config"`
}

type Config struct {
	EwwDefaultNotificationKey *string     `toml:"eww-default-notification-key"`
	EwwWindow                 *string     `toml:"eww-window"`
	MaxNotifications          uint32      `toml:"max-notifications"`
	NotificationOrientation   Orientation `toml:"notification-orientation"`
	Timeout                   Timeout     `toml:"timeout"`
}

type Orientation string

const (
	Horizontal Orientation = "h"
	Vertical   Orientation = "v"
)

type TimeoutByUrgency struct {
	Low      uint32 `toml:"low"`
	Normal   uint32 `toml:"normal"`
	Critical uint32 `toml:"critical"`
}

type Timeout struct {
	ByUrgency TimeoutByUrgency `toml:"urgency"`
}

func (o *Orientation) UnmarshalText(text []byte) error {
	switch string(text) {
	case "h":
		*o = Horizontal
	case "v":
		*o = Vertical
	default:
		*o = Vertical
	}
	return nil
}

func GetConfigDir() (string, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		configDir = filepath.Join(homeDir, ".config")
	}
	return configDir, nil
}

func LoadConfig() (*Config, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	configFilePath := filepath.Join(configDir, "end", "config.toml")

	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		fmt.Printf("Could not find config file! Should be at %s\n", configFilePath)
		return nil, nil
	}

	configData, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var configFile ConfigFile

	configFile.Config = DefaultConfig

	if err := toml.Unmarshal(configData, &configFile); err != nil {
		fmt.Println("There were errors in your config.toml!")
		fmt.Printf("Error: %v\n", err)
		return nil, nil
	}

	mergedConfig := mergeWithDefaults(configFile.Config)
	return &mergedConfig, nil
}

func mergeWithDefaults(cfg Config) Config {
	result := cfg

	if result.Timeout.ByUrgency.Low == 0 &&
		result.Timeout.ByUrgency.Normal == 0 &&
		result.Timeout.ByUrgency.Critical == 0 {
		result.Timeout = DefaultConfig.Timeout
	} else {
		if result.Timeout.ByUrgency.Low == 0 {
			result.Timeout.ByUrgency.Low = DefaultConfig.Timeout.ByUrgency.Low
		}
		if result.Timeout.ByUrgency.Normal == 0 {
			result.Timeout.ByUrgency.Normal = DefaultConfig.Timeout.ByUrgency.Normal
		}
	}

	if result.NotificationOrientation == "" {
		result.NotificationOrientation = DefaultConfig.NotificationOrientation
	}

	return result
}
