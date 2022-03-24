/* This file contains functionality around the
 * various ServiceNow commands, including "!sn",
 * "!cmrs", and their respective alerts.
 */

package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var SNOW_ALERTS = []string{"cmr-alert", "snow-alert"}

func init() {
	ALERTS["cmr-alert"] = "-c"
	ALERTS["snow-alert"] = "-s"

	COMMANDS["cmrs"] = &Command{cmdCmrs,
		"display upcoming CMRs",
		"service-now",
		"!cmrs [time[dh]|all|blocking|impacting|waiting|ongoing] [property] [state]",
		[]string{"chgs"}}
	COMMANDS["sn"] = &Command{cmdSnow,
		"show Service Now data for the given ticket",
		"cli via Yahoo::ServiceNow::Simple",
		"!sn [-S search]|[<ticket>]",
		[]string{"chg", "cm", "cmr", "inc", "snow"}}
}

func cmdCmrs(r Recipient, chName string, args []string) (result string) {
	if reject, ok := channelCheck(r, chName, true, false); !ok {
		return reject
	}

	if len(args) > 3 {
		result = "Usage: " + COMMANDS["cmrs"].Usage
		return
	}

	cmd := []string{"-c"}

	which := ""

	if len(args) > 0 {
		counterRe := regexp.MustCompile(`(([0-9]+)([hd])?|all|blocking|impacting|waiting|ongoing)`)
		m := counterRe.FindStringSubmatch(args[0])
		if len(m) < 1 {
			result = fmt.Sprintf("Invalid argument '%s' for time.\n", args[0])
			result += "Usage: " + COMMANDS["cmrs"].Usage
			return
		}
		which = m[1]
		if which != "all" {
			if which == "ongoing" {
				cmd = append(cmd, "-o")
			} else if which == "waiting" {
				cmd = append(cmd, "-w")
			} else if which == "blocking" {
				cmd = append(cmd, "-b", "'"+args[1]+"'")
				args = args[1:]
			} else if which == "impacting" {
				cmd = append(cmd, "-i", args[1])
				args = args[1:]
				if len(args) > 1 {
					cmd = append(cmd, "-q", "'"+args[1]+"'")
					args = args[1:]
				}
			} else {
				targ := args[0]
				n, _ := strconv.Atoi(m[2])
				if len(m[3]) < 1 {
					/* no [md] was given, so expand to minutes */
					n *= 60
					targ = fmt.Sprintf("%d", n)
				}
				cmd = append(cmd, "-t", targ)
			}
		}
	}

	if len(args) > 1 {
		cmd = append(cmd, "-p", args[1])
	}

	result = cmdSnow(r, chName, cmd)
	if len(result) < 1 {
		result = "No " + which + " CMRs found."
	}
	return
}

func cmdSnow(r Recipient, chName string, args []string) (result string) {
	if reject, ok := channelCheck(r, chName, true, false); !ok {
		return reject
	}

	input := strings.Join(args, " ")
	verbose(2, "Running 'snow' with '%s'...", input)
	// unmatch <#something|channel>
	if strings.HasPrefix(input, "#") {
		input = input[1:]
	}

	/* Slack expands '#channel' to e.g. '<#CBEAWGAPJ|channel>';
	 * we usually have a channel per inc, so users
	 * may invoke e.g. '!sn #inc123456' */
	slack_channel_re := regexp.MustCompile(`(?i)<(#[A-Z0-9]+)\|([^>]+)>`)
	m := slack_channel_re.FindStringSubmatch(input)
	if len(m) > 0 {
		input = m[2]
	}

	/* Sometimes people run '!inc INC123467.'.
	 * Let's be nice and let them. */
	input = strings.TrimRight(input, "!\"'#$%&()*+,-./:;<=>?@[]^_`{|}~\\")

	/* Folks also seem to (via copy and paste?)
	 * use *bold* INC numbers without noticing.
	 * I.e., '!inc *INC1234567*' - so let's trim
	 * left, too. Keep '-', though, for options
	 * below. */
	input = strings.TrimLeft(input, "!\"'#$%&()*+,./:;<=>?@[]^_`{|}~\\")

	/* 'snow' needs e.g. INC12345 to be upper
	 * case, but we want to be able to pass
	 * options; if we have options, assume the
	 * user knows what they're doing and don't
	 * uppercase.  Instead, we build a list of
	 * valid, uppercased tickets with any invalid
	 * characters stripped: */
	tickets := []string{}
	if !strings.HasPrefix(input, "-") {
		input = strings.ToUpper(input)
		/* In addition, we trim all strings that don't
		 * look like snow tickets. */
		badchars := regexp.MustCompile(`[^A-Z0-9]+`)
		snow_re := regexp.MustCompile(`^[A-Z]+[0-9]+$`)
		for _, f := range strings.Fields(input) {
			stripped := badchars.ReplaceAllString(f, "")
			if snow_re.MatchString(stripped) {
				tickets = append(tickets, stripped)
			}
		}
	} else {
		tickets = []string{input}
	}

	var lines string
	for _, t := range tickets {
		cmd := strings.TrimSpace(fmt.Sprintf("snow -u %s %s", CONFIG["mentionName"], t))
		out, _ := runCommand(cmd)
		lines += string(out)
	}

	l := strings.Split(lines, "\n")
	cmrSearch := listContains(args, "-c")
	if len(l) > 15 && (len(args) < 2 || cmrSearch) {
		result = strings.Join(l[0:15], "\n")
		result += "\n[...]\n"
		if cmrSearch {
			which := "upcoming"
			if listContains(args, "-o") {
				which = "ongoing"
			}
			result += fmt.Sprintf("<%s|All %s CMRs>", l[len(l)-2], which)
		}
	} else {
		result = strings.Join(l, "\n")
	}

	if (len(result) < 1) && (len(args) < 2) {
		result = fmt.Sprintf("No data found for '%s'.", input)
	}

	return
}

