package route

import (
	"bytes"
	stderrors "errors"
	"image"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"

	R "github.com/strange-dragon/metaX/constant"
	"github.com/strange-dragon/metaX/engine"
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
		// Resolve it to the route-first real provider before the stock image paths.
		if engine.IsAggregatedProvider(uri.Provider) {
			resolved, resolveErr := app.ResolveAggregatedProviderID(uri.ID, routing.Category(query.Category), routing.ActiveConfig())
			if resolveErr != nil {
				if stderrors.Is(resolveErr, routing.ErrUnknownCategory) || stderrors.Is(resolveErr, routing.ErrNoAvailableProvider) {
					abortWithStatusMessage(c, http.StatusBadRequest, resolveErr)
					return
				}
				abortWithError(c, resolveErr)
				return
			}
			uri.Provider = resolved.Provider
			uri.ID = resolved.ID
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
		if query.URL != "" /* specified URL */ {
			var provider mt.Provider
			if isActorProvider {
				provider = app.MustGetActorProviderByName(uri.Provider)
			} else {
				provider = app.MustGetMovieProviderByName(uri.Provider)
			}
			// query.Ratio should apply only to the primary images.
			if typ != primaryImageType || query.Ratio < 0 {
				query.Ratio = ratio
			}
			img, err = app.GetImageByURL(provider, query.URL, query.Ratio, query.Position, query.Auto)
		} else if isActorProvider /* actor */ {
			switch typ {
			case primaryImageType:
				img, err = app.GetActorPrimaryImage(uri.AsProviderID())
			case thumbImageType, backdropImageType:
				abortWithStatusMessage(c, http.StatusBadRequest, "unsupported image type")
				return
			}
		} else /* movie */ {
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
