package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/cheezecakee/eww-notify-go/internal/config"
	"github.com/cheezecakee/eww-notify-go/internal/daemon"
)

// Command line options
type Command struct {
	Action string
	Args   []string
}

func main() {
	// Define flags
	var (
		stopFlag   = flag.Bool("stop", false, "Stop the notification daemon")
		closeFlag  = flag.String("close", "", "Close notification by ID")
		actionFlag = flag.String("action", "", "Invoke action (format: 'id actionkey')")
		version    = flag.Bool("version", false, "Show version information")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s                    # Start daemon\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -stop              # Stop daemon\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -close 123         # Close notification with ID 123\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -action \"123 ok\"   # Invoke 'ok' action on notification 123\n", os.Args[0])
	}

	flag.Parse()

	// Handle version flag
	if *version {
		fmt.Printf("eww-notification-daemon v1.2.0\n")
		return
	}

	// Handle command flags (send to existing daemon)
	if *stopFlag {
		if err := daemon.SendIPCCommand("kill"); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Stop command sent to daemon")
		return
	}

	if *closeFlag != "" {
		// Validate ID is numeric
		if _, err := strconv.ParseUint(*closeFlag, 10, 32); err != nil {
			fmt.Fprintf(os.Stderr, "Error: Invalid notification ID '%s'\n", *closeFlag)
			os.Exit(1)
		}

		if err := daemon.SendIPCCommand("close " + *closeFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Close command sent for notification %s\n", *closeFlag)
		return
	}

	if *actionFlag != "" {
		parts := strings.Fields(*actionFlag)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Error: Action flag requires format 'id actionkey'\n")
			os.Exit(1)
		}

		// Validate ID is numeric
		if _, err := strconv.ParseUint(parts[0], 10, 32); err != nil {
			fmt.Fprintf(os.Stderr, "Error: Invalid notification ID '%s'\n", parts[0])
			os.Exit(1)
		}

		if err := daemon.SendIPCCommand("action " + *actionFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Action command sent for notification %s\n", parts[0])
		return
	}

	// No flags provided - start daemon
	if err := startDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
		os.Exit(1)
	}
}

// startDaemon starts the notification daemon
func startDaemon() error {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		// Fall back to default config with warning
		fmt.Printf("Warning: Could not load config (%v), using defaults\n", err)
		defaultCfg := config.DefaultConfig
		cfg = &defaultCfg
	}

	// Create daemon
	d, err := daemon.NewDaemon(*cfg)
	if err != nil {
		return fmt.Errorf("failed to create daemon: %w", err)
	}

	// Create IPC server
	ipcServer := daemon.NewIPCServer(d)

	// Start IPC server
	if err := ipcServer.Start(); err != nil {
		return fmt.Errorf("failed to start IPC server: %w", err)
	}
	defer func() {
		if err := ipcServer.Stop(); err != nil {
			fmt.Printf("Warning: Failed to stop IPC server: %v\n", err)
		}
	}()

	// Start daemon
	if err := d.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	defer func() {
		if err := d.Stop(); err != nil {
			fmt.Printf("Warning: Failed to stop daemon: %v\n", err)
		}
	}()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	fmt.Println("Daemon is running. Press Ctrl+C to stop.")
	<-sigChan

	fmt.Println("\nShutting down daemon...")
	return nil
}
