package constants

import "path/filepath"

const (
	// IPC socket path for daemon communication
	IPCSocketPath = "/tmp/eww-socket"

	// Image temp directory for notification images
	ImageTempDir = "/tmp/end-images"

	// Application info
	AppName     = "eww-notification-daemon"
	AppVendor   = "eww"
	AppVersion  = "1.2.0"
	SpecVersion = "1.2"
)

// GetImageTempDir returns the full path to the image temp directory
func GetImageTempDir() string {
	return ImageTempDir
}

// GetImagePath returns a full path for an image file
func GetImagePath(filename string) string {
	return filepath.Join(ImageTempDir, filename)
}
