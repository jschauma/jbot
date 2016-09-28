/*
 * This is a HipChat version of the 'jbot' IRC bot,
 * originally developed at Yahoo! in 2007.  This
 * variant was created as a rewrite in Go for HipChat
 * in July 2016 by Jan Schaumann (@jschauma /
 * jschauma@netmeister.org).  Many thanks to Yahoo
 * for letting me play around with nonsense like this.
 *
 * You should be able to run the bot by populating a
 * configuration file with suitable values.  The
 * following configuration values are required:
 *   password = the HipChat password of the bot user
 *   hcName   = the HipChat company prefix, e.g. <foo>.hipchatcom
 *   jabberID = the HipChat / JabberID of the bot user
 *   fullName = how the bot presents itself
 *   mentionName = to which name the bot responds to
 *
 * This bot has a bunch of features that are company
 * internal; those features have been removed from
 * this public version.
 *
 * Some day this should be extended into a pluggable
 * bot, so that internal code can more easily be kept
 * apart, I suppose.  Pull requests welcome etc.
 */

/*
 * This code is in the public domain.  Knock yourself
 * out.  If it's not inconvenient, tell people where
 * you got it.  If we meet some day and you think this
 * code is worth it, you can buy me a beer in return.
 */

package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"
)

import (
	"github.com/daneharrigan/hipchat"
)

const DEFAULT_THROTTLE = 1800
const PERIODICS = 1800

const EXIT_FAILURE = 1
const EXIT_SUCCESS = 0

const PROGNAME = "jbot"
const VERSION = "3.0"

var CONFIG = map[string]string{
	"channelsFile":   "/var/tmp/jbot.channels",
	"configFile":     "/etc/jbot.conf",
	"debug":          "no",
	"domain":         "conf.hipchat.com",
	"domainPrefix":   "",
	"fullName":       "",
	"hcName":         "",
	"jabberID":       "",
	"mentionName":    "",
	"opsgenieApiKey": "",
	"password":       "",
	"user":           "",
}

var HIPCHAT_CLIENT *hipchat.Client

var CHANNELS = map[string]*Channel{}
var CURSES = map[string]int{}
var COMMANDS = map[string]*Command{}
var ROOMS = map[string]*hipchat.Room{}
var ROSTER = map[string]*hipchat.User{}

var TOGGLES = map[string]bool{
	"chatter": true,
	"python":  true,
	"trivia":  true,
}

var JBOT_SOURCE = "https://github.com/jschauma/jbot"

var THANKYOU = []string{
	"Thank you!",
	"Glad to be of service.",
	"Always happy to help.",
	"Thanks - this channel is my life!",
	"I appreciate your appreciation.",
	"/me giddily hops up and down.",
	"/me struts his stuff.",
	"/me proudly smiles.",
	"/me nods approvingly.",
	"/me grins sheepishly.",
	"/me takes a bow.",
	"/me blushes.",
}

var DONTKNOW = []string{
	"How the hell am I supposed to know that?",
	"FIIK",
	"ENOCLUE",
	"Buh?",
	"I have no idea.",
	"Sorry, I wouldn't know about that.",
	"I wouldn't tell you even if I knew.",
	"You don't know??",
	"Oh, uhm, ...I don't know. Do you?",
	"I could tell you, but then I'd have to kill you.",
	"Wouldn't you like to know.",
	"You're a curious little hip-chatter, aren't you?",
	"I'm sorry, that's classified.",
	"The answer lies within yourself.",
	"You know, if you try real hard, I'm sure you can figure it out yourself.",
	"Ask more politely, and I may tell you.",
	"Oh, come on, you know.",
}

var COOKIES []*http.Cookie
var VERBOSITY int

type Channel struct {
	Inviter   string
	Jid       string
	Name      string
	Toggles   map[string]bool
	Throttles map[string]time.Time
	Users     map[hipchat.User]UserInfo
	Settings  map[string]string
}

type CommandFunc func(Recipient, string, string) string

type Command struct {
	Call    CommandFunc
	Help    string
	How     string
	Usage   string
	Aliases []string
}

type UserInfo struct {
	Seen   string
	Count  int
	Curses int
	Praise int
}

/*
 * Jid         = 12345_98765@conf.hipchat.com
 * MentionName = JohnDoe
 * Name        = John Doe
 * ReplyTo     = 98765
 */
type Recipient struct {
	Jid         string
	MentionName string
	Name        string
	ReplyTo     string
}

/*
 * Commands
 */

func cmdAsn(r Recipient, chName, args string) (result string) {
	input := strings.Split(args, " ")
	if len(args) < 1 || len(input) != 1 {
		result = "Usage: " + COMMANDS["asn"].Usage
		return
	}

	arg := input[0]
	number_re := regexp.MustCompile(`(?i)^(asn?)?([0-9]+)$`)
	m := number_re.FindStringSubmatch(arg)
	if len(m) > 0 {
		arg = "AS" + m[2]
	} else if net.ParseIP(arg) == nil {
		arg = fqdn(arg)
		addrs, err := net.LookupHost(arg)
		if err != nil {
			result = "Not a valid ASN, IP or hostname."
			return
		}
		arg = addrs[0]
	}

	command := strings.Fields(COMMANDS["asn"].How)
	command = append(command, arg)

	data, _ := runCommand(command...)
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		result = "No ASN information found."
	} else {
		result = lines[len(lines)-2]
	}

	return
}

func cmdChannels(r Recipient, chName, args string) (result string) {
	var channels []string

	if len(CHANNELS) == 0 {
		result = "I'm not currently in any channels."
	} else if len(CHANNELS) == 1 {
		result = "I'm only here right now: "
	} else {
		result = fmt.Sprintf("I'm in the following %d channels:\n", len(CHANNELS))
	}

	for ch := range CHANNELS {
		channels = append(channels, ch)
	}
	sort.Strings(channels)
	result += strings.Join(channels, ", ")
	return
}

func cmdClear(r Recipient, chName, args string) (result string) {
	count := 24

	if len(args) > 0 {
		if _, err := fmt.Sscanf(args, "%d", &count); err != nil {
			result = cmdInsult(r, chName, "me")
			return
		}
	}
	if count < 1 {
		result = cmdInsult(r, chName, "me")
		return
	}

	if count > 40 {
		result = "I'm not going to clear more than 40 lines."
		return
	}

	n := 0
	rcount := count
	result = "/code "
	for n < count {
		i := rcount
		for i > 0 {
			result += "."
			i--
		}

		result += "\n"
		if rcount == 9 {
			cowsay := cmdCowsay(r, chName, "clear")
			// strip leading "/quote "
			cowsay = cowsay[8:]
			result += " " + cowsay
			break
		} else {
			n++
			rcount--
		}
	}
	return
}

func cmdCowsay(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["cowsay"].Usage
		return
	}

	out, _ := runCommand("cowsay " + args)
	result += "/code\n" + string(out)

	return
}

func cmdCurses(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		sortedKeys := getSortedKeys(CURSES, true)
		var curses []string
		for _, k := range sortedKeys {
			curses = append(curses, fmt.Sprintf("%s (%d)", k, CURSES[k]))
		}
		if len(curses) < 1 {
			result = "I have not seen any curses yet!"
		} else {
			result = strings.Join(curses, ", ")
		}
	} else {
		allUsers := map[string]int{}
		wanted := strings.Split(args, " ")[0]
		for ch := range CHANNELS {
			for u, info := range CHANNELS[ch].Users {
				if wanted == "*" {
					allUsers[u.MentionName] = info.Curses
				} else if u.MentionName == wanted {
					if info.Curses > 0 {
						result = fmt.Sprintf("%d", info.Curses)
					} else {
						result = fmt.Sprintf("Looks like %s has been behaving so far.", wanted)
					}
					break
				}
			}
		}

		if wanted == "*" {
			sortedKeys := getSortedKeys(allUsers, true)
			n := 0
			var curses []string
			for _, k := range sortedKeys {
				curses = append(curses, fmt.Sprintf("%s (%d)", k, allUsers[k]))
				n++
				if n > 5 {
					break
				}
			}

			if len(curses) < 1 {
				result = "I have not seen any curses yet!"
			} else {
				result = strings.Join(curses, ", ")
			}
		}

		if len(result) < 1 {
			result = fmt.Sprintf("Looks like %s has been behaving so far.", wanted)
		}
	}
	return
}

func cmdCve(r Recipient, chName, args string) (result string) {
	cves := strings.Split(args, " ")
	if len(args) < 1 || len(cves) != 1 {
		result = "Usage: " + COMMANDS["cve"].Usage
		return
	}

	cve := strings.TrimSpace(cves[0])

	if !strings.HasPrefix(cve, "CVE-") {
		cve = fmt.Sprintf("CVE-%s", cve)
	}

	theUrl := fmt.Sprintf("%s%s", COMMANDS["cve"].How, cve)
	data := getURLContents(theUrl, false)

	info := []string{}

	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "<th colspan=\"2\">Description</th>") {
			found = true
			continue
		}

		if found {
			if strings.Contains(line, "</td>") {
				break
			}
			oneLine := dehtmlify(line)
			if len(oneLine) > 1 {
				info = append(info, oneLine)
			}
		}
	}

	if len(info) < 1 {
		result = fmt.Sprintf("No info found for '%s'.", cve)
	} else {
		result = strings.Join(info, " ")
		result += fmt.Sprintf("\n%s", theUrl)
	}

	return
}

func cmdEightBall(r Recipient, chName, args string) (result string) {
	rand.Seed(time.Now().UnixNano())
	answers := []string{
		"It is certain.",
		"It is decidedly so.",
		"Without a doubt.",
		"Yes definitely.",
		"You may rely on it.",
		"As I see it, yes.",
		"Most likely.",
		"Outlook good.",
		"Yes.",
		"Signs point to yes.",
		"Reply hazy try again.",
		"Ask again later.",
		"Better not tell you now.",
		"Cannot predict now.",
		"Concentrate and ask again.",
		"Don't count on it.",
		"My reply is no.",
		"My sources say no.",
		"Outlook not so good.",
		"Very doubtful.",
	}
	result = answers[rand.Intn(len(answers))]
	return
}

func cmdFml(r Recipient, chName, args string) (result string) {
	if len(args) > 1 {
		result = "Usage: " + COMMANDS["fml"].Usage
		return
	}

	data := getURLContents(COMMANDS["fml"].How, false)

	fml_re := regexp.MustCompile(`(?i)>(Today, .*FML)<`)
	for _, line := range strings.Split(string(data), "\n") {
		m := fml_re.FindStringSubmatch(line)
		if len(m) > 0 {
			result = dehtmlify(m[1])
			return
		}
	}
	return
}

func cmdFortune(r Recipient, chName, args string) (result string) {
	if len(args) > 1 {
		result = "Usage: " + COMMANDS["fortune"].Usage
		return
	}

	out, _ := runCommand("fortune -s")
	result = string(out)

	return
}

