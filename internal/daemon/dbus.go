package daemon

import (
	"errors"
	"fmt"
	"log"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/cheezecakee/eww-notify-go/internal/state"
)

const (
	NotificationServiceName = "org.freedesktop.Notifications"
	NotificationObjectPath  = "/org/freedesktop/Notifications"
	NotificationInterface   = "org.freedesktop.Notifications"
)

type NotificationServer struct {
	conn   *dbus.Conn
	state  *state.NotificationState
	daemon *Daemon // Add reference to daemon
}

func NewNotificationServer(notificationState *state.NotificationState) (*NotificationServer, error) {
	log.Println("DEBUG: Creating NotificationServer")
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to session bus: %w", err)
	}

	server := &NotificationServer{
		conn:  conn,
		state: notificationState,
	}

	notificationState.DbusConn = conn
	log.Println("DEBUG: NotificationServer created successfully")

	return server, nil
}

func (ns *NotificationServer) SetupDBusService() error {
	log.Println("DEBUG: Setting up DBus service")
	reply, err := ns.conn.RequestName(NotificationServiceName, dbus.NameFlagAllowReplacement|dbus.NameFlagReplaceExisting)
	if err != nil {
		return fmt.Errorf("failed to request service name: %w", err)
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("failed to become primary owner of %s", NotificationServiceName)
	}

	log.Printf("DEBUG: Successfully acquired service name: %s", NotificationServiceName)

	err = ns.conn.Export(ns, NotificationObjectPath, NotificationInterface)
	if err != nil {
		return fmt.Errorf("failed to export notification interface: %w", err)
	}

	err = ns.conn.Export(introspect.Introspectable(ns.introspectData()), NotificationObjectPath, "org.freedesktop.DBus.Introspectable")
	if err != nil {
		return fmt.Errorf("failed to export introspection interface: %w", err)
	}

	log.Println("DEBUG: DBus service setup complete")
	return nil
}

func (ns *NotificationServer) Close() error {
	return ns.conn.Close()
}

func (ns *NotificationServer) GetServerInformation() (string, string, string, string, *dbus.Error) {
	log.Println("DEBUG: GetServerInformation called")
	return "golang-notification-daemon", "eww", "1.2.0", "1.2", nil
}

func (ns *NotificationServer) GetCapabilities() ([]string, *dbus.Error) {
	log.Println("DEBUG: GetCapabilities called")
	capabilities := []string{
		"body",
		"hints",
		"persistence",
		"icon-static",
		"actions-icons",
		"actions",
	}
	return capabilities, nil
}

func (ns *NotificationServer) Notify(
	appName string,
	replacesId uint32,
	appIcon string,
	summary string,
	body string,
	actions []string,
	hints map[string]dbus.Variant,
	expireTimeout int32,
) (uint32, *dbus.Error) {
	log.Printf("DEBUG: Notify called - App: %s, Summary: %s, Body: %s", appName, summary, body)
	log.Printf("DEBUG: ReplaceID: %d, ExpireTimeout: %d", replacesId, expireTimeout)
	log.Printf("DEBUG: Actions: %v", actions)

	// Convert dbus.Variant hints to internal format
	internalHints := make(map[string]any)
	for key, variant := range hints {
		internalHints[key] = variant.Value()
		log.Printf("DEBUG: Hint %s = %v (type: %T)", key, variant.Value(), variant.Value())
	}

	// CRITICAL FIX: Actually call the daemon's HandleNotification method
	if ns.daemon == nil {
		log.Println("ERROR: Daemon reference is nil!")
		return 0, dbus.MakeFailedError(fmt.Errorf("daemon not initialized"))
	}

	notificationId, err := ns.daemon.HandleNotification(
		appName,
		replacesId,
		appIcon,
		summary,
		body,
		actions,
		internalHints,
		expireTimeout,
	)
	if err != nil {
		log.Printf("ERROR: Failed to handle notification: %v", err)
		return 0, dbus.MakeFailedError(err)
	}

	log.Printf("DEBUG: Notify returning ID: %d", notificationId)
	return notificationId, nil
}

func (ns *NotificationServer) CloseNotification(id uint32) *dbus.Error {
	log.Printf("DEBUG: CloseNotification called for ID: %d", id)
	found := ns.state.RemoveNotification(id)
	if !found {
		return dbus.MakeFailedError(fmt.Errorf("notification with ID %d not found", id))
	}

	err := ns.EmitNotificationClosed(id, state.CloseNotification)
	if err != nil {
		return dbus.MakeFailedError(fmt.Errorf("failed to emit NotificationClosed signal: %w", err))
	}
	return nil
}

// Signal emission methods
func (ns *NotificationServer) EmitActionInvoked(id uint32, actionKey string) error {
	log.Printf("DEBUG: Emitting ActionInvoked signal for ID %d, action: %s", id, actionKey)
	return ns.conn.Emit(
		NotificationObjectPath,
		NotificationInterface+".ActionInvoked",
		id,
		actionKey,
	)
}

func (ns *NotificationServer) EmitNotificationClosed(id uint32, reason state.NotificationCloseReason) error {
	reasonId := uint32(reason) + 1
	log.Printf("DEBUG: Emitting NotificationClosed signal for ID %d, reason: %s (%d)", id, reason.String(), reasonId)

	return ns.conn.Emit(
		NotificationObjectPath,
		NotificationInterface+".NotificationClosed", // Fix typo: was "NoticationClosed"
		id,
		reasonId,
	)
}

// Helper methods
func (ns *NotificationServer) introspectData() string {
	return `<interface name="org.freedesktop.Notifications">
		<method name="GetCapabilities">
			<arg direction="out" name="capabilities" type="as"/>
		</method>
		<method name="Notify">
			<arg direction="in" name="app_name" type="s"/>
			<arg direction="in" name="replaces_id" type="u"/>
			<arg direction="in" name="app_icon" type="s"/>
			<arg direction="in" name="summary" type="s"/>
			<arg direction="in" name="body" type="s"/>
			<arg direction="in" name="actions" type="as"/>
			<arg direction="in" name="hints" type="a{sv}"/>
			<arg direction="in" name="expire_timeout" type="i"/>
			<arg direction="out" name="id" type="u"/>
		</method>
		<method name="CloseNotification">
			<arg direction="in" name="id" type="u"/>
		</method>
		<method name="GetServerInformation">
			<arg direction="out" name="name" type="s"/>
			<arg direction="out" name="vendor" type="s"/>
			<arg direction="out" name="version" type="s"/>
			<arg direction="out" name="spec_version" type="s"/>
		</method>
		<signal name="NotificationClosed">
			<arg name="id" type="u"/>
			<arg name="reason" type="u"/>
		</signal>
		<signal name="ActionInvoked">
			<arg name="id" type="u"/>
			<arg name="action_key" type="s"/>
		</signal>
	</interface>`
}

func (ns *NotificationServer) GetConnection() *dbus.Conn {
	return ns.conn
}

func (ns *NotificationServer) HandleDBusError(err error) *dbus.Error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, errors.New("not found")):
		return dbus.MakeFailedError(err)
	default:
		return dbus.MakeFailedError(err)
	}
}
