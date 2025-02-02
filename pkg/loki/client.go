package loki

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Option func(*Client)

type Client struct {
	url    string
	client *http.Client
}

func New(url string, options ...Option) *Client {
	url = strings.TrimRight(url, "/")

	c := &Client{
		url:    url,
		client: http.DefaultClient,
	}

	for _, option := range options {
		option(c)
	}

	return c
}

func WithClient(client *http.Client) Option {
	return func(c *Client) {
		c.client = client
	}
}

func handleError(resp *http.Response) error {
	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return errors.New("error " + resp.Status)
	}

	return errors.New(string(data))
}

type Response[T Stream] struct {
	Status Status `json:"status"`

	Data struct {
		ResultType ResultType `json:"resultType"`
		Result     []T        `json:"result"`
	} `json:"data"`
}

type Status string

const (
	StatusSuccess Status = "success"
)

type ResultType string

const (
	ResultTypeStreams ResultType = "streams"
	ResultTypeVector  ResultType = "vector"
	ResultTypeMatrix  ResultType = "matrix"
)

type Stream struct {
	Stream map[string]string `json:"stream"`
	Values []Log             `json:"values"`
}

type Log struct {
	Timestamp time.Time
	Text      string
}

func (l *Log) UnmarshalJSON(data []byte) error {
	var tmp []any

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	if len(tmp) != 2 {
		return errors.New("expected array of two elements")
	}

	tsStr, ok := tmp[0].(string)

	if !ok {
		return errors.New("timestamp is not a string")
	}

	tsInt, err := strconv.ParseInt(tsStr, 10, 64)

	if err != nil {
		return err
	}

	l.Timestamp = time.Unix(0, tsInt)

	line, ok := tmp[1].(string)

	if !ok {
		return errors.New("log line is not a string")
	}

	l.Text = line

	return nil
}
