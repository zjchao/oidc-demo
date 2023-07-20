package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"oidc-demo/http-client/app1/environment"
	"oidc-demo/http-client/app1/storage"
	"oidc-demo/http-client/userinfo"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	OidcIssuer    string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	ListenAddress string

	s *storage.Storage
)

func init() {
	env := environment.Load()
	OidcIssuer = env.OidcIssuer
	ClientID = env.ClientID
	ClientSecret = env.ClientSecret
	RedirectURL = env.RedirectURL
	ListenAddress = env.ListenAddress

	s = storage.New()
}

// Login http redirect to oidc provider
func Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider, err := oidc.NewProvider(ctx, OidcIssuer)
	if err != nil {
		http.Error(w, fmt.Sprintf("init provider failed: %s", err), http.StatusInternalServerError)
		return
	}

	config := Oauth2Config(provider)
	url := config.AuthCodeURL("state")

	http.Redirect(w, r, url, http.StatusFound)
}

// Logout flush the token of cookie
func Logout(w http.ResponseWriter, r *http.Request) {
	flushFromCookie(w)
	w.Write([]byte("logout"))
}

// LoginCallback the callback that the oidc provider with call when the user login successfully
func LoginCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	provider, err := oidc.NewProvider(ctx, OidcIssuer)
	if err != nil {
		http.Error(w, fmt.Sprintf("init provider failed: %s", err), http.StatusInternalServerError)
		return
	}

	// exchange token with the server using authorization code
	config := Oauth2Config(provider)
	oauth2Token, err := config.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, fmt.Sprintf("exchange token with server failed: %s", err), http.StatusUnauthorized)
		return
	}

	// get rawIDToken with token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(w, fmt.Sprintf("get rawIDToken with token failed"), http.StatusUnauthorized)
		return
	}

	// verify IDToken with idTokenVerifier, the idTokenVerifier is generated by provider
	idTokenVerifier := provider.Verifier(&oidc.Config{ClientID: ClientID})
	idToken, err := idTokenVerifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("verify IDToken with oidc provider failed: %s", err), http.StatusUnauthorized)
		return
	}

	var ui userinfo.UserInfo
	if err = idToken.Claims(&ui); err != nil {
		http.Error(w, fmt.Sprintf("parse id token failed: %s", err), http.StatusInternalServerError)
		return
	}
	ui.AccessToken = oauth2Token.AccessToken
	ui.IDToken = rawIDToken

	s.AddUser(ui.Subject, ui.Name, ui.Audience, ui.Email)

	SetIntoCookie(w, oauth2Token)
	bytes, _ := json.Marshal(&ui)
	w.Write(bytes) // output the userinfo structure as json
}

func Oauth2Config(provider *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     ClientID,
		ClientSecret: ClientSecret,
		RedirectURL:  RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, oidc.ScopeOfflineAccess, "profile", "email", "groups"},
	}
}

func SetIntoCookie(w http.ResponseWriter, oauth2Token *oauth2.Token) {
	rawIDToken, _ := oauth2Token.Extra("id_token").(string)
	cookies := []http.Cookie{
		{Name: "access_token", Value: oauth2Token.AccessToken, Path: "/", MaxAge: 60 * 5, HttpOnly: true},
		{Name: "token_type", Value: oauth2Token.TokenType, Path: "/", MaxAge: 60 * 5, HttpOnly: true},
		{Name: "refresh_token", Value: oauth2Token.RefreshToken, Path: "/", MaxAge: 60 * 5, HttpOnly: true},
		{Name: "expiry", Value: oauth2Token.Expiry.Format(time.RFC3339), Path: "/", MaxAge: 60 * 5, HttpOnly: true},
		{Name: "id_token", Value: rawIDToken, Path: "/", MaxAge: 60 * 5, HttpOnly: true},
	}
	for _, c := range cookies {
		http.SetCookie(w, &c)
	}
}

func flushFromCookie(w http.ResponseWriter) {
	cookies := []http.Cookie{
		{Name: "access_token", MaxAge: -1},
		{Name: "token_type", MaxAge: -1},
		{Name: "refresh_token", MaxAge: -1},
		{Name: "expiry", MaxAge: -1},
		{Name: "id_token", MaxAge: -1},
	}
	for _, c := range cookies {
		http.SetCookie(w, &c)
	}
}

type token struct {
	AccessToken  string
	TokenType    string
	RefreshToken string
	Expiry       time.Time
	IdToken      string
}

func GetFromCookie(r *http.Request) (*token, error) {
	at, err := r.Cookie("access_token")
	if err == http.ErrNoCookie {
		return nil, err
	}
	tt, err := r.Cookie("token_type")
	if err == http.ErrNoCookie {
		return nil, err
	}
	rt, err := r.Cookie("refresh_token")
	if err == http.ErrNoCookie {
		return nil, err
	}
	exp, err := r.Cookie("expiry")
	if err == http.ErrNoCookie {
		return nil, err
	}
	it, err := r.Cookie("id_token")
	if err == http.ErrNoCookie {
		return nil, err
	}

	expValue, _ := time.Parse(time.RFC3339, exp.Value)
	return &token{
		AccessToken:  at.Value,
		TokenType:    tt.Value,
		RefreshToken: rt.Value,
		Expiry:       expValue,
		IdToken:      it.Value,
	}, nil
}
