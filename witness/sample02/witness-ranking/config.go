package main

import "os"

type Config struct {
	DataDir    string
	RPCURL     string
	ListenAddr string
	WebDir     string
}

func getenv(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		DataDir:    getenv("DATA_DIR", "/data/php"),
		RPCURL:     getenv("RPC_URL", "https://api.steememory.com"),
		ListenAddr: getenv("LISTEN_ADDR", ":8080"),
		WebDir:     getenv("WEB_DIR", "./web"),
	}
}
