package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var (
	errEmptyDestinationURL       = errors.New("empty destination URL")
	errUnsupportedDestinationURL = errors.New("unsupported destination URL scheme")
	errInvalidDestinationURL     = errors.New("invalid destination URL")
)

func health(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(w, "OK, I'm healthy")
}

func createLink(store *linkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var input struct {
			URL string `json:"url"`
		}
		decoder := json.NewDecoder(req.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		destinationURL, err := normalizeDestinationURL(input.URL)
		if err != nil {
			http.Error(w, destinationURLErrorMessage(err), http.StatusBadRequest)
			return
		}

		link, err := store.create(destinationURL)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(struct {
			Code string `json:"code"`
		}{Code: link.Code})
	}
}

func normalizeDestinationURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errEmptyDestinationURL
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", errInvalidDestinationURL
	}
	addedScheme := u.Scheme == ""
	if u.Scheme == "" {
		u, err = url.Parse("https://" + raw)
		if err != nil {
			return "", errInvalidDestinationURL
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errUnsupportedDestinationURL
	}
	if !u.IsAbs() || u.Host == "" {
		return "", errInvalidDestinationURL
	}
	if addedScheme && !strings.Contains(u.Hostname(), ".") && net.ParseIP(u.Hostname()) == nil {
		return "", errInvalidDestinationURL
	}

	return u.String(), nil
}

func destinationURLErrorMessage(err error) string {
	switch {
	case errors.Is(err, errEmptyDestinationURL):
		return "enter a link to shorten"
	case errors.Is(err, errUnsupportedDestinationURL):
		return "use a web link beginning with http:// or https://"
	default:
		return "that doesn't look like a web address; try example.com or https://example.com"
	}
}

func followLink(store *linkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		code := req.PathValue("code")
		link, err := store.follow(code)
		if errors.Is(err, ErrLinkNotFound) {
			http.Error(w, "link not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, req, link.DestinationURL, http.StatusFound)
	}
}
