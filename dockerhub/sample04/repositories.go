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

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36"

// ------------------------------------------------------------
// Target Docker Hub namespaces
// ------------------------------------------------------------

var namespaces = []string{
	"steemit",
	"moeckisteem",
	"ojagggyo",
	"justyy",
}

// ------------------------------------------------------------
// Docker Hub authentication response
// ------------------------------------------------------------

type AuthResponse struct {
	AccessToken string `json:"access_token"`
}

// ------------------------------------------------------------
// Docker Hub repository
// ------------------------------------------------------------

type Repository struct {
	Name        string `json:"name"`
	LastUpdated string `json:"last_updated"`
}

// ------------------------------------------------------------
// Docker Hub repositories response
// ------------------------------------------------------------

type RepositoriesResponse struct {
	Count   int          `json:"count"`
	Next    string       `json:"next"`
	Results []Repository `json:"results"`
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
// Get latest 3 repositories for a namespace
// ------------------------------------------------------------

func getLatestRepositories(
	client *http.Client,
	accessToken string,
	namespace string,
) ([]Repository, error) {

	apiURL := fmt.Sprintf(
		"https://hub.docker.com/v2/namespaces/%s/repositories?page_size=3&ordering=-last_updated",
		namespace,
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
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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

	var result RepositoriesResponse

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf(
			"JSON parse error: %w",
			err,
		)
	}

	if len(result.Results) == 0 {
		return nil, fmt.Errorf(
			"%s にリポジトリがありません",
			namespace,
		)
	}

	// API に page_size=3 と ordering=-last_updated を指定しているため、
	// Results は最終更新日の新しい順に最大3件になる。
	if len(result.Results) > 3 {
		result.Results = result.Results[:3]
	}

	return result.Results, nil
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
	// Process namespaces
	// --------------------------------------------------------

	for _, namespace := range namespaces {

		repositories, err := getLatestRepositories(
			client,
			accessToken,
			namespace,
		)

		if err != nil {
			fmt.Println()
			fmt.Printf(
				"%s: ERROR: %v\n",
				namespace,
				err,
			)
			continue
		}

		// ----------------------------------------------------
		// Result
		// ----------------------------------------------------

		fmt.Println()
		fmt.Println("========================================")
		fmt.Printf(" Namespace: %s\n", namespace)
		fmt.Println("========================================")

		for _, repository := range repositories {

			fmt.Printf(
				"%s %s\n",
				repository.Name,
				repository.LastUpdated,
			)
		}
	}

	fmt.Println()
	fmt.Println("========================================")
}
