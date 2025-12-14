/*
 * video-listing Copyright (C) 2023 Mook
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mook/video-listing/injest"
	"github.com/mook/video-listing/server"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

func serve(ctx context.Context, mediaDir string, queue injest.Queue) error {
	s := server.NewServer(mediaDir, queue)

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", ":"+os.Getenv("PORT"))
	if err != nil {
		return err
	}
	defer listener.Close()

	fmt.Printf("Listening on %s...\n", listener.Addr())
	if err = http.Serve(listener, s); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func doInjest(ctx context.Context, injester *injest.Injester) error {
	var wg sync.WaitGroup
	var err error
	wg.Go(func() {
		err = injester.Run(ctx)
	})
	wg.Go(func() {
		time.Sleep(time.Millisecond)
		injester.Queue(injest.QueueOptions{
			Directory: ".",
		})
	})
	wg.Wait()
	if err != nil {
		return err
	}
	return nil
}

func run(ctx context.Context) error {
	mediaDir := flag.String("dir", "/media", "listing directory root")
	verbose := flag.Bool("verbose", false, "extra logging")
	flag.Parse()

	if *verbose {
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		logrus.SetLevel(logrus.WarnLevel)
	}

	if info, err := os.Stat(*mediaDir); err != nil {
		return fmt.Errorf("Media directory %s is invalid: %w", *mediaDir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("Media directory %s is not a directory", *mediaDir)
	}

	injester := injest.New(*mediaDir)
	wg, ctx := errgroup.WithContext(ctx)
	wg.Go(func() error {
		return serve(ctx, *mediaDir, injester.Queue)
	})
	wg.Go(func() error {
		return doInjest(ctx, injester)
	})

	if err := wg.Wait(); err != nil {
		return err
	}
	return nil
}

func main() {
	logrus.SetLevel(logrus.TraceLevel)
	if err := run(context.Background()); err != nil {
		logrus.WithError(err).Fatal("process exited")
	}
}
