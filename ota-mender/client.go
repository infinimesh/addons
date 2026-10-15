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

// errToken is a 401 or 403 for a personal access token. A login with a user can still refresh.
var errToken = errors.New("mender rejected the token")

type client struct {
	base  string
	user  string
	pass  string
	token string
	attr  string
	http  *http.Client
	sleep func(context.Context, time.Duration) error
}

func newClient(base, user, pass, token, attr string) (*client, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, errors.New("MENDER_URL is required")
	}
	if strings.TrimSpace(token) == "" && (strings.TrimSpace(user) == "" || strings.TrimSpace(pass) == "") {
		return nil, errors.New("set MENDER_TOKEN or MENDER_USER and MENDER_PASSWORD")
	}
	attr = strings.TrimSpace(attr)
	if attr == "" {
		attr = "infinimesh_id"
	}
	return &client{
		base: base, user: user, pass: pass, token: strings.TrimSpace(token), attr: attr,
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
		if c.token == "" {
			if err := c.login(ctx); err != nil {
				last = err
				continue
			}
		}
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
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
		if res.StatusCode == http.StatusUnauthorized && c.user != "" {
			c.token = ""
			last = errToken
			continue
		}
		if (res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden) && c.user == "" {
			return 0, nil, errToken
		}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			if res.StatusCode == http.StatusTooManyRequests {
				retryAfter = retryAfterHeader(res.Header.Get("Retry-After"), time.Now())
			}
			last = fmt.Errorf("mender %s %s: %d", method, path, res.StatusCode)
			continue
		}
		return res.StatusCode, raw, nil
	}
	if last == nil {
		last = errors.New("mender unreachable")
	}
	return 0, nil, last
}

func (c *client) login(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/management/v1/useradm/auth/login", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("mender login: %d", res.StatusCode)
	}
	tok := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if tok == "" {
		return errors.New("mender login returned an empty token")
	}
	c.token = tok
	return nil
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
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

type inventoryDevice struct {
	ID string `json:"id"`
}

func (c *client) findDevice(ctx context.Context, infinimeshID string) (string, bool, error) {
	q := url.Values{}
	q.Set(c.attr, infinimeshID)
	q.Set("per_page", "2")
	code, raw, err := c.do(ctx, http.MethodGet, "/api/management/v1/inventory/devices?"+q.Encode(), nil)
	if err != nil {
		return "", false, err
	}
	if code != http.StatusOK {
		return "", false, fmt.Errorf("mender inventory: %d", code)
	}
	var rows []inventoryDevice
	if err := json.Unmarshal(raw, &rows); err != nil {
		return "", false, err
	}
	if len(rows) == 0 || rows[0].ID == "" {
		return "", false, nil
	}
	return rows[0].ID, true, nil
}

func (c *client) setGroup(ctx context.Context, menderID, group string) error {
	path := "/api/management/v1/inventory/devices/" + url.PathEscape(menderID) + "/group"
	code, _, err := c.do(ctx, http.MethodPut, path, map[string]string{"group": group})
	if err != nil {
		return err
	}
	if code != http.StatusNoContent && code != http.StatusOK {
		return fmt.Errorf("mender group: %d", code)
	}
	return nil
}

func (c *client) clearGroup(ctx context.Context, menderID, group string) error {
	path := "/api/management/v1/inventory/devices/" + url.PathEscape(menderID) + "/group/" + url.PathEscape(group)
	code, _, err := c.do(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if code == http.StatusNoContent || code == http.StatusNotFound || code == http.StatusOK {
		return nil
	}
	return fmt.Errorf("mender group: %d", code)
}

type release struct {
	Name string `json:"name"`
}

func (c *client) hasArtifact(ctx context.Context, name string) (bool, error) {
	q := url.Values{}
	q.Set("name", name)
	code, raw, err := c.do(ctx, http.MethodGet, "/api/management/v1/deployments/deployments/releases?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	if code != http.StatusOK {
		return false, fmt.Errorf("mender releases: %d", code)
	}
	var rows []release
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.Name == name {
			return true, nil
		}
	}
	return false, nil
}

type deviceDeployment struct {
	Deployment struct {
		ID           string `json:"id"`
		ArtifactName string `json:"artifact_name"`
		Created      string `json:"created"`
	} `json:"deployment"`
	Device struct {
		Status string `json:"status"`
	} `json:"device"`
}

type openDeployment struct {
	ID           string `json:"id"`
	ArtifactName string `json:"artifact_name"`
	Created      string `json:"created"`
	Status       string `json:"status"`
}

func (c *client) history(ctx context.Context, menderID string) ([]deviceDeployment, error) {
	path := "/api/management/v1/deployments/deployments/devices/" + url.PathEscape(menderID) + "?per_page=20"
	code, raw, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, nil
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("mender history: %d", code)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var rows []deviceDeployment
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *client) listOpen(ctx context.Context, status string) ([]openDeployment, error) {
	var all []openDeployment
	for page := 1; page <= 20; page++ {
		path := fmt.Sprintf("/api/management/v1/deployments/deployments?status=%s&per_page=500&page=%d&sort=desc", url.QueryEscape(status), page)
		code, raw, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if code != http.StatusOK {
			return nil, fmt.Errorf("mender deployments: %d", code)
		}
		var rows []openDeployment
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &rows); err != nil {
				return nil, err
			}
		}
		all = append(all, rows...)
		if len(rows) < 500 {
			break
		}
	}
	return all, nil
}

func (c *client) deviceIDs(ctx context.Context, deploymentID string) ([]string, error) {
	path := "/api/management/v1/deployments/deployments/" + url.PathEscape(deploymentID) + "/device_list"
	code, raw, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, nil
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("mender device list: %d", code)
	}
	var ids []string
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func (c *client) openFor(ctx context.Context, menderID string) ([]openDeployment, error) {
	var out []openDeployment
	for _, status := range []string{"pending", "inprogress"} {
		rows, err := c.listOpen(ctx, status)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			ids, err := c.deviceIDs(ctx, row.ID)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				if id == menderID {
					out = append(out, row)
					break
				}
			}
		}
	}
	return out, nil
}

func (c *client) abort(ctx context.Context, deploymentID string) error {
	path := "/api/management/v1/deployments/deployments/" + url.PathEscape(deploymentID) + "/status"
	code, _, err := c.do(ctx, http.MethodPut, path, map[string]string{"status": "aborted"})
	if err != nil {
		return err
	}
	if code == http.StatusNoContent || code == http.StatusNotFound || code == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("mender abort: %d", code)
}

func (c *client) createDeployment(ctx context.Context, menderID, version string) (int, error) {
	body := map[string]any{
		"name":          "infinimesh-" + menderID + "-" + version,
		"artifact_name": version,
		"devices":       []string{menderID},
	}
	code, _, err := c.do(ctx, http.MethodPost, "/api/management/v1/deployments/deployments", body)
	return code, err
}
