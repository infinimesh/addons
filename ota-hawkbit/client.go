package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
)

type client struct {
	base   string
	user   string
	pass   string
	http   *http.Client
	groups map[string]struct{}
	sleep  func(ctx context.Context, d time.Duration) error
}

func newClient(base, user, pass string, groups []string) (*client, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, errors.New("HAWKBIT_URL is required")
	}
	if user == "" || pass == "" {
		return nil, errors.New("HAWKBIT_USER and HAWKBIT_PASSWORD are required")
	}
	g := map[string]struct{}{}
	for _, name := range groups {
		name = strings.TrimSpace(name)
		if name != "" {
			g[name] = struct{}{}
		}
	}
	return &client{
		base: base, user: user, pass: pass, groups: g,
		http: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		payload = b
	}
	var last error
	var retryAfter time.Duration
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := c.wait(ctx, otacore.RetryWait(attempt-1, retryAfter)); err != nil {
				return 0, nil, err
			}
		}
		retryAfter = 0
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
		if err != nil {
			return 0, nil, err
		}
		req.SetBasicAuth(c.user, c.pass)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			last = err
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if err != nil {
			return res.StatusCode, nil, err
		}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			if res.StatusCode == http.StatusTooManyRequests {
				retryAfter = retryAfterHeader(res.Header.Get("Retry-After"), time.Now())
			}
			last = fmt.Errorf("hawkbit %s %s: %d", method, path, res.StatusCode)
			continue
		}
		return res.StatusCode, raw, nil
	}
	if last == nil {
		last = errors.New("hawkbit unreachable")
	}
	return 0, nil, last
}

func (c *client) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryAfterHeader(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	t, err := http.ParseTime(v)
	if err != nil {
		return 0
	}
	d := t.Sub(now)
	if d < 0 {
		return 0
	}
	return d
}

type page struct {
	Content []map[string]any `json:"content"`
}

func decodeList(raw []byte) ([]map[string]any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "{}" {
		return nil, nil
	}
	if raw[0] == '[' {
		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, err
		}
		return rows, nil
	}
	var p page
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return p.Content, nil
}

func decodeOne(raw []byte) (map[string]any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "{}" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *client) ensureTarget(ctx context.Context, id, name string) error {
	code, _, err := c.do(ctx, http.MethodGet, "/rest/v1/targets/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	if code == http.StatusOK {
		return nil
	}
	if code != http.StatusNotFound {
		return fmt.Errorf("hawkbit target %s: %d", id, code)
	}
	if name == "" {
		name = id
	}
	code, _, err = c.do(ctx, http.MethodPost, "/rest/v1/targets", []map[string]string{{
		"controllerId": id,
		"name":         name,
	}})
	if err != nil {
		return err
	}
	if code != http.StatusCreated && code != http.StatusOK && code != http.StatusConflict {
		return fmt.Errorf("hawkbit create target %s: %d", id, code)
	}
	return nil
}

func (c *client) deleteTarget(ctx context.Context, id string) error {
	code, _, err := c.do(ctx, http.MethodDelete, "/rest/v1/targets/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	if code == http.StatusOK || code == http.StatusNoContent || code == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("hawkbit delete target %s: %d", id, code)
}

func (c *client) tagID(ctx context.Context, name string) (float64, error) {
	q := url.QueryEscape(`name=="` + strings.ReplaceAll(name, `"`, "") + `"`)
	code, raw, err := c.do(ctx, http.MethodGet, "/rest/v1/targettags?limit=1&q="+q, nil)
	if err != nil {
		return 0, err
	}
	if code != http.StatusOK {
		return 0, fmt.Errorf("hawkbit target tags: %d", code)
	}
	rows, err := decodeList(raw)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if str(row["name"]) == name {
			return num(row["id"]), nil
		}
	}
	code, raw, err = c.do(ctx, http.MethodPost, "/rest/v1/targettags", []map[string]string{{"name": name}})
	if err != nil {
		return 0, err
	}
	if code != http.StatusCreated && code != http.StatusOK {
		return 0, fmt.Errorf("hawkbit create tag %s: %d", name, code)
	}
	rows, err = decodeList(raw)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("hawkbit create tag %s: empty response", name)
	}
	return num(rows[0]["id"]), nil
}

func (c *client) setTag(ctx context.Context, tag, controller string, on bool) error {
	id, err := c.tagID(ctx, tag)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/rest/v1/targettags/%.0f/assigned/%s", id, url.PathEscape(controller))
	method := http.MethodPost
	if !on {
		method = http.MethodDelete
	}
	code, _, err := c.do(ctx, method, path, nil)
	if err != nil {
		return err
	}
	if code == http.StatusOK || code == http.StatusCreated || code == http.StatusNoContent || code == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("hawkbit tag %s: %d", tag, code)
}

type dist struct {
	ID   float64
	Name string
}

