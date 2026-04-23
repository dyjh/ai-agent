package main

import (
	"flag"
	"log"

	"local-agent/internal/openapi"
)

func main() {
	out := flag.String("out", "docs/openapi.json", "output OpenAPI JSON path")
	flag.Parse()
	if err := openapi.WriteFile(*out); err != nil {
		log.Fatalf("write openapi: %v", err)
	}
}
