package engine

import (
	"image"

	"github.com/strange-dragon/metaX/common/number"
	R "github.com/strange-dragon/metaX/constant"
	"github.com/strange-dragon/metaX/detector"
	"github.com/strange-dragon/metaX/engine/providerid"
	"github.com/strange-dragon/metaX/imageutil"
	"github.com/strange-dragon/metaX/model"
	mt "github.com/strange-dragon/metaX/provider"
)

// Default position constants for different kind of images.
const (
	defaultActorPrimaryImagePosition  = 0.5
	defaultMoviePrimaryImagePosition  = 1.0
	defaultMovieThumbImagePosition    = 0.5
	defaultMovieBackdropImagePosition = 0.0
)

func (e *Engine) GetActorPrimaryImage(pid providerid.ProviderID) (image.Image, error) {
	info, err := e.GetActorInfoByProviderID(pid, true)
	if err != nil {
		return nil, err
	}
	if len(info.Images) == 0 {
		return nil, mt.ErrImageNotFound
	}
	return e.GetImageByURL(
		e.MustGetActorProviderByName(pid.Provider), info.Images[0],
		R.PrimaryImageRatio, defaultActorPrimaryImagePosition, false,
	)
}

// MovieImageKind selects the derived image an aggregated request wants.
type MovieImageKind uint8

const (
	MovieImagePrimary MovieImageKind = iota
	MovieImageThumb
	MovieImageBackdrop
)

const (
	// AggregateImageCandidateLimit bounds how many providers the aggregated
	// image path consults before settling for the sharpest artwork it found.
	AggregateImageCandidateLimit = 4
	// AggregateImageMinArea is the source pixel count at which artwork counts
	// as sharp enough to stop asking the remaining providers. It sits between
	// JavDB's 292x438 covers (~128k) and the 800x538 DMM/JavBus artwork
	// (~430k) those sites serve for the same title.
	AggregateImageMinArea = 180_000
)

// GetAggregatedMovieImage walks candidates in the given order and returns the
// sharpest artwork it can get for the requested kind. Resolution is compared
// before cropping so a provider is judged on what it actually serves: the
// preferred provider wins whenever it has a usable image, and providers that
// return nothing are skipped. Artwork sharper than AggregateImageMinArea ends
// the walk immediately, which keeps the common case at a single download.
func (e *Engine) GetAggregatedMovieImage(candidates []providerid.ProviderID, kind MovieImageKind, ratio, pos float64) (image.Image, error) {
	var (
		best     image.Image
		bestInfo *model.MovieInfo
		bestArea int
		lastErr  error
	)
	for _, pid := range candidates {
		raw, info, err := e.getMovieArtwork(pid, kind == MovieImagePrimary)
		if err != nil {
			lastErr = err
			continue
		}
		if area := imageArea(raw); area > bestArea {
			best, bestInfo, bestArea = raw, info, area
		}
		if bestArea >= AggregateImageMinArea {
			break
		}
	}
	if best == nil {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, mt.ErrImageNotFound
	}
	return e.cropMovieImage(best, bestInfo, kind, ratio, pos), nil
}

func imageArea(img image.Image) int {
	bounds := img.Bounds()
	return bounds.Dx() * bounds.Dy()
}

// getMovieArtwork downloads the provider's artwork without cropping so callers
// can compare source resolutions across providers.
func (e *Engine) getMovieArtwork(pid providerid.ProviderID, preferBigThumb bool) (image.Image, *model.MovieInfo, error) {
	if !e.IsMovieProvider(pid.Provider) {
		return nil, nil, mt.ErrProviderNotFound
	}
	rawURL, info, err := e.getPreferredMovieImageURLAndInfo(pid, preferBigThumb)
	if err != nil {
		return nil, nil, err
	}
	img, err := e.getImageByURL(e.MustGetMovieProviderByName(pid.Provider), rawURL)
	if err != nil {
		return nil, nil, err
	}
	return img, info, nil
}