func (c *client) findDist(ctx context.Context, name string) (dist, bool, error) {
	q := url.QueryEscape(`name=="` + strings.ReplaceAll(name, `"`, "") + `"`)
	code, raw, err := c.do(ctx, http.MethodGet, "/rest/v1/distributionsets?limit=50&q="+q, nil)
	if err != nil {
		return dist{}, false, err
	}
	if code != http.StatusOK {
		return dist{}, false, fmt.Errorf("hawkbit distribution sets: %d", code)
	}
	rows, err := decodeList(raw)
	if err != nil {
		return dist{}, false, err
	}
	for _, row := range rows {
		if str(row["name"]) == name {
			return dist{ID: num(row["id"]), Name: name}, true, nil
		}
	}
	return dist{}, false, nil
}

func (c *client) assigned(ctx context.Context, controller string) (dist, bool, error) {
	code, raw, err := c.do(ctx, http.MethodGet, "/rest/v1/targets/"+url.PathEscape(controller)+"/assignedDS", nil)
	if err != nil {
		return dist{}, false, err
	}
	if code == http.StatusNotFound || code == http.StatusNoContent {
		return dist{}, false, nil
	}
	if code != http.StatusOK {
		return dist{}, false, fmt.Errorf("hawkbit assigned distribution set: %d", code)
	}
	row, err := decodeOne(raw)
	if err != nil || row == nil || str(row["name"]) == "" {
		return dist{}, false, err
	}
	return dist{ID: num(row["id"]), Name: str(row["name"])}, true, nil
}

type action struct {
	ID       float64
	Status   string
	DistID   float64
	Messages []string
}

func (c *client) latestAction(ctx context.Context, controller string) (action, bool, error) {
	path := "/rest/v1/targets/" + url.PathEscape(controller) + "/actions?limit=1&sort=id:DESC&representation=full"
	code, raw, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return action{}, false, err
	}
	if code == http.StatusNotFound {
		return action{}, false, nil
	}
	if code != http.StatusOK {
		return action{}, false, fmt.Errorf("hawkbit actions: %d", code)
	}
	rows, err := decodeList(raw)
	if err != nil || len(rows) == 0 {
		return action{}, false, err
	}
	act := actionFrom(rows[0])
	if act.DistID == 0 && act.ID != 0 {
		one, ok, err := c.oneAction(ctx, controller, act.ID)
		if err != nil {
			return action{}, false, err
		}
		if ok {
			if one.DistID != 0 {
				act.DistID = one.DistID
			}
			if act.Status == "" {
				act.Status = one.Status
			}
		}
	}
	if act.ID != 0 {
		msgs, err := c.latestMessages(ctx, controller, act.ID)
		if err != nil {
			return action{}, false, err
		}
		act.Messages = msgs
	}
	return act, true, nil
}

func (c *client) latestMessages(ctx context.Context, controller string, id float64) ([]string, error) {
	path := fmt.Sprintf("/rest/v1/targets/%s/actions/%.0f/status?limit=1&sort=id:DESC", url.PathEscape(controller), id)
	code, raw, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, nil
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("hawkbit action status: %d", code)
	}
	rows, err := decodeList(raw)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return messageList(rows[0]["messages"]), nil
}

func messageList(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	default:
		return nil
	}
}

func (c *client) oneAction(ctx context.Context, controller string, id float64) (action, bool, error) {
	path := fmt.Sprintf("/rest/v1/targets/%s/actions/%.0f", url.PathEscape(controller), id)
	code, raw, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return action{}, false, err
	}
	if code == http.StatusNotFound {
		return action{}, false, nil
	}
	if code != http.StatusOK {
		return action{}, false, fmt.Errorf("hawkbit action: %d", code)
	}
	row, err := decodeOne(raw)
	if err != nil || row == nil {
		return action{}, false, err
	}
	return actionFrom(row), true, nil
}

func actionFrom(row map[string]any) action {
	status := str(row["detailStatus"])
	if status == "" {
		status = str(row["status"])
	}
	return action{ID: num(row["id"]), Status: status, DistID: distID(row)}
}

func distID(row map[string]any) float64 {
	if ds, ok := row["distributionSet"].(map[string]any); ok {
		if id := num(ds["id"]); id != 0 {
			return id
		}
	}
	links, _ := row["_links"].(map[string]any)
	link, _ := links["distributionset"].(map[string]any)
	return idFromHref(str(link["href"]))
}

func idFromHref(href string) float64 {
	href = strings.TrimSpace(strings.TrimRight(href, "/"))
	i := strings.LastIndex(href, "/")
	if i < 0 || i == len(href)-1 {
		return 0
	}
	f, err := strconv.ParseFloat(href[i+1:], 64)
	if err != nil {
		return 0
	}
	return f
}

func (c *client) assign(ctx context.Context, controller string, ds dist) error {
	body := []map[string]any{{"id": ds.ID, "type": "forced"}}
	code, _, err := c.do(ctx, http.MethodPost, "/rest/v1/targets/"+url.PathEscape(controller)+"/assignedDS", body)
	if err != nil {
		return err
	}
	if code == http.StatusOK || code == http.StatusCreated {
		return nil
	}
	return fmt.Errorf("hawkbit assign %s: %d", ds.Name, code)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}
