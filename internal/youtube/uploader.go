package youtube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sila/internal/database"
)

type Uploader struct {
	client   *Client
	oauthMgr *OAuthManager
}

func NewUploader(client *Client, oauthMgr *OAuthManager) *Uploader {
	return &Uploader{
		client:   client,
		oauthMgr: oauthMgr,
	}
}

type UploadResult struct {
	Success bool   `json:"success"`
	VideoID string `json:"video_id,omitempty"`
	URL     string `json:"url,omitempty"`
	Error   string `json:"error,omitempty"`
}

// UploadVideo handles video or shorts upload to YouTube
func (u *Uploader) UploadVideo(acc *database.Account, item *database.PostQueueItem) UploadResult {
	// Refresh token if needed
	if acc.TokenExpiresAt > 0 && time.Now().Unix() >= (acc.TokenExpiresAt-300) {
		newToken, err := u.oauthMgr.RefreshAccessToken(acc)
		if err != nil {
			return UploadResult{Success: false, Error: "Gagal me-refresh token YouTube: " + err.Error()}
		}
		acc.AccessToken = newToken
	}

	var mediaList []string
	if item.MediaPaths != "" {
		_ = json.Unmarshal([]byte(item.MediaPaths), &mediaList)
	}

	if len(mediaList) == 0 {
		return UploadResult{Success: false, Error: "Media video tidak ditemukan dalam payload"}
	}

	videoURL := mediaList[0]

	// 1. Download video data stream
	resp, err := http.Get(videoURL)
	if err != nil {
		return UploadResult{Success: false, Error: fmt.Sprintf("Gagal mengunduh media video dari URL: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return UploadResult{Success: false, Error: fmt.Sprintf("Gagal mengunduh media video: HTTP %d", resp.StatusCode)}
	}

	videoBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return UploadResult{Success: false, Error: "Gagal membaca bytes media video: " + err.Error()}
	}

	title := item.Title
	if title == "" {
		// Use first line of caption or default
		lines := strings.Split(item.Caption, "\n")
		if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
			title = strings.TrimSpace(lines[0])
			if len(title) > 95 {
				title = title[:95]
			}
		} else {
			title = fmt.Sprintf("Video %s", time.Now().Format("02 Jan 2006"))
		}
	}

	description := item.Caption
	if item.PostType == "youtube_short" && !strings.Contains(strings.ToLower(title), "#shorts") && !strings.Contains(strings.ToLower(description), "#shorts") {
		description = strings.TrimSpace(description + "\n\n#Shorts")
	}

	// 2. Initiate Resumable Upload
	metadata := map[string]interface{}{
		"snippet": map[string]interface{}{
			"title":       title,
			"description": description,
			"categoryId":  "22", // People & Blogs
		},
		"status": map[string]interface{}{
			"privacyStatus":           "public",
			"selfDeclaredMadeForKids": false,
		},
	}

	metaJSON, _ := json.Marshal(metadata)

	initReq, err := http.NewRequest(http.MethodPost, "https://www.googleapis.com/upload/youtube/v3/videos?uploadType=resumable&part=snippet,status", bytes.NewReader(metaJSON))
	if err != nil {
		return UploadResult{Success: false, Error: err.Error()}
	}
	initReq.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	initReq.Header.Set("Content-Type", "application/json; charset=UTF-8")
	initReq.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", len(videoBytes)))
	initReq.Header.Set("X-Upload-Content-Type", "video/mp4")

	initResp, err := u.client.httpClient.Do(initReq)
	if err != nil {
		return UploadResult{Success: false, Error: "Gagal inisialisasi upload YouTube: " + err.Error()}
	}
	defer initResp.Body.Close()

	if initResp.StatusCode >= 400 {
		b, _ := io.ReadAll(initResp.Body)
		return UploadResult{Success: false, Error: fmt.Sprintf("Gagal inisialisasi upload: %s", string(b))}
	}

	uploadLocation := initResp.Header.Get("Location")
	if uploadLocation == "" {
		return UploadResult{Success: false, Error: "Header Location tidak ditemukan dari YouTube resumable upload"}
	}

	// 3. Upload Binary Stream
	uploadReq, err := http.NewRequest(http.MethodPut, uploadLocation, bytes.NewReader(videoBytes))
	if err != nil {
		return UploadResult{Success: false, Error: err.Error()}
	}
	uploadReq.Header.Set("Content-Type", "video/mp4")
	uploadReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(videoBytes)))

	uploadResp, err := u.client.httpClient.Do(uploadReq)
	if err != nil {
		return UploadResult{Success: false, Error: "Gagal mentransfer video stream ke YouTube: " + err.Error()}
	}
	defer uploadResp.Body.Close()

	upBody, err := io.ReadAll(uploadResp.Body)
	if err != nil {
		return UploadResult{Success: false, Error: "Gagal membaca respon akhir YouTube: " + err.Error()}
	}

	if uploadResp.StatusCode != http.StatusOK && uploadResp.StatusCode != http.StatusCreated {
		return UploadResult{Success: false, Error: fmt.Sprintf("Upload YouTube gagal (HTTP %d): %s", uploadResp.StatusCode, string(upBody))}
	}

	var ytResult struct {
		ID string `json:"id"`
	}
	json.Unmarshal(upBody, &ytResult)

	if ytResult.ID == "" {
		return UploadResult{Success: false, Error: "Video ID tidak diterima dari respon YouTube"}
	}

	videoURLFinal := fmt.Sprintf("https://youtu.be/%s", ytResult.ID)
	if item.PostType == "youtube_short" {
		videoURLFinal = fmt.Sprintf("https://www.youtube.com/shorts/%s", ytResult.ID)
	}

	return UploadResult{
		Success: true,
		VideoID: ytResult.ID,
		URL:     videoURLFinal,
	}
}
