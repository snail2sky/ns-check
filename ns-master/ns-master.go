package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type NameserversResponse struct {
	Nameservers []string `json:"nameservers"`
	EndpointURL string   `json:"endpointURL"`
}

var (
	port        int
	endpoint    string
	endpointURL string
	nameservers string
)

func init() {
	flag.IntVar(&port, "port", 5353, "Port number for the server")
	flag.StringVar(&endpoint, "endpoint", "/nameservers", "Endpoint URL for fetching nameservers")
	flag.StringVar(&endpointURL, "endpoint-url", "http://127.0.0.1:5353/nameservers", "Endpoint url will used by client")
	flag.StringVar(&nameservers, "nameservers", "8.8.8.8,8.8.4.4,1.1.1.1", "Comma-separated list of nameservers")
	flag.Parse()
}

func main() {
	http.HandleFunc(endpoint, nameserversHandler)
	addr := fmt.Sprintf(":%d", port)
	log.Printf("Server listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func nameserversHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s send a request", r.RemoteAddr)
	var response NameserversResponse
	response.Nameservers = strings.Split(nameservers, ",")
	response.EndpointURL = endpointURL

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func parseNameserverList(nameserverList string) []string {
	nameservers := make([]string, 0)
	for _, ns := range splitAndTrim(nameserverList, ",") {
		nameservers = append(nameservers, ns)
	}
	return nameservers
}

func splitAndTrim(str, sep string) []string {
	values := make([]string, 0)
	items := strings.Split(str, sep)
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}
