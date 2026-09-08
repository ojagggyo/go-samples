package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"
)

const (
	namespace = "steemit"
	userAgent = "MyDockerHubClient/1.0"
)

// ------------------------------------------------------------
// Docker Hub authentication response
// ------------------------------------------------------------

type AuthResponse struct {
	AccessToken string `json:"access_token"`
}

// ------------------------------------------------------------
// Docker Hub tag
// ------------------------------------------------------------

type Tag struct {
	Name        string `json:"name"`
	LastUpdated string `json:"last_updated"`
}

// ------------------------------------------------------------
// Docker Hub tags response
// ------------------------------------------------------------

type TagsResponse struct {
	Count   int    `json:"count"`
	Next    string `json:"next"`
	Results []Tag  `json:"results"`
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
	req.Header.Set("User-Agent", userAgent)

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
// Get tags
// ------------------------------------------------------------

func getRepositoryTags(
	client *http.Client,
	accessToken string,
	repository string,
) ([]Tag, error) {

	apiURL := fmt.Sprintf(
		"https://hub.docker.com/v2/namespaces/%s/repositories/%s/tags?page_size=100",
		namespace,
		repository,
	)

	var allTags []Tag

	for apiURL != "" {

		fmt.Printf(
			"GET %s\n",
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

		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)

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

		var result TagsResponse

		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf(
				"JSON parse error: %w",
				err,
			)
		}

		allTags = append(
			allTags,
			result.Results...,
		)

		apiURL = result.Next
	}

	return allTags, nil
}

// ------------------------------------------------------------
// Get latest 3 tags
// ------------------------------------------------------------

func getLatestTags(
	client *http.Client,
	accessToken string,
	repository string,
) ([]Tag, error) {

	tags, err := getRepositoryTags(
		client,
		accessToken,
		repository,
	)

	if err != nil {
		return nil, err
	}

	if len(tags) == 0 {
		return nil, fmt.Errorf(
			"%s にタグがありません",
			repository,
		)
	}

	// --------------------------------------------------------
	// Sort by last_updated descending
	// --------------------------------------------------------

	sort.Slice(
		tags,
		func(i, j int) bool {
			return tags[i].LastUpdated > tags[j].LastUpdated
		},
	)

	// --------------------------------------------------------
	// Return maximum 3 tags
	// --------------------------------------------------------

	if len(tags) > 3 {
		tags = tags[:3]
	}

	return tags, nil
}

// ------------------------------------------------------------
// Main
// ------------------------------------------------------------

func main() {

	fmt.Println("========================================")
	fmt.Println(" Docker Hub Repository Checker")
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
	// Repositories
	// --------------------------------------------------------

	repositories := []string{
		"hivemind",
		"jussi",
		"condenser",
		"steem",
	}

	// --------------------------------------------------------
	// Get latest 3 tags
	// --------------------------------------------------------

	results := make(map[string][]Tag)

	for _, repository := range repositories {

		tags, err := getLatestTags(
			client,
			accessToken,
			repository,
		)

		if err != nil {
			fmt.Println()
			fmt.Println("ERROR:")
			fmt.Println(err)
			os.Exit(1)
		}

		results[repository] = tags
	}

	// --------------------------------------------------------
	// Result
	// --------------------------------------------------------

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println(" Latest 3 Tags")
	fmt.Println("========================================")

	for _, repository := range repositories {

		fmt.Printf(
			"%s\n",
			repository,
		)

		for i, tag := range results[repository] {

			fmt.Printf(
				"  %d. %-30s %s\n",
				i+1,
				tag.Name,
				tag.LastUpdated,
			)
		}

		fmt.Println()
	}

	fmt.Println("========================================")
}
