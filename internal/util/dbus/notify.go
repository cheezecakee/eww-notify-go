package dbus

import (
	"github.com/cheezecakee/eww-notify-go/internal/config"
)

type Hints map[string]any
type ImageData [int32, int32, int32, bool, int32, int32, []any]{}

type HintKeyNotifyType string
type HintKeyUrgency string

const HintKeyNotifyType = "end-type"
const HintKeyUrgency = "urgency"

	
