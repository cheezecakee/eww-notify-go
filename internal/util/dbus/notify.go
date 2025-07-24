package dbus

type Hints map[string]any

// Format: (width, height, stride, hasAlpha, bitsPerSample, channels, pixelData)
type ImageData struct {
	Width         int32
	Height        int32
	Stride        int32
	HasAlpha      bool
	BitsPerSample int32
	Channels      int32
	PixelData     []byte
}

const (
	HintKeyNotifyType = "end-type"
	HintKeyUrgency    = "urgency"
)

func GetStringHint(hints Hints, key string) (string, bool) {
	if val, exists := hints[key]; exists {
		if str, ok := val.(string); ok {
			return str, true
		}
	}

	return "", false
}

func GetByteHint(hints Hints, key string) (uint8, bool) {
	if val, exists := hints[key]; exists {
		if b, ok := val.(uint8); ok {
			return b, true
		}
	}
	return 0, false
}

func GetImageDataHint(hints Hints, key string) (*ImageData, bool) {
	if val, exists := hints[key]; exists {
		if imgData, ok := val.(*ImageData); ok {
			return imgData, true
		}
	}
	return nil, false
}

func GetUrgency(hints Hints) uint8 {
	if urgency, ok := GetByteHint(hints, HintKeyUrgency); ok {
		return urgency
	}
	return 1
}

func ConfigKeyUrgency(urgency uint8) string {
	switch urgency {
	case 0:
		return "low"
	case 2:
		return "critical"
	default:
		return "normal"
	}
}
