package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// On définit une clé personnalisée pour éviter les collisions dans le contexte
type contextKey string
const UserKey contextKey = "user"

type UserClaims struct {
	Roles []string `json:"roles"`
	Name  string   `json:"name"`
	Email string   `json:"preferred_username"`
}

func AuthMiddleware(tenantID, clientID string, next http.Handler) http.Handler {
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, "https://login.microsoftonline.com/"+tenantID+"/v2.0")
	if err != nil {
		panic(err) // À gérer proprement selon ton log strategy
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Extraction du jeton depuis le Cookie
		cookie, err := r.Cookie("auth_token")
		if err != nil {
			// Si pas de cookie, on redirige vers le login au lieu d'afficher une erreur
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		tokenStr := cookie.Value

		// 2. Vérification du jeton
		idToken, err := verifier.Verify(r.Context(), tokenStr)
		if err != nil {
			// Jeton invalide ou expiré -> Redirection login
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		// 3. Extraction et vérification des rôles (identique à avant)
		var claims UserClaims
		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "Erreur interne", http.StatusInternalServerError)
			return
		}

		// (Logique de vérification du rôle "search_user"...)

		newCtx := context.WithValue(r.Context(), UserKey, claims)
		next.ServeHTTP(w, r.WithContext(newCtx))
	})

	// return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// 	// 1. Extraction du Bearer Token
	// 	authHeader := r.Header.Get("Authorization")
	// 	if !strings.HasPrefix(authHeader, "Bearer ") {
	// 		http.Error(w, "Authentification requise", http.StatusUnauthorized)
	// 		return
	// 	}
	//
	// 	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	//
	// 	// 2. Vérification du jeton (Signature, Expiration, Audience)
	// 	idToken, err := verifier.Verify(r.Context(), tokenStr)
	// 	if err != nil {
	// 		http.Error(w, "Jeton invalide : "+err.Error(), http.StatusUnauthorized)
	// 		return
	// 	}
	//
	// 	// 3. Extraction des Claims
	// 	var claims UserClaims
	// 	if err := idToken.Claims(&claims); err != nil {
	// 		http.Error(w, "Erreur lors du parsing des claims", http.StatusInternalServerError)
	// 		return
	// 	}
	//
	// 	// 4. Vérification du rôle "utilisateur"
	// 	hasRole := false
	// 	for _, role := range claims.Roles {
	// 		if role == "search_user" {
	// 			hasRole = true
	// 			break
	// 		}
	// 	}
	//
	// 	if !hasRole {
	// 		http.Error(w, "Accès interdit : rôle insuffisant", http.StatusForbidden)
	// 		return
	// 	}
	//
	// 	// 5. Injection dans le contexte et passage au handler suivant
	// 	newCtx := context.WithValue(r.Context(), UserKey, claims)
	// 	next.ServeHTTP(w, r.WithContext(newCtx))
	// })
}
