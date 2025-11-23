package injest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/sirupsen/logrus"
)

const (
	bangumiURL       = "https://api.bgm.tv/v0/subjects/%s"
	bahamutURL       = "https://acg.gamer.com.tw/acgDetail.php?s=%s"
	wikiDataEndpoint = "https://query.wikidata.org/sparql"
	wikiDataQuery    = `
		SELECT ?label ?bangumi ?bahamut WHERE {
			?item p:P8729/ps:P8729 "%d".
			OPTIONAL {
				?item rdfs:label ?label.
				FILTER(LANG(?label) = "zh")
			}
			OPTIONAL {
				?item p:P5732/ps:P5732 ?bangumi.
			}
			OPTIONAL {
				?item p:P6367/ps:P6367 ?bahamut.
			}
		}
	`
)

var (
	bahamutMatcher = regexp.MustCompile(`<h1>(.*?)</h1>`)
)

type wikiDataResponse struct {
	Results struct {
		Bindings []map[string]struct {
			Value string `json:"value"`
		} `json:"bindings"`
	} `json:"results"`
}

// Get the Chinese title, given the AniList ID.
func getChineseTitle(ctx context.Context, aniListID int, log *logrus.Entry) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wikiDataEndpoint, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/sparql-results+json")
	q := req.URL.Query()
	q.Set("query", fmt.Sprintf(wikiDataQuery, aniListID))
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	log.WithField("url", req.URL).Debug("Sent wikidata request")
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get wikidata response: %d (%s)", resp.StatusCode, resp.Status)
	}
	if resp.Body == nil {
		return "", fmt.Errorf("failed to get wikidata response body")
	}
	defer resp.Body.Close()

	var output wikiDataResponse
	if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
		return "", fmt.Errorf("failed to parse wikidata response: %w", err)
	}
	log.Debugf("Got WikiData resposne: %+v", output)
	for _, binding := range output.Results.Bindings {
		if binding["label"].Value != "" {
			return binding["label"].Value, nil
		}
		if binding["bangumi"].Value != "" {
			result, err := getBangumiTitle(ctx, binding["bangumi"].Value)
			if err == nil {
				return result, nil
			}
			log.WithError(err).Error("failed to title from bangumi")
		}
		if binding["bahamut"].Value != "" {
			result, err := getBahamutTitle(ctx, binding["bahamut"].Value)
			if err == nil {
				return result, nil
			}
			log.WithError(err).Error("failed to title from bahamut")
		}
	}

	return "", fmt.Errorf("failed to get Chinese title")
}

// Get the Chinese title given the Bangumi id
func getBangumiTitle(ctx context.Context, bahamutID string) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, fmt.Sprintf(bangumiURL, bahamutID), http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s got unexpected status %d (%s)", req.URL, resp.StatusCode, resp.Status)
	}
	if resp.Body == nil {
		return "", fmt.Errorf("%s did not get body", req.URL)
	}
	defer resp.Body.Close()

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	if value, ok := data["name_cn"].(string); ok && value != "" {
		return value, nil
	}

	return "", fmt.Errorf("%s did not include title", req.URL)
}

func getBahamutTitle(ctx context.Context, bahamutID string) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, fmt.Sprintf(bahamutURL, bahamutID), http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s got unexpected status %d (%s)", req.URL, resp.StatusCode, resp.Status)
	}
	if resp.Body == nil {
		return "", fmt.Errorf("%s did not get body", req.URL)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		match := bahamutMatcher.FindStringSubmatch(scanner.Text())
		if len(match) > 1 {
			return match[1], nil
		}
	}

	return "", fmt.Errorf("%s did not include title", req.URL)
}
