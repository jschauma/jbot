/* This file contains functionality around the
 * '!ssllabs' command, letting the user display certificate
 * grades and information from SSLLabs.
 *
 * Usage:
 * !ssllabs <hostname>
 */

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

const SSLLABS_SLEEP = 90
const SSLLABS_MAXTRIES = 5

const SSLLABS_ANALYZE_BASEURL = "https://www.ssllabs.com/ssltest/analyze.html?d="

type SsllabsEndpoint struct {
	IpAddress         string
	ServerName        string
	StatusMessage     string
	StatusDetails     string
	Grade             string
	GradeTrustIgnored string
	HasWarnings       bool
}

type SsllabsResult struct {
	Host          string
	Status        string
	StatusMessage string
	Endpoints     []SsllabsEndpoint
}

func init() {
	COMMANDS["ssllabs"] = &Command{cmdSsllabs,
		"show SSLLabs rating for a given site",
		"https://api.ssllabs.com/api/v3/analyze?host=",
		"!ssllabs <hostname>",
		[]string{"ssllab"}}
}

func cmdSsllabs(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["ssllabs"].Usage
		return
	}
	input := strings.Join(args, " ")

	input = strings.TrimPrefix(input, "https://")
	input = strings.TrimSuffix(input, "/")

	input = fqdn(input)
	if _, err := net.LookupHost(input); err != nil {
		result = "Sorry, that does not seem to resolve right now."
		return
	}

	theURL := COMMANDS["ssllabs"].How + url.QueryEscape(input)
	ssllabs := getSsllabsResults(theURL)

	if ssllabs.Status != "READY" {
		if ssllabs.Status == "ERROR" {
			result = fmt.Sprintf("SSLLabs Error: %s\n", ssllabsStatusMessage(ssllabs))
			return
		}

		result = fmt.Sprintf("'%s' submitted to SSLLabs for analysis", input)
		if ssllabs.Status != "DNS" {
			result += fmt.Sprintf("; currently in status '%s'", ssllabsStatusMessage(ssllabs))
		}
		result += fmt.Sprintf(".\nI'll check in on this in a minute or so and get you results when they're ready.")
	}

	go showSsllabsResults(r, theURL, ssllabs)
	return
}

func getSsllabsResults(theURL string) (result SsllabsResult) {
	data := getURLContents(theURL, nil)

	err := json.Unmarshal(data, &result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to unmarshal SSLLabs data from '%s': %s\n", theURL, err)
	}
	return
}

func showSsllabsResults(r Recipient, theURL string, ssllabs SsllabsResult) {
	n := 0
	for {
		if ssllabs.Status == "READY" {
			break
		}

		if n == SSLLABS_MAXTRIES {
			msg := fmt.Sprintf("SSLLabs still doesn't have results in 'READY' state after %d * %d seconds. Giving up.\n",
				SSLLABS_MAXTRIES, SSLLABS_SLEEP)
			msg += fmt.Sprintf("Current result status is: '%s'\n", ssllabsStatusMessage(ssllabs))
			msg += "Perhaps try again in a little while.\n"
			reply(r, msg)
			return
		}

		time.Sleep(SSLLABS_SLEEP * time.Second)
		ssllabs = getSsllabsResults(theURL)
		n++
	}

	msg := fmt.Sprintf("SSLLabs Results for <%s%s|%s>:", SSLLABS_ANALYZE_BASEURL, ssllabs.Host, ssllabs.Host)
	if len(ssllabs.Endpoints) > 1 {
		msg += "\n"
	} else {
		msg += " "
	}
	for _, endpoint := range ssllabs.Endpoints {
		msg += fmt.Sprintf("<%s%s&s=%s|", SSLLABS_ANALYZE_BASEURL, ssllabs.Host, endpoint.IpAddress)

		if len(endpoint.ServerName) > 0 && endpoint.ServerName != ssllabs.Host {
			msg += fmt.Sprintf("%s> (%s)", endpoint.ServerName, endpoint.IpAddress)
		} else {
			msg += fmt.Sprintf("%s>", endpoint.IpAddress)
		}
		msg += fmt.Sprintf(": %s", endpoint.Grade)
		if endpoint.Grade != endpoint.GradeTrustIgnored {
			msg += fmt.Sprintf(" (%s, if trust ignored)", endpoint.GradeTrustIgnored)
		}
		msg += "\n"
	}

	reply(r, msg)
}

func ssllabsStatusMessage(s SsllabsResult) (msg string) {
	if len(s.StatusMessage) > 0 {
		msg = s.StatusMessage
	} else {
		msg = s.Status
	}
	return
}
