package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	namespace  = "steemit"
	repository = "hivemind"
)

// Docker Hub authentication response
type AuthResponse struct {
	AccessToken string `json:"access_token"`
}

// Docker Hub tag list response
type TagResponse struct {
	Count   int    `json:"count"`
	Next    string `json:"next"`
	Results []Tag  `json:"results"`
}

// Docker Hub tag
type Tag struct {
	Name          string  `json:"name"`
	LastUpdated   string  `json:"last_updated"`
	TagLastPushed string  `json:"tag_last_pushed"`
	FullSize      int64   `json:"full_size"`
	Images        []Image `json:"images"`
}

// Docker image
type Image struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Digest       string `json:"digest"`
	Size         int64  `json:"size"`
}

// ------------------------------------------------------------
// Docker Hub authentication
// ------------------------------------------------------------
func getAccessToken(client *http.Client) (string, error) {

	username := os.Getenv("DOCKERHUB_USERNAME")
	pat := os.Getenv("DOCKERHUB_PAT")

	if username == "" {
		return "", fmt.Errorf(
			"DOCKERHUB_USERNAME が設定されていません",
		)
	}

	if pat == "" {
		return "", fmt.Errorf(
			"DOCKERHUB_PAT が設定されていません",
		)
	}

	// Docker Hub authentication request
	reqBody := fmt.Sprintf(
		`{"identifier":%q,"secret":%q}`,
		username,
		pat,
	)

	req, err := http.NewRequest(
		http.MethodPost,
		"https://hub.docker.com/v2/auth/token",
		bytes.NewBufferString(reqBody),
	)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf(
			"Docker Hub authentication failed: %s\n%s",
			resp.Status,
			string(body),
		)
	}

	var auth AuthResponse

	if err := json.Unmarshal(body, &auth); err != nil {
		return "", fmt.Errorf(
			"authentication response parse error: %w",
			err,
		)
	}

	if auth.AccessToken == "" {
		return "", fmt.Errorf(
			"access_token が取得できませんでした",
		)
	}

	return auth.AccessToken, nil
}

// ------------------------------------------------------------
// Get all tags
// ------------------------------------------------------------
func getTags(
	client *http.Client,
	accessToken string,
) ([]Tag, error) {

	apiURL := fmt.Sprintf(
		//"https://hub.docker.com/v2/namespaces/%s/repositories/%s/tags?page_size=10",
		"https://hub.docker.com/v2/namespaces/%s/repositories/%s/tags?page_size=3&ordering=last_updated",
		namespace,
		repository,
	)

	var allTags []Tag

	page := 1

	//for apiURL != "" {
	for apiURL != "" && page == 1 {

		fmt.Printf(
			"[%d] GET %s\n",
			page,
			apiURL,
		)

		req, err := http.NewRequest(
			http.MethodGet,
			apiURL,
			nil,
		)
		if err != nil {
			return nil, err
		}

		req.Header.Set(
			"Authorization",
			"Bearer "+accessToken,
		)

		req.Header.Set(
			"Accept",
			"application/json",
		)

		req.Header.Set(
			"User-Agent",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36",
		)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf(
				"Docker Hub API returned %s\n%s",
				resp.Status,
				string(body),
			)
		}

		var result TagResponse

		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf(
				"JSON parse error: %w",
				err,
			)
		}

		fmt.Printf(
			"    %d tags received\n",
			len(result.Results),
		)

		allTags = append(
			allTags,
			result.Results...,
		)

		// Docker Hub API が返した次ページURL
		apiURL = result.Next

		page++
	}

	return allTags, nil
}

// ------------------------------------------------------------
// Main
// ------------------------------------------------------------
func main() {

	fmt.Println("========================================")
	fmt.Println(" Docker Hub Tag Downloader")
	fmt.Println("========================================")
	fmt.Println()

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// --------------------------------------------------------
	// Authentication
	// --------------------------------------------------------

	fmt.Println("Docker Hub authentication...")

	accessToken, err := getAccessToken(client)
	if err != nil {
		fmt.Println()
		fmt.Println("ERROR:")
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println("Authentication successful.")
	fmt.Println()

	// --------------------------------------------------------
	// Get tags
	// --------------------------------------------------------

	fmt.Printf(
		"Repository: %s/%s\n",
		namespace,
		repository,
	)

	fmt.Println("Downloading tags...")
	fmt.Println()

	tags, err := getTags(client, accessToken)
	if err != nil {
		fmt.Println()
		fmt.Println("ERROR:")
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Printf("Total tags: %d\n", len(tags))
	fmt.Println("========================================")
	fmt.Println()

	// --------------------------------------------------------
	// Display
	// --------------------------------------------------------

	for _, tag := range tags {

		fmt.Printf(
			"Tag          : %s\n",
			tag.Name,
		)

		fmt.Printf(
			"Last Updated : %s\n",
			tag.LastUpdated,
		)

		fmt.Printf(
			"Last Pushed  : %s\n",
			tag.TagLastPushed,
		)

		fmt.Printf(
			"Full Size    : %d bytes\n",
			tag.FullSize,
		)

		if len(tag.Images) > 0 {

			fmt.Println("Images:")

			for _, image := range tag.Images {

				fmt.Printf(
					"  OS           : %s\n",
					image.OS,
				)

				fmt.Printf(
					"  Architecture : %s\n",
					image.Architecture,
				)

				fmt.Printf(
					"  Digest       : %s\n",
					image.Digest,
				)

				fmt.Printf(
					"  Size         : %d bytes\n",
					image.Size,
				)
			}
		}

		fmt.Println("----------------------------------------")
	}
}
