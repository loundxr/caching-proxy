package main

import (
	"fmt"
	"os"

	"github.com/loundxr/caching-proxy/internal/config"
)

func main() {
	cfg, err := config.ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Println("flags: %w", err)
		return
	}
	fmt.Println(cfg)
}
