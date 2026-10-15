package meta

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"sila/internal/database"
)

type Publisher struct {
	client *Client
}

func NewPublisher(client *Client) *Publisher {
	return &Publisher{client: client}
}

type PublishResult struct {
	Success bool   `json:"success"`
	PostID  string `json:"post_id,omitempty"`
	PostURL string `json:"post_url,omitempty"`
	Error   string `json:"error,omitempty"`
}

// PublishPost delegates to platform specific publisher
func (p *Publisher) PublishPost(acc *database.Account, item *database.PostQueueItem) PublishResult {
	switch acc.Platform {
	case "facebook":
		return p.publishFacebook(acc, item)
	case "instagram":
		return p.publishInstagram(acc, item)
	case "threads":
		return p.publishThreads(acc, item)
	default:
		return PublishResult{Success: false, Error: fmt.Sprintf("Platform %s tidak didukung oleh Meta publisher", acc.Platform)}
	}
}

// publishFacebook publishes to a Facebook Page feed or photo/video
func (p *Publisher) publishFacebook(acc *database.Account, item *database.PostQueueItem) PublishResult {
	var mediaList []string
	if item.MediaPaths != "" {
		_ = json.Unmarshal([]byte(item.MediaPaths), &mediaList)
	}

	pageID := acc.AccountID
	token := acc.AccessToken

	// If there are photos
	if len(mediaList) > 0 && (item.PostType == "image" || item.PostType == "carousel") {
		params := url.Values{}
		params.Set("url", mediaList[0])
		params.Set("caption", item.Caption)

		endpoint := fmt.Sprintf("/%s/photos", pageID)
		body, err := p.client.Post(endpoint, params, token)
		if err != nil {
			return PublishResult{Success: false, Error: err.Error()}
		}

		var res struct {
			ID     string `json:"id"`
			PostID string `json:"post_id"`
		}
		json.Unmarshal(body, &res)
		id := res.PostID
		if id == "" {
			id = res.ID
		}
		return PublishResult{
			Success: true,
			PostID:  id,
			PostURL: fmt.Sprintf("https://www.facebook.com/%s", id),
		}
	}

	// Text post on page feed
	params := url.Values{}
	params.Set("message", item.Caption)

	endpoint := fmt.Sprintf("/%s/feed", pageID)
	body, err := p.client.Post(endpoint, params, token)
	if err != nil {
		return PublishResult{Success: false, Error: err.Error()}
	}

	var res struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &res)
	return PublishResult{
		Success: true,
		PostID:  res.ID,
		PostURL: fmt.Sprintf("https://www.facebook.com/%s", res.ID),
	}
}

// publishInstagram publishes to Instagram Business (Container -> Publish 2-step flow)
func (p *Publisher) publishInstagram(acc *database.Account, item *database.PostQueueItem) PublishResult {
	var mediaList []string
	if item.MediaPaths != "" {
		_ = json.Unmarshal([]byte(item.MediaPaths), &mediaList)
	}

	if len(mediaList) == 0 {
		return PublishResult{Success: false, Error: "Instagram membutuhkan minimal 1 media gambar atau video"}
	}

	igUserID := acc.AccountID
	token := acc.AccessToken

	// Step 1: Create Media Container
	containerParams := url.Values{}
	containerParams.Set("caption", item.Caption)

	mediaURL := mediaList[0]
	if item.PostType == "reel" || isVideo(mediaURL) {
		containerParams.Set("media_type", "REELS")
		containerParams.Set("video_url", mediaURL)
	} else {
		containerParams.Set("image_url", mediaURL)
	}

	endpoint := fmt.Sprintf("/%s/media", igUserID)
	body, err := p.client.Post(endpoint, containerParams, token)
	if err != nil {
		return PublishResult{Success: false, Error: fmt.Sprintf("Gagal membuat IG media container: %v", err)}
	}

	var containerRes struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &containerRes); err != nil || containerRes.ID == "" {
		return PublishResult{Success: false, Error: "ID Container Instagram tidak valid"}
	}

	creationID := containerRes.ID

	// If video, wait for container status to be READY
	if item.PostType == "reel" || isVideo(mediaURL) {
		time.Sleep(5 * time.Second)
	}

	// Step 2: Publish Media Container
	pubParams := url.Values{}
	pubParams.Set("creation_id", creationID)

	pubEndpoint := fmt.Sprintf("/%s/media_publish", igUserID)
	pubBody, err := p.client.Post(pubEndpoint, pubParams, token)
	if err != nil {
		return PublishResult{Success: false, Error: fmt.Sprintf("Gagal mempublikasikan media IG: %v", err)}
	}

	var pubRes struct {
		ID string `json:"id"`
	}
	json.Unmarshal(pubBody, &pubRes)

	return PublishResult{
		Success: true,
		PostID:  pubRes.ID,
		PostURL: fmt.Sprintf("https://www.instagram.com/p/%s", pubRes.ID),
	}
}

// publishThreads publishes text or media to Threads
func (p *Publisher) publishThreads(acc *database.Account, item *database.PostQueueItem) PublishResult {
	threadsUserID := acc.AccountID
	token := acc.AccessToken

	params := url.Values{}
	params.Set("text", item.Caption)
	params.Set("media_type", "TEXT")

	var mediaList []string
	if item.MediaPaths != "" {
		_ = json.Unmarshal([]byte(item.MediaPaths), &mediaList)
	}
	if len(mediaList) > 0 {
		params.Set("image_url", mediaList[0])
		params.Set("media_type", "IMAGE")
	}

	// Step 1: Create Container
	endpoint := fmt.Sprintf("/%s/threads", threadsUserID)
	body, err := p.client.Post(endpoint, params, token)
	if err != nil {
		return PublishResult{Success: false, Error: fmt.Sprintf("Gagal membuat postingan Threads: %v", err)}
	}

	var containerRes struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &containerRes)
	if containerRes.ID == "" {
		return PublishResult{Success: false, Error: "ID Threads container tidak ditemukan"}
	}

	// Step 2: Publish
	pubParams := url.Values{}
	pubParams.Set("creation_id", containerRes.ID)

	pubEndpoint := fmt.Sprintf("/%s/threads_publish", threadsUserID)
	pubBody, err := p.client.Post(pubEndpoint, pubParams, token)
	if err != nil {
		return PublishResult{Success: false, Error: fmt.Sprintf("Gagal mempublish postingan Threads: %v", err)}
	}

	var pubRes struct {
		ID string `json:"id"`
	}
	json.Unmarshal(pubBody, &pubRes)

	return PublishResult{
		Success: true,
		PostID:  pubRes.ID,
		PostURL: fmt.Sprintf("https://www.threads.net/post/%s", pubRes.ID),
	}
}

func isVideo(urlStr string) bool {
	return len(urlStr) > 4 && (urlStr[len(urlStr)-4:] == ".mp4" || urlStr[len(urlStr)-4:] == ".mov")
}
