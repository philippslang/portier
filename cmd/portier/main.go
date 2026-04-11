package main

import (
	"context"
	"log"
	"os"

	"github.com/philippslang/portier"
)

func main() {
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := portier.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	srv, err := portier.NewServer(cfg)
	if err != nil {
		log.Fatalf("Server init error: %v", err)
	}

	if err := srv.Run(context.Background()); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
