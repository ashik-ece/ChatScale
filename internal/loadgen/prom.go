package loadgen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// promQuery runs an instant query and returns the value of each result keyed
// by the given label (or by "" when label is empty and there is one result).
func promQuery(ctx context.Context, baseURL, expr, label string) (map[string]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		baseURL+"/api/v1/query?query="+url.QueryEscape(expr), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]any            `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Status != "success" {
		return nil, fmt.Errorf("prometheus status %q", body.Status)
	}
	out := make(map[string]float64, len(body.Data.Result))
	for _, r := range body.Data.Result {
		s, _ := r.Value[1].(string)
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v != v { // skip unparsable and NaN
			continue
		}
		out[r.Metric[label]] = v
	}
	return out, nil
}

// promScalar returns the single value of expr, or 0 when it has no data.
func promScalar(ctx context.Context, baseURL, expr string) (float64, error) {
	m, err := promQuery(ctx, baseURL, expr, "")
	if err != nil {
		return 0, err
	}
	return m[""], nil
}
