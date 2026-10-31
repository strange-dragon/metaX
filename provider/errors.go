package provider

import (
	"net/http"

	"github.com/strange-dragon/metaX/errors"
)

var (
	ErrInvalidID          = errors.New(http.StatusBadRequest, "invalid id")
	ErrInvalidURL         = errors.New(http.StatusBadRequest, "invalid url")
	ErrInvalidKeyword     = errors.New(http.StatusBadRequest, "invalid keyword")
	ErrInfoNotFound       = errors.New(http.StatusNotFound, "info not found")
	ErrImageNotFound      = errors.New(http.StatusNotFound, "image not found")
	ErrProviderNotFound   = errors.New(http.StatusNotFound, "provider not found")
	ErrIncompleteMetadata = errors.New(http.StatusInternalServerError, "incomplete metadata")
)

// ErrMovieNotFound and ErrActorNotFound are the HTTP-aware roots of the
// per-provider not-found sentinels. A provider wraps one of them with its own
// prefix so the HTTP layer can answer 404 for "the source has no such record"
// instead of an opaque 500, while the provider keeps its own message:
//
//	ErrMovieNotFound = fmt.Errorf("airav: %w", provider.ErrMovieNotFound)
var (
	ErrMovieNotFound = errors.New(http.StatusNotFound, "movie not found")
	ErrActorNotFound = errors.New(http.StatusNotFound, "actor not found")
)
