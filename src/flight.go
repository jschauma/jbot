/* This file contains functionality around the
 * flight emissions display, which may be invoked via
 * the '!flight' command or when a chatter string
 * contains 'FRM :airplane: TO'.
 */

package main

import (
	"fmt"
	"regexp"
	"strings"
)

const TRAVELNAV_URL = "https://travelnav.com/emissions-from-<from>-to-<to>"
const WIKI_AIRPORT_URL = "https://en.wikipedia.org/wiki/List_of_airports_by_IATA_code:_"

func init() {
	COMMANDS["airport"] = &Command{cmdAirport,
		"try to determine the airport location",
		WIKI_AIRPORT_URL,
		"!airport <code>",
		nil,
	}
	COMMANDS["flight"] = &Command{cmdFlight,
		"display carbon emissions for the given flight",
		TRAVELNAV_URL,
		"!flight <from> <to>",
		[]string{":airplane:", "✈️"},
	}
}

func cmdAirport(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["airport"].Usage
		return
	}

	result = lookupAirportDetails(args[0])
	if result == args[0] {
		result = "Sorry, I'm unable to find the airport code " + args[0] + "."
	}

	return
}

func cmdFlight(r Recipient, chName string, args []string) (result string) {
	if len(args) != 2 {
		result = "Usage: " + COMMANDS["flight"].Usage
		return
	}

	from := strings.ToLower(strings.TrimSpace(args[0]))
	to := strings.ToLower(strings.TrimSpace(args[1]))

	theURL := strings.Replace(TRAVELNAV_URL, "<from>", from, -1)
	theURL = strings.Replace(theURL, "<to>", to, -1)
	data := getURLContents(theURL, nil)

	lb_re := regexp.MustCompile(`&nbsp;<strong>([0-9,]+)</strong> lbs CO2</h2>`)
	kg_re := regexp.MustCompile(`&nbsp;<strong>([0-9,]+)</strong> kg CO2e</h2>`)

	for _, line := range strings.Split(string(data), "\n") {
		m := lb_re.FindStringSubmatch(line)
		if len(m) > 0 {
			result += m[1] + " lbs CO2"
			continue
		}
		m = kg_re.FindStringSubmatch(line)
		if len(m) > 0 {
			result += " / " + m[1] + " kgs CO2e"
			break
		}
	}

	if len(result) < 1 {
		result = fmt.Sprintf("Sorry, I couldn't determine the carbon emissions for a flight from '%s' to '%s'.", from, to)
	} else {
		from = lookupAirportDetails(from)
		to = lookupAirportDetails(to)
		result = "Carbon emissions for a flight from " + from +
			" to " + to + ": " + result
	}

	return
}

func lookupAirportDetails(code string) (result string) {
	if len(code) < 1 {
		return
	}

	code = strings.ToUpper(code)
	wikiURL := "https://en.wikipedia.org/wiki/List_of_airports_by_IATA_code:_"
	wikiURL += string(code[0])
	data := getURLContents(wikiURL, nil)
	n := 0
	table_entry := fmt.Sprintf("<td>%s</td>", code)
	sup_re := regexp.MustCompile(`<sup .+?</sup>`)
	for _, line := range strings.Split(string(data), "\n") {
		if line == table_entry {
			n++
			continue
		}
		if n == 1 {
			n++
			continue
		}
		if n == 2 {
			n++
			line = sup_re.ReplaceAllString(line, "")
			result = dehtmlify(line)
			continue
		}
		if n == 3 {
			line = sup_re.ReplaceAllString(line, "")
			result += ", " + dehtmlify(line)
			break
		}
	}

	if len(result) > 0 {
		result += " (" + code + ")"
	} else {
		result = code
	}

	return
}
