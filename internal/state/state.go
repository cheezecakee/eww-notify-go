package state

import (
	"slices"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/cheezecakee/eww-notify-go/internal/config"
)

type NotificationState struct {
	mu            sync.RWMutex
	Notifications []Notification
	Config        config.Config
	IdCounter     uint32
	DbusConn      *dbus.Conn
}

func NewNotificationState(cfg config.Config, conn *dbus.Conn) *NotificationState {
	return &NotificationState{
		Notifications: make([]Notification, 0),
		Config:        cfg,
		IdCounter:     0,
		DbusConn:      conn,
	}
}

func (ns *NotificationState) NextId() uint32 {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	ns.IdCounter++
	return ns.IdCounter
}

func (ns *NotificationState) AddNotification(notification Notification) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	for i, existing := range ns.Notifications {
		if existing.Id == notification.Id {
			ns.Notifications[i] = notification
			return
		}
	}

	maxNotifications := int(ns.Config.MaxNotifications)
	if maxNotifications > 0 && len(ns.Notifications) >= maxNotifications {
		oldestIdx := ns.findOldestNoticationIndex()
		if oldestIdx >= 0 {
			ns.removeNotificationByIndex(oldestIdx)
		}
	}

	ns.Notifications = append(ns.Notifications, notification)
}

func (ns *NotificationState) RemoveNotification(id uint32) bool {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	for i, notification := range ns.Notifications {
		if notification.Id == id {
			ns.removeNotificationByIndex(i)
			return true
		}
	}
	return false
}

func (ns *NotificationState) GetNotifications() []Notification {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	notifications := make([]Notification, len(ns.Notifications))
	copy(notifications, ns.Notifications)
	return notifications
}

func (ns *NotificationState) GetNotificationsById(id uint32) (Notification, bool) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	for _, notification := range ns.Notifications {
		if notification.Id == id {
			return notification, true
		}
	}
	return Notification{}, false
}

func (ns *NotificationState) UpdateConfig(newConfig config.Config) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	ns.Config = newConfig
}

func (ns *NotificationState) GetConfig() config.Config {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	return ns.Config
}

// Helper
// removeNotificationByIndex removes a notification at the given index
// Caller must hold the lock
func (ns *NotificationState) removeNotificationByIndex(index int) {
	if index >= 0 && index < len(ns.Notifications) {
		ns.Notifications = slices.Delete(ns.Notifications, index, index+1)
	}
}

// findOldestNotificationIndex finds the index of the oldest notification
// Returns -1 if no notifications exist
// Caller must hold the lock
func (ns *NotificationState) findOldestNoticationIndex() int {
	if len(ns.Notifications) == 0 {
		return -1
	}
	oldestIdx := 0
	oldestTime := ns.Notifications[0].Timestamp

	for i, notification := range ns.Notifications[1:] {
		if notification.Timestamp.Before(oldestTime) {
			oldestIdx = i + 1
			oldestTime = notification.Timestamp
		}
	}

	return oldestIdx
}

// CleanupExpiredNotifications removes all expired notifications
func (ns *NotificationState) CleanupExpiredNotifications() []uint32 {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	var expiredIds []uint32
	var remainingNotifications []Notification

	for _, notification := range ns.Notifications {
		if notification.IsExpired() {
			expiredIds = append(expiredIds, notification.Id)
		} else {
			remainingNotifications = append(remainingNotifications, notification)
		}
	}

	ns.Notifications = remainingNotifications
	return expiredIds
}
