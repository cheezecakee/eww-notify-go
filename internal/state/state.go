package state

import (
	"github.com/cheezecakee/eww-notify-go/internal/config"
	"github.com/cheezecakee/eww-notify-go/internal/util/dbus"
)

type NotificationState struct {
	Notifications []Notification
	Config        config.Config
	IdCounter     uint32
	Client        Client
}