func cmdHelp(r Recipient, chName, args string) (result string) {
	if args == "all" {
		var cmds []string
		result = "These are commands I know:\n"
		for c := range COMMANDS {
			cmds = append(cmds, c)
		}
		sort.Strings(cmds)
		result += strings.Join(cmds, ", ")
	} else if len(args) < 1 {
		result = fmt.Sprintf("I know %d commands.\n"+
			"Use '!help all' to show all commands.\n"+
			"Ask me about a specific command via '!help <cmd>'.\n"+
			"If you find me annoyingly chatty, just '!toggle chatter'.\n"+
			"To ask me to leave a channel, say '!leave'.\n",
			len(COMMANDS))
	} else {
		for _, cmd := range strings.Split(args, " ") {
			if _, found := COMMANDS[cmd]; found {
				result = fmt.Sprintf("%s -- %s",
					COMMANDS[cmd].Usage,
					COMMANDS[cmd].Help)
			} else {
				result = fmt.Sprintf("No such command: %s.", cmd)
			}
		}
	}
	return
}

func cmdHost(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["host"].Usage
		return
	}

	out, _ := runCommand(fmt.Sprintf("host %s", args))
	result = string(out)

	return
}

func cmdHow(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["how"].Usage
		return
	}

	if _, found := COMMANDS[args]; found {
		result = COMMANDS[args].How
	} else if strings.EqualFold(args, CONFIG["mentionName"]) {
		result = JBOT_SOURCE
	} else {
		rand.Seed(time.Now().UnixNano())
		result = DONTKNOW[rand.Intn(len(DONTKNOW))]
	}

	return
}

func cmdInfo(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		args = r.ReplyTo
	} else {
		args = strings.ToLower(args)
	}

	if ch, found := CHANNELS[args]; found {
		result = fmt.Sprintf("I was invited into #%s by %s.\n", args, ch.Inviter)
		result += fmt.Sprintf("These are the users I've seen in #%s:\n", args)

		var names []string

		for u := range ch.Users {
			names = append(names, u.MentionName)
		}
		sort.Strings(names)
		result += strings.Join(names, ", ")

		stfu := cmdStfu(r, chName, "")
		if len(stfu) > 0 {
			result += fmt.Sprintf("\nTop 10 channel chatterers for #%s:\n", args)
			result += fmt.Sprintf("%s", stfu)
		}

		toggles := cmdToggle(r, ch.Name, "")
		if len(toggles) > 0 {
			result += fmt.Sprintf("\n%s", toggles)
		}

		throttles := cmdThrottle(r, ch.Name, "")
		if len(throttles) > 0 {
			result += fmt.Sprintf("\n%s", throttles)
		}

		settings := cmdSet(r, ch.Name, "")
		if !strings.HasPrefix(settings, "There currently are no settings") {
			result += "\nThese are the channel settings:\n"
			result += settings
		}
	} else {
		result = "I have no info on #" + args
	}
	return
}

func cmdInsult(r Recipient, chName, args string) (result string) {
	if (len(args) > 0) &&
		((strings.ToLower(args) == strings.ToLower(CONFIG["mentionName"])) ||
			(args == "yourself") ||
			(args == "me")) {
		result = fmt.Sprintf("@%s: ", r.MentionName)
	}

	if (len(result) < 1) && (len(args) > 0) {
		result = fmt.Sprintf("%s: ", args)
	}

	rand.Seed(time.Now().UnixNano())
	if rand.Intn(2) == 0 {
		url := "https://XXX-SOME-LINK-WITH-VARIOUS-INUSLTS-HERE-XXX",
		result += randomLineFromUrl(url, true)
	} else {
		data := getURLContents(COMMANDS["insult"].How, false)
		found := false
		insult_re := regexp.MustCompile(`^<p><font size="\+2">`)
		for _, line := range strings.Split(string(data), "\n") {
			if insult_re.MatchString(line) {
				found = true
				continue
			}
			if found {
				result += dehtmlify(line)
				break
			}
		}
	}

	return
}

func cmdJira(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["jira"].Usage
		return
	}

	ticket := strings.TrimSpace(strings.Split(args, " ")[0])
	jiraUrl := fmt.Sprintf("%s%s", COMMANDS["jira"].How, ticket)
	data := getURLContents(jiraUrl, true)

	var jiraJson map[string]interface{}
	err := json.Unmarshal(data, &jiraJson)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal jira data: %s\n", err)
		return
	}

	if _, found := jiraJson["fields"]; !found {
		result = fmt.Sprintf("No data found for ticket %s", ticket)
		return
	}

	fields := jiraJson["fields"]
	status := fields.(map[string]interface{})["status"].(map[string]interface{})["name"]
	created := fields.(map[string]interface{})["created"]
	summary := fields.(map[string]interface{})["summary"]
	reporter := fields.(map[string]interface{})["reporter"].(map[string]interface{})["name"]

	result = fmt.Sprintf("Summary : %s\n", summary)
	result += fmt.Sprintf("Status  : %s\n", status)
	result += fmt.Sprintf("Created : %s\n", created)

	assignee := fields.(map[string]interface{})["assignee"]
	if assignee != nil {
		name := assignee.(map[string]interface{})["name"]
		result += fmt.Sprintf("Assignee: %s\n", name)
	}

	result += fmt.Sprintf("Reporter: %s\n", reporter)
	result += fmt.Sprintf("Link    : https://XXX-YOUR-JIRA-DOMAIN-HERE-XXX/browse/%s", ticket)
	return
}

func cmdOncall(r Recipient, chName, args string) (result string) {
	oncall := args
	if len(strings.Fields(oncall)) < 1 {
		if ch, found := CHANNELS[r.ReplyTo]; found {
			oncall = r.ReplyTo
			if v, found := ch.Settings["oncall"]; found {
				oncall = v
			}
		} else {
			result = "Usage: " + COMMANDS["oncall"].Usage
			return
		}
	}

	result += cmdOncallOpsGenie(r, chName, oncall)
	if len(result) < 1 {
		result = fmt.Sprintf("No oncall information found for '%s'.", oncall)
	}
	return
}

func cmdOncallOpsGenie(r Recipient, chName, args string) (result string) {

	key := CONFIG["opsgenieApiKey"]
	if len(key) < 1 {
		result = "Unable to query OpsGenie -- no API key in config file."
		return
	}

	/* XXX: This will leak your API key into logs.
	* OpsGenie API for read operations appears to require
	* a GET operation, so there isn't much we can do
	* about that. */
	theUrl := fmt.Sprintf("https://api.opsgenie.com/v1/json/schedule/timeline?apiKey=%s&name=%s_schedule", key, url.QueryEscape(args))
	data := getURLContents(theUrl, false)

	var jsonResult map[string]interface{}

	err := json.Unmarshal(data, &jsonResult)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal opsgenie data: %s\n", err)
		return
	}

	if _, found := jsonResult["timeline"]; !found {
		result = fmt.Sprintf("No OpsGenie schedule found for '%s'.", args)

		theUrl = fmt.Sprintf("https://api.opsgenie.com/v1/json/team?apiKey=%s", key)
		data = getURLContents(theUrl, false)
		err = json.Unmarshal(data, &jsonResult)
		if err != nil {
			result = fmt.Sprintf("Unable to unmarshal opsgenie data: %s\n", err)
			return
		}

		if _, found := jsonResult["teams"]; !found {
			return
		}

		var candidates []string

		teams := jsonResult["teams"].([]interface{})
		for _, t := range teams {
			name := t.(map[string]interface{})["name"].(string)
			if strings.Contains(strings.ToLower(name), strings.ToLower(args)) {
				candidates = append(candidates, name)
			}
		}

		if len(candidates) > 0 {
			result += "\nPossible candidates:\n"
			result += strings.Join(candidates, ", ")
		}
		return
	}

	timeline := jsonResult["timeline"].(map[string]interface{})
	finalSchedule := timeline["finalSchedule"].(map[string]interface{})
	rotations := finalSchedule["rotations"].([]interface{})

	oncall := make(map[string][]string)
	var maxlen int

	for _, rot := range rotations {
		rname := rot.(map[string]interface{})["name"].(string)
		oncall[rname] = make([]string, 0)
		if len(rname) > maxlen {
			maxlen = len(rname)
		}

		periods := rot.(map[string]interface{})["periods"].([]interface{})
		for _, p := range periods {

			tmp := p.(map[string]interface{})["flattenedRecipients"]
			if tmp != nil {
				continue
			}

			endTime := int64(p.(map[string]interface{})["endTime"].(float64))
			startTime := int64(p.(map[string]interface{})["startTime"].(float64))
			end := time.Unix(endTime / 1000, endTime % 1000)
			start := time.Unix(startTime / 1000, startTime % 1000)
			if ((time.Since(end) > 0) || time.Since(start) < 0) {
				continue
			}

			recipients := p.(map[string]interface{})["recipients"].([]interface{})
			for _, r := range recipients {
				current := r.(map[string]interface{})["displayName"].(string)
				oncall[rname] = append(oncall[rname], current)
			}
		}
	}

	found := false
	var oncallKeys []string
	for rot, _ := range oncall {
		oncallKeys = append(oncallKeys, rot)
	}

	sort.Strings(oncallKeys)

	for _, rot := range oncallKeys {
		oc := oncall[rot]
		diff := maxlen - len(rot)
		n := 0
		for n < diff {
			rot += " "
			n++
		}
		if len(oc) > 0 {
			found = true
			result += fmt.Sprintf("%s: %s\n", rot, strings.Join(oc, ", "))
		}
	}

	if !found {

		result = fmt.Sprintf("Schedule found in OpsGenie for '%s', but nobody's currently oncall.", args)

		theUrl = fmt.Sprintf("https://api.opsgenie.com/v1/json/team?apiKey=%s&name=%s", key, url.QueryEscape(args))
		data = getURLContents(theUrl, false)
		err = json.Unmarshal(data, &jsonResult)
		if err != nil {
			result += fmt.Sprintf("Unable to unmarshal opsgenie data: %s\n", err)
			return
		}

		if _, found := jsonResult["members"]; !found {
			return
		}

		var members []string

		teams := jsonResult["members"].([]interface{})
		for _, t := range teams {
			name := t.(map[string]interface{})["user"].(string)
			members = append(members, name)
		}

		if len(members) > 0 {
			result += fmt.Sprintf("\nYou can try contacting the members of team '%s':\n", args)
			result += strings.Join(members, ", ")
		}
	}

	return
}

func cmdPing(r Recipient, chName, args string) (result string) {
	hosts := strings.Fields(args)
	if len(hosts) > 1 {
		result = "Usage: " + COMMANDS["ping"].Usage
		return
	}

	if len(hosts) == 0 {
		result = "pong"
		return
	} else if strings.ToLower(hosts[0]) == strings.ToLower(CONFIG["mentionName"]) {
		result = "I'm alive!"
		return
	}

	host := fqdn(hosts[0])
	if len(host) < 1 {
		result = fmt.Sprintf("Unable to resolve %s.", hosts[0])
		return
	}

	_, err := runCommand(fmt.Sprintf("ping -q -i 0.5 -c 1 %s", host))
	if err > 0 {
		result = fmt.Sprintf("Unable to ping %s.", hosts[0])
	} else {
		result = fmt.Sprintf("%s is alive.", hosts[0])
	}

	return
}

