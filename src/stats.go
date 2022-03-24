/* This file contains functionality relating to the
 * '!stats' command. */

package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type CountedStats struct {
	Today       int
	Last24h     int
	ThisWeek    int
	Last7d      int
	ThisMonth   int
	Last30d     int
	ThisQuarter int
	Last120d    int
	ThisYear    int
	Last365d    int
	LastNd      int
}

func init() {
	COMMANDS["stats"] = &Command{cmdStats,
		"display stats I collected over time",
		"builtin",
		"```!stats -- list available stats\n" +
			"!stats remove number     -- delete stats identified by number\n" +
			"!stats reset number      -- set all stats identified by number to zero\n" +
			"!stats add condition     -- create a new stats counter for statements\n" +
			"       condition may be:    [no]match:\"<pattern>\" [(AND|OR) [no]match:\"<pattern>\"]\n" +
			"                            <pattern> would be valid (Go) regex\n" +
			"!stats number [<since>]  -- show stats identified by number:\n" +
			"       <since> can be:      today, week, month, quarter, year, <num>d\n" +
			"```",
		nil}
}

func allStats(ch *Channel) (result string) {
	if len(ch.MsgStats) < 1 {
		result = "There are no stats kept for this channel."
		return
	}

	result += "I keep track of the following:\n"
	for count, c := range ch.MsgStats {
		result += fmt.Sprintf("%d. '%s'\n", count+1, c.Condition)
	}
	return
}

func addStat(chName string, args []string) (result string) {
	chInfo, found := CHANNELS[chName]
	if !found {
		result = "How did this happen?"
		return
	}

	condition := strings.Join(args, " ")

	var left, op, right string
	if len(args) == 1 {
		left = args[0]
	} else {
		if len(args) == 3 {
			left = args[0]
			op = args[1]
			right = args[2]
		}

		if (op != "AND" && op != "OR") || len(args) != 3 {
			result = "Invalid condition. Should be:\n"
			result += "[no]match:\"pattern\" [(AND|OR) [no]match:\"pattern\"]"
			return
		}
	}

	stringsToCheck := []string{left}
	if len(right) > 0 {
		stringsToCheck = []string{left, right}
	}

	condition_re := regexp.MustCompile(`(?i)(((no)?match):(.*))`)
	for _, s := range stringsToCheck {
		if m := condition_re.FindStringSubmatch(s); len(m) > 0 {
			givenRegex := m[2]
			if _, err := regexp.Compile(givenRegex); err != nil {
				result = fmt.Sprintf("Invalid regex '%s'.", givenRegex)
				return
			}
		} else {
			result = "Invalid condition. I need at least one 'match:\"<pattern>\"' or 'nomatch:\"<pattern>\"."
			return
		}
	}

	for _, stat := range chInfo.MsgStats {
		if stat.Condition == condition {
			result = "Looks like I'm already collecting stats on thіs.\n"
			result += "Check '!stats' to see which ones I'm already tracking."
			return
		}
	}

	var s MsgStat
	s.Created = time.Now()
	s.Condition = condition
	s.Seen = []time.Time{}
	chInfo.MsgStats = append(chInfo.MsgStats, s)
	CHANNELS[chName] = chInfo

	result = fmt.Sprintf("Ok, tracking this now as stat #%d.", len(chInfo.MsgStats))
	return
}

func cmdStats(r Recipient, chName string, args []string) (result string) {
	chInfo, found := CHANNELS[chName]
	if !found {
		result = "This command only works in a channel."
		return
	}

	if len(args) < 1 {
		result = allStats(chInfo)
		return
	}

	if len(args) == 1 {
		switch args[0] {
		case "add":
			fallthrough
		case "help":
			fallthrough
		case "remove":
			fallthrough
		case "reset":
			result = "Usage: " + COMMANDS["stats"].Usage
			return
		}
		result = oneStat(args[0], "all", chInfo)
		return
	}

	switch args[0] {
	case "add":
		result = addStat(chName, args[1:])
	case "remove":
		fallthrough
	case "reset":
		result = removeOrResetStat(args[0], chName, args[1:])
	default:
		_, err := strconv.Atoi(args[0])
		if err != nil {
			result = "Usage: " + COMMANDS["stats"].Usage
		} else {
			result = oneStat(args[0], strings.Join(args[1:], " "), chInfo)
		}
	}

	return
}

