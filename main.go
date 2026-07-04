package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Bharanidharan2006/rce_sandbox_server/sandbox"
	"github.com/Bharanidharan2006/rce_sandbox_server/ws"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "child" {
		sandbox.RunChild()
	} else if len(os.Args) > 1 && os.Args[1] == "server" {
		http.HandleFunc("/ws", ws.HandleConnection)
		log.Printf("Web Server Started at Port: 8080")
		log.Fatal(http.ListenAndServe(":8080", nil))
	} else {
		log.Fatal("usage: go run main.go child | server")
	}
}