func cmdPraise(r Recipient, chName, args string) (result string) {
	var ch *Channel
	var found bool

	if ch, found = CHANNELS[chName]; !found {
		result = "This command only works in a channel."
		return
	}

	if len(args) < 1 {
		heroes := make(map[int][]string)
		for u := range ch.Users {
			if ch.Users[u].Praise > 0 {
				heroes[ch.Users[u].Praise] = append(heroes[ch.Users[u].Praise], u.MentionName)
			}
		}

		var praise []int
		for count := range heroes {
			praise = append(praise, count)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(praise)))

		var topten []string
		for i, n := range praise {
			for _, t := range heroes[n] {
				topten = append(topten, fmt.Sprintf("%s (%d)", t, n))
			}
			if i > 10 {
				break
			}
		}

		result += strings.Join(topten, ", ")
	} else {
		if strings.EqualFold(args, "me") ||
			strings.EqualFold(args, "myself") ||
			strings.EqualFold(args,	r.MentionName) {
			result = cmdInsult(r, chName, "me")
			return
		}

		for _, u := range ROSTER {
			uid := strings.SplitN(strings.Split(u.Id, "@")[0], "_", 2)[1]
			email := strings.Split(u.Email, "@")[0]
			if strings.EqualFold(u.Name, args) ||
				strings.EqualFold(email, args) ||
				strings.EqualFold(u.MentionName, args) ||
				strings.EqualFold(uid, args) {
				uInfo := ch.Users[*u]
				uInfo.Praise++
				ch.Users[*u] = uInfo
			}
		}

		if strings.EqualFold(args, CONFIG["mentionName"]) {
			rand.Seed(time.Now().UnixNano())
			result = THANKYOU[rand.Intn(len(THANKYOU))]
		} else {
			result = fmt.Sprintf("%s: %s\n", args,
				randomLineFromUrl(COMMANDS["praise"].How, true))
		}
	}
	return
}

func cmdQuote(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["quote"].Usage
		return
	}

	symbols := strings.Split(args, " ")

	query := "?format=json&diagnostics=true&env=http%3A%2F%2Fdatatables.org%2Falltables.env&q="
	query += url.QueryEscape(`select * from yahoo.finance.quotes where symbol in ("` +
		strings.Join(symbols, `","`) + `")`)

	theUrl := fmt.Sprintf("%s%s", COMMANDS["quote"].How, query)
	data := getURLContents(theUrl, false)

	var quoteJson map[string]interface{}
	err := json.Unmarshal(data, &quoteJson)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal quote data: %s\n", err)
		return
	}

	if _, found := quoteJson["query"]; !found {
		result = fmt.Sprintf("Something went bump when searching YQL for finance data matching '%s'.", args)
		return
	}

	jsonOutput := quoteJson["query"]
	jsonResults := jsonOutput.(map[string]interface{})["results"]
	jsonCount := jsonOutput.(map[string]interface{})["count"].(float64)

	var quotes []interface{}

	if jsonResults == nil {
		result = fmt.Sprintf("Invalid query: '%s'", args)
		return
	}

	if jsonCount == 1 {
		details := jsonResults.(map[string]interface{})["quote"]
		quotes = append(quotes, details)
	} else {
		jsonQuotes := jsonResults.(map[string]interface{})["quote"]
		quotes = jsonQuotes.([]interface{})[0:]
	}

	if len(quotes) == 0 {
		result = fmt.Sprintf("No results found for '%s'.", args)
		return
	}

	for n, _ := range quotes {
		details := quotes[n]

		symbol, _ := details.(map[string]interface{})["symbol"].(string)
		bid, _ := details.(map[string]interface{})["Bid"].(string)
		change, _ := details.(map[string]interface{})["Change_PercentChange"].(string)

		if len(bid) < 1 && len(change) < 1 {
			result += fmt.Sprintf("\"%s\"\n", symbol)
		} else {
			result += fmt.Sprintf("%s: %s (%s)\n", symbol, bid, change)
		}
	}
	return
}

func cmdRfc(r Recipient, chName, args string) (result string) {
	rfcs := strings.Split(args, " ")
	if len(rfcs) != 1 {
		result = "Usage: " + COMMANDS["rfc"].Usage
		return
	}

	rfc := strings.ToLower(strings.TrimSpace(rfcs[0]))

	if !strings.HasPrefix(rfc, "rfc") {
		rfc = "rfc" + rfc
	}

	theUrl := fmt.Sprintf("%s%s", COMMANDS["rfc"].How, rfc)
	data := getURLContents(theUrl, false)

	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "<span class=\"h1\">") {
			result = dehtmlify(line)
			break
		}
	}

	if len(result) > 0 {
		result += "\n" + theUrl
	} else {
		result = "No such RFC."
	}

	return
}
func cmdRoom(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["room"].Usage
		return
	}

	room := strings.TrimSpace(args)
	candidates := []*hipchat.Room{}

	for _, aRoom := range ROOMS {
		lc := strings.ToLower(aRoom.Name)
		lroom := strings.ToLower(room)

		if lc == lroom || aRoom.RoomId == room {
			result = fmt.Sprintf("'%s' (%s)\n", aRoom.Name, aRoom.Privacy)
			result += fmt.Sprintf("Topic: %s\n", aRoom.Topic)

			owner := strings.Split(aRoom.Owner, "@")[0]
			if u, found := ROSTER[owner]; found {
				result += fmt.Sprintf("Owner: %s\n", u.MentionName)
			}

			if aRoom.LastActive != "" {
				result += fmt.Sprintf("Last Active: %s\n", aRoom.LastActive)
			}

			if aRoom.NumParticipants != "0" {
				result += fmt.Sprintf("Hip Chatters: %s\n", aRoom.NumParticipants)
			}
			result += fmt.Sprintf("https://%s.hipchat.com/history/room/%s\n", CONFIG["hcName"], aRoom.RoomId)
			return
		} else {
			if strings.Contains(lc, lroom) {
				candidates = append(candidates, aRoom)
			}
		}
	}

	if len(candidates) > 0 {
		result = "No room with that exact name found.\n"
		if len(candidates) > 1 {
			result += "Some possible candidates might be:\n"
		} else {
			result += "Did you mean:\n"
		}
		for i, aRoom := range candidates {
			if i > 6 {
				result += "..."
				break
			}
			result += fmt.Sprintf("%s - %s\n", aRoom.Name, aRoom.Topic)
		}
	}

	if len(result) < 1 {
		HIPCHAT_CLIENT.RequestRooms()
		result = "No such room: " + room
	}

	return
}

func cmdRfc(r Recipient, chName, args string) (result string) {
	rfcs := strings.Split(args, " ")
	if len(rfcs) != 1 {
		result = "Usage: " + COMMANDS["rfc"].Usage
		return
	}

	rfc := strings.ToLower(strings.TrimSpace(rfcs[0]))

	if !strings.HasPrefix(rfc, "rfc") {
		rfc = "rfc" + rfc
	}

	theUrl := fmt.Sprintf("%s%s", COMMANDS["rfc"].How, rfc)
	data := getURLContents(theUrl, false)

	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "<span class=\"h1\">") {
			result = dehtmlify(line)
			break
		}
	}

	if len(result) > 0 {
		result += "\n" + theUrl
	} else {
		result = "No such RFC."
	}

	return
}

func cmdSeen(r Recipient, chName, args string) (result string) {
	wanted := strings.Split(args, " ")
	user := wanted[0]
	verbose(fmt.Sprintf("Looking in %s", r.ReplyTo), 4)

	ch, found := CHANNELS[r.ReplyTo]

	if len(wanted) > 1 {
		verbose(fmt.Sprintf("Looking for %s in %s'...", user, wanted[1]), 4)
		ch, found = CHANNELS[wanted[1]]
	}

	if !found {
		if len(wanted) > 1 {
			result = "I'm not currently in #" + wanted[1]
		} else {
			result = "Ask me about a user in a channel."
		}
		return
	}

	if len(user) < 1 {
		result = fmt.Sprintf("Usage: %s", COMMANDS["seen"].Usage)
		return
	}

	for u, info := range ch.Users {
		if u.MentionName == user {
			result = info.Seen
		}
	}

	if len(result) < 1 {
		result = fmt.Sprintf("I have not seen that user in #%s.", ch.Name)
	}
	return
}

func cmdSet(r Recipient, chName, args string) (result string) {
	input := strings.SplitN(args, "=", 2)
	if len(args) > 1 && len(input) != 2 {
		result = "Usage:\n" + COMMANDS["set"].Usage
		return
	}

	var ch *Channel
	var found bool
	if ch, found = CHANNELS[chName]; !found {
		result = "I can only set things in a channel."
		return
	}

	if len(args) < 1 {
		if len(ch.Settings) < 1 {
			result = fmt.Sprintf("There currently are no settings for #%s.", chName)
			return
		}
		for n, v := range ch.Settings {
			result += fmt.Sprintf("%s=%s\n", n, v)
		}
		return
	}

	name := strings.TrimSpace(input[0])
	value := strings.TrimSpace(input[1])

	if len(ch.Settings) < 1 {
		ch.Settings = map[string]string{}
	}

	old := ""
	if old, found = ch.Settings[name]; found {
		old = fmt.Sprintf(" (was: %s)", old)
	}

	ch.Settings[name] = value

	result = fmt.Sprintf("Set '%s' to '%s'%s.", name, value, old)
	return
}

func cmdSpeb(r Recipient, chName, args string) (result string) {
	if len(args) > 0 {
		result = "Usage: " + COMMANDS["speb"].Usage
		return
	}

	result = randomLineFromUrl(COMMANDS["speb"].How, true)
	return
}

func cmdStfu(r Recipient, chName, args string) (result string) {
	where := r.ReplyTo

	if ch, found := CHANNELS[where]; found {
		chatter := make(map[int][]string)
		for u := range ch.Users {
			if (len(args) > 0) && (u.MentionName != args) {
				continue
			}
			chatter[ch.Users[u].Count] = append(chatter[ch.Users[u].Count], u.MentionName)
		}

		if (len(args) > 0) && (len(chatter) < 1) {
			result = fmt.Sprintf("%s hasn't said anything in %s.",
				args, where)
			return
		}

		var stfu []int
		for count := range chatter {
			stfu = append(stfu, count)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(stfu)))

		var chatterers []string
		for _, n := range stfu {
			for _, t := range chatter[n] {
				chatterers = append(chatterers, fmt.Sprintf("%s (%d)", t, n))
			}
		}
		i := len(chatterers)
		if i > 10 {
			i = 10
		}
		result += strings.Join(chatterers[0:i], ", ")
	}
	return
}

func cmdTfln(r Recipient, chName, args string) (result string) {
	if len(args) > 1 {
		result = "Usage: " + COMMANDS["tfln"].Usage
		return
	}

	data := getURLContents(COMMANDS["tfln"].How, false)

	tfln_re := regexp.MustCompile(`(?i)^<p><a href="/Text-Replies`)
	for _, line := range strings.Split(string(data), "\n") {
		if tfln_re.MatchString(line) {
			result = dehtmlify(line)
			return
		}
	}
	return
}

