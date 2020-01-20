/* This file contains functionality around the
 * '!beer' command.
 */

package main

import (
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func init() {
	COMMANDS["beer"] = &Command{cmdBeer,
		"quench your thirst",
		"https://www.beeradvocate.com/",
		"!beer <beer>",
		nil}
}

func cmdBeer(r Recipient, chName string, args []string) (result string) {
	bType := "search"
	theUrl := fmt.Sprintf("%ssearch/?qt=beer&q=", COMMANDS["beer"].How)
	if len(args) < 1 {
		bType = "top"
		theUrl = fmt.Sprintf("%slists/top/", COMMANDS["beer"].How)
	}

	if args[0] == "me" {
		args[0] = r.MentionName
	}

	wantedBeer := strings.Join(args, " ")

	theUrl += url.QueryEscape(wantedBeer)
	data := getURLContents(theUrl, nil)

	type Beer struct {
		Abv      string
		BeerType string
		Brewery  string
		Name     string
		Rating   string
		Url      string
	}

	var beer Beer

	beer_re := regexp.MustCompile(`<a href="/(beer/profile/[0-9]+/[0-9]+/)"><span[^>]+>([^<]+)</span></a><br><span[^>]+><a href="/beer/profile/[0-9]+/">([^<]+)</a>`)
	top_re := regexp.MustCompile(`<a href="/(beer/profile/[0-9]+/[0-9]+/)"><b>([^<]+)</b></a><span[^>]+><br><a href="/beer/profile/[0-9]+/">([^<]+)</a><br><a href="/beer/top-rated/[0-9]+/">([^<]+)</a> \| ([0-9.]+%)</span></td><td.+><b>([0-9.]+)</b>`)

	nextField := ""

	for _, line := range strings.Split(string(data), "\n") {
		if bType != "search" {
			if strings.HasPrefix(line, "<tr><td align=") {
				beers := []Beer{}
				for _, l2 := range strings.Split(line, "</tr>") {
					if m := top_re.FindStringSubmatch(l2); len(m) > 0 {
						b := Beer{m[5], m[4], m[3], m[2], m[6], m[1]}
						beers = append(beers, b)
					}
				}
				if len(beers) > 0 {
					rand.Seed(time.Now().UnixNano())
					beer = beers[rand.Intn(len(beers))]
				}
			}
		} else {
			var data2 []byte
			if m := beer_re.FindStringSubmatch(line); len(m) > 0 {
				beer = Beer{"", "", m[3], m[2], "", m[1]}
				theUrl = fmt.Sprintf("%s%s", COMMANDS["beer"].How, m[1])
				data2 = getURLContents(theUrl, nil)
			} else if strings.Contains(line, "<title>"+wantedBeer) {
				beer = Beer{"", "", "", wantedBeer, "", ""}
				data2 = data
			}

			if len(data2) > 0 {
				next := false
				name_re := regexp.MustCompile(`<meta property="og:title" content="(.*) \| (.*)" />`)
				url_re := regexp.MustCompile(`<meta property="og:url" content="(.*)" />`)
				for _, l2 := range strings.Split(string(data2), "\n") {
					if m := name_re.FindStringSubmatch(l2); len(m) > 0 {
						beer.Name = m[1]
						beer.Brewery = m[2]
					}
					if m := url_re.FindStringSubmatch(l2); len(m) > 0 {
						beer.Url = m[1]
					}
					if strings.Contains(l2, "<b>Style:</b>") {
						nextField = "style"
						next = true
						continue
					}
					if strings.Contains(l2, "<b>Avg:</b>") {
						nextField = "avg"
						next = true
						continue
					}
					if strings.Contains(l2, "<b>ABV:</b>") {
						nextField = "abv"
						next = true
						continue
					}
					if next {
						if nextField == "abv" {
							beer.Abv = dehtmlify(l2)
						} else if nextField == "avg" {
							beer.Rating = dehtmlify(l2)
						} else if nextField == "style" {
							beer.BeerType = dehtmlify(l2)
						}
						next = false
						nextField = ""
						continue
					}
				}
				break
			}
		}
	}

	if len(beer.Name) > 0 {
		result = fmt.Sprintf("<%s%s|%s> by %s - %s\n", COMMANDS["beer"].How, beer.Url, beer.Name, beer.Brewery, beer.Rating)
		result += fmt.Sprintf("%s (%s)\n", beer.BeerType, beer.Abv)
	} else {
		result = fmt.Sprintf("No beer found for '%s'.", wantedBeer)
	}

	return
}
