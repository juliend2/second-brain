package main

import (
	"fmt"
	"html/template"
	"net/http"

	"desrosiers.org/pse/auth"
)

func main() {
	fmt.Println("Starting the server...")
	mux := http.NewServeMux()

	// Config:
	tenantID := "d9eabe26-5c55-4be7-8845-6610a7d6e2ba"
	clientID := "0b6d5637-a3bb-4f70-9917-7ee0c394e0f0"

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/", handleSearchPage)
	mux.HandleFunc("/auth/callback", handleCallback)
	searchHandler := http.HandlerFunc(handleSearchResults)
	mux.Handle("/search", auth.AuthMiddleware(tenantID, clientID, searchHandler))

	http.ListenAndServe(":8080", mux)
}

type SearchPageViewModel struct {
	Title 	string
}

func handleSearchPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/search.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, &SearchPageViewModel{Title: "Recherche"})
}

func handleSearchResults(w http.ResponseWriter, r *http.Request) {
	// get the user from the request's context:
	user, ok := r.Context().Value(auth.UserKey).(auth.UserClaims)
	if !ok {
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}
	query := r.URL.Query().Get("q")

	// TODO:
	// results, err := index.Search(...)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"message": "Bonjour ` + user.Name + `", "query": "` + query + `"}`))
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
    // 1. Échange du code contre le token via oauth2.Config (standard Go)
    // token, err := oauthConfig.Exchange(r.Context(), r.URL.Query().Get("code"))

    // 2. Une fois le jeton obtenu (ID Token), on le place dans le cookie
    http.SetCookie(w, &http.Cookie{
        Name:     "auth_token",
        Value:    idTokenString, // Le JWT brut reçu d'Entra ID
        Path:     "/",
        HttpOnly: true,
				Secure:   false, // TODO: put back to true when in prod (on http://localhost we need true)
        SameSite: http.SameSiteLaxMode,
        MaxAge:   3600, // 1h
    })

    http.Redirect(w, r, "/search", http.StatusTemporaryRedirect)
}
