package dashboard

import (
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	cookieName = "mcp_session"
	tokenTTL   = 24 * time.Hour
)

func createToken(secret []byte, username string) (string, error) {
	claims := jwt.MapClaims{
		"sub": username,
		"exp": jwt.NewNumericDate(time.Now().Add(tokenTTL)),
		"iat": jwt.NewNumericDate(time.Now()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func validateToken(secret []byte, tokenStr string) (string, bool) {
	token, err := jwt.Parse(tokenStr, func(_ *jwt.Token) (any, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return "", false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", false
	}
	sub, _ := claims.GetSubject()
	return sub, sub != ""
}

func setSessionCookie(w http.ResponseWriter, secret []byte, username string) error {
	tokenStr, err := createToken(secret, username)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tokenStr,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(tokenTTL.Seconds()),
	})
	return nil
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			http.Redirect(w, r, "/dashboard/login", http.StatusSeeOther)
			return
		}
		if _, ok := validateToken(s.jwtSecret, cookie.Value); !ok {
			clearSessionCookie(w)
			http.Redirect(w, r, "/dashboard/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
