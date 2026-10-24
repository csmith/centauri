package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/csmith/centauri/proxy"
)

// Parse reads a configuration file from the given reader, and returns the routes that it contains.
func Parse(reader io.Reader) (routes []*proxy.Route, fallback *proxy.Route, err error) {
	var route *proxy.Route
	var lineNumber, routeLine int

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		directive, args, _ := strings.Cut(line, " ")

		var lineErr error
		switch strings.ToLower(directive) {
		case "route":
			if args == "" {
				lineErr = errors.New("no domains specified for route")
				break
			}
			if route != nil {
				if len(route.Upstreams) == 0 {
					return nil, nil, fmt.Errorf("line %d: no upstreams specified for route %s", routeLine, route.Domains)
				}
				routes = append(routes, route)
			}
			route = &proxy.Route{
				Domains:   strings.Split(args, " "),
				Upstreams: []proxy.Upstream{},
			}
			routeLine = lineNumber
		case "upstream":
			if route == nil {
				lineErr = fmt.Errorf("upstream without route: %s", line)
				break
			}
			route.Upstreams = append(route.Upstreams, proxy.Upstream{Host: args})
		case "header":
			if route == nil {
				lineErr = fmt.Errorf("header without route: %s", line)
				break
			}
			lineErr = parseHeader(args, route)
		case "on_error":
			if route == nil {
				lineErr = fmt.Errorf("on_error without route: %s", line)
				break
			}
			lineErr = parseOnError(args, route)
		case "provider":
			if route == nil {
				lineErr = fmt.Errorf("provider without route: %s", line)
				break
			}
			if route.Provider != "" {
				lineErr = fmt.Errorf("route %s has multiple providers", route.Domains)
				break
			}
			route.Provider = args
		case "fallback":
			if route == nil {
				lineErr = fmt.Errorf("fallback without route: %s", line)
				break
			}
			if fallback != nil {
				lineErr = fmt.Errorf("multiple fallback routes specified: %s and %s", route.Domains, fallback.Domains)
				break
			}
			fallback = route
		case "redirect-to-primary":
			if route == nil {
				lineErr = fmt.Errorf("redirect-to-primary without route: %s", line)
				break
			}
			if route.RedirectToPrimary {
				lineErr = fmt.Errorf("multiple redirect-to-primary options specified in route %s", route.Domains)
				break
			}
			if len(route.Domains) < 2 {
				lineErr = fmt.Errorf("redirect-to-primary specified with only a single domain in route %s", route.Domains)
				break
			}
			route.RedirectToPrimary = true
		case "subject":
			if route == nil {
				lineErr = fmt.Errorf("subject without route: %s", line)
				break
			}
			if args == "" {
				lineErr = fmt.Errorf("no domains specified for subject in route %s", route.Domains)
				break
			}
			route.Subject = append(route.Subject, strings.Split(args, " ")...)
		case "#":
			// Ignore comments
		default:
			if len(line) > 0 {
				lineErr = fmt.Errorf("invalid line: %s", line)
			}
		}

		if lineErr != nil {
			return nil, nil, fmt.Errorf("line %d: %w", lineNumber, lineErr)
		}
	}

	if route != nil {
		if len(route.Upstreams) == 0 {
			return nil, nil, fmt.Errorf("line %d: no upstreams specified for route %s", routeLine, route.Domains)
		}
		routes = append(routes, route)
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	return
}

func parseOnError(args string, route *proxy.Route) error {
	parts := strings.Fields(args)
	if len(parts) != 2 {
		return fmt.Errorf("invalid on_error line: %s", args)
	}

	status, err := strconv.Atoi(parts[0])
	if err != nil || status < 400 || status > 599 {
		return fmt.Errorf("invalid status code for on_error: %s (must be 400-599)", parts[0])
	}

	host, path, found := strings.Cut(parts[1], "/")
	if host == "" {
		return fmt.Errorf("no upstream specified for on_error: %s", args)
	}
	if strings.ContainsAny(host, "?#") {
		return fmt.Errorf("on_error path must begin with a /: %s", parts[1])
	}

	mapping := proxy.ErrorMapping{
		Status:   status,
		Upstream: host,
	}

	if found && path != "" {
		target, err := url.Parse("/" + path)
		if err != nil || target.RawQuery == "" && strings.Contains(path, "?") {
			return fmt.Errorf("invalid path for on_error: %s", parts[1])
		}
		mapping.Path = target.Path
		mapping.RawQuery = target.RawQuery
	}

	route.ErrorMappings = append(route.ErrorMappings, mapping)
	return nil
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
			return fmt.Errorf("invalid header replace line: %s", args)
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
