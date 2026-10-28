package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const gqlURL = "https://apis.justwatch.com/graphql"

var client = &http.Client{Timeout: 20 * time.Second}

const titlesQuery = `query($c:Country!,$l:Language!,$n:Int!,$a:String,$f:TitleFilter!){
  popularTitles(country:$c, first:$n, after:$a, filter:$f){
    totalCount
    pageInfo{endCursor hasNextPage}
    edges{node{
      content(country:$c, language:$l){
        title originalReleaseYear shortDescription fullPath posterUrl
        scoring{imdbScore imdbVotes tomatoMeter}
        externalIds{imdbId}
        genres{translation(language:"en")}
      }
      offers(country:$c, platform:WEB){monetizationType standardWebURL package{packageId clearName}}
    }}
  }
}`

const genresQuery = `{genres{shortName translation(language:"en")}}`

const packagesQuery = `query($c:Country!){
  packages(country:$c, platform:WEB){packageId clearName shortName monetizationTypes}
}`

// Offer types that count as "included with a subscription / free to watch".
var streamTypes = map[string]bool{"FLATRATE": true, "FLATRATE_AND_BUY": true, "FREE": true, "ADS": true}

type Service struct {
	ID    int
	Name  string
	Short string `json:"short,omitempty"` // JustWatch short code, used for server-side filtering
	URL   string `json:"-"`               // link to the title on this service (not saved in config)
}

type Genre struct {
	Short string
	Name  string
}

type Movie struct {
	Title     string
	Year      int
	Overview  string
	IMDbID    string
	Genres    []string
	JWURL     string // title page on justwatch.com
	Poster    string // cover image URL (small JPEG)
	IMDbScore *float64
	IMDbVotes float64
	RT        *int
	Streaming []Service
	RentBuy   bool
}

func gql(query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequest("POST", gqlURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JustWatch returned HTTP %d", resp.StatusCode)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return err
	}
	if len(env.Errors) > 0 {
		return fmt.Errorf("JustWatch: %s", env.Errors[0].Message)
	}
	return json.Unmarshal(env.Data, out)
}

// Find runs a title query with the given JustWatch filter and returns up to n
// movies plus the total number of matches.
// Find runs a title query and returns up to n movies (fetched in pages of at
// most 100, which is JustWatch's limit) plus the total number of matches.
func Find(filter map[string]any, country string, n int) ([]Movie, int, error) {
	filter["objectTypes"] = []string{"MOVIE"}
	var all []Movie
	var total int
	var after any
	for len(all) < n {
		page, t, cursor, more, err := findPage(filter, country, min(100, n-len(all)), after)
		if err != nil {
			return nil, 0, err
		}
		all, total = append(all, page...), t
		if !more || cursor == "" {
			break
		}
		after = cursor
	}
	return all, total, nil
}

func findPage(filter map[string]any, country string, n int, after any) ([]Movie, int, string, bool, error) {
	var d struct {
		PopularTitles struct {
			TotalCount int `json:"totalCount"`
			PageInfo   struct {
				EndCursor string `json:"endCursor"`
				HasNext   bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
			Edges []struct {
				Node struct {
					Content struct {
						Title            string `json:"title"`
						Year             int    `json:"originalReleaseYear"`
						ShortDescription string `json:"shortDescription"`
						FullPath         string `json:"fullPath"`
						PosterURL        string `json:"posterUrl"`
						Scoring          *struct {
							IMDbScore *float64 `json:"imdbScore"`
							IMDbVotes *float64 `json:"imdbVotes"`
							RT        *int     `json:"tomatoMeter"`
						} `json:"scoring"`
						Genres []struct {
							Name string `json:"translation"`
						} `json:"genres"`
						ExternalIDs *struct {
							IMDbID string `json:"imdbId"`
						} `json:"externalIds"`
					} `json:"content"`
					Offers []struct {
						Type    string `json:"monetizationType"`
						URL     string `json:"standardWebURL"`
						Package struct {
							ID   int    `json:"packageId"`
							Name string `json:"clearName"`
						} `json:"package"`
					} `json:"offers"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"popularTitles"`
	}
	if err := gql(titlesQuery, map[string]any{"c": country, "l": "en", "n": n, "a": after, "f": filter}, &d); err != nil {
		return nil, 0, "", false, err
	}
	var movies []Movie
	for _, e := range d.PopularTitles.Edges {
		c := e.Node.Content
		m := Movie{Title: c.Title, Year: c.Year, Overview: c.ShortDescription}
		if c.PosterURL != "" {
			m.Poster = "https://images.justwatch.com" +
				strings.NewReplacer("{profile}", "s332", "{format}", "jpg").Replace(c.PosterURL)
		}
		for _, g := range c.Genres {
			m.Genres = append(m.Genres, g.Name)
		}
		if c.FullPath != "" {
			m.JWURL = "https://www.justwatch.com" + c.FullPath
		}
		if c.ExternalIDs != nil {
			m.IMDbID = c.ExternalIDs.IMDbID
		}
		if c.Scoring != nil {
			m.IMDbScore, m.RT = c.Scoring.IMDbScore, c.Scoring.RT
			if c.Scoring.IMDbVotes != nil {
				m.IMDbVotes = *c.Scoring.IMDbVotes
			}
		}
		seen := map[int]bool{}
		for _, o := range e.Node.Offers {
			if !streamTypes[o.Type] {
				m.RentBuy = true
			} else if !seen[o.Package.ID] {
				seen[o.Package.ID] = true
				m.Streaming = append(m.Streaming, Service{ID: o.Package.ID, Name: o.Package.Name, URL: o.URL})
			}
		}
		sort.Slice(m.Streaming, func(i, j int) bool { return m.Streaming[i].Name < m.Streaming[j].Name })
		movies = append(movies, m)
	}
	pi := d.PopularTitles.PageInfo
	return movies, d.PopularTitles.TotalCount, pi.EndCursor, pi.HasNext, nil
}

func Services(country string) ([]Service, error) {
	var d struct {
		Packages []struct {
			ID    int      `json:"packageId"`
			Name  string   `json:"clearName"`
			Short string   `json:"shortName"`
			Types []string `json:"monetizationTypes"`
		} `json:"packages"`
	}
	if err := gql(packagesQuery, map[string]any{"c": country}, &d); err != nil {
		return nil, err
	}
	var out []Service
	for _, p := range d.Packages {
		for _, t := range p.Types {
			if streamTypes[t] {
				out = append(out, Service{ID: p.ID, Name: p.Name, Short: p.Short})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func Genres() ([]Genre, error) {
	var d struct {
		Genres []struct {
			Short string `json:"shortName"`
			Name  string `json:"translation"`
		} `json:"genres"`
	}
	if err := gql(genresQuery, nil, &d); err != nil {
		return nil, err
	}
	var out []Genre
	for _, g := range d.Genres {
		out = append(out, Genre{g.Short, g.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