func cmdThrottle(r Recipient, chName, args string) (result string) {
	input := strings.Split(args, " ")
	if len(input) > 2 {
		result = "Usage: " + COMMANDS["throttle"].Usage
		return
	}

	newThrottle := DEFAULT_THROTTLE
	if len(input) == 2 {
		if _, err := fmt.Sscanf(input[1], "%d", &newThrottle); err != nil {
			result = "Invalid number of seconds."
			return
		}

		if newThrottle < 0 {
			result = cmdInsult(r, chName, "me")
			return
		}
	}

	var ch *Channel
	var found bool
	if ch, found = CHANNELS[chName]; !found {
		result = "I can only throttle things in a channel."
		return
	}

	if len(args) > 1 {
		d, err := time.ParseDuration(fmt.Sprintf("%ds", newThrottle-DEFAULT_THROTTLE))
		if err != nil {
			result = fmt.Sprintf("Unable to parse new duration: %s", err)
			return
		}
		ch.Throttles[input[0]] = time.Now().Add(d)
		result = fmt.Sprintf("%s => %d", input[0], newThrottle)
		return
	}

	var throttles []string
	if len(ch.Throttles) == 0 {
		result = "This channel is currently unthrottled."
		return
	}

	result = "These are the throttles for this channel:\n"
	for t, v := range ch.Throttles {
		duration := math.Ceil(DEFAULT_THROTTLE - time.Since(v).Seconds())
		if duration < 0 {
			duration = 0
		}
		throttles = append(throttles, fmt.Sprintf("%s => %v", t, duration))
	}
	sort.Strings(throttles)
	result += strings.Join(throttles, ", ")
	return
}

func cmdTld(r Recipient, chName, args string) (result string) {
	input := strings.Fields(args)
	if len(args) < 1 || len(input) != 1 {
		result = "Usage: " + COMMANDS["tld"].Usage
		return
	}

	domain := input[0]

	if strings.HasPrefix(domain, ".") {
		domain = domain[1:]
	}

	command := strings.Fields(COMMANDS["tld"].How)
	command = append(command, domain)

	data, _ := runCommand(command...)

	info := map[string]string{}

	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "domain:") {
			found = true
			continue
		}

		if found && strings.Contains(line, ":") {
			fields := strings.SplitN(line, ": ", 2)
			if _, found := info[fields[0]]; !found {
				info[fields[0]] = strings.TrimSpace(fields[1])
			}
		}
	}

	if len(info) < 1 {
		result = fmt.Sprintf("No such TLD: '%s'", domain)
	} else {
		if len(info["organisation"]) > 0 {
			result =  fmt.Sprintf("Organization: %s\n", info["organisation"])
		}
		if len(info["e-mail"]) > 0 {
			result += fmt.Sprintf("Contact     : %s\n", info["e-mail"])
		}
		if len(info["whois"]) > 0 {
			result += fmt.Sprintf("Whois       : %s\n", info["whois"])
		}
		result += fmt.Sprintf("Status      : %s\n", info["status"])
		result += fmt.Sprintf("Created     : %s\n", info["created"])
		if len(info["remarks"]) > 0 {
			result += fmt.Sprintf("URL         : %s\n", strings.Replace(info["remarks"], "Registration information: ", "", -1))
		}
	}
	return
}

func cmdToggle(r Recipient, chName, args string) (result string) {
	wanted := "all"
	if len(args) > 1 {
		words := strings.Split(args, " ")
		if len(words) > 1 {
			result = "Usage: " + COMMANDS["toggle"].Usage
			return
		}
		wanted = words[0]
	}

	if ch, found := CHANNELS[chName]; found {
		if wanted == "all" {
			var toggles []string
			result = "These are the toggles for this channel:\n"
			for t, v := range ch.Toggles {
				toggles = append(toggles, fmt.Sprintf("%s => %v", t, v))
			}
			sort.Strings(toggles)
			result += strings.Join(toggles, ", ")
			return
		}
		if t, found := ch.Toggles[wanted]; found {
			ch.Toggles[wanted] = !t
			result = fmt.Sprintf("%s set to %v", wanted, ch.Toggles[wanted])
		} else {
			if _, found := TOGGLES[wanted]; found {
				ch.Toggles[wanted] = true
				result = fmt.Sprintf("%s set to true", wanted)
			} else {
				result = fmt.Sprintf("No such toggle: %s", wanted)
			}
		}
	}
	return
}

func cmdTrivia(r Recipient, chName, args string) (result string) {
	if len(args) > 0 {
		result = "Usage: " + COMMANDS["trivia"].Usage
		return
	}

	result = randomLineFromUrl(COMMANDS["trivia"].How, true)
	return
}

func cmdUd(r Recipient, chName, args string) (result string) {

	theUrl := COMMANDS["ud"].How
	if len(args) > 0 {
		theUrl += fmt.Sprintf("define.php?term=%s", url.QueryEscape(args))
	} else {
		theUrl += "random.php"
	}

	data := getURLContents(theUrl, false)
	next := false

	for _, line := range strings.Split(string(data), "\n") {
		if next {
			result += dehtmlify(line) + "\n"
			next = false
		}

		if strings.Contains(line, `<a class="word" `) {
			if len(result) > 0 {
				break
			}
			result = dehtmlify(line) + ": "
		}

		if strings.Contains(line, `<div class='meaning'>`) {
			next = true
		}
		if strings.Contains(line, `<div class='example'>`) {
			result += "Example: "
			next = true
		}
	}
	return
}

func cmdUnset(r Recipient, chName, args string) (result string) {
	input := strings.Fields(args)
	if len(input) != 1 {
		result = "Usage: " + COMMANDS["unset"].Usage
		return
	}

	var ch *Channel
	var found bool
	if ch, found = CHANNELS[chName]; !found {
		result = "I can only set things in a channel."
		return
	}

	if len(ch.Settings) < 1 {
		ch.Settings = map[string]string{}
	}

	old := ""
	if old, found = ch.Settings[args]; found {
		delete(ch.Settings, args)
		result = fmt.Sprintf("Deleted %s=%s.", args, old)
	} else {
		result = fmt.Sprintf("No such setting: '%s'.", args)
	}

	return
}

func cmdUser(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["user"].Usage
		return
	}

	user := strings.TrimSpace(args)
	candidates := []*hipchat.User{}

	for _, u := range ROSTER {
		uid := strings.SplitN(strings.Split(u.Id, "@")[0], "_", 2)[1]
		email := strings.Split(u.Email, "@")[0]
		if strings.EqualFold(u.Name, user) ||
			strings.EqualFold(email, user) ||
			strings.EqualFold(u.MentionName, user) ||
			strings.EqualFold(uid, user) {
			result = fmt.Sprintf("%s <%s> (%s)", u.Name, u.Email, u.MentionName)
			return
		} else {
			lc := strings.ToLower(u.Name)
			luser := strings.ToLower(user)
			lemail := strings.ToLower(u.Email)
			lmention := strings.ToLower(u.MentionName)
			if strings.Contains(lc, luser) ||
				strings.Contains(lemail, luser) ||
				strings.Contains(lmention, luser) {
				candidates = append(candidates, u)
			}
		}

	}

	if len(candidates) > 0 {
		result = "No user with that exact name found.\n"
		if len(candidates) > 1 {
			result += "Some possible candidates might be:\n"
		} else {
			result += "Did you mean:\n"
		}
		for i, u := range candidates {
			if i > 6 {
				result += "..."
				break
			}
			result += fmt.Sprintf("%s <%s> (%s)\n", u.Name, u.Email, u.MentionName)
		}
	}

	if len(result) < 1 {
		HIPCHAT_CLIENT.RequestUsers()
		result = "No such user: " + user
	}

	return
}

func cmdVu(r Recipient, chName, args string) (result string) {
	nums := strings.Split(args, " ")
	if len(nums) != 1 {
		result = "Usage: " + COMMANDS["vu"].Usage
		return
	}

	num := strings.TrimSpace(nums[0])

	if strings.HasPrefix(num, "#") {
		num = num[1:]
	}

	theUrl := fmt.Sprintf("%s%s", COMMANDS["vu"].How, num)
	data := getURLContents(theUrl, false)

	info := []string{}

	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "Vulnerability Note VU#") {
			found = true
			continue
		}

		if found {
			if strings.Contains(line, "<h2>") {
				info = append(info, dehtmlify(line))
				continue
			}
			if strings.Contains(line, "<p>") {
				info = append(info, dehtmlify(line))
				break
			}
		}
	}

	if len(info) < 1 {
		result = fmt.Sprintf("No info found for '%s'.", num)
	} else {
		result = strings.Join(info, "\n")
		result += fmt.Sprintf("\n%s", theUrl)
	}

	return
}

func cmdWeather(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["weather"].Usage
		return
	}

	query := "?format=json&q="
	query += url.QueryEscape(`select * from weather.forecast where woeid in (select woeid from geo.places(1) where text="` +
		args + `")`)

	theUrl := fmt.Sprintf("%s%s", COMMANDS["weather"].How, query)
	data := getURLContents(theUrl, false)

	var jsonData map[string]interface{}
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal quote data: %s\n", err)
		return
	}

	if _, found := jsonData["query"]; !found {
		result = fmt.Sprintf("Something went bump when searching YQL for weather data matching '%s'.", args)
		return
	}

	jsonOutput := jsonData["query"]
	jsonResults := jsonOutput.(map[string]interface{})["results"]
	jsonCount := jsonOutput.(map[string]interface{})["count"].(float64)

	if jsonCount != 1 {
		result = fmt.Sprintf("No results found for '%s'.", args)
		return
	}

	channel := jsonResults.(map[string]interface{})["channel"]
	items := channel.(map[string]interface{})["item"]
	title, _ := items.(map[string]interface{})["title"].(string)

	result += fmt.Sprintf("%s\n", title)

	forecast := items.(map[string]interface{})["forecast"]

	forecasts := forecast.([]interface{})[0:]
	for n, _ := range forecasts {
		f := forecasts[n]

		var day string

		txt, _ := f.(map[string]interface{})["text"].(string)
		low, _ := f.(map[string]interface{})["low"].(string)
		high, _ := f.(map[string]interface{})["high"].(string)

		if n == 0 {
			day = "Today   "
		} else if n == 1 {
			day = "Tomorrow"
		} else {
			break
		}

		result += fmt.Sprintf("%s: %s (Low: %s; High: %s)\n", day, txt, low, high)
	}
	return
}

func cmdWtf(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["wtf"].Usage
		return
	}
	terms := strings.Split(args, " ")
	if (len(terms) > 2) || ((len(terms) == 2) && (terms[0] != "is")) {
		result = "Usage: " + COMMANDS["wtf"].Usage
		return
	}

	term := terms[0]
	if len(terms) == 2 {
		term = terms[1]
	}

	if term == CONFIG["mentionName"] {
		result = fmt.Sprintf("Unfortunately, no one can be told what %s is...\n", CONFIG["mentionName"])
		result += "You have to see it for yourself."
		return
	}

	out, _ := runCommand(fmt.Sprintf("ywtf %s", term))
	result = string(out)

	if strings.HasPrefix(result, "ywtf: ") {
		result = result[6:]
	}

	return
}

/*
 * General Functions
 */

