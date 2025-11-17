package injest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

const aniListEndpoint = "https://graphql.anilist.co/"
const aniListQuery = `
	query ($search: String!) {
		Page {
			media(search: $search, type: ANIME) {
				id
				title {
					romaji
					english
					native
				}
				coverImage {
					medium
				}
			}
		}
	}
`

type aniListRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type aniListResponseMedia struct {
	Id    int `json:"id"`
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
	CoverImage struct {
		Medium string `json:"medium"`
	} `json:"coverImage"`
}
type aniListResponse struct {
	Data struct {
		Page struct {
			Media []aniListResponseMedia `json:"media"`
		}
	} `json:"data"`
}

// requestInfo makes a request to AniList and returns the relevant information.
// This handles rate limiting by artificially extending the function runtime.
// Returns whether any changes were made.
func (i *Injester) requestInfo(ctx context.Context, directory string, info *InfoType) error {
	if info.AniListID != 0 {
		// We already fetched what we can from AniList, skip.
		return nil
	}
	// We rate limit our calls to once every ten seconds, way more than AniList's
	// stated rate limit of 30 requests per minute.
	timeout := time.After(10 * time.Second)
	log := logrus.WithField("directory", directory)
	err := func() error {
		log.Debug("Requesting info from AniList...")
		input := aniListRequest{
			Query: aniListQuery,
			Variables: map[string]any{
				"search": path.Base(directory),
			},
		}
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(input); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, aniListEndpoint, &buf)
		if err != nil {
			return err
		}
		req.Header.Add("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			var body bytes.Buffer
			if resp.Body != nil {
				_, _ = io.Copy(&body, resp.Body)
			}
			return fmt.Errorf("Invalid HTTP status %d: %s", resp.StatusCode, body.String())
		}
		if resp.Body == nil {
			return fmt.Errorf("Failed to get response body")
		}
		defer resp.Body.Close()
		var output aniListResponse
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			return err
		}
		logrus.WithField("response", output).Debug("Got response")
		info.changed = true // At this point, we either mark it as not found or save the ID
		if len(output.Data.Page.Media) < 1 {
			// No response
			info.AniListID = -1 // Don't request info about this media again.
			return nil
		}
		media := output.Data.Page.Media[0]
		info.AniListID = media.Id
		if media.Title.English != "" {
			info.EnglishTitle = media.Title.English
		}
		if media.Title.Native != "" {
			info.NativeTitle = media.Title.Native
		}
		if media.CoverImage.Medium != "" {
			coverPath := filepath.Join(i.root, directory, ".cover.jpg")
			if f, err := os.Open(coverPath); errors.Is(err, fs.ErrNotExist) {
				f, err := os.Create(coverPath)
				if err != nil {
					return err
				}
				defer f.Close()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, media.CoverImage.Medium, http.NoBody)
				if err != nil {
					return err
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return err
				}
				if resp.StatusCode != http.StatusOK || resp.Body == nil {
					return fmt.Errorf("Failed to fetch cover image")
				}
				defer resp.Body.Close()
				if _, err := io.Copy(f, resp.Body); err != nil {
					return err
				}
			} else {
				_ = f.Close()
			}
		}
		return nil
	}()
	log.WithError(err).WithField("info", info).Debug("Requested info from AniList")
	<-timeout
	return err
}
