package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

var (
	ErrLinkNotFound          = errors.New("link not found")
	ErrCreationTokenInvalid  = errors.New("creation token invalid")
	ErrCreationTokenNotFound = errors.New("creation token not found")
)

const (
	shortCodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	shortCodeLength   = 8
	defaultLinkTTL    = 90 * 24 * time.Hour

	// Token secrets are copied by hand from an email, so the alphabet is
	// lowercase-only, drops the 0/o and 1/l lookalikes, and contains no
	// characters that break double-click selection. 32 chars × 12 ≈ 60 bits,
	// ample for a use-bounded, revocable token (ADR 0002).
	creationTokenAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	creationTokenLength   = 12
)

type Link struct {
	Code           string    `json:"code"`
	DestinationURL string    `json:"url"`
	CreatedAt      time.Time `json:"createdAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
	RedirectCount  int       `json:"redirectCount"`
}

type CreationToken struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	CreatedAt time.Time  `json:"createdAt"`
	MaxUses   int        `json:"maxUses"`
	UseCount  int        `json:"useCount"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

type linkStore struct {
	db                    *sql.DB
	metrics               *metrics
	generateCode          func() (string, error)
	generateCreationToken func() (id, token string, err error)
	now                   func() time.Time
}

func newLinkStore(db *sql.DB, metrics *metrics) *linkStore {
	return &linkStore{
		db:                    db,
		metrics:               metrics,
		generateCode:          generateShortCode,
		generateCreationToken: generateCreationToken,
		now:                   time.Now,
	}
}

func generateShortCode() (string, error) {
	return randomString(shortCodeAlphabet, shortCodeLength)
}

func randomString(alphabet string, length int) (string, error) {
	out := make([]byte, length)
	validByteLimit := 256 - (256 % len(alphabet))

	for i := range out {
		for {
			var randomByte [1]byte
			if _, err := rand.Read(randomByte[:]); err != nil {
				return "", fmt.Errorf("read cryptographic randomness: %w", err)
			}
			if int(randomByte[0]) >= validByteLimit {
				continue
			}

			out[i] = alphabet[int(randomByte[0])%len(alphabet)]
			break
		}
	}

	return string(out), nil
}

func generateCreationToken() (string, string, error) {
	idBytes := make([]byte, 9)
	if _, err := rand.Read(idBytes); err != nil {
		return "", "", fmt.Errorf("generate token ID: %w", err)
	}
	secret, err := randomString(creationTokenAlphabet, creationTokenLength)
	if err != nil {
		return "", "", fmt.Errorf("generate token secret: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(idBytes), "zib_" + secret, nil
}

func (s *linkStore) create(destinationURL string) (link Link, err error) {
	result := "error"
	defer func() {
		s.metrics.linkOperations.WithLabelValues("create", result).Inc()
	}()

	for {
		code, err := s.generateCode()
		if err != nil {
			return Link{}, fmt.Errorf("generate short code: %w", err)
		}

		link := Link{
			Code:           code,
			DestinationURL: destinationURL,
			CreatedAt:      s.now().UTC().Truncate(time.Second),
			RedirectCount:  0,
		}
		link.ExpiresAt = link.CreatedAt.Add(defaultLinkTTL)
		started := time.Now()
		_, err = s.db.Exec(`
			INSERT INTO links (code, destination_url, created_at, expires_at, redirect_count)
			VALUES (?, ?, ?, ?, ?)
		`, link.Code, link.DestinationURL, link.CreatedAt.Format(time.RFC3339Nano),
			link.ExpiresAt.Unix(), link.RedirectCount)
		if err != nil {
			if isUniqueConstraint(err) {
				// The database is the authority on uniqueness. Generate a new code
				// and retry if another link already has this one. Expected retries are
				// intentionally excluded from the minimal DB metrics.
				continue
			}
			s.metrics.dbOperationDuration.WithLabelValues("create", "error").Observe(time.Since(started).Seconds())
			return Link{}, err
		}

		s.metrics.dbOperationDuration.WithLabelValues("create", "success").Observe(time.Since(started).Seconds())
		result = "success"
		return link, nil
	}
}

func (s *linkStore) issueCreationToken(label string, maxUses int) (CreationToken, string, error) {
	for {
		id, token, err := s.generateCreationToken()
		if err != nil {
			return CreationToken{}, "", err
		}

		now := s.now().UTC().Truncate(time.Second)
		creationToken := CreationToken{
			ID:        id,
			Label:     label,
			CreatedAt: now,
			MaxUses:   maxUses,
		}
		hash := sha256.Sum256([]byte(token))
		_, err = s.db.Exec(`
			INSERT INTO creation_tokens
				(id, token_hash, label, created_at, max_uses, use_count)
			VALUES (?, ?, ?, ?, ?, 0)
		`, creationToken.ID, hash[:], creationToken.Label, creationToken.CreatedAt.Unix(),
			creationToken.MaxUses)
		if err != nil {
			if isUniqueConstraint(err) {
				continue
			}
			return CreationToken{}, "", fmt.Errorf("insert creation token: %w", err)
		}

		return creationToken, token, nil
	}
}

func (s *linkStore) consumeCreationToken(token string) error {
	hash := sha256.Sum256([]byte(token))
	result, err := s.db.Exec(`
		UPDATE creation_tokens
		SET use_count = use_count + 1
		WHERE token_hash = ?
			AND revoked_at IS NULL
			AND use_count < max_uses
	`, hash[:])
	if err != nil {
		return fmt.Errorf("consume creation token: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check creation token result: %w", err)
	}
	if rowsAffected == 0 {
		return ErrCreationTokenInvalid
	}
	return nil
}

func (s *linkStore) revokeCreationToken(id string) error {
	result, err := s.db.Exec(`
		UPDATE creation_tokens
		SET revoked_at = ?
		WHERE id = ? AND revoked_at IS NULL
	`, s.now().UTC().Unix(), id)
	if err != nil {
		return fmt.Errorf("revoke creation token: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check token revocation result: %w", err)
	}
	if rowsAffected == 0 {
		return ErrCreationTokenNotFound
	}
	return nil
}

func (s *linkStore) listCreationTokens() ([]CreationToken, error) {
	rows, err := s.db.Query(`
		SELECT id, label, created_at, max_uses, use_count, revoked_at
		FROM creation_tokens
		ORDER BY created_at DESC, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list creation tokens: %w", err)
	}
	defer rows.Close()

	tokens := []CreationToken{}
	for rows.Next() {
		var token CreationToken
		var createdAt int64
		var revokedAt sql.NullInt64
		if err := rows.Scan(
			&token.ID,
			&token.Label,
			&createdAt,
			&token.MaxUses,
			&token.UseCount,
			&revokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan creation token: %w", err)
		}

		token.CreatedAt = time.Unix(createdAt, 0).UTC()
		if revokedAt.Valid {
			value := time.Unix(revokedAt.Int64, 0).UTC()
			token.RevokedAt = &value
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate creation tokens: %w", err)
	}

	return tokens, nil
}

func isUniqueConstraint(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) &&
		(sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique ||
			sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey)
}

func (s *linkStore) get(code string) (Link, error) {
	var link Link
	var createdAt string
	var expiresAt int64

	err := s.db.QueryRow(`
		SELECT code, destination_url, created_at, expires_at, redirect_count
		FROM links
		WHERE code = ?
	`, code).Scan(
		&link.Code,
		&link.DestinationURL,
		&createdAt,
		&expiresAt,
		&link.RedirectCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, ErrLinkNotFound
	}
	if err != nil {
		return Link{}, fmt.Errorf("get link: %w", err)
	}

	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Link{}, fmt.Errorf("parse created_at: %w", err)
	}
	link.CreatedAt = parsedCreatedAt
	link.ExpiresAt = time.Unix(expiresAt, 0).UTC()

	return link, nil
}

func (s *linkStore) follow(code string) (Link, error) {
	result := "error"
	defer func() {
		s.metrics.linkOperations.WithLabelValues("follow", result).Inc()
	}()

	var link Link
	var createdAt string
	var expiresAt int64

	started := time.Now()
	err := s.db.QueryRow(`
		UPDATE links
		SET redirect_count = redirect_count + 1
		WHERE code = ? AND expires_at > ?
		RETURNING code, destination_url, created_at, expires_at, redirect_count
	`, code, s.now().UTC().Unix()).Scan(
		&link.Code,
		&link.DestinationURL,
		&createdAt,
		&expiresAt,
		&link.RedirectCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		s.metrics.dbOperationDuration.WithLabelValues("follow", "not_found").Observe(time.Since(started).Seconds())
		result = "not_found"
		return Link{}, ErrLinkNotFound
	}
	if err != nil {
		s.metrics.dbOperationDuration.WithLabelValues("follow", "error").Observe(time.Since(started).Seconds())
		return Link{}, fmt.Errorf("increment redirect count: %w", err)
	}

	s.metrics.dbOperationDuration.WithLabelValues("follow", "success").Observe(time.Since(started).Seconds())

	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Link{}, fmt.Errorf("parse created_at: %w", err)
	}
	link.CreatedAt = parsedCreatedAt
	link.ExpiresAt = time.Unix(expiresAt, 0).UTC()

	result = "success"
	return link, nil
}

func (s *linkStore) deleteExpired(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM links
		WHERE expires_at <= ?
	`, s.now().UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("delete expired links: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("check expired link deletion result: %w", err)
	}
	return deleted, nil
}

func (s *linkStore) activeLinkCount(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM links
		WHERE expires_at > ?
	`, s.now().UTC().Unix()).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active links: %w", err)
	}
	return count, nil
}

func (s *linkStore) delete(code string) error {
	resultForMetric := "error"
	defer func() {
		s.metrics.linkOperations.WithLabelValues("delete", resultForMetric).Inc()
	}()

	started := time.Now()
	result, err := s.db.Exec(`
		DELETE FROM links
		WHERE code = ?
	`, code)
	if err != nil {
		s.metrics.dbOperationDuration.WithLabelValues("delete", "error").Observe(time.Since(started).Seconds())
		return fmt.Errorf("delete link: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		s.metrics.dbOperationDuration.WithLabelValues("delete", "error").Observe(time.Since(started).Seconds())
		return fmt.Errorf("check delete result: %w", err)
	}
	if rowsAffected == 0 {
		s.metrics.dbOperationDuration.WithLabelValues("delete", "not_found").Observe(time.Since(started).Seconds())
		resultForMetric = "not_found"
		return ErrLinkNotFound
	}

	s.metrics.dbOperationDuration.WithLabelValues("delete", "success").Observe(time.Since(started).Seconds())
	resultForMetric = "success"
	return nil
}

func (s *linkStore) list() ([]Link, error) {
	rows, err := s.db.Query(`
		SELECT code, destination_url, created_at, expires_at, redirect_count
		FROM links
	`)
	if err != nil {
		return nil, fmt.Errorf("list links: %v", err)
	}
	defer rows.Close()

	links := []Link{}

	for rows.Next() {
		var link Link
		var createdAt string
		var expiresAt int64
		if err := rows.Scan(
			&link.Code,
			&link.DestinationURL,
			&createdAt,
			&expiresAt,
			&link.RedirectCount,
		); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}

		parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return links, fmt.Errorf("parse created_at: %w", err)
		}
		link.CreatedAt = parsedCreatedAt
		link.ExpiresAt = time.Unix(expiresAt, 0).UTC()

		links = append(links, link)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate links: %w", err)
	}

	return links, nil
}
