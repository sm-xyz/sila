package meta

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

// BuildAuthURL creates the Facebook OAuth dialog URL
func (o *OAuthManager) BuildAuthURL(userID int64, platform, redirectReturn string) string {
	callbackURL := fmt.Sprintf("%s/auth/meta/callback", o.cfg.PublicURL)
	scopes := "pages_show_list,pages_read_engagement,pages_manage_posts,pages_manage_metadata,instagram_basic,instagram_content_publish,instagram_manage_comments,instagram_manage_messages,pages_messaging"
	if platform == "threads" {
		scopes += ",threads_basic,threads_content_publish,threads_read_replies,threads_manage_replies"
	}

	state := fmt.Sprintf("%d|%s|%s", userID, platform, url.QueryEscape(redirectReturn))

	params := url.Values{}
	params.Set("client_id", o.cfg.MetaAppID)
	params.Set("redirect_uri", callbackURL)
	params.Set("scope", scopes)
	params.Set("response_type", "code")
	params.Set("state", state)

	return fmt.Sprintf("https://www.facebook.com/v21.0/dialog/oauth?%s", params.Encode())
}

// HandleCallback exchanges the code for long-lived tokens and connects Facebook Pages & Instagram accounts
func (o *OAuthManager) HandleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errParam := r.URL.Query().Get("error")

	if errParam != "" || code == "" {
		errMsg := r.URL.Query().Get("error_description")
		if errMsg == "" {
			errMsg = "Akses ditolak oleh pengguna atau gagal mendapatkan izin Meta."
		}
		renderClosePopup(w, false, errMsg, "meta")
		return
	}

	// Parse state: "userID|platform|returnUrl"
	var userID int64 = 1
	targetPlatform := "facebook"
	if state != "" {
		parts := splitState(state)
		if len(parts) >= 1 {
			if id, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
				userID = id
			}
		}
		if len(parts) >= 2 && parts[1] != "" {
			targetPlatform = parts[1]
		}
	}

	callbackURL := fmt.Sprintf("%s/auth/meta/callback", o.cfg.PublicURL)

	// 1. Exchange short-lived token
	tokenParams := url.Values{}
	tokenParams.Set("client_id", o.cfg.MetaAppID)
	tokenParams.Set("client_secret", o.cfg.MetaAppSecret)
	tokenParams.Set("redirect_uri", callbackURL)
	tokenParams.Set("code", code)

	body, err := o.client.Get("/oauth/access_token", tokenParams, "")
	if err != nil {
		log.Printf("[SILA Meta OAuth Error] Token exchange failed: %v", err)
		renderClosePopup(w, false, "Gagal menukar token akses Meta: "+err.Error(), targetPlatform)
		return
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		renderClosePopup(w, false, "Format respon token Meta tidak valid", targetPlatform)
		return
	}

	// 2. Exchange for Long-Lived Token (60 Days)
	longLivedParams := url.Values{}
	longLivedParams.Set("grant_type", "fb_exchange_token")
	longLivedParams.Set("client_id", o.cfg.MetaAppID)
	longLivedParams.Set("client_secret", o.cfg.MetaAppSecret)
	longLivedParams.Set("fb_exchange_token", tokenResp.AccessToken)

	llBody, err := o.client.Get("/oauth/access_token", longLivedParams, "")
	userAccessToken := tokenResp.AccessToken
	var tokenExpiresAt int64 = time.Now().Unix() + 5184000 // default 60 days
	if err == nil {
		var llResp struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
		}
		if json.Unmarshal(llBody, &llResp) == nil && llResp.AccessToken != "" {
			userAccessToken = llResp.AccessToken
			if llResp.ExpiresIn > 0 {
				tokenExpiresAt = time.Now().Unix() + llResp.ExpiresIn
			}
		}
	}

	// 3. Query Facebook Pages (/me/accounts)
	pageParams := url.Values{}
	pageParams.Set("fields", "id,name,username,picture{url},access_token,instagram_business_account{id,username,name,profile_picture_url}")
	pagesBody, err := o.client.Get("/me/accounts", pageParams, userAccessToken)
	if err != nil {
		renderClosePopup(w, false, "Gagal mengambil daftar halaman Facebook: "+err.Error(), targetPlatform)
		return
	}

	var pagesData struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Username    string `json:"username"`
			AccessToken string `json:"access_token"`
			Picture     struct {
				Data struct {
					URL string `json:"url"`
				} `json:"data"`
			} `json:"picture"`
			InstagramBusinessAccount *struct {
				ID                string `json:"id"`
				Username          string `json:"username"`
				Name              string `json:"name"`
				ProfilePictureURL string `json:"profile_picture_url"`
			} `json:"instagram_business_account"`
		} `json:"data"`
	}

	connectedCount := 0
	if json.Unmarshal(pagesBody, &pagesData) == nil {
		for _, page := range pagesData.Data {
			// Save Facebook Page
			pageAcc := &database.Account{
				ID:             fmt.Sprintf("fb_%s_%d", page.ID, userID),
				UserID:         userID,
				Platform:       "facebook",
				AccountID:      page.ID,
				AccountName:    page.Name,
				AccountUsername: page.Username,
				AvatarURL:      page.Picture.Data.URL,
				AccessToken:    page.AccessToken, // Page Access Token doesn't expire easily
				TokenExpiresAt: 0,
				ExtraMetadata:  `{"type": "page"}`,
			}
			if err := o.repo.UpsertAccount(pageAcc); err == nil {
				connectedCount++
			}

			// If linked Instagram Business exists, save it too!
			if page.InstagramBusinessAccount != nil && page.InstagramBusinessAccount.ID != "" {
				ig := page.InstagramBusinessAccount
				igAcc := &database.Account{
					ID:             fmt.Sprintf("ig_%s_%d", ig.ID, userID),
					UserID:         userID,
					Platform:       "instagram",
					AccountID:      ig.ID,
					AccountName:    ig.Name,
					AccountUsername: ig.Username,
					AvatarURL:      ig.ProfilePictureURL,
					AccessToken:    page.AccessToken, // Instagram Business uses Page Access Token
					TokenExpiresAt: tokenExpiresAt,
					ExtraMetadata:  fmt.Sprintf(`{"linked_page_id": "%s", "type": "instagram_business"}`, page.ID),
				}
				if igAcc.AccountName == "" {
					igAcc.AccountName = "@" + ig.AccountUsername
				}
				if err := o.repo.UpsertAccount(igAcc); err == nil {
					connectedCount++
				}
			}
		}
	}

	// 4. Also check for Threads if requested
	if targetPlatform == "threads" {
		threadsParams := url.Values{}
		threadsParams.Set("fields", "id,username,threads_profile_picture_url,threads_biography")
		thBody, err := o.client.Get("/me", threadsParams, userAccessToken)
		if err == nil {
			var thData struct {
				ID                string `json:"id"`
				Username          string `json:"username"`
				ProfilePictureURL string `json:"threads_profile_picture_url"`
			}
			if json.Unmarshal(thBody, &thData) == nil && thData.ID != "" {
				thAcc := &database.Account{
					ID:             fmt.Sprintf("th_%s_%d", thData.ID, userID),
					UserID:         userID,
					Platform:       "threads",
					AccountID:      thData.ID,
					AccountName:    "@" + thData.Username,
					AccountUsername: thData.Username,
					AvatarURL:      thData.ProfilePictureURL,
					AccessToken:    userAccessToken,
					TokenExpiresAt: tokenExpiresAt,
					ExtraMetadata:  `{"type": "threads_profile"}`,
				}
				if err := o.repo.UpsertAccount(thAcc); err == nil {
					connectedCount++
				}
			}
		}
	}

	renderClosePopup(w, true, fmt.Sprintf("%d akun berhasil ditautkan!", connectedCount), targetPlatform)
}

func splitState(s string) []string {
	var res []string
	curr := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			res = append(res, curr)
			curr = ""
		} else {
			curr += string(s[i])
		}
	}
	res = append(res, curr)
	return res
}

func renderClosePopup(w http.ResponseWriter, success bool, message, platform string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	statusStr := "error"
	title := "Gagal Menghubungkan Akun"
	bgColor := "#ef4444"
	if success {
		statusStr = "success"
		title = "Berhasil Terhubung!"
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
        <p style="font-size: 0.8rem; margin-top: 1.5rem;">Jendela ini akan tertutup secara otomatis...</p>
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
