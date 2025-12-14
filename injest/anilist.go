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
	"regexp"
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
const aniListLookup = `
	query ($id: Int!) {
		Page {
			media(id: $id, type: ANIME) {
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

type titleTransform struct {
	match     func(string) bool
	transform func(base, parent string) string
}

var titleTransforms = []titleTransform{
	{
		match: regexp.MustCompile(`^(?i)\s*season\s*\d+\s*$`).MatchString,
		transform: func(base, parent string) string {
			return parent + " " + regexp.MustCompile(`^(?i)\s*season\s*0*`).ReplaceAllString(base, "")
		},
	},
	{
		match: regexp.MustCompile(`\s+S\d+$`).MatchString,
		transform: func(base, parent string) string {
			return regexp.MustCompile(`\s+S(\d+)$`).ReplaceAllString(base, ` $1`)
		},
	},
}

// requestInfo makes a request to AniList and returns the relevant information.
// This handles rate limiting by artificially extending the function runtime.
func (i *Injester) requestInfo(ctx context.Context, absPath string, info *InfoType, force, byID bool) error {
	log := logrus.WithField("directory", absPath)
	if info.AniListID != 0 && !force {
		// We already fetched what we can from AniList, skip.
		return nil
	}
	// We rate limit our calls to once every ten seconds, way more than AniList's
	// stated rate limit of 30 requests per minute.
	timeout := time.After(10 * time.Second)
	err := func() error {
		var input aniListRequest
		if byID && info.AniListID != 0 {
			input = aniListRequest{
				Query: aniListLookup,
				Variables: map[string]any{
					"id": info.AniListID,
				},
			}
			log.WithField("id", info.AniListID).Debug("Requesting info from AniList...")
		} else {
			search := path.Base(absPath)
			for _, transform := range titleTransforms {
				if transform.match(search) {
					dir, base := path.Split(absPath)
					parent := path.Base(dir)
					search = transform.transform(base, parent)
					break
				}
			}
			log.WithField("search", search).Debug("Requesting info from AniList...")
			input = aniListRequest{
				Query: aniListQuery,
				Variables: map[string]any{
					"search": search,
				},
			}
		}
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(input); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, aniListEndpoint, &buf)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Content-Type", "application/json")
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
			coverPath := filepath.Join(absPath, ".cover.jpg")
			needCover := byID && force
			if !needCover {
				if f, err := os.Open(coverPath); errors.Is(err, fs.ErrNotExist) {
					needCover = true
				} else if err == nil {
					_ = f.Close()
				}
			}
			if needCover {
				f, err := os.Create(coverPath)
				if err != nil {
					return err
				}
				defer f.Close()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, media.CoverImage.Medium, http.NoBody)
				if err != nil {
					return err
				}
				req.Header.Set("User-Agent", userAgent)
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
			}
		}

		result, err := getChineseTitle(ctx, media.Id, log)
		if err == nil {
			info.ChineseTitle = result
		} else {
			log.WithError(err).Error("failed to get Chinese title")
		}

		return nil
	}()
	log.WithError(err).WithField("info", info).Debug("Requested info from AniList")
	<-timeout
	return err
}
