package main

import (
	"strings"

	"github.com/infinimesh/addons/internal/otacore"
)

// Image is the hawkBit build this table was checked against.
const Image = "hawkbit/hawkbit-update-server:0.9.0"

// ActionStatus maps hawkBit 0.9.0 detailStatus onto the five core statuses.
// The wire value is the lowercase enum name. MapStatus matches either case.
// RUNNING means hawkBit is waiting for the device. Core has no pending status,
// so that wait is idle. installing and downloading come from the device's
// feedback messages, not from RUNNING.
// CANCELED and CANCELING become idle with error "canceled".
// status is only pending or finished. finished is the fallback when
// detailStatus is missing, and it means success.
var ActionStatus = map[string]otacore.Mapped{
	"FINISHED":              {Status: "success"},
	"ERROR":                 {Status: "failed"},
	"WARNING":               {Status: "installing"},
	"RUNNING":               {Status: "idle"},
	"CANCELED":              {Status: "idle", Error: "canceled"},
	"CANCELING":             {Status: "idle", Error: "canceled"},
	"RETRIEVED":             {Status: "downloading"},
	"DOWNLOAD":              {Status: "downloading"},
	"SCHEDULED":             {Status: "idle"},
	"CANCEL_REJECTED":       {Status: "installing"},
	"DOWNLOADED":            {Status: "installing"},
	"WAIT_FOR_CONFIRMATION": {Status: "idle"},
}

// devicePhase is the device's own report. The newest feedback wins.
// HawkBit leaves detailStatus at retrieved for both downloading and installing.
func devicePhase(messages []string) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := strings.ToLower(strings.TrimSpace(messages[i]))
		if m == "" || strings.HasPrefix(m, "update server:") {
			continue
		}
		switch {
		case strings.Contains(m, "install"):
			return "installing"
		case strings.Contains(m, "download"):
			return "downloading"
		}
	}
	return ""
}
