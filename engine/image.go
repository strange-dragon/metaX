package engine

import (
	"context"
	"errors"
	"fmt"
	"image"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	//
	// The window is wider than the number of *hosts* a title usually has,
	// because several providers share one image host and a chain that spends
	// its whole budget on the same CDN stops at the first host that is down.
	// Six candidates are enough to reach the independent hosts a Japanese
	// title tends to have — DMM, the provider's own host, JavDB's static
	// host, and the uncensored-only CDNs — while the host short-circuit below
	// keeps the extra candidates from costing an extra download.
	AggregateImageCandidateLimit = 6
	// AggregateImageSearchLimit bounds how many *network searches* resolving
	// that window may spend. Looking a candidate up in the local catalog is
	// free, so the window can be wide without paying for it; searching a site
	// that never carried the title is what costs seconds, and past three of
	// those the request is better answered with the artwork already at hand.
	AggregateImageSearchLimit = 3
	// AggregateImageMinArea is the source pixel count at which artwork counts
	// as sharp enough to stop asking the remaining providers. It sits between
	// JavDB's 292x438 covers (~128k) and the 800x538 DMM/JavBus artwork
	// (~430k) those sites serve for the same title.
	AggregateImageMinArea = 180_000
	// AggregateImageBudget bounds the whole candidate walk. A provider that is
	// behind an edge challenge spends its own timeout before failing, and four
	// of those in a row would reach the 100s gateway limit the way JavLibrary
	// once did. The budget is checked between candidates: a hung provider can
	// still overrun it by one attempt, never by four.
	AggregateImageBudget = 30 * time.Second
)