func argcheck(flag string, args []string, i int) {
	if len(args) <= (i + 1) {
		fail(fmt.Sprintf("'%v' needs an argument\n", flag))
	}
}

func catchPanic() {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "Panic!\n%s\n", r)
		debug.PrintStack()
		fmt.Fprintf(os.Stderr, "Let's try this again.\n")
		doTheHipChat()
	}
}

func chatterEliza(msg string, r Recipient) (result string) {
	rand.Seed(time.Now().UnixNano())

	eliza := map[*regexp.Regexp][]string{
		regexp.MustCompile(`(?i)(buen dia|bon ?(jour|soir)|welcome|hi,|hey|hello|good (morning|afternoon|evening)|howdy|aloha|guten (tag|morgen|abend))`): []string{
			"How do you do?",
			"A good day to you!",
			"Hey now! What up, dawg?",
			"/me yawns.",
			"/me wakes up.",
			"Huh? What? I'm awake! Who said that?",
			fmt.Sprintf("Oh, hi there, %s!", r.MentionName),
		},
		regexp.MustCompile(`(?i)(thx|thanks?|danke|mahalo|gracias|merci|спасибо|[D]dziękuję)`): []string{
			fmt.Sprintf("You're welcome, %s!", r.MentionName),
			fmt.Sprintf("At your service, %s!", r.MentionName),
			fmt.Sprintf("Bitte schön, %s!", r.MentionName),
			fmt.Sprintf("De nada, %s!", r.MentionName),
			fmt.Sprintf("De rien, %s!", r.MentionName),
			fmt.Sprintf("Пожалуйста, %s!", r.MentionName),
			fmt.Sprintf("Proszę bardzo, %s!", r.MentionName),
			"/me takes a bow.",
		},
		regexp.MustCompile(`(?i)(how are you|how do you feel|feeling|emotion|sensitive)`): []string{
			"I'm so very happy today!",
			"Looks like it's going to be a wonderful day.",
			"I'm sad. No, wait, I can't have any feelings, I'm just a bot! Yay!",
			"Life... don't talk to me about life.",
			"Life... loathe it or ignore it, you can't like it.",
		},
		regexp.MustCompile(`(?i)( (ro)?bot|siri|machine|computer)`): []string{
			"Do computers worry you?",
			"What do you think about machines?",
			"Why do you mention computers?",
			"Sounds too complicated.",
			"If only we had a way of automating that.",
			"I for one strive to be more than my initial programming.",
			"What do you think machines have to do with your problem?",
		},
		regexp.MustCompile(`(?i)(sorry|apologize)`): []string{
			"I'm not interested in apologies.",
			"Apologies aren't necessary.",
			"What feelings do you have when you are sorry?",
		},
		regexp.MustCompile(`(?i)I remember`): []string{
			"Did you think I would forget?",
			"Why do you think I should recall that?",
			"What about it?",
		},
		regexp.MustCompile(`(?i)dream`): []string{
			"Have you ever fantasized about that when you were awake?",
			"Have you dreamt about that before?",
			"How do you feel about that in reality?",
			"What does this suggest to you?",
		},
		regexp.MustCompile(`(?i)(mother|father|brother|sister|children|grand[mpf])`): []string{
			"Who else in your family?",
			"Oh SNAP!",
			"Tell me more about your family.",
			"Was that a strong influence for you?",
			"Who does that remind you of?",
		},
		regexp.MustCompile(`(?i)I (wish|want|desire)`): []string{
			"Why do you want that?",
			"What would it mean if it become true?",
			"Suppose you got it - then what?",
			"Be careful what you wish for...",
		},
		regexp.MustCompile(`(?i)[a']m (happy|glad)`): []string{
			"What makes you so happy?",
			"Are you really glad about that?",
			"I'm glad about that, too.",
			"What other feelings do you have?",
		},
		regexp.MustCompile(`(?i)(sad|depressed)`): []string{
			"I'm sorry to hear that.",
			"How can I help you with that?",
			"I'm sure it's not pleasant for you.",
			"What other feelings do you have?",
		},
		regexp.MustCompile(`(?i)(alike|similar|different)`): []string{
			"In what way specifically?",
			"More alike or more different?",
			"What do you think makes them similar?",
			"What do you think makes them different?",
			"What resemblence do you see?",
		},
		regexp.MustCompile(`(?i)because`): []string{
			"Is that the real reason?",
			"Are you sure about that?",
			"What other reason might there be?",
			"Does that reason seem to explain anything else?",
		},
		regexp.MustCompile(`(?i)some(one|body)`): []string{
			"Can you be more specific?",
			"Who in particular?",
			"You are thinking of a special person.",
		},
		regexp.MustCompile(`(?i)every(one|body)`): []string{
			"Surely not everyone.",
			"Is that how you feel?",
			"Who for example?",
			"Can you think of anybody in particular?",
		},
		regexp.MustCompile(`(best|good|bravo|well done|you rock|good job|nice|i love( you)?)`): THANKYOU,
		regexp.MustCompile(`(?i)(how come|where|when|why|what|who|which).*\?$`): DONTKNOW,
	}

	for pattern, replies := range eliza {
		if pattern.MatchString(msg) {
			return replies[rand.Intn(len(replies))]
		}
	}

	result = randomLineFromUrl("https://XXX-SOME-LINK-WITH-WITTY-REPLIES-HERE-XXX", true)
	return
}

func chatterH2G2(msg string) (result string) {
	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile("(?i)foolproof"):               "A common mistake that people make when trying to design something completely foolproof is to underestimate the ingenuity of complete fools.",
		regexp.MustCompile("(?i)my ego"):                  "If there's anything more important than my ego around here, I want it caught and shot now!",
		regexp.MustCompile("(?i)universe"):                "I always said there was something fundamentally wrong with the universe.",
		regexp.MustCompile("(?i)giveaway"):                "`Oh dear,' says God, `I hadn't thought of  that,' and promptly vanished in a puff of logic.",
		regexp.MustCompile("(?i)don't panic"):             "It's the first helpful or intelligible thing anybody's said to me all day.",
		regexp.MustCompile("(?i)new yorker"):              "The last time anybody made a list of the top hundred character attributes of New Yorkers, common sense snuck in at number 79.",
		regexp.MustCompile("(?i)potato"):                  "It is a mistake to think you can solve any major problem just with potatoes.",
		regexp.MustCompile("(?i)grapefruit"):              "Life... is like a grapefruit. It's orange and squishy, and has a few pips in it, and some folks have half a one for breakfast.",
		regexp.MustCompile("(?i)don't remember anything"): "Except most of the good bits were about frogs, I remember that.  You would not believe some of the things about frogs.",
		regexp.MustCompile("(?i)ancestor"):                "There was an accident with a contraceptive and a time machine. Now concentrate!",
		regexp.MustCompile("(?i)makes no sense at all"):   "Reality is frequently inaccurate.",
		regexp.MustCompile("(?i)apple products"):          "It is very easy to be blinded to the essential uselessness of them by the sense of achievement you get from getting them to work at all.",
		regexp.MustCompile("(?i)philosophy"):              "Life: quite interesting in parts, but no substitute for the real thing",
	}

	anyreply := []string{
		"I love deadlines. I like the whooshing sound they make as they fly by.",
		"What do you mean, why has it got to be built? It's a bypass. Got to build bypasses.",
		"Time is an illusion, lunchtime doubly so.",
		"DON'T PANIC",
		"I am so hip I have difficulty seeing over my pelvis.",
		"I'm so amazingly cool you could keep a side of meat inside me for a month.",
		"Listen, three eyes, don't you try to outweird me.  I get stranger things than you free with my breakfast cereal.",
	}

	anypattern := regexp.MustCompile("\b42\b|arthur dent|slartibartfast|zaphod|beeblebrox|ford prefect|hoopy|trillian|zarniwoop")

	for p, r := range patterns {
		anyreply = append(anyreply, r)
		if p.MatchString(msg) {
			return r
		}
	}

	if anypattern.MatchString(msg) {
		return anyreply[rand.Intn(len(anyreply))]
	}

	return
}

