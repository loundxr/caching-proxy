package config

import (
	"flag"
	"fmt"
	"net/url"
)

type Config struct {
	Port   int
	Origin url.URL
}

func ParseFlags(args []string) (Config, error) {
	fs := flag.NewFlagSet("caching-proxy", flag.ContinueOnError)

	port := fs.Int("port", 3000, "port to listen on")
	origin := fs.String("origin", "", "origin server URL")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	if *origin == "" {
		return Config{}, fmt.Errorf("'origin' arg is required")
	}

	originURL, err := url.Parse(*origin)
	if err != nil ||
		(originURL.Scheme != "http" && originURL.Scheme != "https") ||
		originURL.Host == "" {
		return Config{}, fmt.Errorf("invalid origin format: %q: want an absolute http(s) URL", *origin)
	}

	return Config{
		Port:   *port,
		Origin: *originURL,
	}, nil
}
