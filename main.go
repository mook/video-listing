package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/mook/video-listing/pkg/listing"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", listing.HandleListing)
	fmt.Println("Listening...")
	err := http.ListenAndServe(":80", mux)
	log.Fatal(err)
}