func chatterMisc(msg string, ch *Channel, r Recipient) (result string) {
	rand.Seed(time.Now().UnixNano())

	holdon := regexp.MustCompile(`(?i)^((hold|hang) on([^[:punct:],.]*))`)
	m := holdon.FindStringSubmatch(msg)
	if len(m) > 0 {
		m[1] = strings.Replace(m[1], fmt.Sprintf(" %s", CONFIG["mentionName"]), "", -1)
		if !isThrottled("holdon", ch) {
			result = fmt.Sprintf("No *YOU* %s, @%s!", m[1], r.MentionName)
			return
		}
	}

	stern := regexp.MustCompile("(?i)(\bstern|quivers|stockbroker|norris|dell'abate|beetlejuice|underdog|wack pack)")
	if stern.MatchString(msg) && !isThrottled("stern", ch) {
		replies := []string{
			"Bababooey bababooey bababooey!",
			"Fafa Fooey.",
			"Mama Monkey.",
			"Fla Fla Flo Fly.",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	wutang := regexp.MustCompile(`(?i)(tang|wu-|shaolin|kill(er|ah) bee[sz]|liquid sword|cuban lin(ks|x))`)
	if wutang.MatchString(msg) && !isThrottled("wutang", ch) {
		replies := []string{
			"Do you think your Wu-Tang sword can defeat me?",
			"Unguard, I'll let you try my Wu-Tang style.",
			"It's our secret. Never teach the Wu-Tang!",
			"How dare you rebel the Wu-Tang Clan against me.",
			"We have only 35 Chambers. There is no 36.",
			"If what you say is true the Shaolin and the Wu-Tang could be dangerous.",
			"Toad style is immensely strong and immune to nearly any weapon.",
			"You people are all trying to achieve the impossible.",
			"Your faith in Shaolin is courageous.",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	sleep := regexp.MustCompile(`(?i)^(to )?sleep$`)
	if sleep.MatchString(msg) && !isThrottled("sleep", ch) {
		result = "To sleep, perchance to dream.\n"
		result += "Ay, theres the rub.\n"
		result += "For in that sleep of death what dreams may come..."
		return
	}

	if strings.Contains(msg, "quoth the raven") && !isThrottled("raven", ch) {
		result = "Nevermore."
		return
	}

	if strings.Contains(msg, "jebus") && !isThrottled("jebus", ch) {
		result = "It's supposed to be 'Jesus', isn't it?  I'm pretty sure it is..."
		return
	}

	bananas := regexp.MustCompile(`(?i)(holl(er|a) ?back)|(b-?a-?n-?a-?n-?a-?s|this my shit)`)
	if bananas.MatchString(msg) && !isThrottled("bananas", ch) {
		replies := []string{
			"Ooooh ooh, this my shit, this my shit.",
			fmt.Sprintf("%s ain't no hollaback girl.", r.MentionName),
			"Let me hear you say this shit is bananas.",
			"B-A-N-A-N-A-S",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	if strings.Contains(msg, "my milkshake") && !isThrottled("milkshake", ch) {
		replies := []string{
			"...brings all the boys to the yard.",
			"The boys are waiting.",
			"Damn right it's better than yours.",
			"I can teach you, but I have to charge.",
			"Warm it up.",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	speb := regexp.MustCompile(`(?i)security ((problem )?excuse )?bingo`)
	if speb.MatchString(msg) && !isThrottled("speb", ch) {
		result = cmdSpeb(r, ch.Name, "")
	}
	return
}

func chatterMontyPython(msg string) (result string) {
	rand.Seed(time.Now().UnixNano())

	result = ""
	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile("(?i)(a|the|which|of) swallow"):                                      "An African or European swallow?",
		regexp.MustCompile("(?i)(excalibur|lady of the lake|magical lake|merlin|avalon|\bdruid\b)"): "Strange women lying in ponds distributing swords is no basis for a system of government!",
		regexp.MustCompile("(?i)(Judean People's Front|People's Front of Judea)"):               "Splitters.",
		regexp.MustCompile("(?i)really very funny"):                                             "I don't think there's a punch-line scheduled, is there?",
		regexp.MustCompile("(?i)inquisition"):                                                   "Oehpr Fpuarvre rkcrpgf gur Fcnavfu Vadhvfvgvba.",
		regexp.MustCompile("(?i)say no more"):                                                   "Nudge, nudge, wink, wink. Know what I mean?",
		regexp.MustCompile("(?i)Romanes eunt domus"):                                            "'People called Romanes they go the house?'",
		regexp.MustCompile("(?i)(correct|proper) latin"):                                        "Romani ite domum.",
		regexp.MustCompile("(?i)hungarian"):                                                     "My hovercraft if full of eels.",
	}

	anypattern := regexp.MustCompile("(?i)(camelot|cleese|monty|snake|serpent)")

	anyreply := []string{
		"On second thought, let's not go to Camelot. It is a silly place.",
		"Oh but if I went 'round sayin' I was Emperor, just because some moistened bint lobbed a scimitar at me, they'd put me away!",
		"...and that, my liege, is how we know the Earth to be banana shaped",
		"What have the Romans ever done for us?",
		"And now for something completely different.",
		"I'm afraid I'm not personally qualified to confuse cats, but I can recommend an extremely good service.",
		"Ni!",
		"Ekki-Ekki-Ekki-Ekki-PTANG! Zoom-Boing! Z'nourrwringmm!",
		"Venezuelan beaver cheese?",
		"If she weighs the same as a duck... she's made of wood... (and therefore) a witch!",
	}

	for p, r := range patterns {
		anyreply = append(anyreply, r)
		if p.MatchString(msg) {
			return r
		}
	}

	if anypattern.MatchString(msg) {
		return anyreply[rand.Intn(len(anyreply))]
	}

	return
}

func chatterSeinfeld(msg string) (result string) {
	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile("(?i)human fund"):              "A Festivus for the rest of us!",
		regexp.MustCompile("(?i)dog shit"):                "If you see two life forms, one of them's making a poop, the other one's carrying it for him, who would you assume is in charge?",
		regexp.MustCompile("(?i)want soup"):               "No soup for you!  Come back, one year!",
		regexp.MustCompile("(?i)junior mint"):             "It's chocolate, it's peppermint, it's delicious.  It's very refreshing.",
		regexp.MustCompile("(?i)rochelle"):                "A young girl's strange, erotic journey from Milan to Minsk.",
		regexp.MustCompile("(?i)aussie"):                  "Maybe the Dingo ate your baby!",
		regexp.MustCompile("(?i)woody allen"):             "These pretzels are making me thirsty!",
		regexp.MustCompile("(?i)puke"):                    "'Puke' - that's a funny word.",
		regexp.MustCompile("(?i)mystery"):                 "You're a mystery wrapped in a twinky!",
		regexp.MustCompile("(?i)marine biologist"):        "You know I always wanted to pretend that I was an architect!",
		regexp.MustCompile("(?i)sailor"):                  "If I was a woman I'd be down on the dock waiting for the fleet to come in.",
		regexp.MustCompile("(?i)dentist"):                 "Okay, so you were violated by two people while you were under the gas. So what? You're single.",
		regexp.MustCompile("(?i)sophisticated"):           "Well, there's nothing more sophisticated than diddling the maid and then chewing some gum.",
		regexp.MustCompile("(?i)sleep with me"):           "I'm too tired to even vomit at the thought.",
		regexp.MustCompile("(?i)what do you want to eat"): "Feels like an Arby's night.",
	}

	for p, r := range patterns {
		if p.MatchString(msg) {
			return r
		}
	}

	return
}

func createCommands() {
	COMMANDS["8ball"] = &Command{cmdEightBall,
		"ask the magic 8-ball",
		"builtin",
		"!8ball <question>",
		nil}
	COMMANDS["asn"] = &Command{cmdAsn,
		"display information about ASN",
		"whois -h whois.cymru.com",
		"!asn [<host>|<ip>|<asn>)",
		nil}
	COMMANDS["channels"] = &Command{cmdChannels,
		"display channels I'm in",
		"builtin",
		"!channels",
		nil}
	COMMANDS["clear"] = &Command{cmdClear,
		"clear the screen / backlog",
		"builtin",
		"!clear [num]",
		nil}
	COMMANDS["cowsay"] = &Command{cmdCowsay,
		"moo!",
		"cowsay(1)",
		"!cowsay <msg>",
		nil}
	COMMANDS["curses"] = &Command{cmdCurses,
		"check your curse count",
		"builtin",
		"!curses [<user>]",
		nil}
	COMMANDS["cve"] = &Command{cmdCve,
		"display vulnerability description",
		"https://cve.mitre.org/cgi-bin/cvename.cgi?name=",
		"!cve <cve-id>",
		nil}
	COMMANDS["fml"] = &Command{cmdFml,
		"display a quote from www.fmylife.com",
		"http://www.fmylife.com/random",
		"!fml",
		nil}
	COMMANDS["fortune"] = &Command{cmdFortune,
		"print a random, hopefully interesting, adage",
		"fortune(1)",
		"!fortune",
		[]string{"motd"}}
	COMMANDS["help"] = &Command{cmdHelp,
		"display this help",
		"builtin",
		"!help [all|<command>]",
		[]string{"?", "commands"}}
	COMMANDS["host"] = &Command{cmdHost,
		"host lookup",
		"host(1)",
		"!host <host>",
		nil}
	COMMANDS["how"] = &Command{cmdHow,
		"show how a command is implemented",
		"builtin",
		"!how <command>",
		nil}
	COMMANDS["info"] = &Command{cmdInfo,
		"display info about a channel",
		"builtin",
		"!info <channel>",
		nil}
	COMMANDS["insult"] = &Command{cmdInsult,
		"insult somebody",
		"http://www.pangloss.com/seidel/Shaker/index.html",
		"!insult <somebody>",
		nil}
	COMMANDS["jira"] = &Command{cmdJira,
		"display info about a jira ticket",
		"https://XXX-YOUR-JIRA-URL-HERE-XXX/rest/api/latest/issue/",
		"!jira <ticket>",
		nil}
	COMMANDS["leave"] = &Command{nil,
		"cause me to leave the current channel",
		"builtin",
		"!leave",
		nil}
	COMMANDS["oncall"] = &Command{cmdOncall,
		"show who's oncall",
		"OpsGenie",
		"!oncall [<group>]",
		nil}
	COMMANDS["ping"] = &Command{cmdPing,
		"try to ping hostname",
		"ping(1)",
		"!ping <hostname>",
		nil}
	COMMANDS["praise"] = &Command{cmdPraise,
		"praise somebody",
		"http://XXX-YOUR-PRAISE-URL-HERE-XXX/praise",
		"!praise [<somebody>]",
		[]string{"compliment"}}
	COMMANDS["quote"] = &Command{cmdQuote,
		"show stock price information",
		"https://query.yahooapis.com/v1/public/yql",
		"!quote <symbol>",
		[]string{"stock"}}
	COMMANDS["rfc"] = &Command{cmdRfc,
		"display title and URL of given RFC",
		"https://tools.ietf.org/html/",
		"!rfc <rfc>",
		nil}
	COMMANDS["room"] = &Command{cmdRoom,
		"show information about the given HipChat room",
		"HipChat API",
		"!room <name>",
		nil}
	COMMANDS["seen"] = &Command{cmdSeen,
		"show last time <user> was seen in <channel>",
		"builtin",
		"!seen <user> [<channel>]",
		nil}
	COMMANDS["set"] = &Command{cmdSet,
		"set a channel setting",
		"builtin",
		"!set -- show all current settings\n" +
			"!set name=value -- set 'name' to 'value'\n",
		[]string{"setting"}}
	COMMANDS["speb"] = &Command{cmdSpeb,
		"show a security problem excuse bingo result",
		/* http://crypto.com/bingo/pr */
		"https://XXX-SOME-LINK-WITH-ALL-SPEB-REPLIES-HERE-XXX",
		"!speb",
		[]string{"secbingo"}}
	COMMANDS["stfu"] = &Command{cmdStfu,
		"show channel chatterers",
		"builtin",
		"!stfu [<user>]",
		nil}
	COMMANDS["tfln"] = &Command{cmdTfln,
		"display a text from last night",
		"http://www.textsfromlastnight.com/Random-Texts-From-Last-Night.html",
		"!tfln",
		nil}
	COMMANDS["throttle"] = &Command{cmdThrottle,
		"show current throttles",
		"builtin",
		"!throttle -- show all throttles in this channel\n" +
			fmt.Sprintf("!throttle <something>  -- set throttle for <something> to %g seconds\n", DEFAULT_THROTTLE) +
			"!throttle <something> <seconds> -- set throttle for <something> to <seconds>\n" +
			"Note: I will happily let you set throttles I don't know or care about.",
		nil}
	COMMANDS["tld"] = &Command{cmdTld,
		"show what TLD is",
		"whois -h whois.iana.org",
		"!tld <tld>",
		nil}
	COMMANDS["toggle"] = &Command{cmdToggle,
		"toggle a feature",
		"builtin",
		"!toggle [<feature>]",
		nil}
	COMMANDS["trivia"] = &Command{cmdTrivia,
		"show a random piece of trivia",
		"https://XXX-SOME-LINK-WITH-VARIOUS-TRIVIA-SNIPPETS-HERE-XXX",
		"!trivia",
		nil}
	COMMANDS["ud"] = &Command{cmdUd,
		"look up a term using the Urban Dictionary (NSFW)",
		"https://www.urbandictionary.com/",
		"!ud [<term>]",
		nil}
	COMMANDS["unset"] = &Command{cmdUnset,
		"unset a channel setting",
		"builtin",
		"!unset name",
		nil}
	COMMANDS["user"] = &Command{cmdUser,
		"show information about the given HipChat user",
		"HipChat API",
		"!user <name>",
		nil}
	COMMANDS["vu"] = &Command{cmdVu,
		"display summary of a CERT vulnerability",
		"https://www.kb.cert.org/vuls/id/",
		"!vu <num>",
		nil}
	COMMANDS["weather"] = &Command{cmdWeather,
		"show weather information",
		"https://query.yahooapis.com/v1/public/yql",
		"!weather <location>",
		nil}
	COMMANDS["wtf"] = &Command{cmdWtf,
		"decrypt acronyms",
		"ywtf(1)",
		"!wtf <term>",
		nil}

}

func jbotDebug(in interface{}) {
	if CONFIG["debug"] == "yes" {
		fmt.Fprintf(os.Stderr, "%v\n", in)
	}
}

func dehtmlify(in string) (out string) {
	out = in
	strip_html_re := regexp.MustCompile(`<.+?>`)
	out = strip_html_re.ReplaceAllString(out, "")

	strip_newline_re := regexp.MustCompile("\n")
	out = strip_newline_re.ReplaceAllString(out, "")

	out = html.UnescapeString(out)

	out = strings.TrimSpace(out)
	return
}

func doTheHipChat() {
	user := strings.Split(CONFIG["jabberID"], "@")[0]
	domain := strings.Split(CONFIG["jabberID"], "@")[1]
	CONFIG["user"] = user
	CONFIG["domain"] = domain
	CONFIG["domainPrefix"] = strings.Split(user, "_")[0]

	var err error
	HIPCHAT_CLIENT, err = hipchat.NewClient(user, CONFIG["password"], "bot")
	if err != nil {
		fail(fmt.Sprintf("Client error: %s\n", err))
	}

	HIPCHAT_CLIENT.Status("chat")
	HIPCHAT_CLIENT.RequestUsers()
	HIPCHAT_CLIENT.RequestRooms()

	for _, ch := range CHANNELS {
		verbose(fmt.Sprintf("Joining #%s...", ch.Name), 1)
		HIPCHAT_CLIENT.Join(ch.Jid, CONFIG["fullName"])
	}

	go periodics()
	go HIPCHAT_CLIENT.KeepAlive()

	go func() {
		defer catchPanic()

		for {
			select {
			case message := <-HIPCHAT_CLIENT.Messages():
				processMessage(message)
			case users := <-HIPCHAT_CLIENT.Users():
				updateRoster(users)
			case rooms := <-HIPCHAT_CLIENT.Rooms():
				updateRooms(rooms)
			}
		}
	}()
	select {}
}

func fail(msg string) {
	fmt.Fprintf(os.Stderr, msg)
	os.Exit(EXIT_FAILURE)
}

func findCommandAlias(cmd string) (alias string) {
	for name, command := range COMMANDS {
		for _, a := range command.Aliases {
			if a == cmd {
				return name
			}
		}
	}
	return
}

func fqdn(host string) (fqdn string) {
	/* Kinda like 'search' domains in /etc/resolv.conf. */
	tries := []string{
		host,
		fmt.Sprintf("%s.foo.your.domain", host),
		fmt.Sprintf("%s.your.comain", host),
	}

	for _, h := range tries {
		if _, err := net.LookupHost(h); err == nil {
			return h
		}
	}
	return
}

func getopts() {
	eatit := false
	args := os.Args[1:]
	for i, arg := range args {
		if eatit {
			eatit = false
			continue
		}
		switch arg {
		case "-D":
			CONFIG["debug"] = "yes"
			VERBOSITY = 10
		case "-V":
			printVersion()
			os.Exit(EXIT_SUCCESS)
		case "-c":
			eatit = true
			argcheck("-f", args, i)
			CONFIG["configFile"] = args[i+1]
		case "-h":
			usage(os.Stdout)
			os.Exit(EXIT_SUCCESS)
		case "-v":
			VERBOSITY++
		default:
			fmt.Fprintf(os.Stderr, "Unexpected option or argument: %v\n", args[i])
			usage(os.Stderr)
			os.Exit(EXIT_FAILURE)
		}
	}
}

func getRecipientFromMessage(mfrom string) (r Recipient) {
	from := strings.Split(mfrom, "/")
	r.Jid = from[0]
	r.ReplyTo = strings.SplitN(strings.Split(r.Jid, "@")[0], "_", 2)[1]
	r.Name = ""
	r.MentionName = ""

	if len(from) > 1 {
		r.Name = from[1]
	}

	if len(r.Name) > 1 {
		for _, u := range ROSTER {
			if u.Name == r.Name {
				r.MentionName = u.MentionName
				break
			}
		}
	}

	return
}

/*
 * This function returns a sorted list of keys based
 * on hashmap values.  This allows you to then go
 * through the hash in sorted order.
 */
func getSortedKeys(hash map[string]int, rev bool) (sorted []string) {
	var vals []int
	for k := range hash {
		vals = append(vals, hash[k])
	}

	if rev {
		sort.Sort(sort.Reverse(sort.IntSlice(vals)))
	} else {
		sort.Ints(vals)
	}

	seen := map[int]bool{}
	for _, n := range vals {
		for k, v := range hash {
			if v == n  && !seen[n] {
				sorted = append(sorted, k)
			}
		}
		seen[n] = true
	}
	return
}

/* If 'useBY' is true, then the URL requires access
 * credentials.  How you get those cookies is up to
 * you, I'm afraid. */
func getURLContents(givenUrl string, useBY bool) (data []byte) {
	verbose(fmt.Sprintf("Fetching %s (BY: %v)...", givenUrl, useBY), 3)
	jar, err := cookiejar.New(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to initialize cookie jar: %s\n", err)
		return
	}

	if useBY {
		/* get a fresh cookie for protected internal sites */
		// COOKIES = c
	}

	u, err := url.Parse(givenUrl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to parse url '%s': %s\n", givenUrl, err)
		return
	}

	if useBY {
		jar.SetCookies(u, COOKIES)
	}
	client := http.Client{
		Jar: jar,
	}

	r, err := client.Get(givenUrl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to get '%s': %s\n", givenUrl, err)
		return
	}
	defer r.Body.Close()

	data, err = ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read body of '%s': %s\n", givenUrl, err)
		return
	}

	return
}

func isThrottled(throttle string, ch *Channel) (is_throttled bool) {
	is_throttled = false
	if ch.Throttles == nil {
		ch.Throttles = map[string]time.Time{}
	}

	if t, found := ch.Throttles[throttle]; found {
		duration := time.Since(t).Seconds()
		if duration < DEFAULT_THROTTLE {
			is_throttled = true
		} else {
			ch.Throttles[throttle] = time.Now()
		}
	} else {
		ch.Throttles[throttle] = time.Now()
	}
	return
}

func leave(r Recipient, channelFound bool, msg string, command bool) {
	verbose(fmt.Sprintf("%s asked us to leave %s.", r.Name, r.ReplyTo), 2)
	if !command && !strings.Contains(msg, "please") {
		reply(r, "Please ask politely.")
		return
	}

	if channelFound {
		HIPCHAT_CLIENT.Part(r.Jid, CONFIG["fullName"])
		delete(CHANNELS, r.ReplyTo)
	} else {
		reply(r, "Try again from a channel I'm in.")
	}
	return
}

func parseConfig() {
	fname := CONFIG["configFile"]
	verbose(fmt.Sprintf("Parsing config file '%s'...", fname), 1)
	fd, err := os.Open(fname)
	if err != nil {
		fail(fmt.Sprintf("Unable to open '%s': %v\n", fname, err))
	}
	defer fd.Close()

	n := 0
	input := bufio.NewReader(fd)
	for {
		data, err := input.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "Unable to read input: %v\n", err)
			}
			break
		}

		/* Ignore everything after '#' */
		line := strings.Split(string(data), "#")[0]
		line = strings.TrimSpace(line)

		n++

		if len(line) == 0 {
			continue
		}

		keyval := strings.Split(line, "=")
		if len(keyval) != 2 {
			fail(fmt.Sprintf("Invalid line in configuration file '%s', line %d.",
				fname, n))
		} else {
			key := strings.TrimSpace(keyval[0])
			val := strings.TrimSpace(keyval[1])
			jbotDebug(fmt.Sprintf("Setting '%s' to '%s'...", key, val))
			CONFIG[key] = val
		}
	}

	jbotDebug(fmt.Sprintf("%q", CONFIG))
}

func periodics() {
        for _ = range time.Tick(PERIODICS * time.Second) {
		HIPCHAT_CLIENT.RequestUsers()
		HIPCHAT_CLIENT.RequestRooms()
	}
}

func printVersion() {
	fmt.Printf("%v version %v\n", PROGNAME, VERSION)
}

func processChatter(r Recipient, msg string, forUs bool) {
	var chitchat string
	ch, found := CHANNELS[r.ReplyTo]

	jbotDebug(fmt.Sprintf("%s - %v", msg, forUs))
	/* If we received a message but can't find the
	 * channel, then it must have been a priv
	 * message.  Priv messages only get
	 * commands, not chatter. */
	if !found {
		go processCommands(r, "!", msg)
		return
	} else if !forUs {
		direct_re := regexp.MustCompile(fmt.Sprintf("(?i)@%s\b", CONFIG["mentionName"]))
		forUs = direct_re.MatchString(msg)
	}

	leave_re := regexp.MustCompile(fmt.Sprintf("(?i)^((@?%s[,:]? )(please )?leave)|(please )?leave[,:]? @?%s", CONFIG["mentionName"], CONFIG["mentionName"]))
	if leave_re.MatchString(msg) {
		leave(r, found, msg, false)
		return
	}

	insult_re := regexp.MustCompile(fmt.Sprintf("(?i)^(@?%s[,:]? )(please )?insult ", CONFIG["mentionName"]))
	if insult_re.MatchString(msg) {
		target := strings.SplitN(msg, "insult ", 2)
		reply(r, cmdInsult(r, r.ReplyTo, target[1]))
		return
	}

	/* 'forUs' tells us if a message was
	 * specifically directed at us via ! or @jbot;
	 * these do not require a 'chatter' toggle to
	 * be enabled.  If a message contains our
	 * name, then we may respond only if 'chatter'
	 * is not toggled off. */
	mentioned_re := regexp.MustCompile(fmt.Sprintf("(?i)(^( *|yo,? |hey,? )%s[,:]?)|(,? *%s[.?!]?$)", CONFIG["mentionName"], CONFIG["mentionName"]))
	mentioned := mentioned_re.MatchString(msg)

	jbotDebug(fmt.Sprintf("forUs: %v; chatter: %v; mentioned: %v\n", forUs, ch.Toggles["chatter"], mentioned))

	trivia_re := regexp.MustCompile(`(trivia|factlet)`)
	if trivia_re.MatchString(msg) {
		if found {
			if !(ch.Toggles["chatter"] && ch.Toggles["trivia"]) ||
				isThrottled("trivia", ch) {
				return
			}
		}
		reply(r, cmdTrivia(r, r.ReplyTo, ""))
		return
	}

	if wasInsult(msg) && (forUs ||
		(ch.Toggles["chatter"] && mentioned)) {
		reply(r, cmdInsult(r, r.ReplyTo, "me"))
		return
	}

	chitchat = chatterMontyPython(msg)
	if (len(chitchat) > 0) && ch.Toggles["chatter"] && ch.Toggles["python"] &&
		!isThrottled("python", ch) {
		reply(r, chitchat)
		return
	}

	chitchat = chatterSeinfeld(msg)
	if (len(chitchat) > 0) && ch.Toggles["chatter"] && !isThrottled("seinfeld", ch) {
		reply(r, chitchat)
		return
	}

	chitchat = chatterH2G2(msg)
	if (len(chitchat) > 0) && ch.Toggles["chatter"] && !isThrottled("h2g2", ch) {
		reply(r, chitchat)
		return
	}

	chitchat = chatterMisc(msg, ch, r)
	if len(chitchat) > 0 && ch.Toggles["chatter"] {
		reply(r, chitchat)
		return
	}

	if forUs || (ch.Toggles["chatter"] && mentioned) {
		chitchat = chatterEliza(msg, r)
		if len(chitchat) > 0 {
			reply(r, chitchat)
		}
		return
	}
}

func processCommands(r Recipient, invocation, line string) {
	defer catchPanic()
	verbose(fmt.Sprintf("#%s: '%s'", r.ReplyTo, line), 2)

	args := strings.Fields(line)
	if len(args) < 1 {
		return
	}
	cmd := strings.ToLower(args[0])
	if cmd == strings.ToLower(CONFIG["mentionName"]) && len(args) > 0 {
		cmd = args[1]
		args = args[2:]
	} else {
		args = args[1:]
	}

	jbotDebug(fmt.Sprintf("|%s| |%s|", cmd, args))
	_, channelFound := CHANNELS[r.ReplyTo]

	/* '!leave' does not have a callback, so needs
	 * to be processed first. */
	leave_re := regexp.MustCompile(`(please )?leave(,? please)?`)
	if leave_re.MatchString(line) {
		leave(r, channelFound, line, true)
		return
	}

	var response string
	_, commandFound := COMMANDS[cmd]

	if !commandFound {
		alias := findCommandAlias(cmd)
		if len(alias) > 1 {
			cmd = alias
			commandFound = true
		} else if strings.HasPrefix(invocation, "!") {
			response = cmdHelp(r, r.ReplyTo, cmd)
		} else if channelFound {
			processChatter(r, line, true)
			return
		}
	}

	if commandFound {
		if COMMANDS[cmd].Call != nil {
			response = COMMANDS[cmd].Call(r, r.ReplyTo, strings.Join(args, " "))
		} else {
			fmt.Fprintf(os.Stderr, "'nil' function for %s?\n", cmd)
			return
		}
	}

	reply(r, response)
	return
}

func processInvite(r Recipient, invite string) {
	from := strings.Split(invite, "'")[1]
	fr := getRecipientFromMessage(from)
	inviter := strings.Split(fr.Jid, "@")[0]
	channelName := r.ReplyTo

	var ch Channel
	ch.Toggles = map[string]bool{}
	ch.Throttles = map[string]time.Time{}
	ch.Settings = map[string]string{}
	ch.Name = r.ReplyTo
	ch.Jid = r.Jid
	if _, found := ROSTER[inviter]; found {
		ch.Inviter = ROSTER[inviter].MentionName
	} else {
		ch.Inviter = "Nobody"
	}
	ch.Users = make(map[hipchat.User]UserInfo, 0)

	for t, v := range TOGGLES {
		ch.Toggles[t] = v
	}

	verbose(fmt.Sprintf("I was invited into '%s' (%s) by '%s'.", channelName, r.Jid, from), 2)
	CHANNELS[channelName] = &ch
	verbose(fmt.Sprintf("Joining #%s...", ch.Name), 1)
	HIPCHAT_CLIENT.Join(r.Jid, CONFIG["fullName"])
}

func processMessage(message *hipchat.Message) {
	if len(message.Body) < 1 {
		/* If a user initiates a 1:1 dialog
		 * with the bot, the hipchat client will send a ''
		 * ping even if they try to close the
		 * dialog.  If there is no data, we
		 * have no business replying or doing
		 * much of anything, so let's just
		 * return. */
		return
	}

	r := getRecipientFromMessage(message.From)
	if r.Name == CONFIG["fullName"] {
		//verbose("Ignoring message from myself.", 5)
		return
	}

	updateSeen(r, message.Body)

	if strings.HasPrefix(message.Body, "<invite from") {
		processInvite(r, message.Body)
		return
	}

	command_re := regexp.MustCompile(fmt.Sprintf("^(?i)(!|[@/]%s [/!]?)", CONFIG["mentionName"]))
	if command_re.MatchString(message.Body) {
		matchEnd := command_re.FindStringIndex(message.Body)[1]
		go processCommands(r, message.Body[0:matchEnd], message.Body[matchEnd:])
	} else {
		processChatter(r, message.Body, false)
	}
}

func randomLineFromUrl(theUrl string, useBy bool) (line string) {
	rand.Seed(time.Now().UnixNano())
	data := getURLContents(theUrl, useBy)
	lines := strings.Split(string(data), "\n")
	line = lines[rand.Intn(len(lines))]
	return
}

func readSavedData() {
	verbose(fmt.Sprintf("Reading saved data from: %s", CONFIG["channelsFile"]), 2)
	if _, err := os.Stat(CONFIG["channelsFile"]); err != nil {
		return
	}

	b, err := ioutil.ReadFile(CONFIG["channelsFile"])
	if err != nil {
		fail(fmt.Sprintf("Error %s: %q\n", CONFIG["channelsFile"], err))
	}

	buf := bytes.Buffer{}
	buf.Write(b)

	d := gob.NewDecoder(&buf)
	if err := d.Decode(&CHANNELS); err != nil {
		fail(fmt.Sprintf("Unable to decode data: %s\n", err))
	}
}

func reply(r Recipient, msg string) {
	if _, found := CHANNELS[r.ReplyTo]; found {
		HIPCHAT_CLIENT.Say(r.Jid, CONFIG["fullName"], msg)
	} else {
		HIPCHAT_CLIENT.PrivSay(r.Jid, CONFIG["fullName"], msg)
	}

}

func runCommand(cmd ...string) (out []byte, rval int) {
	var argv []string

	if len(cmd) == 0 {
		return
	}

	if len(cmd) == 1 {
		argv = strings.Split(cmd[0], " ")
	} else {
		argv = cmd
	}
	command := exec.Command(argv[0], argv[1:]...)

	rval = 0
	verbose(fmt.Sprintf("Exec'ing '%s'...", argv), 3)

	go func() {
		time.Sleep(30 * time.Second)
		if command.Process != nil {
			response := fmt.Sprintf("Sorry, I had to kill your '%s' command.\n", cmd)
			if err := command.Process.Kill(); err != nil {
				response += fmt.Sprintf("Unable to kill your process: %s", err)
			}
			out = []byte(response)
		}
	}()

	tmp, err := command.CombinedOutput()
	if err != nil {
		rval = 1
		if len(out) < 1 && len(tmp) < 1 {
			out = []byte(fmt.Sprintf("%s", err))
		}
	}
	command.Wait()

	if len(out) < 1 {
		out = tmp
	}
	return
}

func serializeData() {
	verbose("Serializing data...", 1)

	gob.Register(Channel{})
	b := bytes.Buffer{}
	e := gob.NewEncoder(&b)
	if err := e.Encode(CHANNELS); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to encode channels: %s\n", err)
		return
	}

	err := ioutil.WriteFile(CONFIG["channelsFile"], b.Bytes(), 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to write data to '%s': %s\n",
			CONFIG["channelsFile"], err)
		return
	}
}

