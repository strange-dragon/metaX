package route

import (
	stderrors "errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/strange-dragon/metaX/engine"
	"github.com/strange-dragon/metaX/engine/providerid"
	"github.com/strange-dragon/metaX/routing"
)

type infoType uint8

const (
	actorInfoType infoType = iota
	movieInfoType
)

type infoUri struct {
	Provider string `uri:"provider" binding:"required"`
	ID       string `uri:"id" binding:"required"`
}

func (uri *infoUri) AsProviderID() providerid.ProviderID {
	return providerid.ProviderID{Provider: uri.Provider, ID: uri.ID}
}

type infoQuery struct {
	Lazy      bool   `form:"lazy"`
	Aggregate bool   `form:"aggregate"`
	Category  string `form:"category"`
}

func getInfo(app *engine.Engine, typ infoType) gin.HandlerFunc {
	return getInfoWithConfig(app, typ, routing.ActiveConfig())
}

func getInfoWithConfig(app *engine.Engine, typ infoType, routingConfig routing.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri := &infoUri{}
		if err := c.ShouldBindUri(uri); err != nil {
			abortWithStatusMessage(c, http.StatusBadRequest, err)
			return
		}
		query := &infoQuery{
			Lazy:      true,  // enable lazy by default.
			Aggregate: false, // stock detail by default; merged data is opt-in or requested via MetaX.
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

		var (
			info any
			err  error
		)
		switch typ {
		case actorInfoType:
			info, err = app.GetActorInfoByProviderID(uri.AsProviderID(), query.Lazy)
		case movieInfoType:
			if engine.IsAggregatedProvider(uri.Provider) {
				aggregated, aggErr := app.GetAggregatedMovieInfoByNumber(uri.ID, routing.Category(query.Category), query.Lazy, routingConfig)
				if aggErr == nil {
					// Keep the merged identity stable so refreshes continue to
					// resolve through the reserved provider name.
					aggregated.Provider = engine.AggregatedProviderName
					aggregated.ID = uri.ID
					info = aggregated
				}
				err = aggErr
			} else if query.Aggregate {
				info, err = app.GetAggregatedMovieInfoByProviderIDWithCategory(uri.AsProviderID(), query.Lazy, routingConfig, routing.Category(query.Category))
			} else {
				info, err = app.GetMovieInfoByProviderID(uri.AsProviderID(), query.Lazy)
			}
		default:
			panic("invalid info/metadata type")
		}
		if err != nil {
			if stderrors.Is(err, routing.ErrUnknownCategory) || stderrors.Is(err, routing.ErrNoAvailableProvider) {
				abortWithStatusMessage(c, http.StatusBadRequest, err)
				return
			}
			abortWithError(c, err)
			return
		}

		c.JSON(http.StatusOK, &responseMessage{Data: info})
	}
}
