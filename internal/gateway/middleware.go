package gateway

import (
	"net/http"
	"strconv"
	"strings"
)

type Middleware struct {
	secretKey string
}

func NewMiddleware(secretKey string) *Middleware {
	return &Middleware{secretKey: secretKey}
}

func (m *Middleware) AuthRequired(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Handle CORS preflight
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-SILA-SECRET, X-USER-ID")
			w.WriteHeader(http.StatusOK)
			return
		}

		secret := r.Header.Get("X-SILA-SECRET")
		if secret == "" {
			// Fallback check bearer or query param
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				secret = strings.TrimPrefix(authHeader, "Bearer ")
			} else {
				secret = r.URL.Query().Get("secret")
			}
		}

		if secret != m.secretKey {
			http.Error(w, `{"error": "Unauthorized: invalid SILA secret key"}`, http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func (m *Middleware) CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-SILA-SECRET, X-USER-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func GetUserIDFromRequest(r *http.Request) int64 {
	uidStr := r.Header.Get("X-USER-ID")
	if uidStr == "" {
		uidStr = r.URL.Query().Get("user_id")
	}
	if uid, err := strconv.ParseInt(uidStr, 10, 64); err == nil {
		return uid
	}
	return 0
}
