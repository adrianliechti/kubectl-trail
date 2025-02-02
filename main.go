package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrianliechti/kubectl-trail/pkg/loki"

	"github.com/lmittmann/tint"
	"github.com/spf13/pflag"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var flagArgs []string

var flagNamespace string
var flagService string

var flagLimit int
var flagLevel string
var flagSince time.Duration

func init() {
	pflag.StringVar(&flagNamespace, "namespace", "", "namespace to filter logs")
	pflag.StringVar(&flagService, "service", "", "app/service to filter logs")

	pflag.IntVar(&flagLimit, "limit", 100, "limit number of logs")
	pflag.StringVar(&flagLevel, "level", "", "level to filter logs")
	pflag.DurationVar(&flagSince, "since", 15*time.Minute, "since time to filter logs")

	pflag.Parse()

	flagArgs = pflag.Args()
}

func main() {
	ctx := context.Background()

	if len(flagArgs) > 1 {
		panic("too many arguments")
	}

	var input string

	if len(flagArgs) > 0 {
		input = flagArgs[0]
	}

	if input == "{}" {
		input = ""
	}

	var selectors []string
	var filters []string

	if flagNamespace != "" {
		selectors = append(selectors, `namespace="`+flagNamespace+`"`)
	}

	if flagNamespace == "" && !strings.ContainsAny(input, "{}") {
		selectors = append(selectors, `namespace=~".+"`)
	}

	if flagService != "" {
		selectors = append(selectors, `service_name="`+flagService+`"`)
	}

	if flagLevel != "" {
		levels := []string{"info", "warn", "error", "fatal", "critical"}

		if flagLevel == "warn" {
			levels = []string{"warn", "error", "fatal", "critical"}
		}

		if flagLevel == "error" {
			levels = []string{"error", "fatal", "critical"}
		}

		filters = append(filters, `detected_level=~"`+strings.Join(levels, "|")+`"`)
	}

	var query string

	if strings.ContainsAny(input, "{}") {
		query = input
	}

	if len(selectors) > 0 {
		if query != "" {
			panic("cannot mix selectors with query")
		}

		query = "{ " + strings.Join(selectors, ", ") + " }"
	}

	if len(filters) > 0 {
		query = query + " | " + strings.Join(filters, " | ")
	}

	if input != "" && !strings.ContainsAny(input, "{}") {
		query = query + " | " + strings.TrimLeft(input, "| ")
	}

	slog.SetDefault(slog.New(
		tint.NewHandler(os.Stdout, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.Kitchen,
		}),
	))

	l, err := lokiClient("")

	if err != nil {
		panic(err)
	}

	options := &loki.QueryRangeOptions{
		Limit: &flagLimit,
	}

	if flagSince > 0 {
		end := time.Now()
		start := end.Add(-flagSince)

		options.Start = &start
		options.End = &end
	}

	result, err := l.QueryRange(query, options)

	if err != nil {
		panic(err)
	}

	for _, stream := range result.Data.Result {
		labels := stream.Stream

		var level slog.Level

		switch labels["detected_level"] {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error", "fatal", "critical":
			level = slog.LevelError
		}

		for _, value := range stream.Values {
			time := value.Timestamp
			text := value.Text

			r := slog.NewRecord(time, level, text, 0)

			for key, value := range labels {
				r.AddAttrs(slog.String(key, value))
			}

			slog.Default().Handler().Handle(ctx, r)
		}
	}

	_ = result
}

func kubernetesConfig() (clientcmd.ClientConfig, error) {
	home, err := os.UserHomeDir()

	if err != nil {
		return nil, err
	}

	kubeconfig := filepath.Join(home, ".kube", "config")

	data, err := os.ReadFile(kubeconfig)

	if err != nil {
		return nil, err
	}

	return clientcmd.NewClientConfigFromBytes(data)
}

func lokiClient(lokiURL string) (*loki.Client, error) {
	if u, _ := url.Parse(lokiURL); u.IsAbs() {
		return loki.New(u.String()), nil
	}

	clientConfig, err := kubernetesConfig()

	if err != nil {
		return nil, err
	}

	restConfig, err := clientConfig.ClientConfig()

	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(restConfig)

	if err != nil {
		return nil, err
	}

	restClient, err := rest.HTTPClientFor(restConfig)

	if err != nil {
		return nil, err
	}

	parts := strings.Split(lokiURL, "/")

	if lokiURL == "" {
		discoverService := func(namespace string) (string, string, bool) {
			services, err := client.CoreV1().Services(namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=loki",
			})

			if err != nil {
				return "", "", false
			}

			for _, service := range services.Items {
				if service.Spec.ClusterIP != "None" {
					continue
				}

				for _, port := range service.Spec.Ports {
					if port.Port != 3100 {
						continue
					}

					return service.Namespace, service.Name, true
				}
			}

			for _, service := range services.Items {
				for _, port := range service.Spec.Ports {
					if port.Port != 3100 {
						continue
					}

					return service.Namespace, service.Name, true
				}
			}

			return "", "", false
		}

		if namespace, service, ok := discoverService(""); ok {
			parts = []string{namespace, service}
		}
	}

	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid loki service: namespace/service")
	}

	namespace := parts[0]
	service := parts[1]

	lokiURL = fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s:%d/proxy", restConfig.Host, namespace, service, 3100)
	return loki.New(lokiURL, loki.WithClient(restClient)), nil
}
