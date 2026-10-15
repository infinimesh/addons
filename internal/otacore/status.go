package otacore

import "strings"

// Mapped is a core ota status. Error is stored when the backend state needs one,
// for example hawkBit canceled.
type Mapped struct {
	Status string
	Error  string
}

var coreStatus = map[string]struct{}{
	"idle": {}, "downloading": {}, "installing": {}, "success": {}, "failed": {},
}

// MapStatus applies table. The result status has to be one of the five core values.
func MapStatus(table map[string]Mapped, raw string) (Mapped, bool) {
	if _, ok := coreStatus[strings.ToLower(raw)]; ok {
		return Mapped{Status: strings.ToLower(raw)}, true
	}
	m, ok := table[raw]
	if !ok {
		m, ok = table[strings.ToUpper(raw)]
	}
	if !ok {
		return Mapped{}, false
	}
	if _, ok := coreStatus[m.Status]; !ok {
		return Mapped{}, false
	}
	return m, true
}

// IdempotencyKey identifies one deploy of version onto device.
func IdempotencyKey(device, version string) string {
	return device + "\n" + version
}
