package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/mook/video-listing/pkg/listing"
)

var (
	//go:embed res
	resourcesRoot embed.FS
)

func main() {
	resources, err := fs.Sub(resourcesRoot, "res")
	if err != nil {
		panic("failed to load resources")
	}
	listingHandler, err := listing.NewListingHandler(resources)
	if err != nil {
		panic("failed to make listing handler")
	}
	mux := http.NewServeMux()
	mux.Handle("/", listingHandler)
	mux.Handle("/_/", http.StripPrefix("/_/", http.FileServer(http.FS(resources))))
	fmt.Println("Listening...")
	err = http.ListenAndServe(":80", mux)
	log.Fatal(err)
}
