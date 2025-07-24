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
	log.Println("DEBUG: Starting daemon")
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
		log.Printf("DEBUG: Using replace ID: %d", replaceId)
	} else {
		notificationId = d.state.NextId()
		log.Printf("DEBUG: Generated new ID: %d", notificationId)
	}

	// Determine timeout from hints and config
	urgency := dbus.GetUrgency(hints)
	urgencyKey := dbus.ConfigKeyUrgency(urgency)
	log.Printf("DEBUG: Urgency: %d (%s)", urgency, urgencyKey)

	var timeout uint32
	switch urgencyKey {
	case "low":
		timeout = d.config.Timeout.ByUrgency.Low
	case "critical":
		timeout = d.config.Timeout.ByUrgency.Critical
	default: // "normal"
		timeout = d.config.Timeout.ByUrgency.Normal
	}
	log.Printf("DEBUG: Timeout: %d seconds", timeout)

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

	log.Printf("DEBUG: Created notification: %+v", notification)

	d.state.AddNotification(notification)
	log.Printf("DEBUG: Added notification to state. Total notifications: %d", len(d.state.GetNotifications()))

	if timeout > 0 {
		d.scheduleTimeout(notificationId, time.Duration(timeout)*time.Second)
		log.Printf("DEBUG: Scheduled timeout for %d seconds", timeout)
	}

	if err := d.updateDisplay(); err != nil {
		log.Printf("ERROR: Failed to update display: %v", err)
		return notificationId, fmt.Errorf("failed to update display: %w", err)
	}

	log.Printf("DEBUG: Successfully handled notification %d", notificationId)
	return notificationId, nil
}

func (d *Daemon) RemoveNotification(id uint32) error {
	log.Printf("DEBUG: Removing notification %d", id)

	if cancel, exists := d.timeoutTasks[id]; exists {
		cancel()
		delete(d.timeoutTasks, id)
		log.Printf("DEBUG: Cancelled timeout task for notification %d", id)
	}

	if !d.state.RemoveNotification(id) {
		return fmt.Errorf("notification with ID %d not found", id)
	}

	log.Printf("DEBUG: Notification %d removed from state", id)
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
			log.Printf("DEBUG: Timeout reached for notification %d", id)
			d.state.RemoveNotification(id)
			d.dbusServer.EmitNotificationClosed(id, state.Expired)
			d.updateDisplay()
			delete(d.timeoutTasks, id)
		case <-ctx.Done():
			log.Printf("DEBUG: Timeout cancelled for notification %d", id)
			return
		}
	}()
}

func (d *Daemon) updateDisplay() error {
	log.Println("DEBUG: Updating display")
	notifications := d.state.GetNotifications()
	log.Printf("DEBUG: Current notifications count: %d", len(notifications))

	if len(notifications) == 0 {
		log.Println("DEBUG: No notifications, closing window if configured")
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
		log.Printf("ERROR: Failed to set eww value: %v", err)
		return fmt.Errorf("failed to set eww value: %w", err)
	}

	if d.config.EwwWindow != nil {
		log.Printf("DEBUG: Opening eww window: %s", *d.config.EwwWindow)
		return d.openEwwWindow(*d.config.EwwWindow)
	}

	log.Println("DEBUG: No eww window configured")
	return nil
}

func (d *Daemon) buildWidgetString(notifications []state.Notification) string {
	log.Printf("DEBUG: Building widget string for %d notifications", len(notifications))
	var widgets []string

	for i, notification := range notifications {
		log.Printf("DEBUG: Building widget for notification %d: %+v", i, notification)
		widget := d.buildNotificationWidget(notification)
		log.Printf("DEBUG: Built widget %d: %s", i, widget)
		widgets = append(widgets, widget)
	}

	isVertical := d.config.NotificationOrientation == config.Vertical
	result := d.buildWidgetWrapper(isVertical, strings.Join(widgets, ""))

	fmt.Printf("=== Final Widget String ===\n%s\n=== End ===\n", result)

	log.Printf("DEBUG: Final widget string: %s", result)
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

	// Widget selector
	if notifyType, exists := notification.Hints["type"]; exists {
		if typeStr, ok := notifyType.(string); ok && typeStr == "battery" {
			return fmt.Sprintf("(battery-notification :notification %s)", notificationObject)
		}
	}

	if notification.Widget != nil {
		return fmt.Sprintf("(%s :notification %s)", *notification.Widget, notificationObject)
	}

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
	log.Printf("DEBUG: Setting eww variable %s to: %s", variable, value)
	cmd := exec.Command("eww", "update", fmt.Sprintf("%s='%s'", variable, value))
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("ERROR: eww update failed: %v, output: %s", err, string(output))
		return err
	}
	log.Printf("DEBUG: eww update successful")
	return nil
}

func (d *Daemon) openEwwWindow(window string) error {
	log.Printf("DEBUG: Opening eww window: %s", window)
	cmd := exec.Command("eww", "open", window)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("ERROR: eww open failed: %v, output: %s", err, string(output))
		return err
	}
	log.Printf("DEBUG: eww window opened successfully")
	return nil
}

func (d *Daemon) closeEwwWindow(window string) error {
	log.Printf("DEBUG: Closing eww window: %s", window)
	cmd := exec.Command("eww", "close", window)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("ERROR: eww close failed: %v, output: %s", err, string(output))
		return err
	}
	log.Printf("DEBUG: eww window closed successfully")
	return nil
}
