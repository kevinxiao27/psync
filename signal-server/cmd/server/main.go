package main

import (
	"flag"
	"log"

	"github.com/kevinxiao27/psync/signal-server/internal/api"
)

func main() {
	log.Println("Starting PSync Signal Server...")

	port := flag.String("port", ":8080", "The port to listen on")
	flag.Parse()

	server := api.NewServer(":" + *port)

	if err := server.Start(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}

	log.Println("Server execution finished (stub).")
}
