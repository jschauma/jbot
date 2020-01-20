/* This file contains functionality around the
 * '!doh' command, letting the user compare DNS
 * results from different DNS-over-HTTPS providers.
 *
 * Usage:
 * !doh [-c country] name [type]
 */

package main

import (
	"strings"
)

func init() {
	COMMANDS["doh"] = &Command{cmdDoh,
		"display DNS-over-HTTPS results from a few providers",
		"puddy(1)",
		"!doh [-c country] name [type]",
		nil}
}

func cmdDoh(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["doh"].Usage
		return
	}

	var country string
	var name string
	var rrtype string
	if args[0] == "-c" {
		if len(args) < 3 {
			result = "'-c' requires an argument in addition to the name to look up."
			return
		}
		country = args[1]
		args = args[2:]
	}

	if len(args) > 2 {
		result = "Usage: " + COMMANDS["doh"].Usage
		return
	}

	name = args[0]
	if len(args) > 1 {
		rrtype = args[1]
	}

	/* Just in case Slack turned the name into URL. */
	name = strings.TrimPrefix(name, "https://")
	name = strings.TrimSuffix(name, "/")

	cmd := "puddy -d"
	if len(country) > 0 {
		cmd += " -c " + country
	}
	cmd += " " + name
	if len(rrtype) > 0 {
		cmd += " " + rrtype
	}

	out, _ := runCommand(cmd)
	result = string(out)

	return
}
