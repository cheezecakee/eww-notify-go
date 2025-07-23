package config

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
	Config Config
}

type Config struct {
	EwwDefaultNotificationKey *string     `toml:"eww-default-notification-key"`
	EwwWindow                 *string     `toml:"eww-window"`
	MaxNotifications          uint32      `toml:"max-notifications"`
	NotificationOrientation   Orientation `toml:"notification-orientation"`
	Timeout                   Timeout     `toml:"timeout"`
}

type Orientation int

const (
	Horizontal Orientation = iota
	Vertical
)

type TimeoutByUrgency struct {
	Low      uint32 `toml:"low"`
	Normal   uint32 `toml:"normal"`
	Critical uint32 `toml:"critical"`
}

type Timeout struct {
	ByUrgency TimeoutByUrgency
}
