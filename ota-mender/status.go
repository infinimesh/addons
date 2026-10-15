package main

import "github.com/infinimesh/addons/internal/otacore"

// DeviceStatus maps Mender open source device deployment status onto the five
// core statuses. pending is idle. installing and rebooting are installing.
// success and already-installed are success. failure, aborted, and noartifact
// are failed. UNREGISTERED is a device this server does not have.
var DeviceStatus = map[string]otacore.Mapped{
	"PENDING":           {Status: "idle"},
	"DOWNLOADING":       {Status: "downloading"},
	"INSTALLING":        {Status: "installing"},
	"REBOOTING":         {Status: "installing"},
	"SUCCESS":           {Status: "success"},
	"ALREADY-INSTALLED": {Status: "success"},
	"FAILURE":           {Status: "failed"},
	"ABORTED":           {Status: "failed"},
	"NOARTIFACT":        {Status: "failed"},
	"UNREGISTERED":      {Status: "idle", Error: "not registered in mender"},
}
