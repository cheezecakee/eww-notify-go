package state

import (
	"time"
)

type Notification struct {
	Id         uint32
	Timeout    uint32
	Timestamp  time.Time
	NotifyType *string
	AppName    string
	AppIcon    string
	Summary    string
	Body       string
	Hints      map[string]any
	Actions    []string
	Widget     *string
}

type LifeTime string

const (
	Timeout    LifeTime = "timeout"
	Persistent LifeTime = "persistent"
)

type NotificationCloseReason int

const (
	Expired NotificationCloseReason = iota
	Dismiss
	CloseNotification
	Other
)
