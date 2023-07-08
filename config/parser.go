package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/csmith/centauri/proxy"
)

// Parse reads a configuration file from the given reader, and returns the routes that it contains.
func Parse(reader io.Reader) ([]*proxy.Route, error) {
	var routes []*proxy.Route
	var route *proxy.Route

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		directive, args, _ := strings.Cut(line, " ")

		switch strings.ToLower(directive) {
		case "route":
			route = &proxy.Route{
				Domains:   strings.Split(args, " "),
				Upstreams: []proxy.Upstream{},
			}
			routes = append(routes, route)
		case "upstream":
			if route == nil {
				return nil, fmt.Errorf("upstream without route: %s", line)
			}
			route.Upstreams = append(route.Upstreams, proxy.Upstream{Host: args})
		case "header":
			if route == nil {
				return nil, fmt.Errorf("upstream without route: %s", line)
			}
			if err := parseHeader(args, route); err != nil {
				return nil, err
			}
		case "provider":
			if route == nil {
				return nil, fmt.Errorf("provider without route: %s", line)
			}
			if route.Provider != "" {
				return nil, fmt.Errorf("route %s has multiple providers", route.Domains)
			}
			route.Provider = args
		case "#":
			// Ignore comments
		default:
			if len(line) > 0 {
				return nil, fmt.Errorf("invalid line: %s", line)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return routes, nil
}

func parseHeader(args string, route *proxy.Route) error {
	parts := strings.SplitN(args, " ", 3)

	switch strings.ToLower(parts[0]) {
	case "delete":
		if len(parts) != 2 {
			return fmt.Errorf("invalid header delete line: %s", args)
		}

		route.Headers = append(route.Headers, proxy.Header{
			Operation: proxy.HeaderOpDelete,
			Name:      parts[1],
		})
	case "add":
		if len(parts) != 3 {
			return fmt.Errorf("invalid header add line: %s", args)
		}

		route.Headers = append(route.Headers, proxy.Header{
			Operation: proxy.HeaderOpAdd,
			Name:      parts[1],
			Value:     parts[2],
		})
	case "replace":
		if len(parts) != 3 {
			return fmt.Errorf("invalid header set line: %s", args)
		}

		route.Headers = append(route.Headers, proxy.Header{
			Operation: proxy.HeaderOpReplace,
			Name:      parts[1],
			Value:     parts[2],
		})
	case "default":
		if len(parts) != 3 {
			return fmt.Errorf("invalid header default line: %s", args)
		}

		route.Headers = append(route.Headers, proxy.Header{
			Operation: proxy.HeaderOpDefault,
			Name:      parts[1],
			Value:     parts[2],
		})
	default:
		return fmt.Errorf("invalid header operation: %s", parts[0])
	}
	return nil
}