func snowAlerts(chInfo Channel) {
	for _, alert := range SNOW_ALERTS {
		snowAlert(chInfo, alert)
	}
}

func snowAlert(chInfo Channel, alert string) {
	alertSettings, found := chInfo.Settings[alert]
	if !found {
		return
	}

	verbose(4, "Running %s in '%s'...", alert, chInfo.Name)
	r := getRecipientFromMessage(fmt.Sprintf("%s@%s", CONFIG["mentionName"], chInfo.Id))
	/* Example: cmr-alert=30,Native_Ads.GLB,impacting,approved */
	setval := strings.SplitN(alertSettings, ",", 4)
	// alert=''; i.e. unset
	if len(setval[0]) < 1 {
		return
	}

	counterRe := regexp.MustCompile(`([0-9]+)([hd])?`)
	unit := ""
	num := setval[0]
	if m := counterRe.FindStringSubmatch(num); len(m) > 1 {
		num = m[1]
		unit = m[2]
	}

	alert_num, err := strconv.Atoi(num)
	if err != nil {
		msg := fmt.Sprintf("'%s' setting '%s' invalid.\n", alert, num)
		msg += fmt.Sprintf("Please change via '!set %s=<num[,[property|all][,[all|blocking|impacting|ongoing|waiting]]>'.", alert)
		reply(r, msg)
		return
	}

	/* our granularity is minutes */
	if unit == "h" {
		alert_num *= 60
	} else if unit == "d" {
		alert_num *= 60 * 24
	}

	counter_num := 0
	counter, found := chInfo.Settings[alert+"-counter"]
	if found {
		counter_num, err = strconv.Atoi(counter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid '%s-counter' for %s: %d", alert, chInfo.Name, counter_num)
			counter_num = 1
		}
	}

	ongoing := false
	/* if counter_num == 0, then we never ran */
	if counter_num == 0 || counter_num >= alert_num {
		needArg := false
		args := []string{ALERTS[alert]}
		if len(setval) > 2 {
			if setval[2] == "ongoing" {
				ongoing = true
				args = append(args, "-o")
			} else if setval[2] == "waiting" {
				args = append(args, "-w")
			} else if setval[2] == "impacting" {
				args = append(args, "-i", setval[1])
				needArg = true
				if len(setval) > 3 {
					args = append(args, "-q", setval[3])
				}
			} else if setval[2] == "blocking" {
				args = append(args, "-b", setval[1])
				needArg = true
			}
		}

		if !ongoing {
			if alert == "cmr-alert" {
				args = append(args, "-t")
			}
			args = append(args, fmt.Sprintf("%d", alert_num*PERIODICS))
		}

		if len(setval) > 1 && setval[1] != "all" && !needArg {
			args = append(args, "-p", setval[1])
		}

		reply(r, cmdSnow(r, chInfo.Name, args))
		counter_num = 0
	}

	counter_num += 1
	chInfo.Settings[alert+"-counter"] = fmt.Sprintf("%d", counter_num)
}
