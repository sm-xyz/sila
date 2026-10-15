package youtube

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"sila/config"
	"sila/internal/database"
)

type OAuthManager struct {
	cfg    *config.Config
	client *Client
	repo   *database.Repository
}

func NewOAuthManager(cfg *config.Config, client *Client, repo *database.Repository) *OAuthManager {
	return &OAuthManager{
		cfg:    cfg,
		client: client,
		repo:   repo,
	}
}

// BuildAuthURL builds Google OAuth consent screen URL
func (o *OAuthManager) BuildAuthURL(userID int64, redirectReturn string) string {
	callbackURL := fmt.Sprintf("%s/auth/youtube/callback", o.cfg.PublicURL)
	scopes := "https://www.googleapis.com/auth/youtube.upload https://www.googleapis.com/auth/youtube.readonly https://www.googleapis.com/auth/userinfo.profile"

	state := fmt.Sprintf("%d|youtube|%s", userID, url.QueryEscape(redirectReturn))

	params := url.Values{}
	params.Set("client_id", o.cfg.GoogleClientID)
	params.Set("redirect_uri", callbackURL)
	params.Set("response_type", "code")
	params.Set("scope", scopes)
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("state", state)

	return fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?%s", params.Encode())
}

// HandleCallback exchanges Google auth code and stores YouTube channel
func (o *OAuthManager) HandleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errParam := r.URL.Query().Get("error")

	if errParam != "" || code == "" {
		renderClosePopup(w, false, "Gagal mendapatkan izin Google/YouTube", "youtube")
		return
	}

	var userID int64 = 1
	if state != "" {
		var uidStr string
		for _, char := range state {
			if char == '|' {
				break
			}
			uidStr += string(char)
		}
		if id, err := strconv.ParseInt(uidStr, 10, 64); err == nil {
			userID = id
		}
	}

	callbackURL := fmt.Sprintf("%s/auth/youtube/callback", o.cfg.PublicURL)

	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", o.cfg.GoogleClientID)
	form.Set("client_secret", o.cfg.GoogleSecret)
	form.Set("redirect_uri", callbackURL)
	form.Set("grant_type", "authorization_code")

	body, err := o.client.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		log.Printf("[SILA YouTube OAuth Error] Token exchange failed: %v", err)
		renderClosePopup(w, false, "Gagal menukar token Google: "+err.Error(), "youtube")
		return
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		renderClosePopup(w, false, "Format respon token Google tidak valid", "youtube")
		return
	}

	tokenExpiresAt := time.Now().Unix() + tokenResp.ExpiresIn

	// Fetch Channel Info
	chParams := url.Values{}
	chParams.Set("part", "snippet,contentDetails,statistics")
	chParams.Set("mine", "true")

	chBody, err := o.client.Get("https://www.googleapis.com/youtube/v3/channels", chParams, tokenResp.AccessToken)
	if err != nil {
		renderClosePopup(w, false, "Gagal mengambil channel YouTube: "+err.Error(), "youtube")
		return
	}

	var chResp struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				CustomURL   string `json:"customUrl"`
				Thumbnails  struct {
					Default struct {
						URL string `json:"url"`
					} `json:"default"`
				} `json:"thumbnails"`
			} `json:"snippet"`
			Statistics struct {
				SubscriberCount string `json:"subscriberCount"`
				VideoCount      string `json:"videoCount"`
			} `json:"statistics"`
		} `json:"items"`
	}

	if err := json.Unmarshal(chBody, &chResp); err != nil || len(chResp.Items) == 0 {
		renderClosePopup(w, false, "Tidak ditemukan channel YouTube aktif pada akun Google Anda", "youtube")
		return
	}

	ch := chResp.Items[0]
	channelAcc := &database.Account{
		ID:              fmt.Sprintf("yt_%s_%d", ch.ID, userID),
		UserID:          userID,
		Platform:        "youtube",
		AccountID:       ch.ID,
		AccountName:     ch.Snippet.Title,
		AccountUsername: ch.Snippet.CustomURL,
		AvatarURL:       ch.Snippet.Thumbnails.Default.URL,
		AccessToken:     tokenResp.AccessToken,
		RefreshToken:    tokenResp.RefreshToken,
		TokenExpiresAt:  tokenExpiresAt,
		ExtraMetadata:   fmt.Sprintf(`{"subscribers": "%s", "videos": "%s"}`, ch.Statistics.SubscriberCount, ch.Statistics.VideoCount),
	}

	if err := o.repo.UpsertAccount(channelAcc); err != nil {
		renderClosePopup(w, false, "Gagal menyimpan akun YouTube ke database: "+err.Error(), "youtube")
		return
	}

	renderClosePopup(w, true, fmt.Sprintf("Channel YouTube '%s' berhasil ditautkan!", ch.Snippet.Title), "youtube")
}

// RefreshAccessToken refreshes an expired access token using the stored refresh token
func (o *OAuthManager) RefreshAccessToken(acc *database.Account) (string, error) {
	if acc.RefreshToken == "" {
		return acc.AccessToken, nil
	}

	form := url.Values{}
	form.Set("client_id", o.cfg.GoogleClientID)
	form.Set("client_secret", o.cfg.GoogleSecret)
	form.Set("refresh_token", acc.RefreshToken)
	form.Set("grant_type", "refresh_token")

	body, err := o.client.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		return "", err
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		return "", fmt.Errorf("gagal refresh token YouTube")
	}

	acc.AccessToken = tokenResp.AccessToken
	acc.TokenExpiresAt = time.Now().Unix() + tokenResp.ExpiresIn
	_ = o.repo.UpsertAccount(acc)

	return acc.AccessToken, nil
}

func renderClosePopup(w http.ResponseWriter, success bool, message, platform string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	statusStr := "error"
	title := "Gagal Menghubungkan Channel"
	bgColor := "#ef4444"
	if success {
		statusStr = "success"
		title = "YouTube Berhasil Terhubung!"
		bgColor = "#10b981"
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>%s</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; background: #f8fafc; }
        .card { background: white; padding: 2rem; border-radius: 1rem; box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1); text-align: center; max-width: 400px; width: 90%%; }
        .badge { display: inline-block; padding: 0.5rem 1rem; border-radius: 9999px; background: %s; color: white; font-weight: bold; margin-bottom: 1rem; }
        p { color: #64748b; font-size: 0.95rem; line-height: 1.5; }
    </style>
</head>
<body>
    <div class="card">
        <div class="badge">%s</div>
        <h2>%s</h2>
        <p>%s</p>
        <p style="font-size: 0.8rem; margin-top: 1.5rem;">Jendela ini akan tertutup otomatis...</p>
    </div>
    <script>
        try {
            if (window.opener) {
                window.opener.postMessage({
                    type: 'SILA_AUTH_COMPLETE',
                    status: '%s',
                    platform: '%s',
                    message: %q
                }, '*');
            }
        } catch (e) { console.error(e); }
        setTimeout(function() {
            window.close();
        }, 1800);
    </script>
</body>
</html>`, title, bgColor, title, title, message, statusStr, platform, message)

	w.Write([]byte(html))
}
