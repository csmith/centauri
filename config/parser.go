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
		parts := strings.SplitN(line, " ", 2)

		switch parts[0] {
		case "route":
			route = &proxy.Route{
				Domains: strings.Split(strings.TrimPrefix(line, "route "), " "),
			}
			routes = append(routes, route)
		case "upstream":
			if route == nil {
				return nil, fmt.Errorf("upstream without route: %s", line)
			}
			route.Upstream = strings.TrimPrefix(line, "upstream ")
		case "header":
			if route == nil {
				return nil, fmt.Errorf("upstream without route: %s", line)
			}
			if err := parseHeader(line, route); err != nil {
				return nil, err
			}
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

func parseHeader(line string, route *proxy.Route) error {
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 3 {
		return fmt.Errorf("invalid header line: %s", line)
	}

	switch strings.ToLower(parts[1]) {
	case "delete":
		route.Headers = append(route.Headers, proxy.Header{
			Operation: proxy.HeaderOpDelete,
			Name:      parts[2],
		})
	case "add":
		if len(parts) != 4 {
			return fmt.Errorf("invalid header add line: %s", line)
		}

		route.Headers = append(route.Headers, proxy.Header{
			Operation: proxy.HeaderOpAdd,
			Name:      parts[2],
			Value:     parts[3],
		})
	case "replace":
		if len(parts) != 4 {
			return fmt.Errorf("invalid header set line: %s", line)
		}

		route.Headers = append(route.Headers, proxy.Header{
			Operation: proxy.HeaderOpReplace,
			Name:      parts[2],
			Value:     parts[3],
		})
	case "default":
		if len(parts) != 4 {
			return fmt.Errorf("invalid header default line: %s", line)
		}

		route.Headers = append(route.Headers, proxy.Header{
			Operation: proxy.HeaderOpDefault,
			Name:      parts[2],
			Value:     parts[3],
		})
	default:
		return fmt.Errorf("invalid header operation: %s", parts[1])
	}
	return nil
}
