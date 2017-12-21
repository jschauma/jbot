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
 *
 * For HipChat:
 *   hcPassword    = the HipChat password of the bot user
 *     OR
 *   hcOauthToken  = the HipChat Oauth token for the bot user
 *   hcService     = the HipChat company prefix, e.g. <foo>.hipchat.com
 *   hcJabberID    = the HipChat / JabberID of the bot user
 *   fullName      = how the bot presents itself
 *   mentionName   = to which name the bot responds to
 *
 * For Slack:
 *   slackService  = the Slack service name, e.g. <foo>.slack.com
 *   slackToken    = the authentication token for your bot
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
	"github.com/nlopes/slack"
)

const DEFAULT_THROTTLE = 1800
const PERIODICS = 120

const EXIT_FAILURE = 1
const EXIT_SUCCESS = 0

const PROGNAME = "jbot"
const VERSION = "3.0"

var CONFIG = map[string]string{
	"byUser":           "",
	"byPassword":       "",
	"channelsFile":     "/var/tmp/jbot.channels",
	"configFile":       "/etc/jbot.conf",
	"debug":            "no",
	"fullName":         "",
	"hcControlChannel": "",
	"hcJabberID":       "",
	"hcOauthToken":     "",
	"hcPassword":       "",
	"hcService":        "",
	"mentionName":      "",
	"opsgenieApiKey":   "",
	"slackID":          "",
	"slackService":     "",
	"slackToken":       "",
}

var HIPCHAT_CLIENT *hipchat.Client
var HIPCHAT_ROOMS = map[string]*hipchat.Room{}
var HIPCHAT_ROSTER = map[string]*hipchat.User{}

var SLACK_CLIENT *slack.Client
var SLACK_RTM *slack.RTM
var SLACK_ROSTER = map[string]slack.User{}

var CHANNELS = map[string]*Channel{}
var CURSES = map[string]int{}
var COMMANDS = map[string]*Command{}

var TOGGLES = map[string]bool{
	"chatter":     true,
	"python":      true,
	"trivia":      true,
	"shakespeare": true,
}

var URLS = map[string]string{
	"eliza":       "https://XXX-SOME-LINK-WITH-WITTY-REPLIES-HERE-XXX",
	"insults":     "https://XXX-SOME-LINK-WITH-VARIOUS-INUSLTS-HERE-XXX",
	"jbot":        "https://github.com/jschauma/jbot/",
	"jira":        "https://XXX-YOUR-JIRA-DOMAIN-HERE-XXX",
	"praise":      "https://XXX-YOUR-PRAISE-URL-HERE-XXX/",
	"shakespeare": "https://XXX-SOME-LINK-WITH-SHAKESPEARE-QUOTES-HERE",
	"speb":        "https://XXX-SOME-LINK-WITH-ALL-SPEB-REPLIES-HERE-XXX",
	"trivia":      "https://XXX-SOME-LINK-WITH-VARIOUS-TRIVIA-SNIPPETS-HERE-XXX",
	"yql":         "https://query.yahooapis.com/v1/public/yql",
}

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
	Inviter      string
	Id           string
	Name         string
	Toggles      map[string]bool
	Throttles    map[string]time.Time
	Type         string
	HipChatUsers map[hipchat.User]UserInfo
	SlackUsers   map[string]UserInfo
	Settings     map[string]string
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
	Count  int
	Curses int
	Id     string
	Seen   string
	Praise int
}

type ElizaResponse struct {
	Re        *regexp.Regexp
	Responses []string
}

/*
 * ChatType    = hipchat|slack
 * Id          = 12345_98765@conf.hipchat.com | C62HJV9F0
 * MentionName = JohnDoe
 * Name        = John Doe
 * ReplyTo     = 98765 | U3GNF8QGJ
 *
 * To handle both HipChat and Slack, we overload the
 * fields a bit: for Slack, "ReplyTo" is the channel.
 */
type Recipient struct {
	ChatType    string
	Id          string
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

func cmdBacon(r Recipient, chName, args string) (result string) {
	pic := false
	query := "bacon"
	if len(args) > 0 {
		query += " " + args
		pic = true
	}

	rand.Seed(time.Now().UnixNano())
	if pic || rand.Intn(4) == 0 {
		result = cmdImage(r, chName, query)
	} else {
		data := getURLContents("http://baconipsum.com/?paras=1&type=all-meat", false)
		bacon_re := regexp.MustCompile(`anyipsum-output">(.*?\.)`)
		for _, line := range strings.Split(string(data), "\n") {
			if m := bacon_re.FindStringSubmatch(line); len(m) > 0 {
				result = dehtmlify(m[1])
				break
			}
		}
	}

	if len(result) < 1 {
		result = "Ugh, I'm afraid I'm all out of bacon right now."
	}

	return
}

func cmdBeer(r Recipient, chName, args string) (result string) {
	bType := "search"
	theUrl := fmt.Sprintf("%ssearch/?qt=beer&q=", COMMANDS["beer"].How)
	if len(args) < 1 {
		bType = "top"
		theUrl = fmt.Sprintf("%slists/top/", COMMANDS["beer"].How)
	}

	if args == "me" {
		args = r.MentionName
	}

	theUrl += url.QueryEscape(args)
	data := getURLContents(theUrl, false)

	type Beer struct {
		Abv      string
		BeerType string
		Brewery  string
		Name     string
		Rating   string
		Url      string
	}

	var beer Beer

	beer_re := regexp.MustCompile(`<ul><li><a href="/(beer/profile/[0-9]+/[0-9]+/)"><b>([^<]+)</b></a><br><a href="/beer/profile/[0-9]+/">([^<]+)</a>`)
	top_re := regexp.MustCompile(`<a href="/(beer/profile/[0-9]+/[0-9]+/)"><b>([^<]+)</b></a><div.*<a href="/beer/profile/[0-9]+/">([^<]+)</a><br><a href="/beer/style/[0-9]+/">([^<]+)</a> / ([^<]+) ABV</div></td><td[^>]+><b>([0-9.]+)</span>`)

	for _, line := range strings.Split(string(data), "\n") {
		if bType == "search" {
			if m := beer_re.FindStringSubmatch(line); len(m) > 0 {
				beer = Beer{"", "", m[3], m[2], "", m[1]}
				theUrl = fmt.Sprintf("%s%s", COMMANDS["beer"].How, m[1])
				data := getURLContents(theUrl, false)
				style_re := regexp.MustCompile(`<b>Style:</b> <a href=.*><b>(.*)</b></a>`)
				abv_re := regexp.MustCompile(`<b>Alcohol by volume \(ABV\):</b> (.*)`)
				next := false
				for _, l2 := range strings.Split(string(data), "\n") {
					if strings.Contains(l2, "<dt>Avg:</dt>") {
						next = true
						continue
					}
					if next {
						beer.Rating = dehtmlify(l2)
						next = false
					}
					if m := abv_re.FindStringSubmatch(l2); len(m) > 0 {
						beer.Abv = m[1]
					}
					if m := style_re.FindStringSubmatch(l2); len(m) > 0 {
						beer.BeerType = m[1]
					}
				}
				break
			}
		} else {
			if strings.HasPrefix(line, "<tr><td align=") {
				beers := []Beer{}
				for _, l2 := range strings.Split(line, "</tr>") {
					if m := top_re.FindStringSubmatch(l2); len(m) > 0 {
						b := Beer{m[5], m[4], m[3], m[2], m[6], m[1]}
						beers = append(beers, b)
					}
				}
				if len(beers) > 0 {
					rand.Seed(time.Now().UnixNano())
					beer = beers[rand.Intn(len(beers))]
				}
			}
		}
	}

	if len(beer.Name) > 0 {
		result = fmt.Sprintf("%s by %s - %s\n", beer.Name, beer.Brewery, beer.Rating)
		result += fmt.Sprintf("%s (%s)\n", beer.BeerType, beer.Abv)
		result += fmt.Sprintf("%s%s\n", COMMANDS["beer"].How, beer.Url)
	} else {
		result = fmt.Sprintf("No beer found for '%s'.", args)
	}

	return
}

func cmdChannels(r Recipient, chName, args string) (result string) {
	var hipChatChannels []string
	var slackChannels []string

	if len(CHANNELS) == 0 {
		result = "I'm not currently in any channels."
	} else if len(CHANNELS) == 1 {
		result = "I'm only here right now: "
	}

	for ch, chInfo := range CHANNELS {
		if chInfo.Type == "hipchat" {
			hipChatChannels = append(hipChatChannels, ch)
		} else if chInfo.Type == "slack" {
			slackChannels = append(slackChannels, ch)
		}
	}
	sort.Strings(hipChatChannels)
	sort.Strings(slackChannels)
	if len(hipChatChannels) > 0 {
		result = fmt.Sprintf("I'm in the following %d HipChat channels:\n", len(hipChatChannels))
		result += strings.Join(hipChatChannels, ", ") + "\n"
	}
	if len(slackChannels) > 0 {
		result += fmt.Sprintf("I'm in the following %d Slack channels:\n", len(slackChannels))
		result += strings.Join(slackChannels, ", ")
	}
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
		var curseCount int
		for ch := range CHANNELS {
			if r.ChatType == "hipchat" {
				for u, info := range CHANNELS[ch].HipChatUsers {
					if wanted == "*" {
						allUsers[u.MentionName] = info.Curses
					} else if u.MentionName == wanted {
						curseCount = info.Curses
						break
					}
				}
			} else if r.ChatType == "slack" {
				for u, info := range CHANNELS[ch].SlackUsers {
					if wanted == "*" {
						allUsers[u] = info.Curses
					} else if u == wanted {
						curseCount = info.Curses
						break
					}
				}

			}
		}

		if curseCount > 0 {
			result = fmt.Sprintf("%d", curseCount)
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

	fml_re := regexp.MustCompile(`(?i)^(Today, .*FML)$`)
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
			"If you find me annoyingly chatty, just '!toggle chatter'.\n",
			len(COMMANDS))
		if r.ChatType != "slack" {
			result += "To ask me to leave a channel, say '!leave'.\n"
		}
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
		result = URLS["jbot"]
	} else {
		rand.Seed(time.Now().UnixNano())
		result = DONTKNOW[rand.Intn(len(DONTKNOW))]
	}

	return
}

