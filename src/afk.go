/* This file contains functionality around the
 * '!afk' command that lets you set yourself as
 * AFK / OOO until a given time.
 *
 * '!afk' lists all users currently AFK.
 * '!afk YYYY-MM-DDTHH:MM ["message"]' sets yourself as AFK until
 * the given date and time.  "THH::MM" is optional; if
 * not provided, 00:00 is used instead.
 * An optionally provided message will be appended to
 * the response.
 * '!afk back' will delete your current AFK entry.
 */

package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

func init() {
	COMMANDS["afk"] = &Command{cmdAfk,
		"set, delete, or show AFK users",
		"builtin",
		"!afk  - list all users currently AFK\n" +
			"!afk YYYY-MM-DD[THH:MM[@TZ]] [message]  - set yourself as AFK until the given date\n" +
			"        'THH:MM' is optional; if not privided, 00:00 will be used\n" +
			"        '@TZ' is optional; if not provided, I will try to guess your local time or fall back to UTC\n" +
			"!afk back - delete your current AFK entry\n",
		[]string{"ooo"}}
}

func cmdAfk(r Recipient, chName string, args []string) (result string) {
	if !isWorkspaceUser(r.MentionName) {
		result = "Sorry, this functionality is restricted to true workspace users."
		return
	}
	if reject, ok := channelCheck(r, chName, true, false); !ok {
		return reject
	}

	if len(args) < 1 {
		var afks []string
		longest := len("Username") + 2

		result = "The following users have marked themselves as being AFK / OOO:\n"
		for u, i := range AFK_USERS {
			/* We're lazy: instead of doing periodic house
			 * keeping, we simply clean up users whenever
			 * we look at the info. */
			if i.Until.Before(time.Now()) {
				delete(AFK_USERS, u)
				continue
			}

			l := len(u)
			if l > longest {
				longest = l
			}
			afks = append(afks, u)
		}
		sort.Strings(afks)

		if len(AFK_USERS) < 1 {
			result = "There currently are no users marked as being AFK / OOO."
			return
		}

		result += "```\n"
		padlen := longest + 2
		result += fmt.Sprintf("%s| AFK until                     | Message\n", rightpad("Username", " ", padlen))
		result += fmt.Sprintf("%s+-------------------------------+---------------------\n", rightpad("", "-", padlen))
		for _, u := range afks {
			info := AFK_USERS[u]
			msg := "(no custom message)"
			if len(info.Message) > 0 {
				msg = info.Message
			}
			result += fmt.Sprintf("%s| %s | %s\n", rightpad(u, " ", padlen), info.Until, msg)
		}
		result += fmt.Sprintf("%s+-------------------------------+---------------------\n", rightpad("", "-", padlen))
		result += "```\n"
		return
	}

	if strings.EqualFold(args[0], "back") {
		if _, found := AFK_USERS[r.MentionName]; found {
			delete(AFK_USERS, r.MentionName)
			result = "Ok, I've removed your AFK setting."
		} else {
			result = fmt.Sprintf("Sorry, no AFK / OOO setting found for '%s'.", r.MentionName)
		}
		return
	}

	t, errmsg := parseAfk(r, args[0])
	if len(errmsg) > 0 {
		result = errmsg
		return
	}

	if t.Before(time.Now()) {
		result = fmt.Sprintf("I parsed the date you gave me as '%s'.\n", t)
		result += "That appears to be in the past, so I shall refuse to mark you as AFK before now to avoid a time paradox."
		return
	}

	var i AfkInfo
	i.Until = t

	if len(args) > 1 {
		i.Message = strings.Join(args[1:], " ")
	}

	AFK_USERS[r.MentionName] = i
	result = fmt.Sprintf("Ok, I've set you as being AFK until %s.", i.Until)

	return
}

func isAfk(user string) bool {
	var i AfkInfo
	var found bool
	if i, found = AFK_USERS[user]; !found {
		return false
	}

	if i.Until.After(time.Now()) {
		return true
	} else {
		delete(AFK_USERS, user)
		return false
	}
}

func parseAfk(r Recipient, input string) (t time.Time, errmsg string) {
	tz := "UTC"
	tz_re := regexp.MustCompile(`@([A-Z0-9+/-]+)`)

	/* This is some annoyingly complicated back and forth
	 * here.  I hate time parsing.
	 *
	 * So:
	 * We check if the user provided a timezone.
	 *    If so, we try to guess a location, since
	 *    timezone abbreviations do not uniquely identify
	 *    a single location.
	 * If the user did not provide a timezone, then we'll
	 * try to get the location from their directory data.
	 * If this works out, or we got a location from our
	 * guesses, then we proceed to load the location, and
	 * subsequently parse the time in that location.
	 *
	 * If any errors are encountered, we fall back to UTC.
	 */
	if m := tz_re.FindStringSubmatch(input); len(m) > 0 {
		tz = m[1]

		/* Locales and Zones are... annoying.
		 * We'll use some shortcuts to let
		 * users pick what we think they may
		 * want. */
		if tz == "CST" {
			tz = "Asia/Taipei" /* Note: to get US Central time, use '@CST6CDT' */
		}
		if tz == "EST" || tz == "EDT" {
			tz = "EST5EDT"
		}
		if tz == "IST" {
			tz = "Asia/Calcutta"
		}
		if tz == "PST" || tz == "PDT" {
			tz = "PST8PDT"
		}
		if tz == "MST" || tz == "MDT" {
			tz = "MST7MDT"
		}
	} else {
		address := getUserAddress(r, r.MentionName)
		if len(address) > 0 {
			var found bool
			tz, found = locationToTZ(address)
			if !found {
				tz = "UTC"
			}
		}
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		errmsg = fmt.Sprintf("Unable to load time location '%s'!", tz)
		return
	}

	if t, err = time.ParseInLocation("2006-01-02T15:04@MST", input, loc); err != nil {
		if t, err = time.ParseInLocation("2006-01-02T15:04", input, loc); err != nil {
			if t, err = time.ParseInLocation("2006-01-02", input, loc); err != nil {
				errmsg = fmt.Sprintf("Invalid time format: %s\n", input)
				errmsg += "Please use one of the following formats:\n"
				errmsg += " - 'YYYY-MM-DD' (e.g., '2020-07-27')\n"
				errmsg += " - 'YYYY-MM-DDTHH:MM' (e.g., '2020-07-27T11:24')\n"
				errmsg += " - 'YYYY-MM-DDTHH:MM@TZ' (e.g., '2020-07-27T11:24@EDT')\n"
				return
			}
		}
	}

	return
}

func processAfks(r Recipient, msg string) {
	result := ""

	if !strings.Contains(msg, "<@") {
		return
	}

	for _, word := range strings.Split(msg, " ") {
		u := expandSlackUser(word)
		if u == nil || u.ID == "" {
			continue
		}

		if isAfk(u.Name) {
			info := AFK_USERS[u.Name]
			result += fmt.Sprintf("@%s is AFK / OOO until %s", u.Name, info.Until)
			if len(info.Message) > 0 {
				result += fmt.Sprintf(" (%s)", info.Message)
			}
			result += "\n"
		}
	}

	if len(result) > 0 {
		reply(r, result)
	}
}
