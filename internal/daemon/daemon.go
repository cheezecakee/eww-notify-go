package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cheezecakee/eww-notify-go/internal/config"
	"github.com/cheezecakee/eww-notify-go/internal/state"
	"github.com/cheezecakee/eww-notify-go/internal/util/dbus"
)

type Daemon struct {
	config       config.Config
	state        *state.NotificationState
	dbusServer   *NotificationServer
	ctx          context.Context
	cancel       context.CancelFunc
	timeoutTasks map[uint32]context.CancelFunc // tracks timeout goroutines
}

func NewDaemon(cfg config.Config) (*Daemon, error) {
	notificationState := state.NewNotificationState(cfg, nil)

	dbusServer, err := NewNotificationServer(notificationState)
	if err != nil {
		return nil, fmt.Errorf("failed to create DBus server: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	daemon := &Daemon{
		config:       cfg,
		state:        notificationState,
		dbusServer:   dbusServer,
		ctx:          ctx,
		cancel:       cancel,
		timeoutTasks: make(map[uint32]context.CancelFunc),
	}

	return daemon, nil
}

func (d *Daemon) Start() error {
	if err := d.dbusServer.SetupDBusService(); err != nil {
		return fmt.Errorf("failed to setup DBus service: %w", err)
	}

	fmt.Println("Notification daemon started")

	// Start cleanup routine for expired notifications
	go d.cleanupLoop()

	return nil
}

func (d *Daemon) Stop() error {
	fmt.Println("Stopping notification daemon...")

	for _, cancel := range d.timeoutTasks {
		cancel()
	}

	// Cancel main context
	d.cancel()

	if err := d.dbusServer.Close(); err != nil {
		return fmt.Errorf("failed to close DBus server: %w", err)
	}

	return nil
}

func (d *Daemon) HandleNotification(
	appName string,
	replaceId uint32,
	appIcon string,
	summary string,
	body string,
	actions []string,
	hints map[string]any,
	expireTimeout int32,
) (uint32, error) {
	var notificationId uint32

	if replaceId == 0 {
		notificationId = replaceId
	} else {
		notificationId = d.state.NextId()
	}

	// Determin timeout from hints and config
	urgency := dbus.GetUrgency(hints)
	urgencyKey := dbus.ConfigKeyUrgency(urgency)

	var timeout uint32
	switch urgencyKey {
	case "low":
		timeout = d.config.Timeout.ByUrgency.Low
	case "critical":
		timeout = d.config.Timeout.ByUrgency.Critical
	default: // "normal"
		timeout = d.config.Timeout.ByUrgency.Normal
	}

	// Create notification
	notification := state.Notification{
		Id:         notificationId,
		Timeout:    timeout,
		Timestamp:  time.Now(),
		NotifyType: nil, // TODO: extract from hints
		AppName:    appName,
		AppIcon:    appIcon,
		Summary:    summary,
		Body:       body,
		Hints:      hints,
		Actions:    actions,
		Widget:     d.config.EwwDefaultNotificationKey,
	}

	d.state.AddNotification(notification)

	if timeout > 0 {
		d.scheduleTimeout(notificationId, time.Duration(timeout)*time.Second)
	}

	if err := d.updateDisplay(); err != nil {
		return notificationId, fmt.Errorf("failed to update display: %w", err)
	}

	return notificationId, nil
}

func (d *Daemon) RemoveNotification(id uint32) error {
	if cancel, exists := d.timeoutTasks[id]; exists {
		cancel()
		delete(d.timeoutTasks, id)
	}

	if !d.state.RemoveNotification(id) {
		return fmt.Errorf("notification with ID %d not found", id)
	}

	return d.updateDisplay()
}

func (d *Daemon) InvokeAction(id uint32, actionKey string) error {
	if _, exists := d.state.GetNotificationsById(id); !exists {
		return fmt.Errorf("notification with ID %d not found", id)
	}

	return d.dbusServer.EmitActionInvoked(id, actionKey)
}

func (d *Daemon) scheduleTimeout(id uint32, duration time.Duration) {
	if cancel, exists := d.timeoutTasks[id]; exists {
		cancel()
	}

	ctx, cancel := context.WithCancel(d.ctx)
	d.timeoutTasks[id] = cancel

	go func() {
		select {
		case <-time.After(duration):
			d.state.RemoveNotification(id)
			d.dbusServer.EmitNotificationClosed(id, state.Expired)
			d.updateDisplay()
			delete(d.timeoutTasks, id)
		case <-ctx.Done():
			return
		}
	}()
}

func (d *Daemon) updateDisplay() error {
	notifications := d.state.GetNotifications()

	if len(notifications) == 0 {
		if d.config.EwwWindow != nil {
			return d.closeEwwWindow(*d.config.EwwWindow)
		}
		return nil
	}

	// Build widget string
	widgetString := d.buildWidgetString(notifications)

	if err := d.setEwwValue("end-notifications", widgetString); err != nil {
		return fmt.Errorf("failed to set eww value: %w", err)
	}

	if d.config.EwwWindow != nil {
		return d.openEwwWindow(*d.config.EwwWindow)
	}

	return nil
}

// buildWidgetString creates the eww widget string for notifications
func (d *Daemon) buildWidgetString(notifications []state.Notification) string {
	var widgets []string

	for _, notification := range notifications {
		widget := d.buildNotificationWidget(notification)
		widgets = append(widgets, widget)
	}

	// Build wrapper based on orientation
	isVertical := d.config.NotificationOrientation == config.Vertical
	return d.buildWidgetWrapper(isVertical, strings.Join(widgets, ""))
}

// buildNotificationWidget creates a widget for a single notification
func (d *Daemon) buildNotificationWidget(notification state.Notification) string {
	if notification.Widget != nil {
		// Use custom widget
		// TODO: Build JSON data for custom widget
		return fmt.Sprintf("(%s :notification {})", *notification.Widget)
	}

	// Use default label widget
	return fmt.Sprintf(
		"(label :text \"%s\" :xalign 1 :halign \"end\" :css \"label { padding-right: 12px; padding-top: 6px }\")",
		d.escapeString(notification.Summary),
	)
}

// buildWidgetWrapper wraps widgets in appropriate container
func (d *Daemon) buildWidgetWrapper(isVertical bool, widgets string) string {
	orientation := "vertical"
	if !isVertical {
		orientation = "horizontal"
	}
	return fmt.Sprintf("(box :space-evenly false :orientation \"%s\" %s)", orientation, widgets)
}

// escapeString escapes strings for eww
func (d *Daemon) escapeString(s string) string {
	// Basic escaping - you might need more comprehensive escaping
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// cleanupLoop periodically cleans up expired notifications
func (d *Daemon) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second) // check every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			expiredIds := d.state.CleanupExpiredNotifications()
			for _, id := range expiredIds {
				// Cancel timeout task
				if cancel, exists := d.timeoutTasks[id]; exists {
					cancel()
					delete(d.timeoutTasks, id)
				}
				// Emit signal
				d.dbusServer.EmitNotificationClosed(id, state.Expired)
			}
			if len(expiredIds) > 0 {
				d.updateDisplay()
			}
		case <-d.ctx.Done():
			return
		}
	}
}

// Eww command helpers
func (d *Daemon) setEwwValue(variable, value string) error {
	cmd := exec.Command("eww", "update", fmt.Sprintf("%s='%s'", variable, value))
	return cmd.Run()
}

func (d *Daemon) openEwwWindow(window string) error {
	cmd := exec.Command("eww", "open", window)
	return cmd.Run()
}

func (d *Daemon) closeEwwWindow(window string) error {
	cmd := exec.Command("eww", "close", window)
	return cmd.Run()
}
