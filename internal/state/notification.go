package state

import (
	"time"
)

type Notification struct {
	Id         uint32         `toml:"id"`
	Timeout    uint32         `toml:"timeout"`
	Timestamp  time.Time      `toml:"timestamp"`
	NotifyType *string        `toml:"notify_type, omitempty"`
	AppName    string         `toml:"app_name"`
	AppIcon    string         `toml:"app_icon"`
	Summary    string         `toml:"summary"`
	Body       string         `toml:"body"`
	Hints      map[string]any `toml:"hints"`
	Actions    []string       `toml:"actions"`
	Widget     *string        `toml:"widget, omitempty"`
}

type LifetimeType string

const (
	Timeout    LifetimeType = "timeout"
	Persistent LifetimeType = "persistent"
)

type Lifetime struct {
	Type  LifetimeType
	Value uint32
}

type NotificationCloseReason int

const (
	Expired NotificationCloseReason = iota
	Dismiss
	CloseNotification
	Other
)

func (r NotificationCloseReason) String() string {
	switch r {
	case Expired:
		return "expired"
	case Dismiss:
		return "dismiss"
	case CloseNotification:
		return "close_notification"
	case Other:
		return "other"
	default:
		return "unknown"
	}
}

func (n *Notification) GetLifetime() Lifetime {
	if n.Timeout != 0 {
		timeoutAt := uint32(n.Timestamp.Unix()) + n.Timeout
		return Lifetime{
			Type:  Timeout,
			Value: timeoutAt,
		}
	}

	return Lifetime{
		Type:  Persistent,
		Value: uint32(n.Timestamp.Unix()),
	}
}

func (n *Notification) IsExpired() bool {
	if n.Timeout == 0 {
		return false
	}
	expiresAt := n.Timestamp.Add(time.Duration(n.Timeout) * time.Second)
	return time.Now().After(expiresAt)
}
