package daemon

import (
	"context"
	"fmt"
	"log"
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
	timeoutTasks map[uint32]context.CancelFunc
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

	dbusServer.daemon = daemon

	return daemon, nil
}

func (d *Daemon) Start() error {
	if err := d.dbusServer.SetupDBusService(); err != nil {
		return fmt.Errorf("failed to setup DBus service: %w", err)
	}

	fmt.Println("Notification daemon started")
	go d.cleanupLoop()
	return nil
}

func (d *Daemon) Stop() error {
	fmt.Println("Stopping notification daemon...")

	for _, cancel := range d.timeoutTasks {
		cancel()
	}

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
	log.Printf("DEBUG: HandleNotification called - App: %s, Summary: %s, Body: %s", appName, summary, body)
	log.Printf("DEBUG: Hints: %+v", hints)

	var notificationId uint32

	if replaceId != 0 {
		notificationId = replaceId
	} else {
		notificationId = d.state.NextId()
	}

	// Determine timeout from hints and config
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
		NotifyType: nil,
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
		// Even if no window is configured, we should clear the variable
		return d.setEwwValue("end-notifications", "")
	}

	// Build widget string
	widgetString := d.buildWidgetString(notifications)
	log.Printf("DEBUG: Built widget string: %s", widgetString)

	if err := d.setEwwValue("end-notifications", widgetString); err != nil {
		return fmt.Errorf("failed to set eww value: %w", err)
	}

	if d.config.EwwWindow != nil {
		return d.openEwwWindow(*d.config.EwwWindow)
	}

	return nil
}

func (d *Daemon) buildWidgetString(notifications []state.Notification) string {
	var widgets []string

	for _, notification := range notifications {
		widget := d.buildNotificationWidget(notification)
		widgets = append(widgets, widget)
	}

	isVertical := d.config.NotificationOrientation == config.Vertical
	result := d.buildWidgetWrapper(isVertical, strings.Join(widgets, ""))

	fmt.Printf("=== Final Widget String ===\n%s\n=== End ===\n", result)

	return result
}

func (d *Daemon) buildNotificationWidget(notification state.Notification) string {
	summary := d.escapeString(notification.Summary)
	body := d.escapeString(notification.Body)
	appName := d.escapeString(notification.AppName)
	appIcon := d.escapeString(notification.AppIcon)
	hints := d.buildHintsString(notification.Hints)

	// Build Eww-style object
	notificationObject := fmt.Sprintf(`{ id: %d, summary: "%s", body: "%s", app_name: "%s", app_icon: "%s", hints: { %s } }`,
		notification.Id,
		summary,
		body,
		appName,
		appIcon,
		hints,
	)

	// Widget selector - directly return the appropriate widget call
	if notifyType, exists := notification.Hints["type"]; exists {
		if typeStr, ok := notifyType.(string); ok && typeStr == "battery" {
			return fmt.Sprintf("(battery-notification :notification %s)", notificationObject)
		}
	}

	// Check if a custom widget is specified
	if notification.Widget != nil {
		return fmt.Sprintf("(%s :notification %s)", *notification.Widget, notificationObject)
	}

	// Default to base-notification
	return fmt.Sprintf("(base-notification :notification %s)", notificationObject)
}

func (d *Daemon) buildWidgetWrapper(isVertical bool, widgets string) string {
	orientation := "vertical"
	if !isVertical {
		orientation = "horizontal"
	}
	return fmt.Sprintf("(box :space-evenly false :orientation \"%s\" %s)", orientation, widgets)
}

func (d *Daemon) buildHintsString(hints map[string]any) string {
	var hintPairs []string

	for key, value := range hints {
		var valueStr string
		switch v := value.(type) {
		case string:
			valueStr = fmt.Sprintf(`"%s"`, d.escapeString(v))
		case bool:
			valueStr = fmt.Sprintf(`%t`, v)
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			valueStr = fmt.Sprintf(`%v`, v)
		case float32, float64:
			valueStr = fmt.Sprintf(`%v`, v)
		default:
			valueStr = fmt.Sprintf(`"%v"`, d.escapeString(fmt.Sprintf("%v", v)))
		}

		hintPairs = append(hintPairs, fmt.Sprintf(`%s: %s`, key, valueStr))
	}

	return strings.Join(hintPairs, ", ")
}

func (d *Daemon) escapeString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func (d *Daemon) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			expiredIds := d.state.CleanupExpiredNotifications()
			for _, id := range expiredIds {
				if cancel, exists := d.timeoutTasks[id]; exists {
					cancel()
					delete(d.timeoutTasks, id)
				}
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
	cmd := exec.Command("eww", "update", fmt.Sprintf("%s=%s", variable, value))
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

func (d *Daemon) openEwwWindow(window string) error {
	cmd := exec.Command("eww", "open", window)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

func (d *Daemon) closeEwwWindow(window string) error {
	cmd := exec.Command("eww", "close", window)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}
