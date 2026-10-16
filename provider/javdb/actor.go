// Copyright 2026 metaX contributors.
//
// Feature-level comparison reference: https://github.com/sqzw-x/amane.
// Native actor-provider implementation; no Amane source or fixtures are vendored.
package javdb

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/lib/pq"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/model"
	mt "github.com/metatube-community/metatube-sdk-go/provider"
)

const ActorProviderName = "JavDBActor"

var (
	_ mt.ActorProvider = (*JavDBActor)(nil)
	_ mt.ActorSearcher = (*JavDBActor)(nil)
	actorSlugRE        = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	actorFilmCountRE   = regexp.MustCompile(`(?i)^\d+\s*(?:部影片|movie\(s\))$`)
	actorGenderRE      = regexp.MustCompile(`(?i)^(?:男優|男优|女優|女优|male|female)$`)
	actorAvatarRE      = regexp.MustCompile(`(?i)url\(\s*['"]?(https?://[^)'"]+)['"]?\s*\)`)
)

// JavDBActor exposes JavDB's actor search and profile pages through the SDK
// ActorProvider interfaces. It shares the movie provider's hardened HTTP client.
type JavDBActor struct {
	*JavDB
}

func NewActor() *JavDBActor {
	return &JavDBActor{JavDB: New()}
}

func (p *JavDBActor) Name() string { return ActorProviderName }
func (p *JavDBActor) Language() language.Tag { return language.SimplifiedChinese }

func (p *JavDBActor) NormalizeActorID(id string) string {
	id = strings.TrimSpace(id)
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		if parsed, err := p.ParseActorIDFromURL(id); err == nil {
			return parsed
		}
	}
	return id
}

func (p *JavDBActor) actorURL(id string) string {
	u := *p.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/actors/" + url.PathEscape(id)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (p *JavDBActor) ParseActorIDFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("javdb actor: parse URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("javdb actor: expected a canonical absolute actor URL")
	}
	if !p.allowedHost(u.Hostname()) || !p.allowedRedirectURL(u) {
		return "", fmt.Errorf("javdb actor: unexpected actor origin %q", rawURL)
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) != 2 || segments[0] != "actors" || !actorSlugRE.MatchString(segments[1]) {
		return "", fmt.Errorf("javdb actor: invalid actor path %q", u.Path)
	}
	return segments[1], nil
}

func (p *JavDBActor) GetActorInfoByID(id string) (*model.ActorInfo, error) {
	id = strings.TrimSpace(id)
	if !actorSlugRE.MatchString(id) {
		return nil, fmt.Errorf("javdb actor: invalid actor ID %q", id)
	}
	return p.GetActorInfoByURL(p.actorURL(id))
}

func (p *JavDBActor) GetActorInfoByURL(rawURL string) (*model.ActorInfo, error) {
	id, err := p.ParseActorIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	doc, err := p.getHTML(rawURL)
	if err != nil {
		return nil, err
	}

	names := splitActorNames(cleanText(doc.Find("span.actor-section-name").First().Text()))
	if len(names) == 0 {
		return nil, fmt.Errorf("javdb actor: profile %q has no actor name", rawURL)
	}
	aliases := append([]string(nil), names[1:]...)
	for _, node := range doc.Find("span.section-meta").EachIter() {
		for _, token := range splitActorNames(cleanText(node.Text())) {
			if actorGenderRE.MatchString(token) || actorFilmCountRE.MatchString(token) {
				continue
			}
			if token != names[0] {
				aliases = appendUnique(aliases, token)
			}
		}
	}

	images := make([]string, 0, 1)
	style := attrText(doc.Find("div.actor-avatar span.avatar").First(), "style")
	if match := actorAvatarRE.FindStringSubmatch(style); len(match) == 2 && !strings.Contains(strings.ToLower(match[1]), "actor_unknow") {
		images = append(images, match[1])
	}
	return &model.ActorInfo{
		ID:       id,
		Name:     names[0],
		Provider: p.Name(),
		Homepage: p.actorURL(id),
		Aliases:  pq.StringArray(aliases),
		Images:   pq.StringArray(images),
	}, nil
}

func (p *JavDBActor) SearchActor(keyword string) ([]*model.ActorSearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	u := *p.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/search"
	q := u.Query()
	q.Set("q", keyword)
	q.Set("f", "actor")
	q.Set("locale", "zh")
	u.RawQuery = q.Encode()

	doc, err := p.getHTML(u.String())
	if err != nil {
		return nil, err
	}
	// Exact searches can occasionally resolve directly to an actor profile.
	if doc.Find("span.actor-section-name").Length() > 0 {
		canonical := strings.TrimSpace(attrText(doc.Find("link[rel='canonical']").First(), "href"))
		if canonical != "" {
			if info, err := p.GetActorInfoByURL(canonical); err == nil && info != nil && sameActorName(info.Name, keyword, []string(info.Aliases)) {
				return []*model.ActorSearchResult{info.ToSearchResult()}, nil
			}
		}
	}

	results := make([]*model.ActorSearchResult, 0)
	seen := make(map[string]struct{})
	doc.Find("div.actor-box a[href]").Each(func(_ int, box *goquery.Selection) {
		href := strings.TrimSpace(attrText(box, "href"))
		homepage := absoluteURL(p.baseURL, href)
		id, err := p.ParseActorIDFromURL(homepage)
		if err != nil {
			return
		}
		name := cleanText(box.Find("strong").First().Text())
		titleAliases := splitActorNames(attrText(box, "title"))
		if name == "" || !sameActorName(name, keyword, titleAliases) {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		images := pq.StringArray{}
		image := absoluteURL(p.baseURL, firstAttr(box.Find("img").First(), "data-src", "data-original", "src"))
		if image != "" {
			images = append(images, image)
		}
		result := &model.ActorSearchResult{
			ID: id, Name: name, Provider: p.Name(), Homepage: p.actorURL(id),
			Aliases: pq.StringArray(titleAliases), Images: images,
		}
		if result.IsValid() {
			results = append(results, result)
		}
	})
	return results, nil
}

func splitActorNames(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '，' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = cleanText(part)
		if part != "" {
			out = appendUnique(out, part)
		}
	}
	return out
}

func sameActorName(canonical, query string, aliases []string) bool {
	query = strings.ToLower(strings.Join(strings.Fields(query), " "))
	canonical = strings.ToLower(strings.Join(strings.Fields(canonical), " "))
	if query == canonical {
		return true
	}
	for _, alias := range aliases {
		alias = strings.ToLower(strings.Join(strings.Fields(alias), " "))
		if query == alias {
			return true
		}
	}
	return false
}

func init() {
	mt.Register(ActorProviderName, NewActor)
}
