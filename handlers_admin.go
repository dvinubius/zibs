package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

const maximumCreationTokenUses = 100

func getLinks(store *linkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		links, err := store.list()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(links)
	}
}

func deleteLinkByCode(store *linkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		code := req.PathValue("code")
		err := store.delete(code)
		if errors.Is(err, ErrLinkNotFound) {
			http.Error(w, "link not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func issueCreationToken(store *linkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var input struct {
			Label   string `json:"label"`
			MaxUses int    `json:"maxUses"`
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

		input.Label = strings.TrimSpace(input.Label)
		if input.Label == "" || utf8.RuneCountInString(input.Label) > 100 {
			http.Error(w, "label must contain 1 to 100 characters", http.StatusBadRequest)
			return
		}
		if input.MaxUses < 1 || input.MaxUses > maximumCreationTokenUses {
			http.Error(w, "maxUses must be between 1 and 100", http.StatusBadRequest)
			return
		}

		creationToken, token, err := store.issueCreationToken(input.Label, input.MaxUses)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(struct {
			CreationToken
			Token string `json:"token"`
		}{CreationToken: creationToken, Token: token})
	}
}

func revokeCreationToken(store *linkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		err := store.revokeCreationToken(req.PathValue("id"))
		if errors.Is(err, ErrCreationTokenNotFound) {
			http.Error(w, "creation token not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func getCreationTokens(store *linkStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tokens, err := store.listCreationTokens()
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tokens)
	}
}