// cropMovieImage applies the same ratio and position rules the per-kind image
// endpoints use, so a candidate chosen for its resolution still produces the
// framing the caller asked for.
func (e *Engine) cropMovieImage(img image.Image, info *model.MovieInfo, kind MovieImageKind, ratio, pos float64) image.Image {
	switch kind {
	case MovieImageThumb:
		return imageutil.CropImagePosition(img, R.ThumbImageRatio, defaultMovieThumbImagePosition)
	case MovieImageBackdrop:
		return imageutil.CropImagePosition(img, R.BackdropImageRatio, defaultMovieBackdropImagePosition)
	}
	if ratio < 0 /* default primary ratio */ {
		ratio = R.PrimaryImageRatio
	}
	if pos < 0 /* manual position disabled */ {
		pos = defaultMoviePrimaryImagePosition
		if info != nil && number.RequiresFaceDetection(info.Number) {
			if detected, found := detector.FindPrimaryFaceAxisRatio(img, ratio, true); found {
				pos = detected
			}
		}
	}
	return imageutil.CropImagePosition(img, ratio, pos)
}

func (e *Engine) GetMoviePrimaryImage(pid providerid.ProviderID, ratio, pos float64) (image.Image, error) {
	url, info, err := e.getPreferredMovieImageURLAndInfo(pid, true)
	if err != nil {
		return nil, err
	}
	if ratio < 0 /* default primary ratio */ {
		ratio = R.PrimaryImageRatio
	}
	var auto bool
	if pos < 0 /* manual position disabled */ {
		pos = defaultMoviePrimaryImagePosition
		auto = number.RequiresFaceDetection(info.Number)
	}
	return e.GetImageByURL(
		e.MustGetMovieProviderByName(pid.Provider),
		url, ratio, pos, auto,
	)
}

func (e *Engine) GetMovieThumbImage(pid providerid.ProviderID) (image.Image, error) {
	url, _, err := e.getPreferredMovieImageURLAndInfo(pid, false)
	if err != nil {
		return nil, err
	}
	return e.GetImageByURL(
		e.MustGetMovieProviderByName(pid.Provider), url,
		R.ThumbImageRatio, defaultMovieThumbImagePosition, false,
	)
}

func (e *Engine) GetMovieBackdropImage(pid providerid.ProviderID) (image.Image, error) {
	url, _, err := e.getPreferredMovieImageURLAndInfo(pid, false)
	if err != nil {
		return nil, err
	}
	return e.GetImageByURL(
		e.MustGetMovieProviderByName(pid.Provider), url,
		R.BackdropImageRatio, defaultMovieBackdropImagePosition, false,
	)
}

func (e *Engine) GetImageByURL(provider mt.Provider, url string, ratio, pos float64, auto bool) (img image.Image, err error) {
	if img, err = e.getImageByURL(provider, url); err != nil {
		return
	}
	if auto {
		// only turn on advanced for movie providers.
		advancedMode := e.IsMovieProvider(provider.Name())
		axisR, found := detector.FindPrimaryFaceAxisRatio(img, ratio, advancedMode)
		if found {
			pos = axisR // override the default position with detected position.
		}
	}
	return imageutil.CropImagePosition(img, ratio, pos), nil
}

func (e *Engine) getImageByURL(provider mt.Provider, url string) (img image.Image, err error) {
	resp, err := e.Fetch(url, provider)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	img, _, err = imageutil.Decode(resp.Body)
	return
}

func (e *Engine) getPreferredMovieImageURLAndInfo(pid providerid.ProviderID, thumb bool) (url string, info *model.MovieInfo, err error) {
	info, err = e.GetMovieInfoByProviderID(pid, true)
	if err != nil {
		return
	}
	url = info.CoverURL
	if thumb && info.BigThumbURL != "" /* big thumb > cover */ {
		url = info.BigThumbURL
	} else if !thumb && info.BigCoverURL != "" /* big cover > cover */ {
		url = info.BigCoverURL
	}
	return
}