func countStats(stat MsgStat, sinceDays int) (c CountedStats) {

	now := time.Now()
	year, month, day := now.Date()
	y1, w1 := now.ISOWeek()
	q1 := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	q2 := time.Date(year, time.April, 1, 0, 0, 0, 0, time.UTC)
	q3 := time.Date(year, time.July, 1, 0, 0, 0, 0, time.UTC)
	q4 := time.Date(year, time.October, 1, 0, 0, 0, 0, time.UTC)

	var lastNd time.Time

	if sinceDays > 0 {
		d, _ := time.ParseDuration(fmt.Sprintf("-%dh", 24*sinceDays))
		lastNd = now.Add(d)
	}

	d, _ := time.ParseDuration("-24h")
	last24h := now.Add(d)

	d, _ = time.ParseDuration(fmt.Sprintf("-%dh", 24*7))
	last7d := now.Add(d)

	d, _ = time.ParseDuration(fmt.Sprintf("-%dh", 24*30))
	last30d := now.Add(d)

	d, _ = time.ParseDuration(fmt.Sprintf("-%dh", 24*120))
	last120d := now.Add(d)

	d, _ = time.ParseDuration(fmt.Sprintf("-%dh", 24*365))
	last365d := now.Add(d)

	currentQuarter := q4
	nextQuarter := q4
	if now.Before(q4) {
		currentQuarter = q3
	}
	if now.Before(q3) {
		currentQuarter = q2
		nextQuarter = q3
	}
	if now.Before(q2) {
		currentQuarter = q1
		nextQuarter = q2
	}
	if now.Before(q1) {
		fmt.Fprintf(os.Stderr, "Current date (%s) is prior to 01/01??\n", now.Format(time.UnixDate))
		return
	}

	for _, t := range stat.Seen {
		ty, tm, td := t.Date()
		y2, w2 := t.ISOWeek()

		if sinceDays > 0 && t.Sub(lastNd) > 0 {
			c.LastNd += 1
		}

		if t.Sub(last24h) > 0 {
			c.Last24h += 1
		}
		if t.Sub(last7d) > 0 {
			c.Last7d += 1
		}
		if t.Sub(last30d) > 0 {
			c.Last30d += 1
		}
		if t.Sub(last120d) > 0 {
			c.Last120d += 1
		}
		if t.Sub(last365d) > 0 {
			c.Last365d += 1
		}

		if y1 == y2 && w1 == w2 {
			c.ThisWeek += 1
		}

		if t.Equal(currentQuarter) || (t.After(currentQuarter) && t.Before(nextQuarter)) {
			c.ThisQuarter += 1
		}

		if ty == year {
			c.ThisYear += 1
			if tm == month {
				c.ThisMonth += 1
				if td == day {
					c.Today += 1
				}
			}
		}
	}

	return
}

func matchMessage(condition, msg string) (match bool) {
	lm := false
	match = false

	var leftType, op, rightType string
	var leftRe, rightRe *regexp.Regexp
	var err error

	condition_re := regexp.MustCompile(`(nomatch|match):(.*?)($| (AND|OR) (nomatch|match):(.*))`)
	m := condition_re.FindStringSubmatch(condition)
	if len(m) < 1 {
		fmt.Fprintf(os.Stderr, "ill formatted stat condition '%s'!\n", condition)
		return
	}

	leftType = m[1]
	leftRe, err = regexp.Compile(m[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid regex in stats condition '%s'!\n", condition)
		return
	}

	lm = leftRe.MatchString(msg)
	if (leftType == "match" && lm) || (leftType == "nomatch" && !lm) {
		lm = true
		match = true
	}

	if len(m) > 3 {
		match = false

		op = m[4]
		rightType = m[5]
		rightRe, err = regexp.Compile(m[6])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid regex in stats condition '%s'!\n", condition)
			return
		}
		rm := rightRe.MatchString(msg)
		if (rightType == "match" && rm) || (rightType == "nomatch" && !rm) {
			rm = true
		} else {
			rm = false
		}

		if op == "AND" && lm && rm {
			match = true
		} else if op == "OR" && (lm || rm) {
			match = true
		}
	}

	return
}

func oneStat(num, since string, ch *Channel) (result string) {
	msg, n := stringToNum(num, ch)
	if n == 0 {
		result = msg
		return
	}

	stats := ch.MsgStats[n-1]
	if since == "all" {
		result = showStats(stats, -1)
	} else {
		n := -1
		switch since {
		case "today":
			n = 1
		case "week":
			n = 7
		case "month":
			n = 30
		case "quarter":
			n = 120
		case "year":
			n = 365
		}

		if n > 0 {
			result = showStats(stats, n)
			return
		}

		num_re := regexp.MustCompile(`^([0-9]+)d$`)
		if m := num_re.FindStringSubmatch(since); len(m) > 0 {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				result = fmt.Sprintf("Unable to convert '%s' into a number.", m[1])
				return
			}
			if n < 1 {
				result = fmt.Sprintf("Invalid number %s.", m[1])
				return
			}
			result = showStats(stats, n)
		}
	}
	return
}

