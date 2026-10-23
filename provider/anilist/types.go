package anilist

import "github.com/strange-dragon/metaX/provider"

type animeTitles struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
	Native  string `json:"native"`
}

type animeDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

type animeCover struct {
	ExtraLarge string `json:"extraLarge"`
	Large      string `json:"large"`
	Medium     string `json:"medium"`
}

type animeName struct {
	Full   string `json:"full"`
	Native string `json:"native"`
}

type animeNamedNode struct {
	Name animeName `json:"name"`
}

type animeMedia struct {
	ID           int         `json:"id"`
	Title        animeTitles `json:"title"`
	Description  string      `json:"description"`
	CoverImage   animeCover  `json:"coverImage"`
	StartDate    animeDate   `json:"startDate"`
	Duration     int         `json:"duration"`
	AverageScore int         `json:"averageScore"`
	Genres       []string    `json:"genres"`
	Studios      struct {
		Edges []struct {
			Node struct {
				Name string `json:"name"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"studios"`
	Staff struct {
		Edges []struct {
			Role string         `json:"role"`
			Node animeNamedNode `json:"node"`
		} `json:"edges"`
	} `json:"staff"`
	Characters struct {
		Edges []struct {
			Role string `json:"role"`
			Node struct {
				Name animeName `json:"name"`
			} `json:"node"`
			VoiceActors []struct {
				Name animeName `json:"name"`
			} `json:"voiceActors"`
		} `json:"edges"`
	} `json:"characters"`
}

const searchQuery = `query ($search: String, $page: Int, $perPage: Int) {
  Page(page: $page, perPage: $perPage) {
    media(search: $search, type: ANIME, isAdult: true, sort: [SEARCH_MATCH]) {
      id
      title { romaji english native }
      coverImage { extraLarge large medium }
      startDate { year month day }
      duration
      averageScore
      genres
      studios(isMain: true) { edges { node { name } } }
    }
  }
}`

const detailQuery = `query ($id: Int) {
  Media(id: $id, type: ANIME) {
    id
    title { romaji english native }
    description(asHtml: false)
    coverImage { extraLarge large medium }
    startDate { year month day }
    duration
    averageScore
    genres
    studios(isMain: true) { edges { node { name } } }
    staff(perPage: 20) { edges { role node { name { full native } } } }
    characters(perPage: 20, sort: [ROLE, RELEVANCE, ID]) {
      edges {
        role
        node { name { full native } }
        voiceActors(language: JAPANESE) { name { full native } }
      }
    }
  }
}`

func init() {
	provider.Register(Name, New)
}