// GetAggregatedMovieImage walks candidates in the given order and returns the
// sharpest artwork it can get for the requested kind. Resolution is compared
// before cropping so a provider is judged on what it actually serves: the
// preferred provider wins whenever it has a usable image, and providers that
// return nothing are skipped. Artwork sharper than AggregateImageMinArea ends
// the walk immediately, which keeps the common case at a single download.
//
// A host that fails for reasons that are about the host — a connection that
// never opens, a 403, a rate limit — is not asked again for the rest of the
// walk. Providers sharing an image CDN are the norm (FANZA, Jav321 and others
// hand out pics.dmm.co.jp), so without this the budget gets spent four times
// on the same unreachable CDN and the independent source behind it is never
// reached.
func (e *Engine) GetAggregatedMovieImage(candidates []providerid.ProviderID, kind MovieImageKind, ratio, pos float64) (image.Image, error) {
	var (
		best     image.Image
		bestInfo *model.MovieInfo
		bestArea int
		firstErr error
	)
	preferBigThumb := kind == MovieImagePrimary
	deadHosts := make(map[string]struct{}, len(candidates))
	deadline := time.Now().Add(AggregateImageBudget)
	for i, pid := range candidates {
		if i > 0 && time.Now().After(deadline) {
			// Everything attempted so far failed or came back small, and the
			// remaining candidates would not finish before the gateway cuts
			// the request. Answer with what is at hand.
			break
		}
		raw, info, err := e.getMovieArtwork(pid, preferBigThumb, deadHosts)
		if err != nil {
			// Report the preferred provider's failure, not whichever provider
			// happened to be tried last: the first candidate is the one the
			// caller expects artwork from, so its error is the useful one.
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", pid.Provider, err)
			}
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
		if firstErr != nil {
			return nil, firstErr
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
// can compare source resolutions across providers. Hosts in deadHosts are
// skipped before their bytes are fetched, and a host-wide failure is recorded
// so later candidates on the same CDN cost nothing.
func (e *Engine) getMovieArtwork(pid providerid.ProviderID, preferBigThumb bool, deadHosts map[string]struct{}) (image.Image, *model.MovieInfo, error) {
	if !e.IsMovieProvider(pid.Provider) {
		return nil, nil, mt.ErrProviderNotFound
	}
	img, info, _, err := e.downloadMovieArtwork(pid, preferBigThumb, deadHosts)
	return img, info, err
}

// downloadMovieArtwork resolves the provider's artwork URL, downloads it, and
// retries once with freshly fetched metadata when that download fails.
//
// Cached metadata carries whatever artwork URL the provider served when the
// title was first scraped, and image CDNs do move: JavDB's host answers 403 to
// datacenter IPs, Jav321's moves pictures to pics.dmm.co.jp. Reusing the stored
// URL made every image request fail for that title until the row was evicted,
// which is exactly what the Jellyfin plugin reported as "no poster".
//
// The returned host is the picture host the attempt was for, so the caller can
// remember a host that is down instead of paying its timeout once per provider.
func (e *Engine) downloadMovieArtwork(pid providerid.ProviderID, thumb bool, deadHosts map[string]struct{}) (img image.Image, info *model.MovieInfo, host string, err error) {
	if deadHosts == nil {
		// Single-provider callers have no walk to share the verdict with.
		deadHosts = make(map[string]struct{}, 1)
	}
	provider := e.MustGetMovieProviderByName(pid.Provider)
	rawURL, info, err := e.getPreferredMovieImageURLAndInfo(pid, thumb, true)
	if err != nil {
		return nil, nil, "", err
	}
	host = artworkHost(rawURL)
	if host != "" {
		if _, dead := deadHosts[host]; dead {
			return nil, nil, host, mt.ErrImageNotFound
		}
	}
	if img, err = e.getImageByURL(provider, rawURL); err == nil {
		return img, info, host, nil
	}
	// Keep the cached attempt's error: it names the URL the caller asked for.
	cachedErr := err
	if host != "" && hostWideFailure(cachedErr) {
		deadHosts[host] = struct{}{}
	}
	freshURL, freshInfo, freshErr := e.getPreferredMovieImageURLAndInfo(pid, thumb, false)
	if freshErr != nil || freshURL == "" || freshURL == rawURL {
		return nil, nil, host, cachedErr
	}
	if img, err = e.getImageByURL(provider, freshURL); err != nil {
		return nil, nil, host, cachedErr
	}
	return img, freshInfo, host, nil
}

// artworkHost names the picture host of a URL, lowercased, or "" when the URL
// does not parse. Two providers publishing artwork on the same hostname share
// one source no matter how different their pages are.
//
// The port is part of the key on purpose: two servers on one machine are two
// sources, and the image CDNs that matter here all answer on the default port,
// so keeping it changes nothing for them.
func artworkHost(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// hostWideFailure reports whether an artwork download failed in a way that is
// about the host rather than about this one picture. A missing file says
// nothing about the CDN and the next candidate on it may still answer; a
// refused connection, a timeout, a 403 or a rate limit is the host telling
// every client the same thing.
func hostWideFailure(err error) bool {
	if err == nil {
		return false
	}
	// "This provider has no such picture" is a per-picture answer, not a host
	// verdict, and it is what most misses look like after the provider's own
	// error mapping.
	for _, known := range []error{mt.ErrImageNotFound, mt.ErrMovieNotFound, mt.ErrInfoNotFound, mt.ErrInvalidID, mt.ErrInvalidURL} {
		if errors.Is(err, known) {
			return false
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"403", "401", "429", "500", "502", "503", "504",
		"forbidden", "too many requests", "timeout", "timed out",
		"deadline exceeded", "connection refused", "connection reset",
		"no such host", "network is unreachable", "cloudflare",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
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
	img, info, _, err := e.downloadMovieArtwork(pid, true, nil)
	if err != nil {
		return nil, err
	}
	return e.cropMovieImage(img, info, MovieImagePrimary, ratio, pos), nil
}

func (e *Engine) GetMovieThumbImage(pid providerid.ProviderID) (image.Image, error) {
	img, _, _, err := e.downloadMovieArtwork(pid, false, nil)
	if err != nil {
		return nil, err
	}
	return e.cropMovieImage(img, nil, MovieImageThumb, R.ThumbImageRatio, defaultMovieThumbImagePosition), nil
}

func (e *Engine) GetMovieBackdropImage(pid providerid.ProviderID) (image.Image, error) {
	img, _, _, err := e.downloadMovieArtwork(pid, false, nil)
	if err != nil {
		return nil, err
	}
	return e.cropMovieImage(img, nil, MovieImageBackdrop, R.BackdropImageRatio, defaultMovieBackdropImagePosition), nil
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
	// A hotlink-protected image host answers 403 and a rate limiter answers
	// 429, both with an HTML body. Decoding that body only ever reported
	// "unknown image format", which hid the reason and made the failure look
	// like a broken picture instead of a host that refuses this client.
	if resp.StatusCode >= http.StatusBadRequest {
		// A missing file keeps the 404 the HTTP layer answers with; anything
		// else is a host-level refusal and travels as itself so the candidate
		// walk can tell the two apart.
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			return nil, fmt.Errorf("%s: %w", url, mt.ErrImageNotFound)
		}
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	img, _, err = imageutil.Decode(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return img, nil
}

// getPreferredMovieImageURLAndInfo resolves the artwork URL for a provider.
// lazy=true trusts the metadata cache; lazy=false asks the provider again,
// which is how a stale artwork URL gets refreshed after a failed download.
func (e *Engine) getPreferredMovieImageURLAndInfo(pid providerid.ProviderID, thumb, lazy bool) (url string, info *model.MovieInfo, err error) {
	info, err = e.GetMovieInfoByProviderID(pid, lazy)
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
