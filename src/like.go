/* This file contains functionality around the
 * '!like' and '!dislike' commands.  These commands
 * can be used to register your personal preferences
 * about... anything.
 *
 * These likes and dislikes can then be queried on a
 * per-channel basis via '!top likes|dislikes'.
 *
 * Usage:
 * !like <something> [<reason>]
 *
 * Internally, likes and dislikes are stored as a map
 * of the thing to be (dis)liked to a map of reasons
 * with a count.
 */

package main

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

var LIKE_DISLIKE_REPLIES = []string{
	"Noted.",
	"Oh, ok.",
	"I agree!",
	"Huh, I wouldn't have thought so, but ok.",
	"Really!?",
	"OMG! I know, right?",
	"Uhm... okay...",
	"If you say so.",
	"That's nice.",
	"Why not - you do you.",
	"Well, I guess that's an opinion.",
	"Awwww, that's cute.",
	"Hey now!",
	"I'll make a note of that.",
	"If you say so.",
	"That's just like, your opinion, man.",
}

func init() {
	both := map[string]CommandFunc{"like": cmdLike, "dislike": cmdDislike}
	for cmd, f := range both {
		COMMANDS[cmd] = &Command{f,
			cmd + " something",
			"builtin",
			"!" + cmd + " <something> [<reason>]\n" +
				fmt.Sprintf("\nIf you don't specify <something>, then I'll show you all the things that have been %sd in this channel.\n", cmd) +
				fmt.Sprintf("You can use '!top %ss [[r/]search]' to view the most %sd things.\n",
					cmd, cmd) +
				"If you specify 'search', only matching things will be counted.\n" +
				"If you specify 'r/search' only things where the reason matches your search will be counted.\n" +
				"If you specify 'r/', I will show you all the reasons for each thing.\n",
			nil}
	}
}

func cmdLike(r Recipient, chName string, args []string) (result string) {
	return cmdLikeDislike("like", r, chName, args)
}

func cmdDislike(r Recipient, chName string, args []string) (result string) {
	return cmdLikeDislike("dislike", r, chName, args)
}

func cmdLikeDislike(which string, r Recipient, chName string, args []string) (result string) {
	chInfo, found := CHANNELS[chName]
	if !found {
		fmt.Fprintf(os.Stderr, ":: %s: channel %s not found!\n", which, chName)
		result = "This command only works in a channel."
		return
	}

	if len(which) < 1 || (which != "like" && which != "dislike") {
		return "Invalid invocation using '" + which + "'."
	}

	var likesDislikes map[string]map[string]int
	if which == "like" {
		likesDislikes = chInfo.Likes
	} else {
		likesDislikes = chInfo.Dislikes
	}

	if len(likesDislikes) < 1 {
		likesDislikes = map[string]map[string]int{}
	}

	if len(args) < 1 {

		if len(likesDislikes) > 0 {
			result += fmt.Sprintf("\n\nHere's the list of all the things that have been %sd in this channel:\n", which)
			what := []string{}
			for k, _ := range likesDislikes {
				what = append(what, k)
			}
			sort.Strings(what)
			result += strings.Join(what, ", ")
		} else {
			result = "Usage: " + which + " <something> [<reason>]"
		}
		return
	}

	object := strings.ToLower(args[0])
	reason := "no reason"
	if len(args) > 1 {
		reason = strings.ToLower(strings.Join(args[1:], " "))
	}

	reasons, found := likesDislikes[object]
	if !found {
		likesDislikes[object] = map[string]int{reason: 1}
	} else {
		if r, found := reasons[reason]; !found {
			reasons[reason] = 1
		} else {
			reasons[reason] = r + 1
		}
		likesDislikes[object] = reasons
	}

	if which == "like" {
		chInfo.Likes = likesDislikes
	} else {
		chInfo.Dislikes = likesDislikes
	}

	rand.Seed(time.Now().UnixNano())
	return LIKE_DISLIKE_REPLIES[rand.Intn(len(LIKE_DISLIKE_REPLIES))]
}

func topLikesDislikes(chName string, args []string) (result string) {
	if args[0] != "likes" && args[0] != "dislikes" {
		return fmt.Sprintf("Invalid argument '%s' for topLikesDislikes.", args[0])
	}

	pattern := ""
	if len(args) > 1 {
		pattern = strings.Join(args[1:], " ")
	}

	chInfo, found := CHANNELS[chName]
	if !found {
		fmt.Fprintf(os.Stderr, ":: %s: channel %s not found!\n", args[0], chName)
		result = "This command only works in a channel."
		return
	}

	var likesDislikes map[string]map[string]int
	if args[0] == "likes" {
		likesDislikes = chInfo.Likes
	} else {
		likesDislikes = chInfo.Dislikes
	}

	if len(likesDislikes) < 1 {
		result = fmt.Sprintf("Sorry, in this channel nobody %s anything, it seems.", args[0])
		return
	}

	whatByCount := map[string]int{}
	for object, reasons := range likesDislikes {
		count, err := countLikesDislikes(object, reasons, pattern)
		if len(err) > 0 {
			return err
		}
		if count > 0 || len(reasons) < 1 {
			whatByCount[object] = count
		}
	}

	if len(whatByCount) > 0 {
		result = fmt.Sprintf("Top %s ", args[0])
		if len(pattern) > 0 {
			result += "matching your search "
		}
		result += "in this channels:\n"
	} else {
		result = fmt.Sprintf("Looks like none of the %s matched your search.", args[0])
		return
	}

	top := getSortedKeys(whatByCount, true)
	n := 1
	for _, what := range top {
		if n < 11 {
			result += fmt.Sprintf("%2d. %s (%d)\n", n, what, whatByCount[what])
			if pattern == "r/" {
				reasons := []string{}
				for r, _ := range likesDislikes[what] {
					reasons = append(reasons, r)
				}
				sort.Strings(reasons)
				result += "    (" + strings.Join(reasons, ", ") + ")\n"
			}
		}
		n++
	}

	return
}

func countLikesDislikes(object string, likesDislikes map[string]int, pattern string) (count int, e string) {

	which := "object"
	if strings.HasPrefix(pattern, "r/") {
		which = "reason"
		pattern = pattern[2:]
	}

	pattern_re, err := regexp.Compile(`(?i)` + pattern)
	if err != nil {
		e = fmt.Sprintf("Invalid regex: '%s'", pattern)
		return
	}

	count = 0

	if which == "object" {
		if len(pattern) < 1 || pattern_re.Match([]byte(object)) {
			for _, num := range likesDislikes {
				count += num
			}
		}
	} else {
		for reason, num := range likesDislikes {
			if len(pattern) < 1 || pattern_re.Match([]byte(reason)) {
				count += num
			}
		}
	}

	return
}
