/* This file contains functionality around the
 * '!ct' command, letting the user display certificate
 * transparency log information from the
 * https://crt.sh/ site.
 *
 * Usage:
 * !ct <name|serial=serial>
 */

package main

import (
	"fmt"
	"net/url"
	"strings"
)

func init() {
	COMMANDS["ct"] = &Command{cmdCt,
		"display certificate transparency information",
		"https://crt.sh/?",
		"!ct [name|serial=<serial>]",
		nil}
}

func cmdCt(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["ct"].Usage
		return
	}

	input := args[0]
	input = strings.TrimPrefix(input, "https://")
	input = strings.TrimSuffix(input, "/")

	theURL := COMMANDS["ct"].How
	if !strings.Contains(input, "=") {
		theURL += "q=" + url.QueryEscape(input)
	} else {
		kv := strings.SplitN(input, "=", 2)
		theURL += url.QueryEscape(kv[0]) + "=" + url.QueryEscape(kv[1])
	}

	column_count := 0
	type ctresult struct {
		ID         string
		CommonName string
		LoggedAt   string
		NotBefore  string
		NotAfter   string
		IssuerName string
		SANs       string
	}

	var ctr ctresult

	data := string(getURLContents(theURL, nil))
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "<TD style=\"text-align:center\">") {
			column_count++
			switch column_count {
			case 1:
				ctr.ID = dehtmlify(line)
				in, cn, sans := getCNsFromCTID(ctr.ID)
				ctr.IssuerName = in
				ctr.CommonName = cn
				ctr.SANs = sans
			case 2:
				ctr.LoggedAt = dehtmlify(line)
			case 3:
				ctr.NotBefore = dehtmlify(line)
			case 4:
				column_count = 0
				ctr.NotAfter = dehtmlify(line)
				result = fmt.Sprintf("crt.sh ID <%sid=%s|%s>\n```", COMMANDS["ct"].How, ctr.ID, ctr.ID)
				result += fmt.Sprintf("Logged At  : %s\n", ctr.LoggedAt)
				result += fmt.Sprintf("Not Before : %s\n", ctr.NotBefore)
				result += fmt.Sprintf("Not After  : %s\n", ctr.NotAfter)
				result += fmt.Sprintf("Common Name: %s\n", ctr.CommonName)
				result += fmt.Sprintf("SANs       : %s\n", ctr.SANs)
				result += fmt.Sprintf("Issuer Name: %s\n```", ctr.IssuerName)
				return
			}
		}
	}

	result = fmt.Sprintf("No result found at '%s'.\n", theURL)
	return
}

func getCNsFromCTID(id string) (in, cn, sans string) {
	theURL := COMMANDS["ct"].How + "id=" + id
	data := string(getURLContents(theURL, nil))
	cns := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		for _, l := range strings.Split(line, "<BR>") {
			if strings.Contains(l, "commonName") {
				l = strings.Replace(l, "&nbsp;", " ", -1)
				kv := strings.Split(l, "=")
				if len(kv) > 1 {
					cns = append(cns, dehtmlify(kv[1]))
				}
			} else if strings.Contains(l, "DNS:") {
				l = strings.Replace(l, "&nbsp;", " ", -1)
				sans += dehtmlify(strings.Replace(l, "DNS:", "", -1)) + " "
			}
		}
	}

	return cns[0], cns[1], sans
}
