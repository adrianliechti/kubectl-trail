package loki

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type QueryRangeOptions struct {
	Limit *int

	Start *time.Time
	End   *time.Time
}

func (c *Client) QueryRange(query string, options *QueryRangeOptions) (*Response[Stream], error) {
	if options == nil {
		options = new(QueryRangeOptions)
	}

	values := url.Values{}

	if query != "" {
		values.Set("query", query)
	}

	if options.Limit != nil {
		values.Set("limit", "100")
	}

	if options.Start != nil {
		values.Set("start", fmt.Sprintf("%d", options.Start.UnixNano()))
	}

	if options.End != nil {
		values.Set("end", fmt.Sprintf("%d", options.End.UnixNano()))
	}

	u, _ := url.Parse(c.url + "/loki/api/v1/query_range")
	u.RawQuery = values.Encode()

	r, _ := http.NewRequest(http.MethodGet, u.String(), nil)

	resp, err := c.client.Do(r)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleError(resp)
	}

	var response Response[Stream]

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return &response, nil
}