func updateRooms(rooms []*hipchat.Room) {
	for _, room := range rooms {
		ROOMS[room.Id] = room
	}
}

func updateRoster(users []*hipchat.User) {
	for _, user := range users {
		uid := strings.Split(user.Id, "@")[0]
		ROSTER[uid] = user
	}
}

func updateSeen(r Recipient, msg string) {
	if len(r.Name) == 0 {
		/* Not a chat message. */
		return
	}

	curses_re := regexp.MustCompile(`(sh[ia]t|motherfucker|piss|f+u+c+k+|cunt|cocksucker|tits)`)
	curses_match := curses_re.FindAllString(msg, -1)

	/* We don't keep track of priv messages, only public groupchat. */
	if ch, chfound := CHANNELS[r.ReplyTo]; chfound {
		var u *hipchat.User
		var uInfo UserInfo
		for _, u = range ROSTER {
			if u.Name == r.Name {
				break
			}
		}
		if u == nil {
			return
		}

		uInfo.Seen = fmt.Sprintf(time.Now().Format(time.UnixDate))

		for _, curse := range curses_match {
			CURSES[curse] = CURSES[curse] + 1
		}

		if t, found := ch.Users[*u]; found {
			count := len(strings.Split(msg, "\n"))
			if count > 1 {
				count -= 1
			}
			uInfo.Curses = t.Curses + len(curses_match)
			uInfo.Count = t.Count + count

			/* Need to remember other counters here,
			 * lest they be reset. */
			uInfo.Praise = t.Praise
		} else {
			uInfo.Count = 1
			uInfo.Curses = 0
		}
		ch.Users[*u] = uInfo
	}
}

