package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/csmith/centauri/proxy"
)

func Parse(reader io.Reader) ([]*proxy.Route, error) {
	var routes []*proxy.Route
	var route *proxy.Route

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "route ") {
			route = &proxy.Route{
				Domains: strings.Split(strings.TrimPrefix(line, "route "), " "),
			}
			routes = append(routes, route)
		} else if strings.HasPrefix(line, "upstream ") {
			if route == nil {
				return nil, fmt.Errorf("upstream without route: %s", line)
			}
			route.Upstream = strings.TrimPrefix(line, "upstream ")
		} else if len(line) > 0 {
			return nil, fmt.Errorf("invalid line: %s", line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return routes, nil
}
