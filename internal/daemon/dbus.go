package daemon

import (
	"errors"
	"fmt"

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
	conn  *dbus.Conn
	state *state.NotificationState
}

func NewNotificationServer(notificationState *state.NotificationState) (*NotificationServer, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to session bus: %w", err)
	}

	server := &NotificationServer{
		conn:  conn,
		state: notificationState,
	}

	notificationState.DbusConn = conn

	return server, nil
}

func (ns *NotificationServer) SetupDBusService() error {
	reply, err := ns.conn.RequestName(NotificationServiceName, dbus.NameFlagAllowReplacement|dbus.NameFlagReplaceExisting)
	if err != nil {
		return fmt.Errorf("failed to request service name: %w", err)
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("failed to become primary owner of %s", NotificationServiceName)
	}

	err = ns.conn.Export(ns, NotificationObjectPath, NotificationInterface)
	if err != nil {
		return fmt.Errorf("failed to export notification interface: %w", err)
	}

	err = ns.conn.Export(introspect.Introspectable(ns.introspectData()), NotificationObjectPath, "org.freedesktop.DBus.Introspectable")
	if err != nil {
		return fmt.Errorf("failed to export introspection interface: %w", err)
	}

	return nil
}

func (ns *NotificationServer) Close() error {
	return ns.conn.Close()
}

func (ns *NotificationServer) GetServerInformation() (string, string, string, string, *dbus.Error) {
	return "golang-notication-daemon", "eww", "1.2.0", "1.2", nil
}

func (ns *NotificationServer) GetCapabilitied() ([]string, *dbus.Error) {
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
	internalHints := make(map[string]any)
	for key, variant := range hints {
		internalHints[key] = variant.Value()
	}
	// Handle notification creation/update logic here
	// This will be implemented when we create the main daemon logic

	// For now, return a placeholder ID
	var notificationId uint32
	if replacesId != 0 {
		notificationId = replacesId
	} else {
		notificationId = ns.state.NextId()
	}

	// TODO: Create actual notification object and add to state
	// This will be implemented in daemon.go

	return notificationId, nil
}

func (ns *NotificationServer) CloseNotification(id uint32) *dbus.Error {
	found := ns.state.RemoveNotification(id)
	if !found {
		return dbus.MakeFailedError(fmt.Errorf("notication with ID %d not found", id))
	}

	err := ns.EmitNotificationClosed(id, state.CloseNotification)
	if err != nil {
		return dbus.MakeFailedError(fmt.Errorf("failed to emit NotificationClose signal: %w", err))
	}
	return nil
}

// Signal emission methods
func (ns *NotificationServer) EmitActionInvoked(id uint32, actionKey string) error {
	return ns.conn.Emit(
		NotificationObjectPath,
		NotificationInterface+".ActionInvoked",
		id,
		actionKey,
	)
}

func (ns *NotificationServer) EmitNotificationClosed(id uint32, reason state.NotificationCloseReason) error {
	reasonId := uint32(reason) + 1

	return ns.conn.Emit(
		NotificationObjectPath,
		NotificationInterface+".NoticationClosed",
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
	if err != nil {
		return nil
	}

	switch {
	case errors.Is(err, errors.New("not found")):
		return dbus.MakeFailedError(err)
	default:
		return dbus.MakeFailedError(err)
	}
}
