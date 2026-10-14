package main

import (
	"sort"
	"testing"

	"github.com/infinimesh/addons/sdk/api"
)

func TestSeriesMapping(t *testing.T) {
	obj := api.Object{UID: "d1", Name: "pump", Kind: "device"}
	state := &api.DevicesStateRes{Result: api.DevicesStateResult{ReportedState: api.DeviceState{
		Data: map[string]interface{}{"temp": 21.5, "rpm": 1200.0, "label": "x", "off": nil, "nested": map[string]interface{}{"a": 1.0}},
	}}}
	pts := seriesPoints(obj, state)
	sort.Slice(pts, func(i, j int) bool { return pts[i].Key < pts[j].Key })
	want := []point{{"rpm:d1", 1200}, {"temp:d1", 21.5}}
	if len(pts) != len(want) {
		t.Fatalf("points = %v, want %v", pts, want)
	}
	for i := range want {
		if pts[i] != want[i] {
			t.Errorf("point %d = %v, want %v", i, pts[i], want[i])
		}
	}
	l := seriesLabels(obj)
	if len(l) != 3 || l["uid"] != "d1" || l["name"] != "pump" || l["kind"] != "device" {
		t.Errorf("labels = %v", l)
	}
}
