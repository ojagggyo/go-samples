// playlist-create creates a YouTube playlist from multiple video URLs.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const youtubeScope = "https://www.googleapis.com/auth/youtube.force-ssl"

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type credentialsFile struct {
	Installed *oauthClient `json:"installed"`
	Web       *oauthClient `json:"web"`
}

type oauthClient struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
}

type playlistResponse struct {
	ID string `json:"id"`
}

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func main() {
	var urls stringList
	credentialsPath := flag.String("credentials", "credentials.json", "OAuth client JSON downloaded from Google Cloud (optional when -client-id is used)")
	clientID := flag.String("client-id", "", "OAuth desktop client ID copied from Google Cloud")
	tokenPath := flag.String("token", "token.json", "path used to cache the OAuth token")
	urlsFile := flag.String("urls-file", "", "text file containing one YouTube URL per line")
	title := flag.String("title", "", "playlist title (required)")
	description := flag.String("description", "", "playlist description")
	privacy := flag.String("privacy", "private", "private, unlisted, or public")
	flag.Var(&urls, "url", "YouTube video URL; repeat this flag for multiple URLs")
	flag.Parse()

	if *title == "" || (*privacy != "private" && *privacy != "unlisted" && *privacy != "public") {
		flag.Usage()
		os.Exit(2)
	}
	if *urlsFile != "" {
		fileURLs, err := urlsFromFile(*urlsFile)
		if err != nil {
			fatal(err)
		}
		urls = append(urls, fileURLs...)
	}

	videoIDs, invalid := uniqueVideoIDs(urls)
	if len(invalid) > 0 {
		fatal(fmt.Errorf("invalid YouTube video URL(s):\n%s", strings.Join(invalid, "\n")))
	}
	if len(videoIDs) == 0 {
		fatal(errors.New("at least one -url or one URL in -urls-file is required"))
	}

	ctx := context.Background()
	config, err := loadOAuthConfig(*credentialsPath, *clientID)
	if err != nil {
		fatal(err)
	}
	client, err := authenticatedClient(ctx, config, *tokenPath)
	if err != nil {
		fatal(err)
	}

	playlistID, err := createPlaylist(ctx, client, *title, *description, *privacy)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("Created playlist: https://www.youtube.com/playlist?list=%s\n", playlistID)

	for i, videoID := range videoIDs {
		if err := addVideo(ctx, client, playlistID, videoID); err != nil {
			fatal(fmt.Errorf("playlist was created (%s), but adding item %d (%s) failed: %w", playlistID, i+1, videoID, err))
		}
		fmt.Printf("Added %d/%d: https://www.youtube.com/watch?v=%s\n", i+1, len(videoIDs), videoID)
	}
}

func urlsFromFile(filename string) ([]string, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			result = append(result, line)
		}
	}
	return result, nil
}

func uniqueVideoIDs(urls []string) (ids, invalid []string) {
	seen := make(map[string]bool)
	for _, rawURL := range urls {
		id, err := videoID(rawURL)
		if err != nil {
			invalid = append(invalid, rawURL)
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, invalid
}

func videoID(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("not an absolute URL")
	}
	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	var id string
	switch host {
	case "youtu.be":
		id = path.Base(strings.TrimSuffix(u.Path, "/"))
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else if len(parts) >= 2 && (parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live") {
			id = parts[1]
		}
	}
	if !videoIDPattern.MatchString(id) {
		return "", errors.New("video ID is missing or invalid")
	}
	return id, nil
}

func loadOAuthConfig(filename, clientID string) (*oauth2.Config, error) {
	if clientID != "" {
		return &oauth2.Config{ClientID: clientID, Endpoint: google.Endpoint, Scopes: []string{youtubeScope}}, nil
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	var credentials credentialsFile
	if err := json.Unmarshal(b, &credentials); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	c := credentials.Installed
	if c == nil {
		c = credentials.Web
	}
	if c == nil || c.ClientID == "" {
		return nil, errors.New("credentials must contain an installed or web OAuth client; alternatively specify -client-id")
	}
	return &oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: google.Endpoint, Scopes: []string{youtubeScope}}, nil
}

func authenticatedClient(ctx context.Context, config *oauth2.Config, tokenFilename string) (*http.Client, error) {
	if b, err := os.ReadFile(tokenFilename); err == nil {
		var token oauth2.Token
		if err := json.Unmarshal(b, &token); err != nil {
			return nil, fmt.Errorf("parse cached token: %w", err)
		}
		return config.Client(ctx, &token), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	token, err := authorizeInBrowser(ctx, config)
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(tokenFilename, b, 0600); err != nil {
		return nil, fmt.Errorf("save OAuth token: %w", err)
	}
	return config.Client(ctx, token), nil
}

func authorizeInBrowser(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	config.RedirectURL = "http://" + listener.Addr().String() + "/oauth2/callback"

	stateBytes := make([]byte, 24)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, err
	}
	state := hex.EncodeToString(stateBytes)
	verifier := oauth2.GenerateVerifier()
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/callback" || r.URL.Query().Get("state") != state {
			http.Error(w, "invalid OAuth callback", http.StatusBadRequest)
			return
		}
		if e := r.URL.Query().Get("error"); e != "" {
			errCh <- fmt.Errorf("OAuth authorization failed: %s", e)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- errors.New("OAuth callback did not include a code")
			return
		}
		fmt.Fprint(w, "Authorization completed. You may close this tab and return to the terminal.")
		codeCh <- code
	})}
	go server.Serve(listener)
	defer server.Close()

	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier))
	fmt.Println("Open this URL in a browser and authorize access:")
	fmt.Println(authURL)
	if err := openBrowser(authURL); err != nil {
		fmt.Println("Could not open a browser automatically; open the URL above manually.")
	}

	select {
	case code := <-codeCh:
		return config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Minute):
		return nil, errors.New("OAuth authorization timed out")
	}
}

func openBrowser(target string) error {
	if os.PathSeparator == '\\' {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	}
	return errors.New("automatic browser opening is not configured for this OS")
}

func createPlaylist(ctx context.Context, client *http.Client, title, description, privacy string) (string, error) {
	body := map[string]any{"snippet": map[string]string{"title": title, "description": description}, "status": map[string]string{"privacyStatus": privacy}}
	var response playlistResponse
	if err := youtubePost(ctx, client, "https://www.googleapis.com/youtube/v3/playlists?part=snippet,status", body, &response); err != nil {
		return "", err
	}
	return response.ID, nil
}

func addVideo(ctx context.Context, client *http.Client, playlistID, videoID string) error {
	body := map[string]any{"snippet": map[string]any{"playlistId": playlistID, "resourceId": map[string]string{"kind": "youtube#video", "videoId": videoID}}}
	return youtubePost(ctx, client, "https://www.googleapis.com/youtube/v3/playlistItems?part=snippet", body, nil)
}

func youtubePost(ctx context.Context, client *http.Client, endpoint string, body any, result any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("YouTube API returned %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	if result != nil {
		return json.Unmarshal(responseBody, result)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}
