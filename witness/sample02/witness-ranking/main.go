package main

import (
	"fmt"
	"log"
	"os"
)

func usage() {
	fmt.Println(`Usage:
  witness-ranking update
  witness-ranking serve

Environment variables:
  DATA_DIR       default: /data/php
  RPC_URL        default: https://api.steememory.com
  LISTEN_ADDR    default: :8080
  WEB_DIR        default: ./web
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cfg := LoadConfig()

	switch os.Args[1] {
	case "update":
		if err := RunUpdate(cfg); err != nil {
			log.Fatal(err)
		}
	case "serve":
		if err := RunServer(cfg); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
		os.Exit(1)
	}
}
