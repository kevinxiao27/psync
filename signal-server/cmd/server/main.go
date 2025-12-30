package main

import (
	"log"

	"github.com/kevinxiao27/psync/signal-server/internal/api"
)

func main() {
	log.Println("Starting PSync Signal Server...")

	// Create server instance
	// In a real app, port would be from config/flags
	server := api.NewServer(":8080")

	// Start server
	if err := server.Start(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}

	log.Println("Server execution finished (stub).")
}
