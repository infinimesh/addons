package main

import "github.com/infinimesh/addons/internal/otacore"

// Image is the hawkBit build whose Action.Status names this table uses.
const Image = "hawkbit/hawkbit-update-server:0.9.0"

// ActionStatus maps hawkBit action status onto the five core statuses.
// CANCELED and CANCELING become idle with error "canceled".
var ActionStatus = map[string]otacore.Mapped{
	"FINISHED":              {Status: "success"},
	"ERROR":                 {Status: "failed"},
	"WARNING":               {Status: "installing"},
	"RUNNING":               {Status: "installing"},
	"CANCELED":              {Status: "idle", Error: "canceled"},
	"CANCELING":             {Status: "idle", Error: "canceled"},
	"RETRIEVED":             {Status: "downloading"},
	"DOWNLOAD":              {Status: "downloading"},
	"SCHEDULED":             {Status: "idle"},
	"CANCEL_REJECTED":       {Status: "installing"},
	"DOWNLOADED":            {Status: "installing"},
	"WAIT_FOR_CONFIRMATION": {Status: "idle"},
}