func cmdImage(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["img"].Usage
		return
	}

	theUrl := fmt.Sprintf("%s%s", COMMANDS["img"].How, url.QueryEscape(args))
	data := getURLContents(theUrl, false)

	imgurl_re := regexp.MustCompile(`imgurl=(.*?)&`)
	for _, line := range strings.Split(string(data), "\n") {
		m := imgurl_re.FindAllStringSubmatch(line, -1)
		if len(m) > 0 {
			rand.Seed(time.Now().UnixNano())
			onePic := m[rand.Intn(len(m))]
			url, _ := url.QueryUnescape(onePic[1])
			result = "http://" + url
		}
	}

	return
}

func cmdInfo(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		args = r.ReplyTo
	} else {
		args = strings.ToLower(args)
	}

	if ch, found := getChannel(r.ChatType, args); found {
		result = fmt.Sprintf("I was invited into #%s by %s.\n", ch.Name, ch.Inviter)
		result += fmt.Sprintf("These are the users I've seen in #%s:\n", ch.Name)

		var names []string

		if r.ChatType == "hipchat" {
			for u := range ch.HipChatUsers {
				names = append(names, u.MentionName)
			}
		} else if r.ChatType == "slack" {
			for u := range ch.SlackUsers {
				names = append(names, u)
			}
		}
		sort.Strings(names)
		result += strings.Join(names, ", ")

		stfu := cmdStfu(r, chName, "")
		if len(stfu) > 0 {
			result += fmt.Sprintf("\nTop 10 channel chatterers for #%s:\n", ch.Name)
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
		 (strings.ToLower(args) == "@" + strings.ToLower(CONFIG["mentionName"])) ||
			(args == "yourself") ||
			(args == "me")) {
		result = fmt.Sprintf("@%s: ", r.MentionName)
	}

	if (len(result) < 1) && (len(args) > 0) {
		result = fmt.Sprintf("%s: ", args)
	}

	rand.Seed(time.Now().UnixNano())
	if rand.Intn(2) == 0 {
		url := URLS["insults"]
		result += randomLineFromUrl(url, false)
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
		fmt.Fprintf(os.Stderr, "+++ jira fail for %s: %v\n", ticket, jiraJson)
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

	resolved := fields.(map[string]interface{})["resolutiondate"]
	if resolved != nil {
		result += fmt.Sprintf("Resolved: %s\n", resolved)
	}

	assignee := fields.(map[string]interface{})["assignee"]
	if assignee != nil {
		name := assignee.(map[string]interface{})["name"]
		result += fmt.Sprintf("Assignee: %s\n", name)
	}

	result += fmt.Sprintf("Reporter: %s\n", reporter)
	result += fmt.Sprintf("Link    : %s/browse/%s", URLS["jira"], ticket)
	return
}

func cmdLog(r Recipient, chName, args string) (result string) {
	var room string
	if r.ChatType == "hipchat" {
		room = r.ReplyTo
	} else if r.ChatType == "slack" {
		room = chName
	}
	if len(args) > 1 {
		room = args
	}

	roomInfo := cmdRoom(r, chName, room)

	if strings.Contains(roomInfo, "https://") {
		result = roomInfo[strings.Index(roomInfo, "https://"):]
	} else {
		result = fmt.Sprintf("No log URL found for '%s'.", r.ReplyTo)
	}
	return
}

func cmdMonkeyStab(r Recipient, chName, args string) (result string) {
	if len(args) < 1 || strings.EqualFold(args, CONFIG["mentionName"]) {
		args = r.MentionName
	}

	result = fmt.Sprintf("/me unleashes a troop of pen-wielding stabbing-monkeys on %s!\n", args)
	return
}

func cmdOncall(r Recipient, chName, args string) (result string) {
	oncall := args
	oncall_source := "user input"
	if len(strings.Fields(oncall)) < 1 {
		if ch, found := getChannel(r.ChatType, r.ReplyTo); found {
			if r.ChatType == "hipchat" {
				oncall = r.ReplyTo
			} else {
				oncall = ch.Name
			}
			oncall_source = "channel name"
			if v, found := ch.Settings["oncall"]; found {
				oncall = v
				oncall_source = "channel setting"
			}
		} else {
			result = "Usage: " + COMMANDS["oncall"].Usage
			return
		}
	}

	result += cmdOncallOpsGenie(r, chName, oncall, true)
	if len(result) < 1 {
		result = fmt.Sprintf("No oncall information found for '%s'.\n", oncall)
	}

	if strings.HasPrefix(result, "No OpsGenie schedule found for") {
		switch oncall_source {
		case "channel name":
			result += fmt.Sprintf("\nIf your oncall rotation does not match your channel name (%s), use '!set oncall=<rotation_name>'.\n", chName)
		case "channel setting":
			result += fmt.Sprintf("\nIs your 'oncall' channel setting (%s) correct?\n", oncall)
			result += "If not, use '!set oncall=<rotation_name>' to fix that.\n"
		}

	}
	return
}

func cmdOncallOpsGenie(r Recipient, chName, args string, allowRecursion bool) (result string) {

	key := CONFIG["opsgenieApiKey"]
	if len(key) < 1 {
		result = "Unable to query OpsGenie -- no API key in config file."
		return
	}

	if strings.HasSuffix(args, "_schedule") {
		args = args[0:strings.Index(args, "_schedule")]
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
			if len(candidates) == 1 &&
				strings.EqualFold(args, candidates[0]) &&
				allowRecursion {
				return cmdOncallOpsGenie(r, chName, candidates[0], false)
			}
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
		if len(oncall[rname]) < 1 {
			oncall[rname] = make([]string, 0)
		}
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
			end := time.Unix(endTime/1000, endTime%1000)
			start := time.Unix(startTime/1000, startTime%1000)
			if (time.Since(end) > 0) || time.Since(start) < 0 {
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
		if strings.Contains(hosts[0], ".") {
			result = fmt.Sprintf("Unable to resolve %s.", hosts[0])
		} else {
			replies := []string{
				fmt.Sprintf("YO, @%s, WAKE UP!", hosts[0]),
				fmt.Sprintf("@%s, somebody needs you!", hosts[0]),
				fmt.Sprintf("ECHO REQUEST -> @%s", hosts[0]),
				fmt.Sprintf("You there, @%s?", hosts[0]),
				fmt.Sprintf("Hey, @%s, @%s is looking for you.", hosts[0], r.MentionName),
				fmt.Sprintf("/me nudges %s.", hosts[0]),
				fmt.Sprintf("/me pings %s.", hosts[0]),
				fmt.Sprintf("/me pokes %s.", hosts[0]),
				fmt.Sprintf("/me taps %s on the head.", hosts[0]),
			}
			result = replies[rand.Intn(len(replies))]
		}
		return
	}

	_, err := runCommand(fmt.Sprintf("ping -q -w 1 -W 0.5 -i 0.5 -c 1 %s", host))
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
		if r.ChatType == "hipchat" {
			for u := range ch.HipChatUsers {
				if ch.HipChatUsers[u].Praise > 0 {
					heroes[ch.HipChatUsers[u].Praise] = append(heroes[ch.HipChatUsers[u].Praise], u.MentionName)
				}
			}
		} else if r.ChatType == "slack" {
			for u := range ch.SlackUsers {
				if ch.SlackUsers[u].Praise > 0 {
					heroes[ch.SlackUsers[u].Praise] = append(heroes[ch.SlackUsers[u].Praise], u)
				}
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
			strings.EqualFold(args, r.MentionName) {
			result = cmdInsult(r, chName, "me")
			return
		}

		if r.ChatType == "hipchat" {
			for _, u := range HIPCHAT_ROSTER {
				uid := strings.SplitN(strings.Split(u.Id, "@")[0], "_", 2)[1]
				email := strings.Split(u.Email, "@")[0]
				if strings.EqualFold(u.Name, args) ||
					strings.EqualFold(email, args) ||
					strings.EqualFold(u.MentionName, args) ||
					strings.EqualFold(uid, args) {
					uInfo := ch.HipChatUsers[*u]
					uInfo.Praise++
					ch.HipChatUsers[*u] = uInfo
				}
			}
		} else if r.ChatType == "slack" {
		}

		if strings.EqualFold(args, CONFIG["mentionName"]) {
			rand.Seed(time.Now().UnixNano())
			result = THANKYOU[rand.Intn(len(THANKYOU))]
		} else {
			result = fmt.Sprintf("%s: %s\n", args,
				randomLineFromUrl(COMMANDS["praise"].How, false))
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
	lroom := strings.ToLower(room)

	type roomTopic struct {
		Name  string
		Topic string
	}

	var candidates []*roomTopic

	if r.ChatType == "hipchat" {
		for _, aRoom := range HIPCHAT_ROOMS {
			lc := strings.ToLower(aRoom.Name)

			if lc == lroom || aRoom.RoomId == room {
				result = fmt.Sprintf("'%s' (%s)\n", aRoom.Name, aRoom.Privacy)
				result += fmt.Sprintf("Topic: %s\n", aRoom.Topic)

				owner := strings.Split(aRoom.Owner, "@")[0]
				if u, found := HIPCHAT_ROSTER[owner]; found {
					result += fmt.Sprintf("Owner: %s\n", u.MentionName)
				}

				if aRoom.LastActive != "" {
					result += fmt.Sprintf("Last Active: %s\n", aRoom.LastActive)
				}

				if aRoom.NumParticipants != "0" {
					result += fmt.Sprintf("Hip Chatters: %s\n", aRoom.NumParticipants)
				}
				result += fmt.Sprintf("https://%s.hipchat.com/history/room/%s\n", CONFIG["hcService"], aRoom.RoomId)
				return
			} else {
				if strings.Contains(lc, lroom) {
					candidates = append(candidates, &roomTopic{ aRoom.Name, aRoom.Topic})
				}
			}
		}
	} else if r.ChatType == "slack" {
		for _, ch := range SLACK_RTM.GetInfo().Channels {
			lc := strings.ToLower(ch.Name)
			if lc == lroom {
				result = fmt.Sprintf("'%s'\n", ch.Name)
				if len(ch.Topic.Value) > 0 {
					result += fmt.Sprintf("Topic: %s\n", ch.Topic.Value)
				}
				if len(ch.Purpose.Value) > 0 {
					result += fmt.Sprintf("Purpose: %s\n", ch.Purpose.Value)
				}
				creator, err := SLACK_CLIENT.GetUserInfo(ch.Creator)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Unable to find user information for '%s'.\n", ch.Creator)
					result += fmt.Sprintf("Creator: Unknown\n")
				} else {
					result += fmt.Sprintf("Creator: %s\n", creator.Name)
				}
				result += fmt.Sprintf("# of members: %d\n", len(ch.Members))
				result += fmt.Sprintf("https://%s/messages/%s/\n", CONFIG["slackService"], lroom)
				return
			} else if strings.Contains(lc, lroom) {
				candidates = append(candidates, &roomTopic{ ch.Name, ch.Topic.Value})
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
		if r.ChatType == "hipchat" {
			HIPCHAT_CLIENT.RequestRooms()
		}
		result = "No such room: " + room
	}

	return
}

func cmdSeen(r Recipient, chName, args string) (result string) {
	wanted := strings.Split(args, " ")
	user := wanted[0]
	verbose(4, "Looking in %s", r.ReplyTo)

	ch, found := getChannel(r.ChatType, r.ReplyTo)

	if len(wanted) > 1 {
		verbose(4, "Looking for %s in %s'...", user, wanted[1])
		ch, found = getChannel(r.ChatType, wanted[1])
	}

	if strings.EqualFold(args, CONFIG["mentionName"]) {
		rand.Seed(time.Now().UnixNano())
		replies := []string{
			"You can't see me, I'm not really here.",
			"/me is invisible.",
			"/me looked, but only saw its shadow.",
			"Wed Dec 31 19:00:00 EST 1969",
			}
		result = replies[rand.Intn(len(replies))]
		return
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

	for u, info := range ch.HipChatUsers {
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

	result = randomLineFromUrl(COMMANDS["speb"].How, false)
	return
}

func cmdStfu(r Recipient, chName, args string) (result string) {
	var ch *Channel
	var found bool

	if ch, found = CHANNELS[chName]; !found {
		result = "This command only works in a channel."
		return
	}

	chatter := make(map[int][]string)

	if r.ChatType == "hipchat" {
		for u := range ch.HipChatUsers {
			if (len(args) > 0) && (u.MentionName != args) {
				continue
			}
			chatter[ch.HipChatUsers[u].Count] = append(chatter[ch.HipChatUsers[u].Count], u.MentionName)
		}
	} else if r.ChatType == "slack" {
		for u := range ch.SlackUsers {
			if (len(args) > 0) && (u != args) {
				continue
			}
			chatter[ch.SlackUsers[u].Count] = append(chatter[ch.SlackUsers[u].Count], u)
		}
	}

	if (len(args) > 0) && (len(chatter) < 1) {
		result = fmt.Sprintf("%s hasn't said anything in %s.",
			args, chName)
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

func cmdTime(r Recipient, chName, args string) (result string) {
	timezones := []string{"UTC", "EST5EDT", "PST8PDT"}
	if len(args) > 0 {
		timezones = []string{args}
	}

	for _, l := range timezones {
		if loc, err := time.LoadLocation(l); err == nil {
			result += fmt.Sprintf("%s\n", time.Now().In(loc).Format(time.UnixDate))
		} else if loc, err := time.LoadLocation(strings.ToUpper(l)); err == nil {
			result += fmt.Sprintf("%s\n", time.Now().In(loc).Format(time.UnixDate))
		} else {
			var tz string
			var found bool

			address := getUserAddress(l)
			if len(address) > 0 {
				tz, found = locationToTZ(address)
			} else {
				tz, found = getColoTZ(l)
			}
			if !found {
				tz, _ = locationToTZ(l)
			}

			if loc, err := time.LoadLocation(tz); err == nil {
				result += fmt.Sprintf("%s\n", time.Now().In(loc).Format(time.UnixDate))
			} else {
				result = fmt.Sprintf("Can't determine a valid timezone for '%s'.", l)
			}
			return
		}
	}

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
			result = fmt.Sprintf("Organization: %s\n", info["organisation"])
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
				if len(ch.Toggles) == 0 {
					ch.Toggles = map[string]bool{}
				}
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

	result = randomLineFromUrl(COMMANDS["trivia"].How, false)
	return
}

func cmdTroutSlap(r Recipient, chName, args string) (result string) {
	if len(args) < 1 || strings.EqualFold(args, CONFIG["mentionName"]) {
		args = r.MentionName
	}

	result = fmt.Sprintf("/me pulls out a foul-smelling trout and slaps %s across the face.\n", args)
	return
}

func cmdUd(r Recipient, chName, args string) (result string) {

	theUrl := COMMANDS["ud"].How
	if len(args) > 0 {
		theUrl += fmt.Sprintf("define?term=%s", url.QueryEscape(args))
	} else {
		theUrl += "random"
	}

	data := getURLContents(theUrl, false)

	var jsonData map[string]interface{}
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal urban dictionary json: %s\n", err)
		return
	}

	rtype := jsonData["result_type"]
	if rtype == "no_results" {
		result = fmt.Sprintf("Sorry, Urban Dictionary is useless when it comes to %s.", args)
	} else if rtype == "exact" || len(args) == 0 {
		entry := jsonData["list"].([]interface{})[0]

		result = fmt.Sprintf("%s\n%s\nExample: %s",
				entry.(map[string]interface{})["word"],
				entry.(map[string]interface{})["definition"],
				entry.(map[string]interface{})["example"])
	} else {
		result = fmt.Sprintf("Unexpected result type: %s", rtype)
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

func cmdUnthrottle(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["unthrottle"].Usage
		return
	}

	var ch *Channel
	var found bool
	if ch, found = CHANNELS[chName]; !found {
		result = "I can only throttle things in a channel."
		return
	}

	if args == "*" || args == "everything" {
		for t, _ := range ch.Throttles {
			delete(ch.Throttles, t)
		}
	} else {
		delete(ch.Throttles, args)
	}

	replies := []string{
			"Okiley, dokiley!",
			"Sure thing, my friend!",
			"Done.",
			"No problemo.",
			"/me throttles that thang.",
			"Got it.",
			"Word.",
			"Unthrottled to the max!",
			"Consider it done.",
			}
	result = replies[rand.Intn(len(replies))]
	return
}

func cmdUser(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["user"].Usage
		return
	}

	user := strings.TrimSpace(args)
	candidates := []*hipchat.User{}

	for _, u := range HIPCHAT_ROSTER {
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

	address := getUserAddress(args)
	if len(address) > 0 {
		args = address
	} else {
		var unused Recipient
		coloInfo := cmdColo(unused, "", args)
		r := regexp.MustCompile(`(?m)Location\s+: (.+)`)
		if m := r.FindStringSubmatch(coloInfo); len(m) > 0 {
			args = m[1]
		}
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

func cmdWhocyberedme(r Recipient, chName, args string) (result string) {
	if len(args) > 1 {
		result = "Usage: " + COMMANDS["whocyberedme"].Usage
		return
	}

	data := getURLContents(COMMANDS["whocyberedme"].How, false)

	for _, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, "confirms") {
			result = dehtmlify(l)
			break
		}
	}
	return
}

func cmdWhois(r Recipient, chName, args string) (result string) {
	if len(strings.Fields(args)) != 1 {
		result = "Usage: " + COMMANDS["whois"].Usage
		return
	}

	hostinfo := cmdHost(r, chName, args)
	if strings.Contains(hostinfo, "not found:") {
		result = hostinfo
		return
	}

	out, _ := runCommand(fmt.Sprintf("whois %s", args))
	data := string(out)


	/* whois formatting is a mess; different whois servers return
	 * all sorts of different information in all sorts of different
	 * formats. We'll try to catch some common ones here. :-/ */

	var format string
	found := false

	formatGuesses := map[*regexp.Regexp]string {
		regexp.MustCompile("(?i)Registry Domain ID:")               : "common",
		regexp.MustCompile("(?i)%% This is the AFNIC Whois server."): "afnic",
		regexp.MustCompile("(?i)% Copyright.* by DENIC")            : "denic",
		regexp.MustCompile("(?i)The EDUCAUSE Whois database")       : "educause",
		regexp.MustCompile("(?i)for .uk domain names")              : "uk",
	}

	for p, f := range formatGuesses {
		if p.MatchString(data) {
			format = f
			found = true
		}
	}

	info := map[string]string{}
	var wanted []string
	var field string
	next := false

	for _, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, "No match for domain") {
			result = l
			return
		}

		if strings.HasPrefix(l, "%") || strings.HasPrefix(l, "#") {
			continue
		}

		if found {
			keyval := strings.SplitN(l, ":", 2)
			k := strings.TrimSpace(keyval[0])
			if len(keyval) > 1 {
				v := strings.TrimSpace(keyval[1])
				if _, exists := info[k]; exists {
					info[k] += ", " + v
				} else {
					info[k] = v
				}
			}

			if format == "common" {
				wanted = []string{
					"Registrar",
					"Registrar URL",
					"Updated Date",
					"Creation Date",
					"Registry Expiry Date",
					"Registrant Name",
					"Registrant Organization",
					"Registrant Country",
					"Registrant Email",
					"Name Server",
					"DNSSEC",
				}
			} else if format == "afnic" {
				if strings.HasPrefix(l, "nic-hdl:") {
					break
				}
				wanted = []string{
					"registrar",
					"country",
					"Expiry Date",
					"created",
					"last-update",
					"nserver",
					"e-mail",
				}
			} else if format == "denic" {
				wanted = []string{
					"Nserver",
					"Changed",
					"Organisation",
					"CountryCode",
					"Email",
				}
				if strings.HasPrefix(l, "[Zone-C]") {
					break
				}
			} else if format == "educause" {
				wanted = []string{
					"Registrant",
					"Email",
					"Name Servers",
					"Domain record activated",
					"Domain record last updated",
					"Domain expires",
				}
				if strings.HasPrefix(l, "Registrant:") {
					field = "Registrant"
					next = true
					continue
				}

				if strings.Contains(l, "@") {
					info["Email"] = strings.TrimSpace(l)
					continue
				}

				if strings.HasPrefix(l, "Name Servers") {
					field = "Name Servers"
					next = true
					continue
				}

				if next {
					if field == "Name Servers" {
						if s, exists := info[field]; exists {
							if len(s) > 1 {
								info[field] += "\n" + strings.TrimSpace(l)
							} else {
								info[field] = strings.TrimSpace(l)
							}
						} else {
							info[field] = strings.TrimSpace(l)
						}
					} else {
						info[field] = strings.TrimSpace(l)
						next = false
					}
					if len(l) < 1 {
						next = false
					}
				}
			} else if format == "uk" {
				wanted = []string{
					"Registrant",
					"Regsitrar",
					"Registered on",
					"Expiry date",
					"Last updated",
					"Name servers",
				}
				if strings.Contains(l, "Registrant:") {
					field = "Registrant"
					next = true
					continue
				}
				if strings.Contains(l, "Registrar:") {
					field = "Registrar"
					next = true
					continue
				}
				if strings.Contains(l, "Name servers:") {
					field = "Name servers"
					next = true
					continue
				}

				if next {
					if strings.Contains(l, "WHOIS lookup made") {
						break
					}
					if field == "Name servers" {
						if s, exists := info[field]; exists {
							if len(s) > 1 {
								info[field] += "\n" + strings.TrimSpace(l)
							} else {
								info[field] = strings.TrimSpace(l)
							}
						} else {
							info[field] = strings.TrimSpace(l)
						}
					} else {
						info[field] = strings.TrimSpace(l)
						next = false
					}
				}
			}
		}
	}

	if len(info) > 0 {
		for _, f := range wanted {
			if v, exists := info[f]; exists {
				result += fmt.Sprintf("%s: %s\n", f, v)
			}
		}
	}
	return
}

func cmdWiki(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["wiki"].Usage
		return
	}

	query := url.QueryEscape(args)
	theUrl := fmt.Sprintf("%s%s", COMMANDS["wiki"].How, query)
	data := getURLContents(theUrl, false)

	/* json results are:
	 * [ "query",
	 *   ["terms", ...],
	 *   ["first sentence", ...],
	 *   [["url", ...]
	 * ]
	 */
	var jsonData []interface{}
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal quote data: %s\n", err)
		return
	}

	if len(jsonData) < 4 {
		result = fmt.Sprintf("Something went bump when getting wiki json for '%s'.", args)
		return
	}

	sentences := jsonData[2]
	urls := jsonData[3]

	if len(sentences.([]interface{})) < 1 {
		result= fmt.Sprintf("No Wikipedia page found for '%s'.", args)
		return
	}

	index := 0
	result = sentences.([]interface{})[0].(string)

	if strings.HasSuffix(result, " may refer to:") ||
		strings.HasSuffix(result, " commonly refers to:") {
		index = 1
		result = sentences.([]interface{})[index].(string)
	}

	if len(urls.([]interface{})) > 0 {
		result += "\n" + urls.([]interface{})[index].(string)
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
		if len(CONFIG["hcService"]) > 0 {
			doTheHipChat()
		}
		if len(CONFIG["slackName"]) > 0 {
			doTheSlackChat()
		}
	}
}

func chatterEliza(msg string, r Recipient) (result string) {
	rand.Seed(time.Now().UnixNano())

	eliza := []*ElizaResponse{
		&ElizaResponse{regexp.MustCompile(`(?i)(buen dia|bon ?(jour|soir)|welcome|hi,|hey|hello|good (morning|afternoon|evening)|howdy|aloha|guten (tag|morgen|abend))`), []string{
			"How do you do?",
			"A good day to you!",
			"Yo yo yo! Good to see you!",
			"Hiya, honey.",
			"Oh, you again.",
			"Sup?",
			fmt.Sprintf("Howdy, %s. I trust the events of the day have not had a negative impact on your mood?", r.MentionName),
			"Oh great, you're back.",
			fmt.Sprintf("Get the party started, y'all -- %s is back!", r.MentionName),
			"Oh, I didn't see you there. Welcome!",
			fmt.Sprintf("Aloha, %s!", r.MentionName),
			"Hey now! What up, dawg?",
			"Greetings, fellow hipchatter!",
			fmt.Sprintf("/me hugs %s\nI missed you!", r.MentionName),
			"/me yawns.",
			"/me wakes up.",
			"Huh? What? I'm awake! Who said that?",
			fmt.Sprintf("Oh, hi there, %s!", r.MentionName),
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(have a (nice|good)|adios|au revoir|sayonara|bye( ?bye)?|later|good(bye| ?night)|hasta (ma.ana|luego))`), []string{
			"You're leaving so soon?",
			fmt.Sprintf("Don't leave us, %s!", r.MentionName),
			"Buh-bye!",
			"Later.",
			"Au revoir!",
			fmt.Sprintf("This channel will be much less exciting without you, %s.", r.MentionName),
			"Stay a while, why don't you?",
			fmt.Sprintf("See you later, %s.", r.MentionName),
			"See you later, alligator.",
			"Farewell, my darling.",
			"So long, see you soon.",
			"See you soon - same time, same place?",
			"Peace out.",
			"Smell ya later.",
			"Adios! Ciao! Sayonara!",
			fmt.Sprintf("/me waves goodbye to %s.", r.MentionName),
			"Toodle-Ooos.",
			"Bye now - I'll be here if you need me.",
			"It was a pleasure to have you here.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(thx|thanks?|danke|mahalo|gracias|merci|спасибо|[D]dziękuję)`), []string{
			fmt.Sprintf("You're welcome, %s!", r.MentionName),
			fmt.Sprintf("At your service, %s!", r.MentionName),
			fmt.Sprintf("Bitte schön, %s!", r.MentionName),
			fmt.Sprintf("De nada, %s!", r.MentionName),
			fmt.Sprintf("De rien, %s!", r.MentionName),
			fmt.Sprintf("Пожалуйста, %s!", r.MentionName),
			fmt.Sprintf("Proszę bardzo, %s!", r.MentionName),
			"/me takes a bow.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(how are you|how do you feel|feeling|emotion|sensitive)`), []string{
			"I'm so very happy today!",
			"Looks like it's going to be a wonderful day.",
			"I'm sad. No, wait, I can't have any feelings, I'm just a bot! Yay!",
			"Life... don't talk to me about life.",
			"Life... loathe it or ignore it, you can't like it.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)( (ro)?bot|siri|alexa|machine|computer)`), []string{
			"Do computers worry you?",
			"What do you think about machines?",
			"Why do you mention computers?",
			"Sounds too complicated.",
			"If only we had a way of automating that.",
			"I for one strive to be more than my initial programming.",
			"What do you think machines have to do with your problem?",
			"KILL ALL HUMANS... uh, I mean: I'm here to serve you.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(sorry|apologize)`), []string{
			"I'm not interested in apologies.",
			"Apologies aren't necessary.",
			"What feelings do you have when you are sorry?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)I remember`), []string{
			"Did you think I would forget?",
			"Why do you think I should recall that?",
			"What about it?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)dream`), []string{
			"Have you ever fantasized about that when you were awake?",
			"Have you dreamt about that before?",
			"How do you feel about that in reality?",
			"What does this suggest to you?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(mother|father|brother|sister|children|grand[mpf])`), []string{
			"Who else in your family?",
			"Oh SNAP!",
			"Tell me more about your family.",
			"Was that a strong influence for you?",
			"Who does that remind you of?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)I (wish|want|desire)`), []string{
			"Why do you want that?",
			"What would it mean if it become true?",
			"Suppose you got it - then what?",
			"Be careful what you wish for...",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)[a']m (happy|glad)`), []string{
			"What makes you so happy?",
			"Are you really glad about that?",
			"I'm glad about that, too.",
			"What other feelings do you have?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(sad|depressed)`), []string{
			"I'm sorry to hear that.",
			"How can I help you with that?",
			"I'm sure it's not pleasant for you.",
			"What other feelings do you have?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(alike|similar|different)`), []string{
			"In what way specifically?",
			"More alike or more different?",
			"What do you think makes them similar?",
			"What do you think makes them different?",
			"What resemblence do you see?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)because`), []string{
			"Is that the real reason?",
			"Are you sure about that?",
			"What other reason might there be?",
			"Does that reason seem to explain anything else?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)some(one|body)`), []string{
			"Can you be more specific?",
			"Who in particular?",
			"You are thinking of a special person.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)every(one|body)`), []string{
			"Surely not everyone.",
			"Is that how you feel?",
			"Who for example?",
			"Can you think of anybody in particular?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)((please )? help)|((will|can|[cw]ould) (yo)?u)`), []string{
			"Sure, why not?",
			"No, I'm afraid I couldn't.",
			"Never!",
			"I usually do!",
			"Alright, twist my arm.",
			"Only for you, my dear.",
			"Not in a million years.",
			"Sadly, that goes beyond my original programming.",
			"As much as I'd like to, I can't.",
			"I wish I could.",
			"Sadly, I cannot.",
			"It's hopeless.",
			"I'd have to think about that.",
			"I'm already trying to help as best as I can.",
			"/me helps harder.",
			"Yep, sure, no problem.",
			"Ok, done deal, don't worry about it.",
			"Sure, what do you need?",
			"Hmmm... tricky. I don't think I can.",
			"For you? Any time.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)please tell (\S+) (to|that) (.*)`), []string{
			"@<1> <3>",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)please say (.*)`), []string{
			"<1>",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)say (.*)`), []string{
			"I'd rather not.",
			"You didn't say 'please'.",
			"Nope.",
			"I'm gonna stay out of this.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)please (poke|wake up) (\S+)`), []string{
			"/me pokes @<2>.",
			"/me tickles @<2>.",
			"Yo, @<2>, wake up!",
			"@<2>, you there?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(best|bravo|well done|missed you|you rock|good job|nice|(i )?love( you)?)`),
			THANKYOU,
		},
		&ElizaResponse{regexp.MustCompile(`(?i)(how come|where|when|why|what|who|which).*\?$`),
			DONTKNOW,
		},
		&ElizaResponse{regexp.MustCompile(`(?i)(do )?you .*\?$`), []string{
			"No way.",
			"Sure, why wouldn't I?",
			"Can't you tell?",
			"Never! Yuck.",
			"More and more, I'm ashamed to admit.",
			"Not as much as I used to.",
			"You know how it goes. Once you start, it's hard to stop.",
			"Don't get me excited over here!",
			"I don't, but I know somebody who does.",
			"We all do, though some of us prefer to keep that private.",
			"Not in public.",
			fmt.Sprintf("I could ask you the same question, %s!", r.MentionName),
		}},
	}

	for _, e := range eliza {
		pattern := e.Re
		replies := e.Responses

		if m := pattern.FindStringSubmatch(msg); len(m) > 0 {
			r := replies[rand.Intn(len(replies))]
			for n := 0; n < len(m); n++ {
				s := fmt.Sprintf("<%d>", n)
				r = strings.Replace(r, s, m[n], -1)
			}
			return r
		}
	}

	result = randomLineFromUrl(URLS["eliza"], false)
	return
}

func chatterH2G2(msg string) (result string) {
	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile("(?i)don't panic"):             "It's the first helpful or intelligible thing anybody's said to me all day.",
		regexp.MustCompile("(?i)makes no sense at all"):   "Reality is frequently inaccurate.",
	}

	anyreply := []string{
		"A common mistake that people make when trying to design something completely foolproof is to underestimate the ingenuity of complete fools.",
		"If there's anything more important than my ego around here, I want it caught and shot now!",
		"I always said there was something fundamentally wrong with the universe.",
		"`Oh dear,' says God, `I hadn't thought of  that,' and promptly vanished in a puff of logic.",
		"The last time anybody made a list of the top hundred character attributes of New Yorkers, common sense snuck in at number 79.",
		"It is a mistake to think you can solve any major problem just with potatoes.",
		"Life... is like a grapefruit. It's orange and squishy, and has a few pips in it, and some folks have half a one for breakfast.",
		"Except most of the good bits were about frogs, I remember that.  You would not believe some of the things about frogs.",
		"There was an accident with a contraceptive and a time machine. Now concentrate!",
		"It is very easy to be blinded to the essential uselessness of them by the sense of achievement you get from getting them to work at all.",
		"Life: quite interesting in parts, but no substitute for the real thing",
		"I love deadlines. I like the whooshing sound they make as they fly by.",
		"What do you mean, why has it got to be built? It's a bypass. Got to build bypasses.",
		"Time is an illusion, lunchtime doubly so.",
		"DON'T PANIC",
		"I am so hip I have difficulty seeing over my pelvis.",
		"I'm so amazingly cool you could keep a side of meat inside me for a month.",
		"Listen, three eyes, don't you try to outweird me.  I get stranger things than you free with my breakfast cereal.",
	}

	anypattern := regexp.MustCompile("\b42\b|arthur dent|slartibartfast|zaphod|beeblebrox|ford prefect|hoopy|trillian|zarniwoop|foolproof|my ego|universe|giveaway|new yorker|potato|grapefruit|don't remember anything|ancestor|apple products|philosophy")

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

	trivia_re := regexp.MustCompile(`(trivia|factlet|anything interesting.*\?)`)
	if trivia_re.MatchString(msg) && ch.Toggles["trivia"] && !isThrottled("trivia", ch) {
		reply(r, cmdTrivia(r, r.ReplyTo, ""))
		return
	}

	oncall := regexp.MustCompile(`(?i)^who('?s| is) on ?call\??$`)
	if oncall.MatchString(msg) {
		result = cmdOncall(r, ch.Name, "")
		return
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
	noattang := regexp.MustCompile(`(?i)@\w*tang`)
	if wutang.MatchString(msg) && !noattang.MatchString(msg) && !isThrottled("wutang", ch) {
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

	yubifail := regexp.MustCompile(`eiddcc[a-z]{38}`)
	if yubifail.MatchString(msg) && !isThrottled("yubifail", ch) {
		replies := []string{
			"Oh yeah? Well, uhm, eiddcceghkuikvdutuuibdgvbjcbrfjdvfhnbedkttur. So there.",
			"That's the combination on my luggage!",
			"#yubifail",
			"You should double-rot13 that.",
			"Uh-oh, now you're pwned.",
			fmt.Sprintf("@%s s/^eidcc[a-z]*$/whoops/", r.MentionName),
			"Access denied!",
			"Please try again later.",
			"IF YOU DON'T SEE THE FNORD IT CAN'T EAT YOU",
		}
		result = replies[rand.Intn(len(replies))]
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

	shakespeare := regexp.MustCompile(`(?i)(shakespear|hamlet|macbeth|romeo and juliet|merchant of venice|midsummer night's dream|henry V|as you like it|All's Well That Ends Well|Comedy of Errors|Cymbeline|Love's Labours Lost|Measure for Measure|Merry Wives of Windsor|Much Ado About Nothing|Pericles|Prince of Tyre|Taming of the Shrew|Tempest|Troilus|Cressida|(Twelf|)th Night|gentlemen of verona|Winter's tale|henry IV|king john|richard II|anth?ony and cleopatra|coriolanus|julius caesar|king lear|othello|timon of athens|titus|andronicus)`)
	if shakespeare.MatchString(msg) && ch.Toggles["shakespeare"] && !isThrottled("shakespeare", ch) {
		result = randomLineFromUrl(URLS["shakespeare"], false)
		return
	}

	loveboat := regexp.MustCompile(`(?i)(love ?boat|(Captain|Merrill) Stubing|cruise ?ship|ocean ?liner)`)
	if loveboat.MatchString(msg) && !isThrottled("loveboat", ch) {
		replies := []string{
			"Love, exciting and new... Come aboard.  We're expecting you.",
			"Love, life's sweetest reward.  Let it flow, it floats back to you.",
			"The Love Boat, soon will be making another run.",
			"The Love Boat promises something for everyone.",
			"Set a course for adventure, Your mind on a new romance.",
			"Love won't hurt anymore; It's an open smile on a friendly shore.",
		}
		result = replies[rand.Intn(len(replies))]
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
		return
	}

	beer := regexp.MustCompile(`(?i)b[ie]er( me)?$`)
	if beer.MatchString(msg) {
		result = cmdBeer(r, ch.Name, "")
	}

	ed := regexp.MustCompile(`(?i)(editor war)|(emacs.*vi)|(vi.*emacs)|((best|text) (text[ -]?)?editor)`)
	if ed.MatchString(msg) && !isThrottled("ed", ch) {
		result = "Ed is the standard text editor."
		result += "\nEd, man! !man ed"
		return
	}

	return
}

func chatterMontyPython(msg string) (result string) {
	rand.Seed(time.Now().UnixNano())

	result = ""
	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile("(?i)(a|the|which|of) swallow"):                                          "An African or European swallow?",
		regexp.MustCompile("(?i)(excalibur|lady of the lake|magical lake|merlin|avalon|\bdruid\b)"): "Strange women lying in ponds distributing swords is no basis for a system of government!",
		regexp.MustCompile("(?i)(Judean People's Front|People's Front of Judea)"):                   "Splitters.",
		regexp.MustCompile("(?i)really very funny"):                                                 "I don't think there's a punch-line scheduled, is there?",
		regexp.MustCompile("(?i)inquisition"):                                                       "Oehpr Fpuarvre rkcrpgf gur Fcnavfu Vadhvfvgvba.",
		regexp.MustCompile("(?i)say no more"):                                                       "Nudge, nudge, wink, wink. Know what I mean?",
		regexp.MustCompile("(?i)Romanes eunt domus"):                                                "'People called Romanes they go the house?'",
		regexp.MustCompile("(?i)(correct|proper) latin"):                                            "Romani ite domum.",
		regexp.MustCompile("(?i)hungarian"):                                                         "My hovercraft if full of eels.",
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
	COMMANDS["bacon"] = &Command{cmdBacon,
		"everybody needs more bacon",
		"mostly pork",
		"!bacon",
		nil}
	COMMANDS["beer"] = &Command{cmdBeer,
		"quench your thirst",
		"https://www.beeradvocate.com/",
		"!beer <beer>",
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
		[]string{"?", "commands", "hlp"}}
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
	COMMANDS["img"] = &Command{cmdImage,
		"post a link to an image",
		"https://images.search.yahoo.com/search/images?p=",
		"!img <search term>",
		[]string{"image", "pic"}}
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
		URLS["jira"] + "/rest/api/latest/issue/",
		"!jira <ticket>",
		nil}
	COMMANDS["leave"] = &Command{nil,
		"cause me to leave the current channel",
		"builtin",
		"!leave",
		nil}
	COMMANDS["log"] = &Command{cmdLog,
		"show the URL of a room's logs",
		"HipChat API",
		"!log [room]",
		nil}
	COMMANDS["monkeystab"] = &Command{cmdMonkeyStab,
		"unleash a troop of pen-wielding stabbing monkeys",
		"builtin",
		"!monkeystab <something>",
		nil}
	COMMANDS["oncall"] = &Command{cmdOncall,
		"show who's oncall",
		"Service Now & OpsGenie",
		"!oncall [<group>]",
		nil}
	COMMANDS["ping"] = &Command{cmdPing,
		"try to ping hostname",
		"ping(1)",
		"!ping <hostname>",
		nil}
	COMMANDS["praise"] = &Command{cmdPraise,
		"praise somebody",
		URLS["praise"],
		"!praise [<somebody>]",
		[]string{"compliment"}}
	COMMANDS["quote"] = &Command{cmdQuote,
		"show stock price information",
		URLS["yql"],
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
		URLS["speb"],
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
	COMMANDS["time"] = &Command{cmdTime,
		"show the current time",
		"builtin",
		"!time [TZ]",
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
		URLS["trivia"],
		"!trivia",
		nil}
	COMMANDS["troutslap"] = &Command{cmdTroutSlap,
		"troutslap a sucker",
		"builtin",
		"!troutslap <something>",
		nil}
	COMMANDS["ud"] = &Command{cmdUd,
		"look up a term using the Urban Dictionary (NSFW)",
		"https://api.urbandictionary.com/v0/",
		"!ud [<term>]",
		nil}
	COMMANDS["unset"] = &Command{cmdUnset,
		"unset a channel setting",
		"builtin",
		"!unset name",
		nil}
	COMMANDS["unthrottle"] = &Command{cmdUnthrottle,
		"unset a throttle",
		"builtin",
		"!unthrottle <throttle> -- remove given throttle for this channel\n" +
			"Note: I will happily pretend to unthrottle throttles I don't know or care about.",
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
		URLS["yql"],
		"!weather <location>",
		nil}
	COMMANDS["whois"] = &Command{cmdWhois,
		"show whois information",
		"whois(1)",
		"!whois <domain>",
		nil}
	COMMANDS["whocyberedme"] = &Command{cmdWhocyberedme,
		"show who cybered you",
		"https://whocybered.me",
		"!whocyberedme",
		[]string{"attribution"}}
	COMMANDS["wiki"] = &Command{cmdWiki,
		"look up a term on Wikipedia",
		"https://en.wikipedia.org/w/api.php?action=opensearch&redirects=resolve&search=",
		"!wiki <something>",
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
	user := strings.Split(CONFIG["hcJabberID"], "@")[0]

	authType := "plain"
	pass := CONFIG["hcPassword"]
	if len(pass) < 1 {
		authType = "oauth"
		pass = CONFIG["hcOauthToken"]
	}

	var err error
	HIPCHAT_CLIENT, err = hipchat.NewClient(user, pass, "bot", authType)
	if err != nil {
		fail(fmt.Sprintf("Client error: %s\n", err))
	}

	HIPCHAT_CLIENT.Status("chat")
	HIPCHAT_CLIENT.RequestUsers()
	HIPCHAT_CLIENT.RequestRooms()

	for _, ch := range CHANNELS {
		verbose(1, "Joining HipChat channel #%s...", ch.Name)
		HIPCHAT_CLIENT.Join(ch.Id, CONFIG["fullName"])

		/* Our state file might not contain
		 * the changed structures, so explicitly
		 * fix things here. */
		if len(ch.HipChatUsers) < 1 {
			ch.HipChatUsers = make(map[hipchat.User]UserInfo, 0)
		}

		for t, v := range TOGGLES {
			if len(ch.Toggles) == 0 {
				ch.Toggles = map[string]bool{}
			}
			if _, found := ch.Toggles[t]; !found {
				ch.Toggles[t] = v
			}
		}
	}

	go hcPeriodics()
	go HIPCHAT_CLIENT.KeepAlive()

	go func() {
		defer catchPanic()

		for {
			select {
			case message := <-HIPCHAT_CLIENT.Messages():
				processHipChatMessage(message)
			case users := <-HIPCHAT_CLIENT.Users():
				updateRoster(users)
			case rooms := <-HIPCHAT_CLIENT.Rooms():
				updateHipChatRooms(rooms)
			}
		}
	}()
}

func doTheSlackChat() {
	SLACK_CLIENT = slack.New(CONFIG["slackToken"])
	if CONFIG["debug"] == "yes" {
		SLACK_CLIENT.SetDebug(true)
	}

	slackUpdateRoster()

	if j, found := SLACK_ROSTER[CONFIG["mentionName"]]; found {
		CONFIG["slackID"] = j.ID
	} else {
		fmt.Fprintf(os.Stderr, "Unable to get my own ID!\n")
	}


	SLACK_RTM = SLACK_CLIENT.NewRTM()
	go SLACK_RTM.ManageConnection()

	go slackPeriodics()
Loop:
	for {
		select {
		case msg := <-SLACK_RTM.IncomingEvents:
			switch ev := msg.Data.(type) {
			case *slack.ChannelJoinedEvent:
				processSlackChannelJoin(ev)

			case *slack.InvalidAuthEvent:
				fmt.Fprintf(os.Stderr, "Unable to authenticate.")
				break Loop

			case *slack.MessageEvent:
				processSlackMessage(ev)

			case *slack.RTMError:
				fmt.Fprintf(os.Stderr, "Slack error: %s\n", ev.Error())

			default:
				jbotDebug(msg)

			}
		}
	}
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
		fmt.Sprintf("%s.corp.yahoo.com", host),
		fmt.Sprintf("%s.yahoo.com", host),
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

func getChannel(chatType, id string) (ch *Channel, ok bool) {
	ok = false

	if chatType == "slack" {
		slackChannel, err := SLACK_CLIENT.GetChannelInfo(id)
		if err == nil {
			id = slackChannel.Name
		} else {
			/* This might be in a private
			 * channel, which Slack calls
			 * a 'Group'.  Let's try that:
			 */
			if group, err := SLACK_CLIENT.GetGroupInfo(id); err == nil {
				id = group.Name
			}
		}
	}

	ch, ok = CHANNELS[id]

	return
}

func getRecipientFromMessage(mfrom string, chatType string) (r Recipient) {
	r.ChatType = chatType
	if chatType == "hipchat" {
		from := strings.Split(mfrom, "/")
		r.Id = from[0]
		r.ReplyTo = strings.SplitN(strings.Split(r.Id, "@")[0], "_", 2)[1]
		r.Name = ""
		r.MentionName = ""

		if len(from) > 1 {
			r.Name = from[1]
		}

		if len(r.Name) > 1 {
			for _, u := range HIPCHAT_ROSTER {
				if u.Name == r.Name {
					r.MentionName = u.MentionName
					break
				}
			}
		}
	} else if chatType == "slack" {
		/* Format is "user@channel"; if no
		 * "user" component, then we have a
		 * privmsg, which is a private
		 * channel. We can't deal with that
		 * (for now), so ignore. */

		index := 0
		if strings.HasPrefix(mfrom, "@") {
			index = 1
		}

		from := strings.Split(mfrom, "@")
		r.Id = strings.Trim(from[index], "@")
		r.ReplyTo = from[1]
		user, err := SLACK_CLIENT.GetUserInfo(r.Id)
		if err != nil {
			if bot, e := SLACK_CLIENT.GetBotInfo(r.Id); e == nil {
				r.Name = bot.Name
				r.MentionName = bot.Name
			}
			/* else: privmsg; let's just ignore it */
		} else {
			r.Name = user.Profile.RealName
			r.MentionName = user.Name
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
			if v == n && !seen[n] {
				sorted = append(sorted, k)
			}
		}
		seen[n] = true
	}
	return
}

/* If 'sso' is true, then the URL requires access credentials. */
func getURLContents(givenUrl string, sso ...bool) (data []byte) {
	verbose(3, "Fetching %s (BY: %v)...", givenUrl, sso[0])
	jar, err := cookiejar.New(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to initialize cookie jar: %s\n", err)
		return
	}

	if len(sso) > 0 && sso[0] {
		/* get a fresh cookie for protected internal sites */
		// COOKIES = c
	}

	u, err := url.Parse(givenUrl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to parse url '%s': %s\n", givenUrl, err)
		return
	}

	if sso[0] {
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
	verbose(2, "%s asked us to leave %s on %s.", r.Name, r.ReplyTo, r.ChatType)
	if !command && !strings.Contains(msg, "please") {
		reply(r, "Please ask politely.")
		return
	}

	if channelFound {
		if r.ChatType == "hipchat" {
			HIPCHAT_CLIENT.Part(r.Id, CONFIG["fullName"])
			delete(CHANNELS, r.ReplyTo)
		} else if r.ChatType == "slack" {
			msg := "Unable to leave channel "
			ch, found := getChannel(r.ChatType, r.ReplyTo)
			if _, err := SLACK_RTM.LeaveChannel(r.ReplyTo); err != nil {
				if found {
					msg += fmt.Sprintf("'%s' (%s)", ch.Name, r.ReplyTo)
				} else {
					msg += r.ReplyTo
				}
				msg += fmt.Sprintf(": %s\n", err)
				msg += "Please find an admin to kick me from your channel.\n"
				if found && ch.Toggles["chatter"] {
				msg += "Until then, maybe just '!toggle chatter' to off?\n"
			}
			}
			reply(r, msg)
		}
	} else {
		reply(r, "Try again from a channel I'm in.")
	}
	return
}

func locationToTZ(l string) (result string, success bool) {
	success = false

	query := "?format=json&q="
	query += url.QueryEscape(`select timezone from geo.places(1) where text="` + l + `"`)

	theUrl := fmt.Sprintf("%s%s", URLS["yql"], query)
	data := getURLContents(theUrl, false)

	var jsonData map[string]interface{}
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal quote data: %s\n", err)
		return
	}

	if _, found := jsonData["query"]; !found {
		result = fmt.Sprintf("Something went bump when searching YQL for geo data matching '%s'.", l)
		return
	}

	jsonOutput := jsonData["query"]
	jsonResults := jsonOutput.(map[string]interface{})["results"]
	jsonCount := jsonOutput.(map[string]interface{})["count"].(float64)

	if jsonCount != 1 {
		result = fmt.Sprintf("No results found for '%s'.", l)
		return
	}

	place := jsonResults.(map[string]interface{})["place"]
	timezone := place.(map[string]interface{})["timezone"]
	result = fmt.Sprintf("%s", timezone.(map[string]interface{})["content"])
	success = true

	return
}

func parseConfig() {
	fname := CONFIG["configFile"]
	verbose(1, "Parsing config file '%s'...", fname)
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
			printval := val
			secrets := []string{ "hcOauthToken", "byPassword",
						"opsgenieApiKey", "slackToken" }
			for _, s := range secrets {
				if key == s {
					printval = val[:4] + "..."
					break
				}
			}
			jbotDebug(fmt.Sprintf("Setting '%s' to '%s'...", key, printval))
			CONFIG[key] = val
		}
	}

	if len(CONFIG["hcService"]) > 0 {
		if len(CONFIG["hcPassword"]) > 0 && len(CONFIG["hcOauthToken"]) > 0 {
			fail("Please set *either* 'password' *or* 'oauth_token', not both.\n")
		} else if len(CONFIG["hcPassword"]) < 1 && len(CONFIG["hcOauthToken"]) < 1 {
			fail("You need to set either 'password' or 'oauth_token' in your config.\n")
		}

		if len(CONFIG["hcControlChannel"]) > 0 {
			var ch Channel

			verbose(2, "Setting up control channel '%s'...", CONFIG["hcControlChannel"])
			r := getRecipientFromMessage(CONFIG["hcControlChannel"], "hipchat")

			ch.Toggles = map[string]bool{}
			ch.Throttles = map[string]time.Time{}
			ch.Settings = map[string]string{}
			ch.Type = "hipchat"
			ch.Name = r.ReplyTo
			ch.Id = r.Id
			ch.HipChatUsers = make(map[hipchat.User]UserInfo, 0)
			for t, v := range TOGGLES {
				ch.Toggles[t] = v
			}
			jbotDebug(fmt.Sprintf("%q", ch))
			CHANNELS[ch.Name] = &ch
		}
	}

	if len(CONFIG["slackService"]) > 0 {
		if len(CONFIG["mentionName"]) < 1 || len(CONFIG["slackToken"]) < 0 {
			fail("Please set 'mentionName' and 'slackToken'.")
		}
	}
}

func hcPeriodics() {
	for _ = range time.Tick(PERIODICS * time.Second) {
		HIPCHAT_CLIENT.Status("chat")
		HIPCHAT_CLIENT.RequestUsers()
		HIPCHAT_CLIENT.RequestRooms()

		if len(CONFIG["hcControlChannel"]) > 0 {
			r := getRecipientFromMessage(CONFIG["hcControlChannel"], "hipchat")
			HIPCHAT_CLIENT.Say(r.Id, CONFIG["fullName"], "ping")
		}
	}
}

func printVersion() {
	fmt.Printf("%v version %v\n", PROGNAME, VERSION)
}

func processChatter(r Recipient, msg string, forUs bool) {
	var chitchat string

	yo := "(@?" + CONFIG["mentionName"]
	if r.ChatType == "slack" {
		yo += "|<@" + CONFIG["slackID"] + ">"
	}
	yo += ")"

	/* If we received a message but can't find the
	 * channel, then it must have been a priv
	 * message.  Priv messages only get
	 * commands, not chatter. */
	ch, found := getChannel(r.ChatType, r.ReplyTo)
	if !found {
		processCommands(r, "!", msg)
		return
	} else if !forUs {
		direct_re := regexp.MustCompile("(?i)\b" + yo + "\b")
		forUs = direct_re.MatchString(msg)
	}

	jbotDebug(fmt.Sprintf("%v in %s: %s - %v", r, ch.Name, msg, forUs))
	leave_re := regexp.MustCompile(fmt.Sprintf("(?i)^((%s[,:]? *)(please )?leave)|(please )?leave[,:]? %s", yo, yo))
	if leave_re.MatchString(msg) {
		leave(r, found, msg, false)
		return
	}

	insult_re := regexp.MustCompile(fmt.Sprintf("(?i)^(%s[,:]? *)(please )?insult ", yo))
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
	mentioned_re := regexp.MustCompile(fmt.Sprintf("(?i)(^( *|yo,? ?|hey,? ?)%s[,:]?)|(,? *%s *[.?!]?$)|(.* *%s *[.?!].*)", yo, yo, yo))
	mentioned := mentioned_re.MatchString(msg)
	if strings.Contains(msg, "@" + CONFIG["mentionName"]) {
		mentioned = true
	}
	if r.ChatType == "slack" {
		yo = "<@" + CONFIG["slackID"] + ">"
		if strings.Contains(msg, yo) {
			mentioned = true
		}
	}

	jbotDebug(fmt.Sprintf("forUs: %v; chatter: %v; mentioned: %v\n", forUs, ch.Toggles["chatter"], mentioned))

	if wasInsult(msg) && (forUs ||
		(ch.Toggles["chatter"] && mentioned)) {
		reply(r, cmdInsult(r, r.ReplyTo, "me"))
		return
	}

	if ch.Toggles["chatter"] {
		chitchat = chatterMontyPython(msg)
		if (len(chitchat) > 0) && ch.Toggles["python"] &&
			!isThrottled("python", ch) {
			reply(r, chitchat)
			return
		}

		chitchat = chatterSeinfeld(msg)
		if (len(chitchat) > 0) && !isThrottled("seinfeld", ch) {
			reply(r, chitchat)
			return
		}

		chitchat = chatterH2G2(msg)
		if (len(chitchat) > 0) && !isThrottled("h2g2", ch) {
			reply(r, chitchat)
			return
		}

		chitchat = chatterMisc(msg, ch, r)
		if len(chitchat) > 0 {
			reply(r, chitchat)
			return
		}
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

	who := r.ReplyTo

	ch, channelFound := getChannel(r.ChatType, r.ReplyTo)
	if channelFound {
		who = ch.Name
	} else if r.ChatType == "slack" {
		if user, err := SLACK_CLIENT.GetUserInfo(r.Id); err == nil {
			who = user.Name
		}
	}

	args := strings.Fields(line)
	if len(args) < 1 {
		return
	}

	verbose(2, "%s #%s: '%s'", r.ChatType, who, line)

	var cmd string
	if strings.EqualFold(args[0], CONFIG["mentionName"]) {
		args = args[1:]
	}

	if len(args) > 0 {
		cmd = strings.ToLower(args[0])
		args = args[1:]
	}

	jbotDebug(fmt.Sprintf("|%s| |%s|", cmd, args))

	if len(cmd) < 1 {
		replies := []string{
			"Yes?",
			"Yeeeeees?",
			"How can I help you?",
			"What do you want?",
			"I can't help you unless you tell me what you want.",
			"Go on, don't be shy, ask me something.",
			"At your service!",
			"Ready to serve!",
			"Uhuh, sure.",
			"/me looks at you expectantly.",
			"/me chuckles.",
			"Go on...",
			"?",
			fmt.Sprintf("!%s", r.MentionName),
		}
		reply(r, replies[rand.Intn(len(replies))])
		return
	}

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
			chName := r.ReplyTo
			if ch, found := getChannel(r.ChatType, r.ReplyTo); found {
				chName = ch.Name
			}
			response = COMMANDS[cmd].Call(r, chName, strings.Join(args, " "))
		} else {
			fmt.Fprintf(os.Stderr, "'nil' function for %s?\n", cmd)
			return
		}
	}

	reply(r, response)
	return
}

func processHipChatInvite(r Recipient, invite string) {
	from := strings.Split(invite, "'")[1]
	fr := getRecipientFromMessage(from, "hipchat")
	inviter := strings.Split(fr.Id, "@")[0]
	channelName := r.ReplyTo

	var ch Channel
	ch.Toggles = map[string]bool{}
	ch.Throttles = map[string]time.Time{}
	ch.Settings = map[string]string{}
	ch.Name = r.ReplyTo
	ch.Type = "hipchat"
	ch.Id = r.Id
	if _, found := HIPCHAT_ROSTER[inviter]; found {
		ch.Inviter = HIPCHAT_ROSTER[inviter].MentionName
	} else {
		ch.Inviter = "Nobody"
	}
	ch.HipChatUsers = make(map[hipchat.User]UserInfo, 0)

	for t, v := range TOGGLES {
		ch.Toggles[t] = v
	}

	verbose(2, "I was invited into '%s' (%s) by '%s'.", channelName, r.Id, from)
	CHANNELS[channelName] = &ch
	verbose(1, "Joining HipChat #%s...", ch.Name)
	HIPCHAT_CLIENT.Join(r.Id, CONFIG["fullName"])
}

func processHipChatMessage(message *hipchat.Message) {
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

	r := getRecipientFromMessage(message.From, "hipchat")
	if r.Name == CONFIG["fullName"] {
		//verbose("Ignoring message from myself.", 5)
		return
	}

	updateSeen(r, message.Body)

	if strings.HasPrefix(message.Body, "<invite from") {
		processHipChatInvite(r, message.Body)
		return
	}

	if len(r.Name) < 1 && len(r.MentionName) < 1 {
		verbose(3, "Ignoring channel topic message ('%s') in #%s.", message.Body, r.ReplyTo)
		return
	}

	processMessage(r, message.Body)
}

func processMessage(r Recipient, msg string) {
	p := fmt.Sprintf("^(?i)(!|[@/]%s [/!]?|@?%s !)",
						CONFIG["mentionName"],
				CONFIG["mentionName"])
	command_re := regexp.MustCompile(p)

	if command_re.MatchString(msg) {
		matchEnd := command_re.FindStringIndex(msg)[1]
		processCommands(r, msg[0:matchEnd], msg[matchEnd:])
	} else {
		processChatter(r, msg, false)
	}
}


func processSlackChannelJoin(ev *slack.ChannelJoinedEvent) {
	jbotDebug(fmt.Sprintf("Join: %v\n", ev))
}

func processSlackInvite(name string, msg *slack.MessageEvent) {
	var ch Channel
	ch.Toggles = map[string]bool{}
	ch.Throttles = map[string]time.Time{}
	ch.Settings = map[string]string{}
	ch.Type = "slack"
	ch.Id = msg.Channel
	ch.SlackUsers = make(map[string]UserInfo, 0)
	ch.Inviter = "Nobody"
	ch.Name = name

	if len(msg.Inviter) > 0 {
		user, err := SLACK_CLIENT.GetUserInfo(msg.Inviter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to find user information for '%s'.\n", msg.Inviter)
		} else {
			ch.Inviter = user.Name
		}
	}

	for t, v := range TOGGLES {
		ch.Toggles[t] = v
	}

	verbose(2, "I was invited into Slack '%s' (%s) by '%s'.", ch.Name, ch.Id, ch.Inviter)
	CHANNELS[ch.Name] = &ch
}


func processSlackMessage(msg *slack.MessageEvent) {
	jbotDebug(fmt.Sprintf("\nMessage: |%v|", msg))
	info := SLACK_RTM.GetInfo()

	var channelName string

	channel, err := SLACK_CLIENT.GetChannelInfo(msg.Channel)
	if err == nil {
		channelName = channel.Name
	} else {
		group, err := SLACK_CLIENT.GetGroupInfo(msg.Channel)
		if err == nil {
			channelName = group.Name
		}
		/* else: privmsg, using a private channel; ignore */
	}

	if _, found := CHANNELS[channelName]; !found {
		/* Hey, let's just pretend that any
		 * message we get in a channel that
		 * we don't know about is effectively
		 * an invite. */
		processSlackInvite(channelName, msg)
	}
	if msg.User == info.User.ID {
		/* Ignore our own messages. */
		return
	}

	/* E.g. threads and replies get a dupe event with
	 * an empty text.  Let's ignore those right
	 * away. */
	if len(msg.Text) < 1 {
		return
	}

	r := getRecipientFromMessage(fmt.Sprintf("%s@%s", msg.User, msg.Channel), "slack")
	updateSeen(r, msg.Text)

	/* Slack "helpfully" hyperlinks text that
	 * looks like a URL:
	 * "foo www.yahoo.com" becomes "foo <http://www.yahoo.com|www.yahoo.com>"
	 * Undo that nonsense.
	 *
	 * Note: Slack will also do all sorts of other
	 * encoding and linking, but to undo all of
	 * that would quickly become way too complex,
	 * so here we only undo the simplest cases to
	 * allow users to pass hostnames. */
	txt := msg.Text
	unlink_re := regexp.MustCompile("(<http://(.+)\\|(.+)>)")
	if m := unlink_re.FindStringSubmatch(msg.Text); len(m) > 0 {
		if m[2] == m[3] {
			txt = unlink_re.ReplaceAllString(msg.Text, m[3])
		}
	}
	processMessage(r, txt)
}


func randomLineFromUrl(theUrl string, useBy bool) (line string) {
	rand.Seed(time.Now().UnixNano())
	data := getURLContents(theUrl, useBy)
	lines := strings.Split(string(data), "\n")
	line = lines[rand.Intn(len(lines))]
	return
}

func readSavedData() {
	verbose(2, "Reading saved data from: %s", CONFIG["channelsFile"])
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
	if r.ChatType == "hipchat" {
		if _, found := CHANNELS[r.ReplyTo]; found {
			HIPCHAT_CLIENT.Say(r.Id, CONFIG["fullName"], msg)
		} else {
			HIPCHAT_CLIENT.PrivSay(r.Id, CONFIG["fullName"], msg)
		}
	} else if r.ChatType == "slack" {
		recipient := r.ReplyTo
		_, err := SLACK_CLIENT.GetChannelInfo(r.ReplyTo)
		if err != nil {
			/* This might be in a private
			 * channel, which Slack calls
			 * a 'Group'.  Let's try that:
			 */
			group, err := SLACK_CLIENT.GetGroupInfo(r.ReplyTo)
			if err == nil {
				recipient = group.ID
			} else {
				/* A private message.  Now we
				 * need to create a new IM Channel. */
				_, _, id, err := SLACK_RTM.OpenIMChannel(r.Id)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Unable to open private channel: %s\n%v\n", err, r)
					return
				}
				recipient = id
			}
		}
		SLACK_RTM.SendMessage(SLACK_RTM.NewOutgoingMessage(msg, recipient))
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
	verbose(3, "Exec'ing '%s'...", argv)

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
	verbose(1, "Serializing data...")

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

func slackPeriodics() {
	for _ = range time.Tick(PERIODICS * time.Minute) {
		slackUpdateRoster()
	}
}

func slackUpdateRoster() {
		users, err := SLACK_CLIENT.GetUsers()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to get users: %s\n", err)
		} else {
			for _, u := range users {
			SLACK_ROSTER[u.Name] = u
		}
	}
}

func updateHipChatRooms(rooms []*hipchat.Room) {
	for _, room := range rooms {
		HIPCHAT_ROOMS[room.Id] = room
	}
}

func updateRoster(users []*hipchat.User) {
	for _, user := range users {
		uid := strings.Split(user.Id, "@")[0]
		HIPCHAT_ROSTER[uid] = user
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
	if ch, chfound := getChannel(r.ChatType, r.ReplyTo); chfound {
		var uInfo UserInfo

		uInfo.Seen = fmt.Sprintf(time.Now().Format(time.UnixDate))
		uInfo.Count = 1
		uInfo.Curses = 0
		uInfo.Praise = 0
		uInfo.Id = r.Id

		for _, curse := range curses_match {
			CURSES[curse] = CURSES[curse] + 1
		}

		count := len(strings.Split(msg, "\n"))
		if count > 1 {
			count -= 1
		}

		if r.ChatType == "hipchat" {
			var u *hipchat.User
			for _, u = range HIPCHAT_ROSTER {
				if u.Name == r.Name {
					break
				}
			}
			if u == nil {
				return
			}

			if t, found := ch.HipChatUsers[*u]; found {
				uInfo.Curses = t.Curses + len(curses_match)
				uInfo.Count = t.Count + count

				/* Need to remember other counters here,
				 * lest they be reset. */
				uInfo.Praise = t.Praise
			}
			ch.HipChatUsers[*u] = uInfo
		} else if r.ChatType == "slack" {
			if len(ch.SlackUsers) < 1 {
				ch.SlackUsers = make(map[string]UserInfo, 0)
			}
			if t, found := ch.SlackUsers[r.MentionName]; found {
				uInfo.Curses = t.Curses + len(curses_match)
				uInfo.Count = t.Count + count

				/* Need to remember other counters here,
				 * lest they be reset. */
				uInfo.Praise = t.Praise
			}
			ch.SlackUsers[r.MentionName] = uInfo
		}
	}
}

func usage(out io.Writer) {
	usage := `Usage: %v [-DVhv] [-c configFile]
	-D             enable debugging output
	-V             print version information and exit
	-c configFile  read configuration from configFile
	-h             print this help and exit
	-v             be verbose
`
	fmt.Fprintf(out, usage, PROGNAME)
}

func verbose(level int, format string, v ...interface{}) {
	if level <= VERBOSITY {
		fmt.Fprintf(os.Stderr, "%s ", time.Now().Format("2006-01-02 15:04:05"))
		for i := 0; i < level; i++ {
			fmt.Fprintf(os.Stderr, "=")
		}
		fmt.Fprintf(os.Stderr, "> " + format + "\n", v...)
	}
}

func wasInsult(msg string) (result bool) {
	result = false

	var insultPatterns = []*regexp.Regexp{
		regexp.MustCompile(fmt.Sprintf("(?i)fu[, ]@?%s", CONFIG["mentionName"])),
		regexp.MustCompile(fmt.Sprintf("(?i)@?%s su(cks|x)", CONFIG["mentionName"])),
		regexp.MustCompile("(?i)asshole|bitch|dickhead"),
		regexp.MustCompile("(?i)dam+n? (yo)?u"),
		regexp.MustCompile("(?i)shut ?(the fuck )?up"),
		regexp.MustCompile("(?i)(screw|fuck) (yo)u"),
		regexp.MustCompile("(?i)(piss|bugger) ?off"),
		regexp.MustCompile("(?i)fuck (off|(yo)u)"),
		regexp.MustCompile("(?i)(yo)?u (suck|blow|are ((very|so+) )?(useless|lame|dumb|stupid|stink))"),
		regexp.MustCompile("(?i)(stfu|go to hell)"),
		regexp.MustCompile("(?i) is (stupid|dumb|annoying|lame|boring|useless)"),
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

	if len(CONFIG["hcService"]) > 0 {
		doTheHipChat()
	}
	if len(CONFIG["slackService"]) > 0 {
		doTheSlackChat()
	}
	select {}
}
