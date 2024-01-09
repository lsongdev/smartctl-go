package main

import (
	"log"
	"net/http"
)

func main() {
	server, err := NewServer()
	if err != nil {
		log.Fatal(err)
	}
	go server.StartScheduler()
	http.HandleFunc("/", server.IndexView)
	http.HandleFunc("/report", server.ReportView)
	http.ListenAndServe(server.config.Listen, nil)
}
