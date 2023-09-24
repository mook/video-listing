package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mook/video-listing/pkg/listing"
	"github.com/sirupsen/logrus"
)

var (
	//go:embed res
	resourcesRoot embed.FS
)

func run(ctx context.Context) error {
	resources, err := fs.Sub(resourcesRoot, "res")
	if err != nil {
		return fmt.Errorf("failed to load resources: %w", err)
	}
	db, err := sql.Open("sqlite3", "/config/db.sqlite")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()
	listingConn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get listing database connection: %w", err)
	}
	listingHandler, err := listing.NewListingHandler(ctx, resources, listingConn)
	if err != nil {
		return fmt.Errorf("failed to make listing handler: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/j/", http.StripPrefix("/j/", http.HandlerFunc(listingHandler.ServeJSON)))
	mux.Handle("/v/", http.StripPrefix("/v/", http.HandlerFunc(listingHandler.ServeVideo)))
	mux.Handle("/_/", http.StripPrefix("/_/", http.FileServer(http.FS(resources))))
	fmt.Println("Listening...")
	return http.ListenAndServe(":80", mux)
}

func main() {
	logrus.SetLevel(logrus.TraceLevel)
	if err := run(context.Background()); err != nil {
		logrus.WithError(err).Fatal("process exited")
	}
}
