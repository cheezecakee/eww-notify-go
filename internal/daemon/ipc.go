package daemon

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/cheezecakee/eww-notify-go/internal/state"
	"github.com/cheezecakee/eww-notify-go/internal/util/constants"
)

// IPCServer handles Unix socket communication
type IPCServer struct {
	daemon   *Daemon
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewIPCServer creates a new IPC server
func NewIPCServer(daemon *Daemon) *IPCServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &IPCServer{
		daemon: daemon,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start starts the IPC server
func (s *IPCServer) Start() error {
	// Remove existing socket file if it exists
	if err := os.RemoveAll(constants.IPCSocketPath); err != nil {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create Unix socket listener
	listener, err := net.Listen("unix", constants.IPCSocketPath)
	if err != nil {
		return fmt.Errorf("failed to create Unix socket listener: %w", err)
	}

	s.listener = listener

	// Start accepting connections
	go s.acceptLoop()

	return nil
}

// Stop stops the IPC server
func (s *IPCServer) Stop() error {
	s.cancel()

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			return fmt.Errorf("failed to close IPC listener: %w", err)
		}
	}

	// Clean up socket file
	return os.RemoveAll(constants.IPCSocketPath)
}

// acceptLoop accepts and handles IPC connections
func (s *IPCServer) acceptLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				// Check if we're shutting down
				select {
				case <-s.ctx.Done():
					return
				default:
					fmt.Printf("Failed to accept IPC connection: %v\n", err)
					continue
				}
			}

			// Handle connection in goroutine
			go s.handleConnection(conn)
		}
	}
}

// handleConnection handles a single IPC connection
func (s *IPCServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if err := s.handleCommand(line); err != nil {
			fmt.Printf("Failed to handle IPC command '%s': %v\n", line, err)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading from IPC connection: %v\n", err)
	}
}

// handleCommand processes a single IPC command
func (s *IPCServer) handleCommand(command string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "kill":
		return s.handleKillCommand()

	case "action":
		return s.handleActionCommand(args)

	case "close":
		return s.handleCloseCommand(args)

	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

// handleKillCommand handles the kill command (shutdown daemon)
func (s *IPCServer) handleKillCommand() error {
	fmt.Println("Received kill command, shutting down daemon...")

	// Stop the daemon (this should be handled by the main process)
	go func() {
		if err := s.daemon.Stop(); err != nil {
			fmt.Printf("Error stopping daemon: %v\n", err)
		}
		os.Exit(0)
	}()

	return nil
}

// handleActionCommand handles action invocation
func (s *IPCServer) handleActionCommand(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("action command requires notification ID and action key")
	}

	// Parse notification ID
	id, err := strconv.ParseUint(args[0], 10, 32)
	if err != nil {
		return fmt.Errorf("invalid notification ID: %w", err)
	}

	actionKey := args[1]

	// Invoke action
	if err := s.daemon.InvokeAction(uint32(id), actionKey); err != nil {
		return fmt.Errorf("failed to invoke action: %w", err)
	}

	return nil
}

// handleCloseCommand handles notification closure
func (s *IPCServer) handleCloseCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("close command requires notification ID")
	}

	// Parse notification ID
	id, err := strconv.ParseUint(args[0], 10, 32)
	if err != nil {
		return fmt.Errorf("invalid notification ID: %w", err)
	}

	// Remove notification
	if err := s.daemon.RemoveNotification(uint32(id)); err != nil {
		return fmt.Errorf("failed to remove notification: %w", err)
	}

	// Emit closed signal
	if err := s.daemon.dbusServer.EmitNotificationClosed(uint32(id), state.Dismiss); err != nil {
		return fmt.Errorf("failed to emit notification closed signal: %w", err)
	}

	return nil
}

// SendIPCCommand sends a command to the IPC socket (utility function for CLI)
func SendIPCCommand(command string) error {
	conn, err := net.Dial("unix", constants.IPCSocketPath)
	if err != nil {
		return fmt.Errorf("daemon is not running, run end first")
	}
	defer conn.Close()

	_, err = conn.Write([]byte(command + "\n"))
	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	return nil
}
