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
	namespace = "steemit"
)

// ------------------------------------------------------------
// Docker Hub authentication response
// ------------------------------------------------------------

type AuthResponse struct {
	AccessToken string `json:"access_token"`
}

// ------------------------------------------------------------
// Docker Hub repository response
// ------------------------------------------------------------

type RepositoryResponse struct {
	LastUpdated string `json:"last_updated"`
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
	req.Header.Set("User-Agent", "MyDockerHubClient/1.0")

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
// Get repository last updated
// ------------------------------------------------------------

func getRepositoryLastUpdated(
	client *http.Client,
	accessToken string,
	repository string,
) (string, error) {

	apiURL := fmt.Sprintf(
		"https://hub.docker.com/v2/namespaces/%s/repositories/%s",
		namespace,
		repository,
	)

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
		return "", err
	}

	req.Header.Set(
		"Authorization",
		"Bearer "+accessToken,
	)

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MyDockerHubClient/1.0")

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
			"Docker Hub API returned %s\n%s",
			resp.Status,
			string(body),
		)
	}

	var result RepositoryResponse

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf(
			"JSON parse error: %w",
			err,
		)
	}

	if result.LastUpdated == "" {
		return "", fmt.Errorf(
			"%s の last_updated が取得できませんでした",
			repository,
		)
	}

	return result.LastUpdated, nil
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
	// Get last updated
	// --------------------------------------------------------

	results := make(map[string]string, len(repositories))

	for _, repository := range repositories {

		lastUpdated, err := getRepositoryLastUpdated(
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

		results[repository] = lastUpdated
	}

	// --------------------------------------------------------
	// Result
	// --------------------------------------------------------

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println(" Repository Last Updated")
	fmt.Println("========================================")

	for _, repository := range repositories {
		fmt.Printf(
			"%-10s : %s\n",
			repository,
			results[repository],
		)
	}

	fmt.Println("========================================")
}
