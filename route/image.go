package route

import (
	"bytes"
	stderrors "errors"
	"image"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"

	R "github.com/strange-dragon/metaX/constant"
	"github.com/strange-dragon/metaX/engine"
	"github.com/strange-dragon/metaX/engine/providerid"
	"github.com/strange-dragon/metaX/imageutil"
	"github.com/strange-dragon/metaX/imageutil/badge"
	mt "github.com/strange-dragon/metaX/provider"
	"github.com/strange-dragon/metaX/routing"
)

type imageType uint8

const (
	primaryImageType imageType = iota
	thumbImageType
	backdropImageType
)

type imageUri struct {
	infoUri // same as info uri
}

type imageQuery struct {
	URL      string  `form:"url"`
	Ratio    float64 `form:"ratio"`
	Position float64 `form:"pos"`
	Auto     bool    `form:"auto"`
	Badge    string  `form:"badge"`
	Quality  int     `form:"quality"`
	Category string  `form:"category"`
}

func getImage(app *engine.Engine, typ imageType) gin.HandlerFunc {
	var ratio float64
	switch typ {
	case primaryImageType:
		ratio = R.PrimaryImageRatio
	case thumbImageType:
		ratio = R.ThumbImageRatio
	case backdropImageType:
		ratio = R.BackdropImageRatio
	default:
		panic("invalid image type")
	}

	return func(c *gin.Context) {
		uri := &imageUri{}
		if err := c.ShouldBindUri(uri); err != nil {
			abortWithStatusMessage(c, http.StatusBadRequest, err)
			return
		}
		query := &imageQuery{
			Ratio:    -1,
			Position: -1,
			Quality:  90,
		}
		if err := c.ShouldBindQuery(query); err != nil {
			abortWithStatusMessage(c, http.StatusBadRequest, err)
			return
		}

		requestApp, requestErr := requestEngine(c, app)
		if requestErr != nil {
			abortWithStatusMessage(c, http.StatusBadRequest, requestErr)
			return
		}
		app = requestApp

		// The merged search candidate carries the reserved provider name.
		// Resolve it to real providers before the stock image paths: artwork is
		// served from the configured image provider first, then route order, and
		// the candidates below are tried in that order.
		var candidates []providerid.ProviderID
		if engine.IsAggregatedProvider(uri.Provider) {
			resolved, resolveErr := app.ResolveAggregatedProviderIDs(uri.ID, routing.Category(query.Category), routing.ActiveConfig(), engine.AggregateImageCandidateLimit)
			if resolveErr != nil {
				if stderrors.Is(resolveErr, routing.ErrUnknownCategory) || stderrors.Is(resolveErr, routing.ErrNoAvailableProvider) {
					abortWithStatusMessage(c, http.StatusBadRequest, resolveErr)
					return
				}
				abortWithError(c, resolveErr)
				return
			}
			candidates = resolved
			uri.Provider = resolved[0].Provider
			uri.ID = resolved[0].ID
		}

		// TODO: how to handle providers that implement
		//   both actor and movie provider interfaces?
		var isActorProvider bool
		switch {
		case app.IsActorProvider(uri.Provider):
			isActorProvider = true
		case app.IsMovieProvider(uri.Provider):
			isActorProvider = false
		default:
			abortWithError(c, mt.ErrProviderNotFound)
			return
		}

		var (
			img image.Image
			err error
		)
		switch {
		case len(candidates) > 0 && query.URL == "":
			// Merged identity: pick the sharpest artwork across the candidate
			// chain instead of trusting a single provider's image host.
			img, err = app.GetAggregatedMovieImage(candidates, movieImageKind(typ), query.Ratio, query.Position)
		case query.URL != "" /* specified URL */ :
			// The bytes come from the caller-supplied URL, so resolution is
			// irrelevant; walk the chain only to find a fetcher that accepts it.
			fetchers := candidates
			if len(fetchers) == 0 {
				fetchers = []providerid.ProviderID{uri.AsProviderID()}
			}
			// query.Ratio should apply only to the primary images.
			if typ != primaryImageType || query.Ratio < 0 {
				query.Ratio = ratio
			}
			deadline := time.Now().Add(engine.AggregateImageBudget)
			for i, pid := range fetchers {
				if i > 0 && time.Now().After(deadline) {
					// Each fetcher spends its own download timeout, and four of
					// those in a row is what a scan looks like as a hang.
					break
				}
				var provider mt.Provider
				if isActorProvider {
					if !app.IsActorProvider(pid.Provider) {
						continue
					}
					provider = app.MustGetActorProviderByName(pid.Provider)
				} else {
					if !app.IsMovieProvider(pid.Provider) {
						continue
					}
					provider = app.MustGetMovieProviderByName(pid.Provider)
				}
				if img, err = app.GetImageByURL(provider, query.URL, query.Ratio, query.Position, query.Auto); err == nil {
					break
				}
			}
			if img == nil && len(candidates) > 0 {
				// The caller-supplied URL comes from the provider that won the
				// artwork merge. When this deployment cannot fetch that host —
				// JavDB's image CDN answers 403 to datacenter IPs — serve the
				// candidate chain's own artwork instead of failing the image.
				img, err = app.GetAggregatedMovieImage(candidates, movieImageKind(typ), query.Ratio, query.Position)
			}
		case isActorProvider /* actor */ :
			switch typ {
			case primaryImageType:
				img, err = app.GetActorPrimaryImage(uri.AsProviderID())
			case thumbImageType, backdropImageType:
				abortWithStatusMessage(c, http.StatusBadRequest, "unsupported image type")
				return
			}
		default: /* movie */
			switch typ {
			case primaryImageType:
				img, err = app.GetMoviePrimaryImage(uri.AsProviderID(), query.Ratio, query.Position)
			case thumbImageType:
				img, err = app.GetMovieThumbImage(uri.AsProviderID())
			case backdropImageType:
				img, err = app.GetMovieBackdropImage(uri.AsProviderID())
			}
		}
		if err != nil {
			abortWithError(c, err)
			return
		}

		if query.Badge != "" {
			if img, err = badge.Badge(img, query.Badge); err != nil {
				abortWithError(c, err)
				return
			}
		}

		c.Header("X-MetaTube-Image-Width", strconv.Itoa(img.Bounds().Dx()))
		c.Header("X-MetaTube-Image-Height", strconv.Itoa(img.Bounds().Dy()))

		buf := &bytes.Buffer{}
		if err = imageutil.EncodeToJPEG(buf, img, query.Quality); err != nil {
			panic(err)
		}

		c.Render(http.StatusOK, render.Reader{
			ContentType:   jpegImageMIMEType,
			ContentLength: int64(buf.Len()),
			Reader:        buf,
		})
	}
}

const jpegImageMIMEType = "image/jpeg"

func movieImageKind(typ imageType) engine.MovieImageKind {
	switch typ {
	case thumbImageType:
		return engine.MovieImageThumb
	case backdropImageType:
		return engine.MovieImageBackdrop
	default:
		return engine.MovieImagePrimary
	}
}