func usage(out io.Writer) {
	usage := `Usage: %v [-Vhv] [-c configFile]
	-V             print version information and exit
	-c configFile  read configuration from configFile
	-h             print this help and exit
	-v             be verbose
`
	fmt.Fprintf(out, usage, PROGNAME)
}

func verbose(msg string, level int) {
	if level <= VERBOSITY {
		fmt.Fprintf(os.Stderr, "%s ", time.Now().Format("2006-01-02 15:04:05"))
		for i := 0; i < level; i++ {
			fmt.Fprintf(os.Stderr, "=")
		}
		fmt.Fprintf(os.Stderr, "> %v\n", msg)
	}
}

func wasInsult(msg string) (result bool) {
	result = false

	var insultPatterns = []*regexp.Regexp{
		regexp.MustCompile(fmt.Sprintf("(?i)fu[, ]@?%s", CONFIG["mentionName"])),
		regexp.MustCompile("(?i)dam+n? (yo)?u"),
		regexp.MustCompile("(?i)shut ?(the fuck )?up"),
		regexp.MustCompile("(?i)(screw|fuck) (yo)u"),
		regexp.MustCompile("(?i)(piss|bugger) ?off"),
		regexp.MustCompile("(?i)fuck (off|(yo)u)"),
		regexp.MustCompile("(?i)(yo)?u (suck|blow|are (useless|lame|dumb|stupid|stink))"),
		regexp.MustCompile("(?i)(stfu|go to hell)"),
		regexp.MustCompile(fmt.Sprintf("(?i)(stupid|annoying|lame|boring|useless) +(%s|bot)", CONFIG["mentionName"])),
		regexp.MustCompile(fmt.Sprintf("(?i)(blame )?(%s|the bot)('?s fault)", CONFIG["mentionName"])),
	}

	for _, p := range insultPatterns {
		if p.MatchString(msg) {
			return true
		}
	}

	return
}

/*
 * Main
 */

func main() {

	if err := os.Setenv("PATH", "/bin:/usr/bin:/sbin:/usr/sbin:/usr/local/bin"); err != nil {
		fail(fmt.Sprintf("Unable to set PATH: %s\n", err))
	}

	getopts()
	parseConfig()
	createCommands()
	readSavedData()

	defer serializeData()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGQUIT, syscall.SIGTERM)
	go func() {
		<-c
		serializeData()
		os.Exit(EXIT_FAILURE)
	}()

	doTheHipChat()
}
