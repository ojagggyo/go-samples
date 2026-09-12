package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type youtubeAPIError struct {
	Code int
	Body string
}

func (e *youtubeAPIError) Error() string {
	return fmt.Sprintf("YouTube API returned %d %s: %s", e.Code, http.StatusText(e.Code), strings.TrimSpace(e.Body))
}

func retryable(err error) bool {
	var api *youtubeAPIError
	if errors.As(err, &api) {
		if api.Code == 429 || api.Code == 500 || api.Code == 502 || api.Code == 503 || api.Code == 504 {
			return true
		}
		if api.Code != 409 {
			return false
		}
		var body struct {
			Error struct {
				Status string
				Errors []struct{ Reason string }
			}
		}
		if json.Unmarshal([]byte(api.Body), &body) != nil {
			return false
		}
		if body.Error.Status == "ABORTED" {
			return true
		}
		for _, detail := range body.Error.Errors {
			if detail.Reason == "SERVICE_UNAVAILABLE" {
				return true
			}
		}
		return false
	}
	var network net.Error
	return errors.As(err, &network)
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func playlistVideos(ctx context.Context, client *http.Client, playlistID, videoID string) (map[string]bool, error) {
	ids := make(map[string]bool)
	q := url.Values{"part": {"snippet"}, "playlistId": {playlistID}, "maxResults": {"50"}}
	if videoID != "" {
		q.Set("videoId", videoID)
	}
	for {
		var response struct {
			NextPageToken string
			Items         []struct {
				Snippet struct{ ResourceID struct{ VideoID string } }
			}
		}
		if err := youtubeRequest(ctx, client, http.MethodGet, "https://www.googleapis.com/youtube/v3/playlistItems?"+q.Encode(), "", &response); err != nil {
			return nil, err
		}
		for _, item := range response.Items {
			ids[item.Snippet.ResourceID.VideoID] = true
		}
		if response.NextPageToken == "" {
			return ids, nil
		}
		q.Set("pageToken", response.NextPageToken)
	}
}

func addVideoWithRetry(ctx context.Context, client *http.Client, playlistID, videoID string, wait func(context.Context, time.Duration) error) error {
	err := addVideo(ctx, client, playlistID, videoID)
	// POSTの結果が不明なときは、再送前に追加済みか確認する。
	for retry := 0; err != nil && retryable(err) && retry < 5; retry++ {
		delay := time.Second * time.Duration(1<<retry)
		fmt.Fprintf(os.Stderr, "Temporary API error for %s; checking before retry %d/5 in %s\n", videoID, retry+1, delay)
		if waitErr := wait(ctx, delay); waitErr != nil {
			return waitErr
		}
		present, checkErr := playlistVideos(ctx, client, playlistID, videoID)
		if checkErr != nil {
			if !retryable(checkErr) {
				return fmt.Errorf("could not verify previous insertion: %w", checkErr)
			}
			err = checkErr
			continue
		}
		if present[videoID] {
			return nil
		}
		err = addVideo(ctx, client, playlistID, videoID)
	}
	return err
}
