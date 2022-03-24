/* This file contains functionality around the
 * '!autoreply' command that lets you configure
 * automatic replies for input matching a pattern.
 *
 * '!autoreply' lists the current autoreply patterns
 * and responses.
 * '!autoreply delete "pattern"' will delete the
 * autoreply for the given pattern.
 * '!autoreply set "pattern" "response" [throttle]
 * will set the given response for the given pattern.
 * The optional 'throttle' will tell jbot not to reply
 * to matching messages within that many minutes of a
 * previous reply.  Defaults to 30.
 */

package main

import (
	"fmt"
	"regexp"
	"strconv"
)

func init() {
	COMMANDS["autoreply"] = &Command{cmdAutoreply,
		"set, delete, or show automatic replies for configured patterns",
		"builtin",
		"!autoreply delete \"pattern\" -- delete the autoreply for this pattern\n" +
			"        if \"pattern\" is \"ALL\", delete all autoreplies\n" +
			"!autoreply set \"pattern\" \"reply\" [throttle] -- set the reply for the given pattern;\n" +
			"        if 'throttle' is given, set the time in minutes to ignore subsequent triggers (default: 30)\n" +
			"        note: all patterns are matched case-insensitively\n",
		nil}
}

func cmdAutoreply(r Recipient, chName string, args []string) (result string) {
	chInfo, found := CHANNELS[chName]
	if !found {
		result = "This command only works in a channel."
		return
	}

	if len(args) < 1 {
		if len(chInfo.AutoReplies) < 1 {
			result = "There are no autoreplies configured for this channel."
			return
		}

		result += "These are the auto replies for this channel:\n"
		for p, r := range chInfo.AutoReplies {
			result += fmt.Sprintf("\"%s\" => \"%s\" (%d)\n", p, r.ReplyString, r.ReplyThrottle)
		}
		return
	}

	if (len(args) < 2) ||
		((args[0] != "set") && (args[0] != "delete")) ||
		((args[0] == "set") && ((len(args) < 3) || (len(args) > 4))) ||
		((args[0] == "delete") && (len(args) != 2)) {
		result = "Usage: " + COMMANDS["autoreply"].Usage
		return
	}

	if args[0] == "set" {
		throttle := 30
		if len(args) == 4 {
			if n, err := strconv.Atoi(args[3]); err == nil {
				if n < 0 {
					result = "Error: Invalid negative throttle."
					return
				}
				throttle = n
			}
		}

		if _, err := regexp.Compile("(?i)" + args[1]); err != nil {
			result = fmt.Sprintf("\"%s\" is not a valid regular expression.", args[1])
			return
		}

		autoReply := &AutoReply{args[2], throttle}
		if chInfo.AutoReplies == nil {
			chInfo.AutoReplies = map[string]AutoReply{}
		}
		chInfo.AutoReplies[args[1]] = *autoReply
		result = fmt.Sprintf("Set autoreply for pattern \"%s\" to \"%s\"\n", args[1], args[2])
		return
	}

	if args[0] == "delete" {
		if args[1] == "ALL" {
			if len(chInfo.AutoReplies) < 1 {
				result = "No auto replies configured."
				return
			}
			chInfo.AutoReplies = map[string]AutoReply{}
			result = "Deleted all existing auto replies."
		} else {
			_, found := chInfo.AutoReplies[args[1]]
			if !found {
				result = fmt.Sprintf("No autoreply set for pattern \"%s\".", args[1])
			} else {
				delete(chInfo.AutoReplies, args[1])
				result = fmt.Sprintf("Autoreply for pattern \"%s\" deleted.", args[1])
			}
		}
		return
	}

	return
}

func processAutoReplies(r Recipient, msg string) (replied bool) {
	replied = false

	ch, found := getChannel(r)
	if !found {
		/* ignore anything not in a channel */
		return
	}

	for p, response := range ch.AutoReplies {
		pattern, err := regexp.Compile("(?i)" + p)
		if err != nil {
			/* This should never happen, because we checked the pattern
			 * at intake, but ok.  Let's bail. */
			return
		}

		if pattern.MatchString(msg) {
			if !isThrottled("autoreply: "+p, ch) {
				reply(r, response.ReplyString)
				replied = true
				cmdThrottle(r, ch.Name, []string{"autoreply: " + p, fmt.Sprintf("%d", response.ReplyThrottle*60)})
			}
		}
	}

	response := athere(msg, ch, r)
	if len(response) > 0 {
		reply(r, response)
		replied = true
	}

	return
}