func removeOrResetStat(action, chName string, args []string) (result string) {
	chInfo, found := CHANNELS[chName]
	if !found {
		result = "How did this happen?"
		return
	}

	if len(args) != 1 {
		result = fmt.Sprintf("Usage: !stats %s <number>", action)
		return
	}

	msg, n := stringToNum(args[0], chInfo)
	if n == 0 {
		result = msg
		return
	}

	if action == "remove" {
		if len(chInfo.MsgStats) == 1 {
			chInfo.MsgStats = []MsgStat{}
		} else {
			chInfo.MsgStats = append(chInfo.MsgStats[:n-1], chInfo.MsgStats[n:]...)
		}
		result = fmt.Sprintf("Ok, deleted item %d.", n)
	} else if action == "reset" {
		stat := chInfo.MsgStats[n-1]
		stat.Seen = []time.Time{}
		stat.Created = time.Now()
		chInfo.MsgStats[n-1] = stat
		result = fmt.Sprintf("Ok, reset item %d.", n)
	} else {
		return "How did this happen?"
	}
	CHANNELS[chName] = chInfo

	return
}

func showStats(stat MsgStat, sinceDays int) (result string) {
	created := stat.Created.Format(time.UnixDate)
	if len(stat.Seen) < 1 {
		result = fmt.Sprintf("I haven't seen any messages matching this condition since %s.\n", created)
		return
	}

	countedStats := countStats(stat, sinceDays)
	if sinceDays > 0 {
		result = fmt.Sprintf("Total last %d days: %d\n", sinceDays, countedStats.LastNd)
		return
	}

	result = fmt.Sprintf("I started tracking this condition on %s.\n", created)
	result += fmt.Sprintf("Total since then: %d\n", len(stat.Seen))

	if countedStats.Today > 0 {
		result += fmt.Sprintf("Total today: %d\n", countedStats.Today)
	}
	if countedStats.Last24h > countedStats.Today {
		result += fmt.Sprintf("Total last 24 hours: %d\n", countedStats.Last24h)
	}
	if countedStats.ThisWeek > countedStats.Last24h {
		result += fmt.Sprintf("Total this week: %d\n", countedStats.ThisWeek)
	}
	if countedStats.Last7d > countedStats.ThisWeek {
		result += fmt.Sprintf("Total last 7 days: %d\n", countedStats.Last7d)
	}
	if countedStats.ThisMonth > countedStats.Last7d {
		result += fmt.Sprintf("Total this month: %d\n", countedStats.ThisMonth)
	}
	if countedStats.Last30d > countedStats.ThisMonth {
		result += fmt.Sprintf("Total last 30 days: %d\n", countedStats.Last30d)
	}
	if countedStats.ThisQuarter > countedStats.Last30d {
		result += fmt.Sprintf("Total this Quarter: %d\n", countedStats.ThisQuarter)
	}
	if countedStats.Last120d > countedStats.ThisQuarter {
		result += fmt.Sprintf("Total last 120 days: %d\n", countedStats.Last120d)
	}
	if countedStats.ThisYear > countedStats.ThisQuarter {
		result += fmt.Sprintf("Total this Year: %d\n", countedStats.ThisYear)
	}
	if countedStats.Last365d > countedStats.ThisYear {
		result += fmt.Sprintf("Total last 365 days: %d\n", countedStats.Last365d)
	}

	return
}

func stringToNum(num string, ch *Channel) (result string, n int) {
	n, err := strconv.Atoi(num)
	if err != nil {
		result = "I can only identify stats to display by number.\n"
		result += "See '!help stats' for more information.\n"
		return
	}

	total := len(ch.MsgStats)
	if n < 1 || n > total {
		result = fmt.Sprintf("Please specify a positive number smaller than or equal to %d.", total)
		n = 0
		return
	}

	return
}

func trackStats(r Recipient, msg string) {
	ch, found := getChannel(r)
	if !found {
		/* ignore anything not in a channel */
		return
	}

	for n, s := range ch.MsgStats {
		match := matchMessage(s.Condition, msg)
		if match {
			s.Seen = append(s.Seen, time.Now())
		}
		ch.MsgStats[n] = s
	}

	CHANNELS[ch.Name] = ch
}
