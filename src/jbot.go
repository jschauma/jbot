/*
 * This is a HipChat and Slack version of the 'jbot'
 * IRC bot, originally developed at Yahoo! in 2007.
 * This variant was created as a rewrite in Go for
 * HipChat in July 2016 by Jan Schaumann (@jschauma
 * / jschauma@netmeister.org); support for Slack was
 * added some time in early 2017.  Many thanks to
 * Yahoo for letting me play around with nonsense like
 * this.
 *
 * You should be able to run the bot by populating a
 * configuration file with suitable values.  The
 * following configuration values are required:
 *
 * fullName        = how the bot presents itself
 * mentionName     = to which name the bot responds to
 *
 * For HipChat:
 *   hcPassword    = the HipChat password of the bot user
 *     OR
 *   hcOauthToken  = the HipChat Oauth token for the bot user
 *   hcService     = the HipChat company prefix, e.g. <foo>.hipchat.com
 *   hcJabberID    = the HipChat / JabberID of the bot user
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
	"crypto/tls"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"math"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

import (
	"github.com/daneharrigan/hipchat"
	"github.com/google/shlex"
	"github.com/nlopes/slack"

	/* XXX mtls include */
)

const EXIT_FAILURE = 1
const EXIT_SUCCESS = 0

const PROGNAME = "jbot"
const VERSION = "3.0"

const DEFAULT_THROTTLE = 1800
const PERIODICS = 60

/* Periodics are run PERIODICS * Seconds;
 * Intervals are run every I * PERIODICS * Seconds */
const CVE_FEED_UPDATE_INTERVAL = 10
const SLACK_LIVE_CHECK = 30
const SLACK_CHANNEL_UPDATE_INTERVAL = 180

/* API docs say 4000 chars, but experimentation
 * suggests we need some buffer room. */
const SLACK_MAX_LENGTH = 3500

var LAST_SLACK_MESSAGE_TIME time.Time

var SLACK_UNLINK_RE1 = regexp.MustCompile("(<https?://([^|]+)\\|([^>]+)>)")
var SLACK_UNLINK_RE2 = regexp.MustCompile("<(https?://[^>]+)>")

var CONFIG = map[string]string{
	"botOwner":             "",
	"byUser":               "",
	"byPassword":           "",
	"channelsFile":         "/var/tmp/jbot.channels",
	"countersFile":         "/var/tmp/jbot.counters",
	"configFile":           "/etc/jbot.conf",
	"debug":                "no",
	"emailDomain":          "",
	"fullName":             "",
	"giphyApiKey":          "",
	"hcControlChannel":     "",
	"hcJabberID":           "",
	"hcOauthToken":         "",
	"hcPassword":           "",
	"hcService":            "",
	"jiraPassword":         "",
	"jiraUser":             "",
	"mentionName":          "",
	"openweathermapApiKey": "",
	"opsgenieApiKey":       "",
	"slackID":              "",
	"slackService":         "",
	"slackToken":           "",
	"SMTP":                 "",
	"timezonedbApiKey":     "",
	"x509Cert":             "",
	"x509Key":              "",
}

var SECRETS = []string{
	"byPassword",
	"hcOauthToken",
	"giphyApiKey",
	"opsgenieApiKey",
	"slackToken",
}

var HIPCHAT_CLIENT *hipchat.Client
var HIPCHAT_ROOMS = map[string]*hipchat.Room{}
var HIPCHAT_ROSTER = map[string]*hipchat.User{}

var SLACK_CLIENT *slack.Client
var SLACK_RTM *slack.RTM

var CURRENTLY_UPDATING_CHANNELS = false
var SLACK_CHANNELS = map[string]slack.Channel{}

/* We usually look up channels by name... */
var CHANNELS = map[string]*Channel{}

/* ...but we also need to look up channels by ID.
 * Even if this is duplication of effort, it seems
 * preferable to get an O(1) lookup with a duplicate
 * data set than doing an O(n) lookup on a single data
 * set. */
var CHANNELS_BY_ID = map[string]*Channel{}

/* Similarly for users.  This is silly, but the Slack
 * API does not appear to have a decent way of
 * searching for a user by username? */
var SLACK_USERS_BY_ID = map[string]slack.User{}
var SLACK_USERS_BY_NAME = map[string]slack.User{}

var COMMANDS = map[string]*Command{}
var COUNTERS = map[string]map[string]int{
	"atnoisers": map[string]int{},
	"commands":  map[string]int{},
	"curses":    map[string]int{},
	"cursers":   map[string]int{},
	"insulted":  map[string]int{},
	"praised":   map[string]int{},
	"replies":   map[string]int{},
	"thanked":   map[string]int{},
	"yubifail":  map[string]int{},
}

var TOGGLES = map[string]bool{
	"chatter":     false,
	"corpbs":      true,
	"python":      true,
	"trivia":      true,
	"shakespeare": true,
	"schneier":    true,
}

var URLS = map[string]string{
	"insults": "http://localhost/quips",
	"jbot":    "https://github.com/jschauma/jbot/",
	"parrots": "http://localhost/parrots",
	"praise":  "http://localhost/praise",
	"pwgen":   "https://www.netmeister.org/pwgen/",
	"speb":    "http://localhost/speb",
	"trivia":  "http://localhost/trivia",
}

var ALERTS = map[string]string{}

var VERBOSITY int

type PhishCount struct {
	Count int
	Total int
	First time.Time
	Last  time.Time
}

const PHISH_MAX = 5
const PHISH_TIME = 1200

type AutoReply struct {
	ReplyString   string
	ReplyThrottle int
}

type Channel struct {
	AutoReplies  map[string]AutoReply
	CVEs         map[string]CVEItem
	Inviter      string
	Id           string
	Name         string
	Toggles      map[string]bool
	Throttles    map[string]time.Time
	Type         string
	HipChatUsers map[hipchat.User]UserInfo
	SlackUsers   map[string]UserInfo
	Settings     map[string]string
	Phishy       *PhishCount
	Verified     bool
}

type CommandFunc func(Recipient, string, []string) string

type Command struct {
	Call    CommandFunc
	Help    string
	How     string
	Usage   string
	Aliases []string
}

type UserInfo struct {
	Count      int
	Curses     int
	CurseWords map[string]int
	Id         string
	Seen       string
	Yubifail   int
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
	ThreadTs    string
}

/*
 * Commands
 */

func addressedToTheBot(in string) bool {
	at_mention := "<@" + CONFIG["slackID"] + ">"
	if strings.EqualFold(in, CONFIG["mentionName"]) ||
		strings.EqualFold(in, "@"+CONFIG["mentionName"]) ||
		strings.EqualFold(in, at_mention) ||
		in == "yourself" {
		return true
	}

	return false
}

func cmdAlerts(r Recipient, chName string, args []string) (result string) {
	chInfo, found := CHANNELS[chName]
	if !found {
		fmt.Fprintf(os.Stderr, ":: alerts: channel %s not found!\n", chName)
		result = "This command only works in a channel."
		return
	}

	if len(args) < 1 {
		result = "Alerts can be used to get periodic notifications about certain events.\n"
		result += "You currently have "
		currentSettings := ""
		for alert, _ := range ALERTS {
			alertSetting, found := chInfo.Settings[alert]
			if found {
				currentSettings += fmt.Sprintf("%s=%s\n", alert, alertSetting)
			}
		}
		if len(currentSettings) > 0 {
			result += "the following alerts set:\n"
			result += currentSettings + "\n"
		} else {
			result += "no alerts set.\n"
		}
		result += "\nYou can also inspect your alert settings via '!set'.\n"
		result += "For each alert, there is an 'alert-counter' in your settings.\n"
		result += "If you want to trigger the alert to be run on the next minute, you can '!unset <alert>-counter'.\n\n"
		result += "The following alerts are possible:\n"
		var keys []string
		for k, _ := range ALERTS {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			result += "- " + k + "\n"
		}
		result += "To learn more about one of them, run '!alerts <alert-name>'.\n"
		result += "\nFinally, to erase an alert, use '!unset <alert>'.\n"
		return
	}

	if _, found := ALERTS[args[0]]; !found {
		result = fmt.Sprintf("No such alert: '%s'. Try just '!alerts'.", args[0])
		return
	}

	switch args[0] {
	case "cmr-alert":
		result = "If you set the 'cmr-alert' settting in your channel, I will look for upcoming or ongoing Change Requests (CHG) in Service Now (aka Change Management Requests or CMRs).\n" +
			"The format of the setting is '<num>[,<property>|all[,ongoing|all]].\n" +
			"'<num>' takes a different meaning whether you're looking for upcoming or ongoing CMRs.\n" +
			"By default, I will look for upcoming CMRs. In that case, '<num>' can be:\n" +
			"<num>   -- the number in minutes in the future until when I should search for upcoming CMRs\n" +
			"<num>h  -- the number in hours in the future until when I should search for upcoming CMRs\n" +
			"<num>d  -- the number in days in the future until when I should search for upcoming CMRs\n\n" +
			"If you do not specify a property, or the property field is 'all', then I will search for CMRs for all properties.\n\n" +
			"If you specify a third third field, then I will look for ongoing CMRs.\n" +
			"An ongoing CMR is one with an Actual Start Date in the past and no Actual End Date.\n" +
			"When searching for ongoing CMRs, the <num> field is the interval in minutes in which I will perform the search.\n\n" +
			"Thus, you can set an alert for CMRs like so:\n" +
			"'!set cmr-alert=1h' would cause me to look for any CMRs coming up in an hour.\n" +
			"        (This is equivalent to running the command '!cmrs' manually every hour.)\n" +
			"'!set cmr-alert=1d,PE-UDB' would cause me to look for any CMRs for the property PE-UDB coming up in the next day.\n" +
			"        (This is equivalent to running the command '!cmrs 1d PE-UDB' manually once a day.)\n" +
			"'!set cmr-alert=30,PE-Index,ongoing' would cause me to look for ongoing CMRs for the property 'PE-Index' every 30 minutes.\n" +
			"        (This is equivalent to running the command '!cmrs ongoing PE-Index' manually every 30 minutes.)\n" +
			"'!set cmr-alert=1h,all,ongoing' would cause me to look for all ongoing CMRs once an hour.\n" +
			"        (This is equivalent to running the command '!cmrs ongoing' manually every 30 hour.)\n"

	case "snow-alert":
		result = "If you set the 'snow-alert' settting in your channel, I will fetch Incident Service-Now tickets on a periodic basis.\n" +
			"The format of the setting is 'n[,<property>].\n" +
			"'n' is the interval in minutes after which I will check for new incidents.\n" +
			"If you specified a 'property'¸ then I will only display new incidents for that property only.\n" +
			"Thus, you can set an alert for new incident tickets like so:\n" +
			"'!set snow-alert=1' would cause me to look for new incident tickets for any property every minute.\n" +
			"'!set snow-alert=10,AdvDataHighway.US' would cause me to look for new tickets for the AdvDataHighway.US property every 10 minutes.\n"

	case "jira-alert":
		if len(args) == 1 || args[1] == "help" {
			result = "If you set the 'jira-alert' setting in your channel, I will run the given filter and display matching tickets on a periodic basis.\n" +
				"The format of the setting is 'n,<filterid>.\n" +
				"'n' is the interval in minutes after which I will run the jira query.\n" +
				"'filterid' is the Jira filter ID I should run\n" +
				"This requires you to have defined your Jira search as a public filter.\n" +
				"\nYou can set multiple alerts by specifying multiple 'n,<filterId>' pairs separated by semicolons.\n" +
				"For example, to run filter 1234 every 5 minutes and filter 9876 every 15 minutes:\n" +
				"!set jira-alert=5,1234;15,9876\n" +
				"\nTo display the names and URLs of the currently set filters, run '!alerts jira-alert info'.\n"
		} else if args[1] == "info" {
			jiraAlert(*chInfo, true)
		}
	}

	return
}

func cmdAsn(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["asn"].Usage
		return
	}

	arg := args[0]
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

func cmdBacon(r Recipient, chName string, args []string) (result string) {
	pic := false
	query := "bacon"
	if len(args) > 0 {
		query += " " + strings.Join(args, " ")
		pic = true
	}

	rand.Seed(time.Now().UnixNano())
	if pic || rand.Intn(4) == 0 {
		result = cmdImage(r, chName, []string{query})
	} else {
		data := getURLContents("https://baconipsum.com/?paras=1&type=all-meat", nil)
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

func cmdBs(r Recipient, chName string, args []string) (result string) {

	answer := ""

	rand.Seed(time.Now().UnixNano())
	var s = []string{
		fmt.Sprintf("Well, <@%s>, I think you should probably", r.Id),
		"A better idea:",
		"Or perhaps",
		"Y'all should",
		"Here's an idea:",
		"To remain competitive, we need to",
		"In order to succeed, we must",
		"Team! Let's",
		"Uhm, lemme think for a second there. How about:",
	}

	answer = s[rand.Intn(len(s))] + " "

	var adverbs = []string{
		"appropriately",
		"assertively",
		"authoritatively",
		"collaboratively",
		"compellingly",
		"competently",
		"completely",
		"continually",
		"conveniently",
		"credibly",
		"distinctively",
		"dramatically",
		"dynamically",
		"efficiently",
		"energistically",
		"enthusiastically",
		"fungibly",
		"globally",
		"holisticly",
		"interactively",
		"intrinsically",
		"monotonectally",
		"objectively",
		"phosfluorescently",
		"proactively",
		"professionally",
		"progressively",
		"quickly",
		"rapidiously",
		"seamlessly",
		"synergistically",
		"uniquely",
	}

	var verbs = []string{
		"actualize",
		"administrate",
		"aggregate",
		"architect",
		"benchmark",
		"brand",
		"build",
		"cloudify",
		"communicate",
		"conceptualize",
		"coordinate",
		"create",
		"cultivate",
		"customize",
		"deliver",
		"deploy",
		"develop",
		"disintermediate",
		"disseminate",
		"drive",
		"e-enable",
		"embrace",
		"empower",
		"enable",
		"engage",
		"engineer",
		"enhance",
		"envisioneer",
		"evisculate",
		"evolve",
		"expedite",
		"exploit",
		"extend",
		"fabricate",
		"facilitate",
		"fashion",
		"formulate",
		"foster",
		"generate",
		"grow",
		"harness",
		"impact",
		"implement",
		"incentivize",
		"incubate",
		"initiate",
		"innovate",
		"integrate",
		"iterate",
		"leverage existing",
		"leverage other's",
		"maintain",
		"matrix",
		"maximize",
		"mesh",
		"monetize",
		"morph",
		"myocardinate",
		"negotiate",
		"network",
		"optimize",
		"orchestrate",
		"paralleltask",
		"plagiarize",
		"pontificate",
		"predominate",
		"procrastinate",
		"productivate",
		"productize",
		"promote",
		"provide access to",
		"pursue",
		"re-engineer",
		"recaptiualize",
		"reconceptualize",
		"redefine",
		"reintermediate",
		"reinvent",
		"repurpose",
		"restore",
		"revolutionize",
		"right-shore",
		"scale",
		"seize",
		"simplify",
		"strategize",
		"streamline",
		"supply",
		"syndicate",
		"synergize",
		"synthesize",
		"target",
		"transform",
		"transition",
		"underwhelm",
		"unleash",
		"utilize",
		"visualize",
		"whiteboard",
	}

	var adjectives = []string{
		"24/365",
		"24/7",
		"B2B",
		"B2C",
		"accurate",
		"adaptive",
		"agile",
		"alternative",
		"an expanded array of",
		"backend",
		"backward-compatible",
		"best-of-breed",
		"bleeding-edge",
		"bricks-and-clicks",
		"business",
		"clicks-and-mortar",
		"client-based",
		"client-centered",
		"client-centric",
		"client-focused",
		"cloud-based",
		"cloud-centric",
		"cloud-ready",
		"cloudified",
		"collaborative",
		"compelling",
		"competitive",
		"cooperative",
		"corporate",
		"costeffective",
		"covalent",
		"cross-media",
		"cross-platform",
		"cross-unit",
		"crossfunctional",
		"customer directed",
		"customized",
		"cutting-edge",
		"distinctive",
		"distributed",
		"diverse",
		"dynamic",
		"e-business",
		"economically sound",
		"effective",
		"efficient",
		"elastic",
		"emerging",
		"empowered",
		"enabled",
		"end-to-end",
		"enterprise",
		"enterprise-wide",
		"equity invested",
		"error-free",
		"ethical",
		"excellent",
		"exceptional",
		"extensible",
		"extensive",
		"flexible",
		"focused",
		"frictionless",
		"front-end",
		"fully researched",
		"fully tested",
		"functional",
		"functionalized",
		"fungible",
		"future-proof",
		"global",
		"goal-oriented",
		"goforward",
		"granular",
		"high-payoff",
		"high-quality",
		"highly efficient",
		"high standards in",
		"holistic",
		"hyper-scale",
		"impactful",
		"inexpensive",
		"innovative",
		"installedbase",
		"integrated",
		"interactive",
		"interdependent",
		"intermandated",
		"interoperable",
		"intuitive",
		"justintime",
		"leading-edge",
		"leveraged",
		"long-termhigh-impact",
		"low-riskhigh-yield",
		"magnetic",
		"maintainable",
		"market-driven",
		"market positioning",
		"mission-critical",
		"multidisciplinary",
		"multifunctional",
		"multimedia based",
		"next-generation",
		"on-demand",
		"one-to-one",
		"open-source",
		"optimal",
		"orthogonal",
		"out-of-the-box",
		"pandemic",
		"parallel",
		"performancebased",
		"plug-and-play",
		"premier",
		"premium",
		"principle-centered",
		"proactive",
		"process-centric",
		"professional",
		"progressive",
		"prospective",
		"quality",
		"real-time",
		"reliable",
		"resource-leveling",
		"resource-maximizing",
		"resource-sucking",
		"revolutionary",
		"robust",
		"scalable",
		"seamless",
		"stand-alone",
		"standardized",
		"standardscompliant",
		"stateoftheart",
		"sticky",
		"strategic",
		"superior",
		"sustainable",
		"synergistic",
		"tactical",
		"teambuilding",
		"teamdriven",
		"technicallysound",
		"timely",
		"top-line",
		"transparent",
		"turnkey",
		"ubiquitous",
		"unique",
		"user-centric",
		"userfriendly",
		"value-added",
		"vertical",
		"viral",
		"virtual",
		"visionary",
		"web-enabled",
		"wireless",
		"world-class",
		"worldwide",
	}

	var nouns = []string{
		"'outsidethebox' thinking",
		"IoT",
		"ROI",
		"actionitems",
		"alignments",
		"applications",
		"architectures",
		"bandwidth",
		"benefits",
		"best practices",
		"blockchain",
		"catalysts for change",
		"channels",
		"clouds",
		"collaborationandidea-sharing",
		"communities",
		"content",
		"convergence",
		"core competencies",
		"crypto currencies",
		"customer service",
		"data",
		"deliverables",
		"e-business",
		"e-commerce",
		"e-markets",
		"e-services",
		"e-tailers",
		"experiences",
		"expertise",
		"functionalities",
		"fungibility",
		"growth strategies",
		"human capital",
		"ideas",
		"imperatives",
		"infomediaries",
		"information",
		"infrastructures",
		"initiatives",
		"innovation",
		"intellectual capital",
		"interfaces",
		"internal or 'organic' sources",
		"leadership",
		"leadership skills",
		"manufactured products",
		"markets",
		"materials",
		"meta-services",
		"methodologies",
		"methods of empowerment",
		"metrics",
		"mindshare",
		"models",
		"networks",
		"niche markets",
		"niches",
		"nosql",
		"opportunities",
		"outsourcing",
		"paradigms",
		"partnerships",
		"platforms",
		"portals",
		"potentialities",
		"processes",
		"process improvements",
		"products",
		"quality vectors",
		"relationships",
		"resources",
		"results",
		"scenarios",
		"schemas",
		"scrums",
		"services",
		"solutions",
		"sources",
		"sprints",
		"storage",
		"strategic theme areas",
		"supplychains",
		"synergy",
		"systems",
		"technologies",
		"technology",
		"testing procedures",
		"total linkage",
		"users",
		"value",
		"virtualization",
		"vortals",
		"web-readiness",
		"webservices",
		"wins",
	}

	answer += adverbs[rand.Intn(len(adverbs))] + " " +
		verbs[rand.Intn(len(verbs))] + " " +
		adjectives[rand.Intn(len(adjectives))] + " " +
		nouns[rand.Intn(len(nouns))] + "!"

	result = answer

	return
}

func cmdCert(r Recipient, chName string, args []string) (result string) {
	names := args
	if len(args) < 1 || len(args) > 3 {
		result = "Usage: " + COMMANDS["cert"].Usage
		return
	}

	names[0] = strings.TrimPrefix(names[0], "https://")
	names[0] = strings.TrimSuffix(names[0], "/")

	ipv6 := false
	ipv6_re := regexp.MustCompile(`(?i)^\[?([a-f0-9:]+)\]?(:[0-9]+)?$`)
	m := ipv6_re.FindStringSubmatch(names[0])
	if len(m) > 0 {
		ipv6 = true
	} else {
		name_port_re := regexp.MustCompile(`(?i)^([^: ]+)(:[0-9]+)?$`)
		m = name_port_re.FindStringSubmatch(names[0])
		if len(m) < 1 {
			result = "Invalid argument. Try an FQDN followed by an optional port.\n"
			result += "For example: www.yahoo.com:443\n"
			return
		}
	}

	if len(m[2]) < 1 {
		if ipv6 {
			names[0] = fmt.Sprintf("[%s]:443", names[0])
		} else {
			names[0] += ":443"
		}
	}

	/* This call is intended to show information
	 * about the cert, even if the cert is not
	 * valid, so here we actually ignore cert
	 * errors for once. */
	config := &tls.Config{InsecureSkipVerify: true}

	chain := false
	if len(names) > 1 {
		if names[1] == "all" || names[1] == "chain" {
			chain = true
		} else {
			config = &tls.Config{InsecureSkipVerify: true, ServerName: names[1]}
		}

		if len(names) == 3 {
			chain = true
		}
	}

	conn, err := tls.Dial("tcp", names[0], config)
	if err != nil {
		result = fmt.Sprintf("Unable to make a TLS connection to '%s'.\n", names[0])
		return
	}

	for n, c := range conn.ConnectionState().PeerCertificates {
		if chain {
			result += fmt.Sprintf("Certificate %d:\n", n)
		}
		result += "```\n"
		result += fmt.Sprintf("Serial Number: ")
		hex := fmt.Sprintf("%x", c.SerialNumber)
		if len(hex)%2 != 0 {
			hex = "0" + hex
		}
		for i, b := range hex {
			if i > 0 && i%2 == 0 {
				result += fmt.Sprintf(":")
			}
			result += fmt.Sprintf("%s", string(b))
		}
		result += fmt.Sprintf("\n")

		result += fmt.Sprintf("Subject      : %s\n", c.Subject)
		result += fmt.Sprintf("Issuer       : %s\n", c.Issuer)

		if c.Subject.String() == c.Issuer.String() {
			result += "Note         : SELF-SIGNED\n"
		}

		result += "Validity     : "
		now := time.Now()
		if now.Before(c.NotBefore) {
			result += "NOT YET"
		} else if now.After(c.NotAfter) {
			result += "EXPIRED"
		}
		result += "\n"

		result += fmt.Sprintf("   Not Before: %s\n", c.NotBefore)
		result += fmt.Sprintf("   Not After : %s\n", c.NotAfter)
		if len(c.DNSNames) > 0 {
			result += fmt.Sprintf("%d SANs:\n%s\n", len(c.DNSNames), strings.Join(c.DNSNames, " "))
		}
		result += "```\n"

		if !chain {
			break
		}
	}

	return
}

func cmdChannels(r Recipient, chName string, args []string) (result string) {
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
			slackChannels = append(slackChannels, chInfo.Name)
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

func cmdCidr(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["cidr"].Usage
		return
	}

	input := args[0]

	/* We're lazy here, but good enough. */
	if !strings.Contains(input, "/") {
		if strings.Contains(input, ":") {
			input += "/128"
		} else {
			input += "/32"
		}
	}
	ip, ipnet, err := net.ParseCIDR(input)
	if err != nil {
		result = fmt.Sprintf("'%s' does not look like a valid CIDR to me.", input)
		return
	}

	result = fmt.Sprintf("Host address: %s\n", ip.String())
	ones, bits := ipnet.Mask.Size()
	diff := bits - ones
	num := math.Exp2(float64(diff))
	first := ip.Mask(ipnet.Mask)

	var last uint32
	isv4 := ip.To4()

	if isv4 != nil {
		ipint := big.NewInt(0)
		ipint.SetBytes(first.To4())
		decip := ipint.Int64()
		last = uint32(decip + int64(num) - 1)

		result += fmt.Sprintf("Host address (decimal): %d\n", decip)
		result += fmt.Sprintf("Host address (hex): %X\n", ipint.Int64())

		if len(ipnet.Mask) == 4 {
			result += fmt.Sprintf("Network mask (decimal): %d.%d.%d.%d\n", ipnet.Mask[0], ipnet.Mask[1], ipnet.Mask[2], ipnet.Mask[3])
		}
		result += fmt.Sprintf("Network mask (hex): %s\n", ipnet.Mask)
	} else {
		result += fmt.Sprintf("Prefix length: %d\n", ones)
	}

	result += fmt.Sprintf("Addresses in network: %0.f\n", num)
	result += fmt.Sprintf("Network address: %s\n", first)
	if isv4 != nil {
		brip := make(net.IP, 4)
		binary.BigEndian.PutUint32(brip, last)
		result += fmt.Sprintf("Broadcast address: %s\n", brip)
	}

	if ip.IsGlobalUnicast() {
		result += fmt.Sprintf("Type: global unicast\n")
	}
	if ip.IsInterfaceLocalMulticast() {
		result += fmt.Sprintf("Type: interface-local multicast\n")
	}
	if ip.IsLinkLocalMulticast() {
		result += fmt.Sprintf("Type: link-local multicast\n")
	}
	if ip.IsLinkLocalUnicast() {
		result += fmt.Sprintf("Type: link-local unicast\n")
	}
	if ip.IsMulticast() {
		result += fmt.Sprintf("Type: multicast\n")
	}

	return
}

func cmdClear(r Recipient, chName string, args []string) (result string) {
	count := 24

	if len(args) > 0 {
		if _, err := fmt.Sscanf(args[0], "%d", &count); err != nil {
			result = cmdInsult(r, chName, []string{"me"})
			return
		}
	}
	if count < 1 {
		result = cmdInsult(r, chName, []string{"me"})
		return
	}

	if count > 40 {
		result = "I'm not going to clear more than 40 lines."
		return
	}

	n := 0
	rcount := count
	result = "```\n"
	for n < count {
		i := rcount
		for i > 0 {
			result += "."
			i--
		}

		result += "\n"
		if rcount == 9 {
			cowsay := cmdCowsay(r, chName, []string{"clear"})
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

func cmdCowsay(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["cowsay"].Usage
		return
	}

	out, _ := runCommand("cowsay " + strings.Join(args, " "))
	result += "```\n" + string(out) + "```\n"

	return
}

func cmdCurses(r Recipient, chName string, args []string) (result string) {
	result = getCountable("curses", chName, r, args)
	return
}

func cmdEightBall(r Recipient, chName string, args []string) (result string) {
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

func cmdFml(r Recipient, chName string, args []string) (result string) {
	if len(args) > 0 {
		result = "Usage: " + COMMANDS["fml"].Usage
		return
	}

	data := getURLContents(COMMANDS["fml"].How, nil)

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

func cmdFortune(r Recipient, chName string, args []string) (result string) {
	if len(args) > 0 {
		result = "Usage: " + COMMANDS["fortune"].Usage
		return
	}

	out, _ := runCommand("fortune -s")
	result = string(out)

	return
}

func cmdGiphy(r Recipient, chName string, args []string) (result string) {
	key := CONFIG["giphyApiKey"]
	if len(key) < 1 {
		result = "Sorry - no giphy API key in config file!\n"
		result += "Try '!img' instead?\n"
		result += "I know, it's not the same..."
		return
	}

	theUrl := COMMANDS["giphy"].How
	if len(args) < 1 {
		theUrl = strings.Replace(theUrl, "search", "random?", 1)
	} else {
		if args[0] == "jbot" {
			result = "https://jbot.corp.yahoo.com/jbot.gif"
			return
		}
		theUrl += "?q=" + url.QueryEscape(strings.Join(args, " "))
	}

	theUrl += "&api_key=" + url.QueryEscape(key)
	theUrl += "&rating=g&limit=30"
	data := getURLContents(theUrl, nil)

	var giphyJson map[string]interface{}
	err := json.Unmarshal(data, &giphyJson)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal giphy data: %s\n", err)
		return
	}

	if _, found := giphyJson["meta"]; !found {
		fmt.Fprintf(os.Stderr, "+++ giphy fail: %v\n", giphyJson)
		result = fmt.Sprintf("No data received from giphy!")
		return
	}

	status := giphyJson["meta"].(map[string]interface{})["status"].(float64)

	if status != 200 {
		fmt.Fprintf(os.Stderr, "+++ giphy return status %f: %v\n", status, giphyJson)
		result = fmt.Sprintf("Giphy responded with a non-200 status code!")
		return
	}

	rand.Seed(time.Now().UnixNano())
	var images map[string]interface{}
	giphyData, sOk := giphyJson["data"].([]interface{})
	if sOk {
		n := giphyJson["pagination"].(map[string]interface{})["count"].(float64)
		if n == 0 {
			result = fmt.Sprintf("No giphy results found for '%s'.", strings.Join(args, " "))
			return
		}
		element := giphyData[rand.Intn(int(n))].(map[string]interface{})
		images = element["images"].(map[string]interface{})
	} else {
		giphyMap := giphyJson["data"].(map[string]interface{})
		images = giphyMap["images"].(map[string]interface{})
	}
	fixed_height := images["fixed_height"].(map[string]interface{})
	result = fixed_height["url"].(string)

	return
}

func cmdHelp(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = fmt.Sprintf("I know %d commands.\n"+
			"Use '!help all' to show all commands.\n"+
			"Ask me about a specific command via '!help <cmd>'.\n"+
			"If you find me annoyingly chatty, just '!toggle chatter'.\n",
			len(COMMANDS))
		result += "To ask me to leave a channel, say '!leave'.\n"
		result += "If you need any other help or have suggestions or complaints, find support in #yaybot.\n"
		return
	}

	if args[0] == "all" {
		var cmds []string
		result = "These are the commands I know:\n"
		for c := range COMMANDS {
			cmds = append(cmds, c)
		}
		sort.Strings(cmds)
		result += strings.Join(cmds, ", ")
		return
	}

	for _, cmd := range args {
		if c, found := COMMANDS[cmd]; found {
			result = printCommandHelp(cmd, c)
			return
		}

		for invocation, c := range COMMANDS {
			for _, a := range c.Aliases {
				if cmd == a {
					result = printCommandHelp(invocation, c)
					return
				}
			}

			/* 35 to account for 'No such command...' */
			if len(cmd) >= (SLACK_MAX_LENGTH - 35) {
				result = cmdInsult(r, chName, []string{"me"})
			} else {
				result = fmt.Sprintf("No such command: %s. Try '!help'.", cmd)
			}
		}
	}
	return
}

func cmdHost(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["host"].Usage
		return
	}

	out, _ := runCommand(fmt.Sprintf("host %s", args[0]))
	result = string(out)

	return
}

func cmdHow(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["how"].Usage
		return
	}

	if _, found := COMMANDS[args[0]]; found {
		result = COMMANDS[args[0]].How
	} else if strings.EqualFold(args[0], CONFIG["mentionName"]) {
		result = URLS["jbot"]
	} else {
		rand.Seed(time.Now().UnixNano())
		result = DONTKNOW[rand.Intn(len(DONTKNOW))]
	}

	return
}

func cmdImage(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["img"].Usage
		return
	}

	theUrl := fmt.Sprintf("%s%s", COMMANDS["img"].How, url.QueryEscape(args[0]))
	data := getURLContents(theUrl, nil)

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

func cmdInfo(r Recipient, chName string, args []string) (result string) {
	var subject string
	if len(args) != 1 {
		subject = r.ReplyTo
	} else {
		subject = args[0]
	}

	slack_channel_re := regexp.MustCompile(`(?i)<(#[A-Z0-9]+)\|([^>]+)>`)
	m := slack_channel_re.FindStringSubmatch(subject)
	if len(m) > 0 {
		result = getChannelInfo(m[1])
		subject = m[2]
	} else {
		result = getChannelInfo(subject)
	}

	subject = strings.ToLower(subject)
	if ch, found := getChannel(r.ChatType, subject); found {
		result += fmt.Sprintf("I was invited into #%s by %s.\n", ch.Name, ch.Inviter)
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

		stfu := cmdStfu(r, ch.Name, []string{})
		if len(stfu) > 0 {
			result += fmt.Sprintf("\nTop 10 channel chatterers for #%s:\n", ch.Name)
			result += fmt.Sprintf("%s", stfu)
		}

		toggles := cmdToggle(r, ch.Name, []string{})
		if len(toggles) > 0 {
			result += fmt.Sprintf("\n%s", toggles)
		}

		throttles := cmdThrottle(r, ch.Name, []string{})
		if len(throttles) > 0 {
			result += fmt.Sprintf("\n%s", throttles)
		}

		settings := cmdSet(r, ch.Name, []string{})
		if !strings.HasPrefix(settings, "There currently are no settings") {
			result += "\nThese are the channel settings:\n"
			result += settings
		}

		autoReplies := cmdAutoreply(r, ch.Name, []string{})
		result += "\n" + autoReplies + "\n"
	} else {
		result += "I'm not currently in #" + subject
	}
	return
}

func cmdInsult(r Recipient, chName string, args []string) (result string) {
	var insultee string
	if len(args) > 0 {
		insultee = strings.Join(args, " ")
	}
	if addressedToTheBot(insultee) || insultee == "me" {
		incrementCounter("insulted", r.MentionName)
		result = fmt.Sprintf("<@%s>: ", r.Id)
	}

	if len(result) < 1 && len(insultee) > 0 {
		incrementCounter("insulted", insultee)
		result = fmt.Sprintf("%s: ", insultee)
	}

	rand.Seed(time.Now().UnixNano())
	if rand.Intn(2) == 0 {
		url := URLS["insults"]
		result += randomLineFromUrl(url)
	} else {
		data := getURLContents(COMMANDS["insult"].How, nil)
		found := false
		insult_re := regexp.MustCompile(`^<p><font size="\+2">`)
		for _, line := range strings.Split(string(data), "\n") {
			if insult_re.MatchString(line) {
				found = true
				continue
			}
			if found {
				result += gothicText(dehtmlify(line))
				break
			}
		}
	}

	return
}

func cmdLatLong(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["latlong"].Usage
		return
	}

	client := &http.Client{}

	v := url.Values{}
	v.Add("action", "gpcm")
	v.Add("c1", args[0])

	latlongURL := COMMANDS["latlong"].How + "_spm4.php"
	req, err := http.NewRequest("POST", latlongURL, strings.NewReader(v.Encode()))
	if err != nil {
		result = fmt.Sprintf("Unable to create a new POST request: %s", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := client.Do(req)
	if err != nil {
		result = fmt.Sprintf("Unable to post data to %s: %s", latlongURL, err)
		return
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		result = fmt.Sprintf("Unable to read body of '%s': %s", latlongURL, err)
		return
	}

	result = string(data)
	return
}

func cmdLog(r Recipient, chName string, args []string) (result string) {
	var room string
	if r.ChatType == "hipchat" {
		room = r.ReplyTo
	} else if r.ChatType == "slack" {
		room = chName
	}

	if len(args) > 1 {
		result = "Usage: " + COMMANDS["log"].Usage
		return
	} else if len(args) == 1 {
		room = args[0]
	}

	roomInfo := cmdRoom(r, chName, []string{room})

	if strings.Contains(roomInfo, "https://") {
		result = roomInfo[strings.Index(roomInfo, "https://"):]
	} else {
		result = fmt.Sprintf("No log URL found for '%s'.", r.ReplyTo)
	}
	return
}

func cmdMan(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["man"].Usage
		return
	}

	if args[0] == "woman" {
		rand.Seed(time.Now().UnixNano())
		replies := []string{
			"That's not very original, now is it?",
			":face_with_rolling_eyes:",
			"Good one. Never seen that before.",
			"What's next? 'make love'?",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	var section string
	if len(args) == 2 {
		section = url.QueryEscape(args[0])
	}

	cmd := url.QueryEscape(args[len(args)-1])

	if len(section) > 0 {
		result = getManResults(section, cmd)
	} else {

		sections := []string{"1", "1p", "2", "2p", "3", "3p", "4", "4p", "5", "5p", "6", "6p", "7", "7p", "8", "8p"}

		for _, section := range sections {
			result = getManResults(section, cmd)
			if len(result) > 0 {
				break
			}
		}
	}

	if len(result) < 1 {
		result = "Sorry, no manual page found."
	}

	return
}

func cmdMonkeyStab(r Recipient, chName string, args []string) (result string) {
	var stabbee string
	if len(args) > 0 {
		stabbee = strings.Join(args, " ")
	}
	if len(args) < 1 || addressedToTheBot(stabbee) || stabbee == "me" {
		stabbee = fmt.Sprintf("<@%s>", r.Id)
	}

	result = fmt.Sprintf("_unleashes a troop of pen-wielding stabbing-monkeys on %s!_\n", stabbee)
	return
}

func cmdOid(r Recipient, chName string, args []string) (result string) {
	oids := args
	if len(oids) != 1 {
		result = "Usage: " + COMMANDS["oid"].Usage
		return
	}

	oid := strings.TrimSpace(oids[0])

	theUrl := fmt.Sprintf("%s%s", COMMANDS["oid"].How, oid)
	urlArgs := map[string]string{"ua": "true"}
	data := getURLContents(theUrl, urlArgs)

	info_key := ""
	found := false
	info := map[string]string{}

	asn_re := regexp.MustCompile(`(?i)^\s*<textarea.*readonly>({.*})</textarea>`)
	info_re := regexp.MustCompile(`(?i)^\s*<br><strong>(.*)</strong>:`)

	for _, line := range strings.Split(string(data), "\n") {
		if m := asn_re.FindStringSubmatch(line); len(m) > 0 {
			info["ASN.1 notation"] = m[1]
			continue
		}

		if m := info_re.FindStringSubmatch(line); len(m) > 0 {
			found = true
			info_key = m[1]
			continue
		}

		if strings.Contains(line, "<br><br>") {
			found = false
			if info_key == "Information" {
				break
			} else {
				continue
			}
		}

		if found {
			oneLine := dehtmlify(line)
			if len(oneLine) > 1 {
				if _, ok := info[info_key]; !ok {
					info[info_key] = oneLine
				} else {
					info[info_key] += "\n" + oneLine
				}
			}
		}
	}

	if len(info) < 1 {
		result = fmt.Sprintf("No info found for OID '%s'.", oid)
	} else {
		var keys []string
		for k, _ := range info {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			result += fmt.Sprintf("%s: %s\n", k, info[k])
		}
	}

	return
}

func cmdOnion(r Recipient, chName string, args []string) (result string) {
	search := false
	theUrl := COMMANDS["onion"].How + "rss"

	if len(args) > 0 {
		theUrl = fmt.Sprintf("%ssearch?blogId=1636079510&q=%s", COMMANDS["onion"].How, url.QueryEscape(strings.Join(args, " ")))
		search = true
	}

	data := getURLContents(theUrl, nil)

	if !search {
		items := strings.Split(string(data), "<item>")
		rss_re := regexp.MustCompile(`^<title><\!\[CDATA\[(.*)\]\]></title><link>(.*)</link>`)
		for _, item := range items {
			m := rss_re.FindStringSubmatch(item)
			if len(m) > 0 {
				result += m[1] + " - " + m[2] + "\n"
				return
			}
		}
	}

	search_re := regexp.MustCompile(`a class="js_link.*href="(.*)"><h2[^>]*>([^<]+)<`)
	for _, line := range strings.Split(string(data), "<div>") {
		m := search_re.FindStringSubmatch(line)
		if len(m) > 0 {
			result = m[2] + " - " + m[1]
			return
		}
	}

	result = fmt.Sprintf("No results found on '%s'.", theUrl)
	return
}

func cmdPing(r Recipient, chName string, args []string) (result string) {
	ping := "ping"
	hosts := args
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
				fmt.Sprintf("Hey, @%s, <@%s> is looking for you.", hosts[0], r.Id),
				fmt.Sprintf("_nudges %s._", hosts[0]),
				fmt.Sprintf("_pings %s._", hosts[0]),
				fmt.Sprintf("_pokes %s._", hosts[0]),
				fmt.Sprintf("_taps %s on the head._", hosts[0]),
			}
			result = replies[rand.Intn(len(replies))]
		}
		return
	}

	/* Alright, alright, we're being lazy here,
	 * but treating anything with a ':' as an IPv6
	 * address is actually good enough. */
	if strings.Contains(host, ":") {
		ping = "ping6"
		/* Yahoo only. :-/ */
		result = "Sorry, I'm running on an IPv4 only system."
		result += "\nI know, I know, that's quite silly, but it is what it is."
		return
	}

	_, err := runCommand(fmt.Sprintf("%s -q -w 1 -W 0.5 -i 0.5 -c 1 %s", ping, host))
	if err > 0 {
		result = fmt.Sprintf("Unable to ping %s.", hosts[0])
	} else {
		result = fmt.Sprintf("%s is alive.", hosts[0])
	}

	return
}

func cmdPraise(r Recipient, chName string, args []string) (result string) {
	if _, found := CHANNELS[chName]; !found {
		fmt.Fprintf(os.Stderr, ":: praise: channel %s not found!\n", chName)
		result = "This command only works in a channel."
		return
	}

	if len(args) < 1 {
		result = "Usage: " + COMMANDS["praise"].Usage
		return
	}

	praisee := strings.Join(args, " ")
	expandedUser := expandSlackUser(praisee)
	if expandedUser != nil && expandedUser.ID != "" {
		praisee = expandedUser.Name
	}
	if strings.EqualFold(praisee, "me") ||
		strings.EqualFold(praisee, "myself") ||
		strings.EqualFold(praisee, r.MentionName) {
		result = cmdInsult(r, chName, []string{"me"})
		return
	}

	incrementCounter("praised", praisee)
	if addressedToTheBot(praisee) {
		rand.Seed(time.Now().UnixNano())
		result = THANKYOU[rand.Intn(len(THANKYOU))]
	} else {
		result = fmt.Sprintf("%s: %s\n", praisee,
			randomLineFromUrl(COMMANDS["praise"].How))
	}
	return
}

func cmdPwgen(r Recipient, chName string, args []string) (result string) {
	if len(args) > 3 {
		result = "Usage: " + COMMANDS["pwgen"].Usage
		return
	}

	theUrl := COMMANDS["pwgen"].How + "?nohtml=1"
	var i int
	lines := 1

	for n, a := range args {
		if _, err := fmt.Sscanf(a, "%d", &i); err != nil {
			result = "'" + a + "' does not look like a number to me."
			return
		}
		if i < 0 || i > 50 {
			result = "Please try a number between 0 and 50."
			return
		}

		if n == 0 {
			theUrl += "&num=" + a
		} else if n == 1 {
			theUrl += "&count=" + a
			lines = i
		} else {
			theUrl += "&complex=1"
		}
	}

	data := string(getURLContents(theUrl, nil))
	for n, line := range strings.Split(string(data), "\n") {
		if n < lines {
			result += line + "\n"
		}
	}
	return
}

func cmdQuote(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["quote"].Usage
		return
	}

	subject := strings.Join(args, " ")

	result = fmt.Sprintf("\"%s\"", subject)

	subject = strings.ToUpper(subject)
	theURL := fmt.Sprintf("%s%s", COMMANDS["quote"].How, url.QueryEscape(subject))
	data := getURLContents(theURL, nil)

	type Quote struct {
		FullExchangeName           string
		FiftyTwoWeekRange          struct{ Fmt string }
		RegularMarketPreviousClose struct{ Fmt string }
		RegularMarketOpen          struct{ Fmt string }
		RegularMarketDayRange      struct{ Fmt string }
		ShortName                  string
	}

	type YahooFinance struct {
		Context struct {
			Dispatcher struct {
				Stores struct {
					StreamDataStore struct {
						QuoteData map[string]Quote
					}
				}
			}
		}
	}

	var jsonString string
	re := regexp.MustCompile(`(?i).*root.App.main = (.*});`)
	for _, l := range strings.Split(string(data), "\n") {
		if m := re.FindStringSubmatch(l); len(m) > 0 {
			jsonString = m[1]
			break
		}
	}

	if len(jsonString) < 1 {
		result = fmt.Sprintf("Unable to get json data from '%s'.", theURL)
		return
	}

	var y YahooFinance
	err := json.Unmarshal([]byte(jsonString), &y)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal json data: %s\n", err)
		return
	}

	for q, d := range y.Context.Dispatcher.Stores.StreamDataStore.QuoteData {
		if q == subject {
			result = fmt.Sprintf("<%s|%s (%s)> trading on '%s':\n```", theURL, q, d.ShortName, d.FullExchangeName)
			result += fmt.Sprintf("Previous Close: $%s\n", d.RegularMarketPreviousClose.Fmt)
			result += fmt.Sprintf("Open          : $%s\n", d.RegularMarketOpen.Fmt)
			result += fmt.Sprintf("Day Range     : $%s\n", d.RegularMarketDayRange.Fmt)
			result += fmt.Sprintf("52 Week Range : $%s\n```", d.FiftyTwoWeekRange.Fmt)
		}
	}
	return
}

func cmdRfc(r Recipient, chName string, args []string) (result string) {
	rfcs := args
	if len(rfcs) != 1 {
		result = "Usage: " + COMMANDS["rfc"].Usage
		return
	}

	rfc := strings.ToLower(strings.TrimSpace(rfcs[0]))

	if !strings.HasPrefix(rfc, "rfc") {
		rfc = "rfc" + rfc
	}

	theUrl := fmt.Sprintf("%s%s", COMMANDS["rfc"].How, rfc)
	data := getURLContents(theUrl, nil)

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

func cmdRoll(r Recipient, chName string, args []string) (result string) {
	if len(args) > 3 {
		result = "Usage: " + COMMANDS["roll"].Usage
		return
	}

	num := 1
	min := 1
	max := 7

	for i, _ := range args {
		if n, err := strconv.Atoi(args[i]); err == nil {
			if i == 0 {
				num = n
			} else if i == 1 {
				max = n
			} else if i == 2 {
				min = n
			}

		} else {
			result = fmt.Sprintf("Argument %d must be an integer.", i+1)
			return
		}
	}

	if min == max {
		result = fmt.Sprintf("Range (%d - %d) must be >0.", min, max)
		return
	}
	if min > max {
		n := max
		max = min
		min = n
	}

	var nums []int
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < num; i++ {
		nums = append(nums, rand.Intn(max-min)+min)
	}
	sort.Ints(nums)

	for i, n := range nums {
		result += fmt.Sprintf("%d", n)
		if len(nums) > 1 && i < (len(nums)-1) {
			result += ", "
		}
	}

	return
}

func cmdRoom(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["room"].Usage
		return
	}

	listUsers := false
	users_re := regexp.MustCompile(`(?i)^(list-?)?users$`)
	if len(args) == 2 && users_re.MatchString(args[1]) {
		listUsers = true
	}

	room := strings.TrimSpace(args[0])
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
					candidates = append(candidates, &roomTopic{aRoom.Name, aRoom.Topic})
				}
			}
		}
	} else if r.ChatType == "slack" {
		for _, ch := range SLACK_CHANNELS {
			lc := strings.ToLower(ch.Name)
			if lc == lroom {
				users := getAllMembersInChannel(ch.ID)
				if listUsers {
					slackers := []string{}
					for _, u := range users {
						su := getSlackUser(u)
						slackers = append(slackers, su.Name)
					}
					sort.Strings(slackers)
					result = fmt.Sprintf("There are %d users in #%s:\n", len(slackers), ch.Name)
					result += strings.Join(slackers, ", ")
					return
				}

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
				result += fmt.Sprintf("# of members: %d\n", len(users))
				result += fmt.Sprintf("https://%s/messages/%s/\n", CONFIG["slackService"], lroom)
				return
			} else if strings.Contains(lc, lroom) {
				candidates = append(candidates, &roomTopic{ch.Name, ch.Topic.Value})
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

func cmdSeen(r Recipient, chName string, args []string) (result string) {
	wanted := args
	user := wanted[0]
	verbose(4, "Looking in %s", r.ReplyTo)

	ch, found := getChannel(r.ChatType, r.ReplyTo)

	if len(wanted) > 1 {
		chName = wanted[1]
		slack_channel_re := regexp.MustCompile(`(?i)<(#[A-Z0-9]+)\|([^>]+)>`)
		m := slack_channel_re.FindStringSubmatch(wanted[1])
		if len(m) > 0 {
			chName = m[2]
		}
		verbose(4, "Looking for %s in %s'...", user, chName)
		ch, found = getChannel(r.ChatType, chName)
	}

	if addressedToTheBot(user) {
		rand.Seed(time.Now().UnixNano())
		replies := []string{
			"You can't see me, I'm not really here.",
			"_is invisible._",
			"_looked, but only saw its shadow._",
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

	if r.ChatType == "hipchat" {
		for u, info := range ch.HipChatUsers {
			if u.MentionName == user {
				result = info.Seen
			}
		}
	} else if r.ChatType == "slack" {
		if info, found := ch.SlackUsers[user]; found {
			result = info.Seen
		}
	}

	if len(result) < 1 {
		result = fmt.Sprintf("I have not seen that user in #%s.", ch.Name)
	}
	return
}

func cmdSet(r Recipient, chName string, args []string) (result string) {
	var input []string
	if len(args) == 1 {
		input = strings.SplitN(args[0], "=", 2)
	}

	if len(args) > 1 {
		result = "Usage:\n" + COMMANDS["set"].Usage
		if args[0] == "autoreply" {
			result += "To set an autoreply, please use '!autoreply'.\n"
			result += "See '!help autoreply' for details.\n"
		}
		return
	}

	var ch *Channel
	var found bool
	if ch, found = CHANNELS[chName]; !found {
		fmt.Fprintf(os.Stderr, ":: set: channel %s not found!\n", chName)
		result = "I can only set things in a channel."
		return
	}

	if len(args) < 1 {
		if len(ch.Settings) < 1 {
			result = fmt.Sprintf("There currently are no settings for #%s.", chName)
			return
		}

		sorted := []string{}
		for n, _ := range ch.Settings {
			sorted = append(sorted, n)
		}
		sort.Strings(sorted)

		for _, s := range sorted {
			result += fmt.Sprintf("%s=%s\n", s, ch.Settings[s])
		}
		return
	}

	name := strings.TrimSpace(input[0])
	if len(input) == 1 {
		s, found := ch.Settings[name]
		if found {
			result = fmt.Sprintf("%s=%s\n", name, s)
		} else {
			result = fmt.Sprintf("No such setting: %s\n", name)
		}
		return
	}

	value := strings.TrimSpace(input[1])

	/* Users sometimes call "!set oncall=<team>" with
	* the literal brackets; let's help them. */
	slack_user_re := regexp.MustCompile(`(?i)<@([A-Z0-9]+)>`)
	if !slack_user_re.MatchString(value) {
		value = strings.TrimPrefix(value, "<")
		value = strings.TrimPrefix(value, "&lt;")
		value = strings.TrimSuffix(value, ">")
		value = strings.TrimSuffix(value, "&gt;")
	}

	if len(ch.Settings) < 1 {
		ch.Settings = map[string]string{}
	}

	old := ""
	if old, found = ch.Settings[name]; found {
		if value == old {
			result = fmt.Sprintf("'%s' unchanged.", name)
			return
		}
		old = fmt.Sprintf(" (was: %s)", old)
	}

	ch.Settings[name] = value

	result = fmt.Sprintf("Set '%s' to '%s'%s.", name, value, old)
	return
}

func cmdSiginfo(r Recipient, chName string, args []string) (result string) {
	result = "```\n"
	result += fmt.Sprintf("# of SLACK_CHANNELS        : %d\n", len(SLACK_CHANNELS))
	result += fmt.Sprintf("# of CHANNELS              : %d\n", len(CHANNELS))
	result += fmt.Sprintf("# of SLACK_USERS_BY_NAME   : %d\n", len(SLACK_USERS_BY_NAME))
	result += fmt.Sprintf("# of COMMANDS              : %d\n", len(COMMANDS))
	result += fmt.Sprintf("# of COUNTERS              : %d\n", len(COUNTERS))
	for c, m := range COUNTERS {
		s := fmt.Sprintf("# of '%s'", c)
		padding := len("Seconds since last message ") - len(s)
		n := 0
		for n < padding {
			s += " "
			n++
		}
		result += fmt.Sprintf("%s: %d\n", s, len(m))
	}

	result += "```\n"
	return
}

func cmdSms(r Recipient, chName string, args []string) (result string) {
	lookupType := "number"
	shortcode := ""
	if len(args) < 1 {
		shortcode = "773786" // Yahoo! Shortcode
	} else if len(args) > 1 {
		result = "Usage: " + COMMANDS["sms"].Usage
		return
	} else {
		shortcode = args[0]
	}

	shortcode = strings.Replace(shortcode, "-", "", -1)

	var i int
	if _, err := fmt.Sscanf(shortcode, "%d", &i); err != nil {
		lookupType = "search"
	}

	var theUrl string
	if lookupType == "number" {
		theUrl = fmt.Sprintf("%sshort-code-%s/", COMMANDS["sms"].How, shortcode)
	} else if lookupType == "search" {
		theUrl = fmt.Sprintf("%s?fwp_short_code_search=%s/", COMMANDS["sms"].How, url.QueryEscape(shortcode))
	}
	data := getURLContents(theUrl, nil)

	printNext := false
	info := []string{
		"Business/Organization:",
		"Short Code Activation Date:",
		"Short Code Deactivation Date:",
		"Campaign Name:",
	}
	for _, line := range strings.Split(string(data), "\n") {
		if lookupType == "number" {
			if printNext {
				result += dehtmlify(line) + "\n"
				printNext = false
			}
			for _, field := range info {
				if strings.Contains(line, fmt.Sprintf("<td>%s</td>", field)) {
					result += field + " "
					printNext = true
					break
				}
			}
		} else if lookupType == "search" {
			re := regexp.MustCompile(`(?i)<h3><a href="(https://usshortcodedirectory.com/directory/short-code-([0-9]+)/)">(.*)</a></h3>`)
			if m := re.FindStringSubmatch(line); len(m) > 0 {
				result += m[3] + ": " + m[2]
				result += "\n" + m[1] + "\n"
			}
		}
	}

	if len(result) > 0 && lookupType == "number" {
		result = "Short Code: " + shortcode + "\n" + result
	}

	if len(result) < 1 {
		result = "No results found for '" + shortcode + "'."
	}
	return
}

func cmdSpeb(r Recipient, chName string, args []string) (result string) {
	if len(args) > 0 {
		result = "Usage: " + COMMANDS["speb"].Usage
		return
	}

	result = randomLineFromUrl(COMMANDS["speb"].How)
	return
}

func cmdStfu(r Recipient, chName string, args []string) (result string) {
	var ch *Channel
	var found bool

	if ch, found = CHANNELS[chName]; !found {
		fmt.Fprintf(os.Stderr, ":: stfu: channel %s not found!\n", chName)
		result = "This command only works in a channel."
		return
	}

	chatter := make(map[int][]string)

	if r.ChatType == "hipchat" {
		for u := range ch.HipChatUsers {
			if (len(args) > 0) && (u.MentionName != args[0]) {
				continue
			}
			chatter[ch.HipChatUsers[u].Count] = append(chatter[ch.HipChatUsers[u].Count], u.MentionName)
		}
	} else if r.ChatType == "slack" {
		for u := range ch.SlackUsers {
			if (len(args) > 0) && (u != args[0]) {
				continue
			}
			chatter[ch.SlackUsers[u].Count] = append(chatter[ch.SlackUsers[u].Count], u)
		}
	}

	if (len(args) > 0) && (len(chatter) < 1) {
		result = fmt.Sprintf("%s hasn't said anything in %s.",
			args[0], chName)
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

func cmdTfln(r Recipient, chName string, args []string) (result string) {
	if len(args) > 0 {
		result = "Usage: " + COMMANDS["tfln"].Usage
		return
	}

	data := getURLContents(COMMANDS["tfln"].How, nil)

	tfln_re := regexp.MustCompile(`(?i)^<p><a href="/Text-Replies`)
	for _, line := range strings.Split(string(data), "\n") {
		if tfln_re.MatchString(line) {
			result = dehtmlify(line)
			return
		}
	}
	return
}

func cmdThrottle(r Recipient, chName string, args []string) (result string) {
	input := args
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
			result = cmdInsult(r, chName, []string{"me"})
			return
		}
	}

	var ch *Channel
	var found bool
	if ch, found = CHANNELS[chName]; !found {
		fmt.Fprintf(os.Stderr, ":: throttle: channel %s not found!\n", chName)
		result = "I can only throttle things in a channel."
		return
	}

	if len(args) > 0 {
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
	result += strings.Join(throttles, "\n")
	return
}

func cmdTime(r Recipient, chName string, args []string) (result string) {
	timezones := []string{"Asia/Taipei", "Asia/Calcutta", "UTC", "EST5EDT", "PST8PDT"}
	if len(args) > 0 {
		timezones = args
	}

	for _, l := range timezones {
		if loc, err := time.LoadLocation(l); err == nil {
			result += fmt.Sprintf("%s\n", time.Now().In(loc).Format(time.UnixDate))
		} else if loc, err := time.LoadLocation(strings.ToUpper(l)); err == nil {
			result += fmt.Sprintf("%s\n", time.Now().In(loc).Format(time.UnixDate))
		} else {
			var tz string
			var found bool

			address := getUserAddress(r, l)
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

func cmdTld(r Recipient, chName string, args []string) (result string) {
	input := args
	if len(input) != 1 {
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
			result = fmt.Sprintf("```Organization: %s\n", info["organisation"])
		}
		if len(info["e-mail"]) > 0 {
			result += fmt.Sprintf("Contact     : %s\n", info["e-mail"])
		}
		if len(info["whois"]) > 0 {
			result += fmt.Sprintf("Whois       : %s\n", info["whois"])
		}
		result += fmt.Sprintf("Status      : %s\n", info["status"])
		result += fmt.Sprintf("Created     : %s```\n", info["created"])
		if len(info["remarks"]) > 0 {
			result += fmt.Sprintf("%s\n", strings.Replace(info["remarks"], "Registration information: ", "", -1))
		}
	}
	return
}

func cmdToggle(r Recipient, chName string, args []string) (result string) {
	wanted := "all"
	if len(args) > 1 {
		result = "Usage: " + COMMANDS["toggle"].Usage
		return
	} else if len(args) == 1 {
		wanted = args[0]
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

func cmdResetCounter(r Recipient, chName string, args []string) (result string) {

	input := strings.Join(args, " ")
	if CONFIG["botOwner"] != r.MentionName {
		result = fmt.Sprintf("Sorry, %s is not allowed to run this command.", r.MentionName)
		return
	}

	_, err := getCounter(input)
	if len(err) > 0 {
		result = err
		return
	} else {
		COUNTERS[input] = map[string]int{}
		result = input + " reset."
	}
	return
}

func cmdTop(r Recipient, chName string, args []string) (result string) {
	input := strings.Join(args, " ")
	counter, err := getCounter(input)
	if len(err) > 0 {
		result = err
		return
	}

	nums := getSortedKeys(counter, true)
	n := 1
	counts := []string{}
	sep := ", "
	if input == "replies" {
		sep = "\n"
	}

	for _, k := range nums {
		counts = append(counts, fmt.Sprintf("%s (%d)", k, counter[k]))
		n++
		if n > 10 {
			break
		}
	}
	result += strings.Join(counts, sep)

	return
}

func cmdTrivia(r Recipient, chName string, args []string) (result string) {
	if len(args) > 0 {
		result = "Usage: " + COMMANDS["trivia"].Usage
		return
	}

	result = randomLineFromUrl(COMMANDS["trivia"].How)
	return
}

func cmdTroutSlap(r Recipient, chName string, args []string) (result string) {
	slappee := strings.Join(args, " ")
	if addressedToTheBot(slappee) || slappee == "me" {
		slappee = fmt.Sprintf("<@%s>", r.Id)
	}

	result = fmt.Sprintf("_pulls out a foul-smelling trout and slaps %s across the face._\n", slappee)
	return
}

func cmdUd(r Recipient, chName string, args []string) (result string) {

	theUrl := COMMANDS["ud"].How
	if len(args) > 0 {
		theUrl += fmt.Sprintf("define.php?term=%s", url.QueryEscape(strings.Join(args, " ")))
	} else {
		rand.Seed(time.Now().UnixNano())
		n := rand.Intn(1000)
		theUrl += fmt.Sprintf("random.php?page=%d", n)
	}

	data := getURLContents(theUrl, nil)
	desc_re := regexp.MustCompile(`(?i)/><meta content="(.*?)" name="twitter:description" `)
	example_re := regexp.MustCompile(`(?i)<div class="example">(.*?)</div>`)
	tags_re := regexp.MustCompile(`(?i)<div class="tags">(.*?)</div>`)
	notfound_re := regexp.MustCompile(`(?i)<div class="term space">(Sorry, we couldn't find: .*?)</div>`)
	word_re := regexp.MustCompile(`(?i)url=http%3A%2F%2F(.*?).urbanup.com`)

	description := ""
	example := ""
	tags := ""
	word := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := desc_re.FindStringSubmatch(line); len(m) > 0 {
			description = dehtmlify(m[1])
		}
		if m := example_re.FindStringSubmatch(line); len(m) > 0 {
			example = "Example: " + dehtmlify(m[1])
		}
		if m := tags_re.FindStringSubmatch(line); len(m) > 0 {
			tags = "Tags:" + strings.Join(strings.Split(dehtmlify(m[1]), "#"), " #")
		}
		if m := word_re.FindStringSubmatch(line); len(m) > 0 {
			word = m[1] + ":\n"
		}
		if strings.Contains(line, "<a class=\"circle-link\"") {
			break
		}

		if m := notfound_re.FindStringSubmatch(line); len(m) > 0 {
			result += "¯\\_(ツ)_/¯\n" + m[1]
			return
		}
	}

	if len(args) > 0 {
		word = ""
	}
	result = fmt.Sprintf("%s%s\n%s\n%s\n", word, description, example, tags)
	return
}

func cmdUnset(r Recipient, chName string, args []string) (result string) {
	input := args
	if len(input) != 1 {
		result = "Usage: " + COMMANDS["unset"].Usage
		return
	}

	var ch *Channel
	var found bool
	if ch, found = CHANNELS[chName]; !found {
		fmt.Fprintf(os.Stderr, ":: unset: channel %s not found!\n", chName)
		result = "I can only unset things in a channel."
		return
	}

	if len(ch.Settings) < 1 {
		ch.Settings = map[string]string{}
	}

	old := ""
	if old, found = ch.Settings[args[0]]; found {
		delete(ch.Settings, args[0])
		result = fmt.Sprintf("Deleted %s=%s.", args[0], old)
	} else {
		result = fmt.Sprintf("No such setting: '%s'.", args[9])
	}

	return
}

func cmdUnthrottle(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["unthrottle"].Usage
		return
	}

	var ch *Channel
	var found bool
	if ch, found = CHANNELS[chName]; !found {
		fmt.Fprintf(os.Stderr, ":: unthrottle : channel %s not found!\n", chName)
		result = "I can only unthrottle things in a channel."
		return
	}

	if args[0] == "*" || args[0] == "everything" {
		for t, _ := range ch.Throttles {
			delete(ch.Throttles, t)
		}
	} else {
		delete(ch.Throttles, args[0])
	}

	replies := []string{
		"Okiley, dokiley!",
		"Sure thing, my friend!",
		"Done.",
		"No problemo.",
		"_throttles that thang._",
		"Got it.",
		"Word.",
		"Unthrottled to the max!",
		"Consider it done.",
	}
	result = replies[rand.Intn(len(replies))]
	return
}

func cmdUser(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["user"].Usage
		return
	}

	user := strings.TrimSpace(args[0])
	if r.ChatType == "slack" {
		return getSlackUserInfo(user)
	} else {
		return getHipchatUserInfo(user)
	}
}

func cmdVu(r Recipient, chName string, args []string) (result string) {
	nums := args
	if len(nums) != 1 {
		result = "Usage: " + COMMANDS["vu"].Usage
		return
	}

	num := strings.TrimSpace(nums[0])

	if strings.HasPrefix(num, "#") {
		num = num[1:]
	}

	theUrl := fmt.Sprintf("%s%s", COMMANDS["vu"].How, num)
	data := getURLContents(theUrl, nil)

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

func cmdWeather(r Recipient, chName string, args []string) (result string) {
	var where string
	apikey := CONFIG["openweathermapApiKey"]
	if len(apikey) < 1 {
		result = "Missing OpenWeatherMap API Key."
		return
	}

	if len(args) < 1 {
		where = r.MentionName
	} else {
		where = args[0]
	}

	u := expandSlackUser(where)
	if u != nil && u.ID != "" {
		where = u.Name
	}

	if where == CONFIG["mentionName"] {
		where = "ne1"
	}

	latlon := ""
	address := getUserAddress(r, where)
	if len(address) > 0 && !strings.HasPrefix(address, "Unable to look up") {
		where = address
	} else {
		var unused Recipient
		coloInfo := cmdColo(unused, "", []string{where, "coords"})
		if !strings.HasPrefix(coloInfo, "Sorry,") {
			latlon = coloInfo
		}
	}

	query := "weather?appid=" + apikey + "&"
	if strings.Contains(latlon, ",") {
		ll := strings.SplitN(latlon, ",", 2)
		query += fmt.Sprintf("lat=%s&lon=%s", ll[0], ll[1])
	} else {
		re := regexp.MustCompile(`^([0-9-]+)(,.*)?$`)
		if re.MatchString(where) {
			query += "zip="
		} else {
			query += "q="
		}
		query += url.QueryEscape(where)
	}

	theURL := fmt.Sprintf("https://api.openweathermap.org/data/2.5/%s", query)
	data := getURLContents(theURL, nil)

	type OpenWeatherMapResult struct {
		Coord struct {
			Lat float64
			Lon float64
		}
		Main struct {
			Humidity float64
			Pressure float64
			Temp     float64
			Temp_max float64
			Temp_min float64
		}
		Id   int
		Name string
		Sys  struct {
			Country string
			Sunrise int64
			Sunset  int64
		}
		Weather []struct {
			Description string
			Main        string
		}
		Wind struct {
			Deg   float64
			Speed float64
		}
	}

	var w OpenWeatherMapResult
	err := json.Unmarshal(data, &w)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal weather data: %s\n", err)
		return
	}

	if len(w.Name) < 1 {
		result = fmt.Sprintf("Sorry, location '%s' not found.\n", where)
		return
	}

	result = fmt.Sprintf("Weather in %s, %s: <https://openweathermap.org/city/%d|%s>\n", w.Name, w.Sys.Country,
		w.Id,
		w.Weather[0].Description)
	result += fmt.Sprintf("Temperature: %s (low: %s, high: %s)\n",
		tempStringFromKelvin(w.Main.Temp),
		tempStringFromKelvin(w.Main.Temp_min),
		tempStringFromKelvin(w.Main.Temp_max))
	result += fmt.Sprintf("Wind: %.1f m/s\n", w.Wind.Speed)
	result += fmt.Sprintf("Humidity: %.2f\n", w.Main.Humidity)
	result += fmt.Sprintf("Pressure: %.2f hpa\n", w.Main.Pressure)

	gmapLink := fmt.Sprintf("https://www.google.com/maps/@%f,%f,12z", w.Coord.Lat, w.Coord.Lon)
	result += fmt.Sprintf("Coordinates: <%s|[%.3f, %.3f]>\n", gmapLink, w.Coord.Lat, w.Coord.Lon)
	return
}

func tempStringFromKelvin(t float64) (s string) {
	c := t - 273.15
	f := c*9/5 + 32

	s = fmt.Sprintf("%.2f F / %.2f C", f, c)
	return
}

func cmdWhocyberedme(r Recipient, chName string, args []string) (result string) {
	if len(args) > 0 {
		result = "Usage: " + COMMANDS["whocyberedme"].Usage
		return
	}

	data := getURLContents(COMMANDS["whocyberedme"].How, nil)

	for _, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, "confirms") {
			result = dehtmlify(l)
			break
		}
	}
	return
}

func cmdWhois(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["whois"].Usage
		return
	}

	hostinfo := cmdHost(r, chName, args)
	if strings.Contains(hostinfo, "not found:") {
		result = hostinfo
		return
	}

	out, _ := runCommand(fmt.Sprintf("whois %s", args[0]))
	data := string(out)

	/* whois formatting is a mess; different whois servers return
	 * all sorts of different information in all sorts of different
	 * formats. We'll try to catch some common ones here. :-/ */

	var format string
	found := false

	formatGuesses := map[*regexp.Regexp]string{
		regexp.MustCompile("(?i)Registry Domain ID:"):                "common",
		regexp.MustCompile("(?i)%% This is the AFNIC Whois server."): "afnic",
		regexp.MustCompile("(?i)% Copyright.* by DENIC"):             "denic",
		regexp.MustCompile("(?i)The EDUCAUSE Whois database"):        "educause",
		regexp.MustCompile("(?i)for .uk domain names"):               "uk",
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

func cmdWiki(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["wiki"].Usage
		return
	}
	wiki := strings.Join(args, " ")

	query := url.QueryEscape(wiki)
	theUrl := fmt.Sprintf("%s%s", COMMANDS["wiki"].How, query)
	data := getURLContents(theUrl, nil)

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
		result = fmt.Sprintf("Unable to unmarshal wiki data: %s\n", err)
		return
	}

	if len(jsonData) < 4 {
		result = fmt.Sprintf("Something went bump when getting wiki json for '%s'.", wiki)
		return
	}

	sentences := jsonData[2]
	urls := jsonData[3]

	if len(sentences.([]interface{})) < 1 {
		result = fmt.Sprintf("No Wikipedia page found for '%s'.", wiki)
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

func cmdWtf(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["wtf"].Usage
		return
	}
	terms := args
	if (len(terms) > 2) || ((len(terms) == 2) && (terms[0] != "is")) {
		result = "Usage: " + COMMANDS["wtf"].Usage
		return
	}

	term := terms[0]
	if len(terms) == 2 {
		term = terms[1]
	}

	// Slack expands '#channel' to e.g. '<#CBEAWGAPJ|channel>'
	slack_channel_re := regexp.MustCompile(`(?i)<(#[A-Z0-9]+)\|([^>]+)>`)
	m := slack_channel_re.FindStringSubmatch(term)
	if len(m) > 0 {
		result = getChannelInfo(m[1])
		if len(result) > 0 {
			return
		} else {
			term = m[2]
		}
	}

	slack_user := term
	u := expandSlackUser(term)
	if u != nil && u.ID != "" {
		slack_user = u.Name
	}
	if slack_user != term {
		result = cmdBy(r, "", []string{slack_user})
		if len(result) > 0 {
			if strings.HasPrefix(result, "No such user") {
				term = slack_user
			} else {
				return
			}
		}
	}

	if term == CONFIG["mentionName"] {
		result = fmt.Sprintf("Unfortunately, no one can be told what %s is...\n", CONFIG["mentionName"])
		result += "You have to see it for yourself."
		return
	}

	if term == "pi" {
		result = fmt.Sprintf("%.64v", math.Pi)
		return
	}

	out, _ := runCommand(fmt.Sprintf("ywtf %s", term))
	result = string(out)

	if strings.HasPrefix(result, "ywtf: ") {
		result = result[6:]
	}

	return
}

func cmdXkcd(r Recipient, chName string, args []string) (result string) {
	latest := false
	theUrl := COMMANDS["xkcd"].How
	if len(args) < 1 {
		theUrl = "https://xkcd.com/"
		latest = true
	} else if _, err := strconv.Atoi(args[0]); err == nil {
		result = "https://xkcd.com/" + args[0]
		return
	} else {
		theUrl += "process?action=xkcd&query=" + url.QueryEscape(args[0])
	}

	data := getURLContents(theUrl, nil)
	xkcd_re := regexp.MustCompile(`^Permanent link to this comic: (https://xkcd.com/[0-9]+/)`)
	for n, line := range strings.Split(string(data), "\n") {
		m := xkcd_re.FindStringSubmatch(line)
		if latest {
			if len(m) > 0 {
				result = dehtmlify(m[1])
				break
			}
		} else if n == 2 {
			xkcd := strings.Split(line, " ")[0]
			result = "https://xkcd.com/" + xkcd + "/"
		}
	}

	return
}

func cmdYubifail(r Recipient, chName string, args []string) (result string) {
	result = getCountable("yubifail", chName, r, args)
	return
}

/*
 * General Functions
 */

func argcheck(flag string, args []string, i int) {
	if len(args) <= (i + 1) {
		fail("'%v' needs an argument\n", flag)
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

func createCommands() {
	COMMANDS["8ball"] = &Command{cmdEightBall,
		"ask the magic 8-ball",
		"builtin",
		"!8ball <question>",
		nil}
	COMMANDS["alerts"] = &Command{cmdAlerts,
		"display alert settings and help",
		"builtin",
		"!alerts [cmr-alert|jira-alert|snow-alert]",
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
	COMMANDS["bs"] = &Command{cmdBs,
		"Corporate B.S. Generator",
		"builtin, but inspired from http://www.atrixnet.com/bs-generator.html",
		"!bs",
		nil}
	COMMANDS["cert"] = &Command{cmdCert,
		"display information about the x509 cert found at the given hostname",
		"crypto/tls",
		"!cert fqdn [<sni>] [chain]",
		[]string{"certs"}}
	COMMANDS["channels"] = &Command{cmdChannels,
		"display channels I'm in",
		"builtin",
		"!channels",
		nil}
	COMMANDS["cidr"] = &Command{cmdCidr,
		"display CIDR information",
		"builtin (net.ParseCIDR)",
		"!cidr <cidr>",
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
	COMMANDS["giphy"] = &Command{cmdGiphy,
		"get a gif from giphy",
		"https://api.giphy.com/v1/gifs/search",
		"!giphy",
		[]string{"gif"}}
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
	COMMANDS["latlong"] = &Command{cmdLatLong,
		"look up latitude and longitude for a given location",
		"https://www.latlong.net/",
		"!latlong location",
		[]string{"coords"}}
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
	COMMANDS["man"] = &Command{cmdMan,
		"summarize manual page",
		"http://man7.org/linux/man-pages/",
		"!man [<section>] <command>",
		nil}
	COMMANDS["monkeystab"] = &Command{cmdMonkeyStab,
		"unleash a troop of pen-wielding stabbing monkeys",
		"builtin",
		"!monkeystab <something>",
		nil}
	COMMANDS["oid"] = &Command{cmdOid,
		"display OID information",
		"http://oid-info.com/cgi-bin/display?action=display&oid=",
		"!oid <oid>",
		nil}
	COMMANDS["onion"] = &Command{cmdOnion,
		"get your finest news headlines",
		"https://www.theonion.com/",
		"!onion [<term>]",
		nil}
	COMMANDS["ping"] = &Command{cmdPing,
		"try to ping hostname",
		"ping(1)",
		"!ping <hostname>",
		nil}
	COMMANDS["praise"] = &Command{cmdPraise,
		"praise somebody",
		URLS["praise"],
		"!praise <somebody>",
		[]string{"compliment"}}
	COMMANDS["pwgen"] = &Command{cmdPwgen,
		"generate a password for you",
		URLS["pwgen"],
		"!pwgen [length] [count] [complex]",
		nil}
	COMMANDS["quote"] = &Command{cmdQuote,
		"show stock price information",
		"https://finance.yahoo.com/quote/",
		"!quote <symbol>",
		[]string{"stock"}}
	COMMANDS["reset"] = &Command{cmdResetCounter,
		"reset a global counter (requires bot admin privs)",
		"builtin",
		"!reset <counter>",
		nil}
	COMMANDS["rfc"] = &Command{cmdRfc,
		"display title and URL of given RFC",
		"https://tools.ietf.org/html/",
		"!rfc <rfc>",
		nil}
	COMMANDS["roll"] = &Command{cmdRoll,
		"roll a die / generate N random numbers in the given range",
		"builtin",
		"!roll [N [end [begin]]]\n" +
			"If N is not given, default to 1\n" +
			"If end is not given, default to 6\n" +
			"If begin is not given, default to 1\n",
		nil}
	COMMANDS["room"] = &Command{cmdRoom,
		"show information about the given chat room",
		"HipChat / Slack API",
		"!room <name> [list-users]",
		[]string{"channel"}}
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
	COMMANDS["siginfo"] = &Command{cmdSiginfo,
		"show information about internal data structures",
		"builtin",
		"!siginfo",
		nil}
	COMMANDS["sms"] = &Command{cmdSms,
		"show short code information",
		"https://usshortcodedirectory.com/directory/",
		"!sms <numbers>",
		nil}
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
			fmt.Sprintf("!throttle <something>  -- set throttle for <something> to %d seconds\n", DEFAULT_THROTTLE) +
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
	COMMANDS["top"] = &Command{cmdTop,
		"display top 10 stats of <counter>",
		"builtin",
		"!top <counter>",
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
		"https://www.urbandictionary.com/",
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
		"https://api.openweathermap.org/data/2.5/",
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
		[]string{"ywtf"}}
	COMMANDS["xkcd"] = &Command{cmdXkcd,
		"find an xkcd for you",
		"https://relevantxkcd.appspot.com/",
		"!xkcd <words>",
		nil}
	COMMANDS["yubifail"] = &Command{cmdYubifail,
		"check your yubifail count",
		"builtin",
		"!yubifail [<user>]",
		nil}
}

func jbotDebug(in interface{}) {
	if CONFIG["debug"] == "yes" {
		fmt.Fprintf(os.Stderr, "%v\n", in)
	}
}

func joinKnownChannels() {
	verbose(1, "Joining channels slack thinks I'm in...")

	var params slack.GetConversationsForUserParameters
	params.UserID = CONFIG["slackID"]
	params.Limit = 999
	params.Cursor = ""
	params.Types = []string{"public_channel", "private_channel"}

	channels, cursor, err := SLACK_CLIENT.GetConversationsForUser(&params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to GetConversationsForUser: %s\n", err)
		return
	}

	for cursor != "" {
		params.Cursor = cursor
		nextChannels, nextCursor, err := SLACK_CLIENT.GetConversationsForUser(&params)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to GetConversationsForUser: %s\n", err)
			break
		}
		channels = append(channels, nextChannels...)
		cursor = nextCursor
	}

	for _, c := range channels {
		if _, found := CHANNELS[c.Name]; !found {
			ch := newSlackChannel(c.Name, c.ID, "Slack")
			CHANNELS[ch.Name] = &ch
			CHANNELS_BY_ID[ch.Id] = &ch
		}
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
		fail("Client error: %s\n", err)
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

	SLACK_RTM = SLACK_CLIENT.NewRTM()
	go SLACK_RTM.ManageConnection()

	/* If we introduced a new channel property,
	 * but the serialized data does not contain it, it
	 * would be undefined (e.g. 'off' / nonexistent
	 * for a toggle).  So here we
	 * quickly initialize all (unknown) data.
	 */
	updateChannels()
	go getAllSlackUsers()

	joinKnownChannels()
	go updateSlackChannels()
	go slackPeriodics()
Loop:
	for {
		select {
		case msg := <-SLACK_RTM.IncomingEvents:
			switch ev := msg.Data.(type) {

			case *slack.ChannelJoinedEvent:
				processSlackChannelJoin(ev)

			case *slack.ChannelRenameEvent:
				processSlackChannelRename(ev)

			case *slack.InvalidAuthEvent:
				fmt.Fprintf(os.Stderr, "Unable to authenticate.")
				break Loop

			case *slack.MessageEvent:
				processSlackMessage(ev)

			case *slack.RateLimitEvent:
				processSlackRateLimit(ev)

			case *slack.RTMError:
				fmt.Fprintf(os.Stderr, "Slack error: %s\n", ev.Error())

			case *slack.UserChangeEvent:
				processSlackUserChangeEvent(ev)
			default:
				jbotDebug(msg)

			}
		}
	}
}

func expandSlackUser(in string) (u *slack.User) {
	// Slack expands '@user' to e.g. '<@CBEAWGAPJ>'
	slack_user_re := regexp.MustCompile(`(?i)<@([A-Z0-9]+)>`)
	m := slack_user_re.FindStringSubmatch(in)
	if len(m) > 0 {
		u, _ = SLACK_CLIENT.GetUserInfo(m[1])
	}

	return
}

func fail(format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", v...)
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

func getAllMembersInChannel(id string) (allMembers []string) {
	verbose(2, "Fetching all members of #%s ...", id)
	params := slack.GetUsersInConversationParameters{
		ChannelID: id,
		Limit:     1000,
	}

	for {
		members, cursor, err := SLACK_CLIENT.GetUsersInConversation(&params)
		if err != nil {
			fmt.Printf("Unable to get conversation: %s\n", err)
			break
		}
		allMembers = append(allMembers, members...)
		if len(cursor) > 0 {
			params.Cursor = cursor
		} else {
			break
		}
	}

	return
}

func getAllSlackUsers() {
	verbose(2, "Fetching all users real quick...")
	users, err := SLACK_CLIENT.GetUsers()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to GetUsers: %v\n", err)
		return
	}

	verbose(3, "Fetched %d users.", len(users))
	for _, u := range users {
		SLACK_USERS_BY_ID[u.ID] = u
		SLACK_USERS_BY_NAME[u.Name] = u
	}
}

func getChannel(chatType, id string) (ch *Channel, ok bool) {
	ok = false

	name := id

	if chatType == "slack" {
		uId := strings.ToUpper(id)
		if ch, found := CHANNELS_BY_ID[uId]; found {
			name = ch.Name
		} else {
			verbose(2, "Encountered unknown channel '%s'...", id)
			slackChannel, err := SLACK_CLIENT.GetConversationInfo(uId, false)
			if err == nil {
				name = slackChannel.Name
				ch := newSlackChannel(name, id, "Mysterio")
				verbose(3, "Creating new channel '%s'...", name)
				CHANNELS[ch.Name] = &ch
				CHANNELS_BY_ID[ch.Id] = &ch
			}

		}
	}

	ch, ok = CHANNELS[name]

	return
}

func getChannelInfo(id string) (info string) {
	verbose(3, "Getting channel info for '%s'...", id)
	var ch slack.Channel
	found := false
	if strings.HasPrefix(id, "#") {
		id = id[1:]
	}

	/* This function is called either with the
	 * channel name or the "#<ID>".  If called
	 * with the ID, then our lookup will fail,
	 * since SLACK_CHANNELS is indexed by name,
	 * but then we can call the API and get the
	 * channel info. */
	ch, found = SLACK_CHANNELS[id]
	if !found {
		c, err := SLACK_CLIENT.GetConversationInfo(id, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, ":: %v\n", err)
			return
		}
		ch = *c
		ch.Members = []string{}
		SLACK_CHANNELS[ch.Name] = ch
	}

	topic := ""
	if len(ch.Topic.Value) > 0 {
		topic = fmt.Sprintf(" -- \"%s\"", ch.Topic.Value)
	}
	members := getAllMembersInChannel(ch.ID)
	info = fmt.Sprintf("%s (%d members)%s\n%s\n",
		ch.Name, len(members),
		topic, ch.Purpose.Value)
	return
}

func getCounter(c string) (counter map[string]int, err string) {
	cnt, ok := COUNTERS[c]
	if !ok {
		if len(c) > 0 {
			err = "I don't keep track of that.\n"
		}
		err += "These are the things I currently track:\n"
		var counters []string
		for c := range COUNTERS {
			counters = append(counters, c)
		}
		sort.Strings(counters)
		err += strings.Join(counters, ", ")
	} else {
		counter = cnt
	}
	return
}

func getHipchatUserInfo(user string) (result string) {
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
		result = "No such user: " + user
	}

	return
}

func getManResults(section, cmd string) (result string) {
	nsection := section
	if strings.HasSuffix(section, "p") {
		nsection = string(section[0])
	}
	theUrl := fmt.Sprintf("%sman%s/%s.%s.html", COMMANDS["man"].How, nsection, cmd, section)
	data := getURLContents(theUrl, nil)

	section_re := regexp.MustCompile(`(?i)^<h2><a id="(NAME|SYNOPSIS|DESCRIPTION)" href="#`)
	p := false
	count := 0
	section = ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "</pre>") {
			p = false
			count = 0
		}
		if m := section_re.FindStringSubmatch(line); len(m) > 0 {
			if len(result) == 0 {
				result += "```"
			}
			section = m[1]
			result += "\n" + m[1]
			p = true
			count = 0
			continue
		}
		if p && count < 3 {
			count++
			result += "\n        " + dehtmlify(line)
		}

		if count == 3 {
			result += "\n        ..."
			p = false
			count = 0
			if section == "DESCRIPTION" {
				break
			}
		}
	}

	if len(result) > 0 {
		result += "```\n" + theUrl
	}

	return
}

func getRecipientFromMessage(mfrom string, chatType string, ts ...string) (r Recipient) {
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
		 * channel. */
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
			/* Given timestamps (if any) are
			 * t[0] = parent-ts
			 * t[1] = this-ts
			 *
			 * If the message was not in a thread,
			 * then t[1] == "", so only set ThreadTs
			 * if that is found. */
			if len(ts) > 1 && len(ts[1]) > 0 && ts[0] != ts[1] {
				r.ThreadTs = ts[1]
			}
		}
	}

	return
}

func getSlackUser(user string) (u *slack.User) {
	if slackUser, found := SLACK_USERS_BY_NAME[user]; found {
		u = &slackUser
	} else if slackUser, found := SLACK_USERS_BY_ID[user]; found {
		u = &slackUser
	} else {
		u = expandSlackUser(user)
	}

	if u == nil {
		u, _ = SLACK_CLIENT.GetUserInfo(user)
	}
	if u == nil {
		u, _ = SLACK_CLIENT.GetUserByEmail(user + "@" + CONFIG["emailDomain"])
	}

	return
}

func getSlackUserInfo(user string) (result string) {
	if strings.EqualFold(user, CONFIG["mentionName"]) {
		result = fmt.Sprintf("Dat's me! Loveable old %s! :bendefuturamar:", user)
		return
	}

	u := getSlackUser(user)
	if u != nil {
		if u.IsBot {
			result = fmt.Sprintf("%s is a :robot_face: - that's all I'm willing to say.", u.Name)
			return
		}

		if u.IsAdmin {
			result = ":boss-8031: "
		}
		result += fmt.Sprintf("%s <%s> - %s", u.RealName, u.Profile.Email, u.Profile.Title)
		if len(u.Profile.StatusText) > 0 {
			result += " (" + u.Profile.StatusText + ")"
		}
		if len(u.Profile.StatusEmoji) > 0 {
			result += " " + u.Profile.StatusEmoji
		}
	} else {
		result = "No such user: " + user
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

/* Additional arguments can influence how the request is made:
 * - if args["auth"] is "x509", then the URL requires x509 client a cert / key
 *   from CONFIG["x509Cert"] and CONFIG["x509Key"]
 * - if args["corp"] is "true", then the URL requires a second type of credentials
 * - if args["ua"] is "true", then we fake the User-Agent
 * - if args["basic-auth-user"] is set, use that username for basic HTTP auth
 * - if args["basic-auth-password"] is set, use that password for basic HTTP auth
 * - if any args["header"] is set, use that value to set the given header
 *   set the given 'key=value' headers
 */
func getURLContents(givenURL string, args map[string]string) (data []byte) {
	verbose(3, "Fetching %s...", givenURL)

	_, err := url.Parse(givenURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to parse url '%s': %s\n", givenURL, err)
		return
	}

	client := &http.Client{}

	if x509, ok := args["auth"]; ok && x509 == "x509" {
		if len(CONFIG["x509Cert"]) < 1 || len(CONFIG["x509Key"]) < 1 {
			fmt.Fprintf(os.Stderr, "URL '%s' requires an x509 cert/key, but none found in config.\n", givenURL)
			return
		}

		reloader := &mtls.CertReloader{Config: mtls.Config{
			Cert: CONFIG["x509Cert"],
			Key:  CONFIG["x509Key"],
		}}

		client = &http.Client{
			Transport: &http.Transport{
				TLSHandshakeTimeout: time.Minute * 5,
				TLSClientConfig: &tls.Config{
					GetClientCertificate: reloader.GetClientCertificate,
					Renegotiation:        tls.RenegotiateOnceAsClient,
				},
			},
		}
	}

	request, err := http.NewRequest("GET", givenURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create new request for '%s': %s\n", givenURL, err)
		return
	}

	var ba_user string
	var ba_pass string

	for key, val := range args {
		if key == "ua" {
			request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/63.0.3239.132 Safari/537.36")
		} else if key == "basic-auth-user" {
			ba_user = val
		} else if key == "basic-auth-password" {
			ba_pass = val
		} else {
			request.Header.Set(key, val)
		}
	}

	if len(ba_user) > 0 {
		request.SetBasicAuth(ba_user, ba_pass)
	}

	response, err := client.Do(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to GET '%s': %s\n", givenURL, err)
		return
	}

	defer response.Body.Close()

	data, err = ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read body of '%s': %s\n", givenURL, err)
		return
	}

	return
}

/*
 * !countable -> your total 'countable' account
 * !countable @user -> that user's total countable count
 * !countable #channel -> total countable count in this channel
 *
 * These are handled in 'cmdTop':
 * !top countable -> top 5 countableers
 */
func getCountable(which, chName string, r Recipient, args []string) (result string) {
	wanted := ""
	if len(args) > 1 {
		result = "Too many arguments."
		return
	} else if len(args) == 1 {
		wanted = args[0]
	}
	verbose(3, "Getting %s count for %s in %s, looking for %s...", which, r.MentionName, chName, wanted)

	channelLookup := false
	// Slack expands '#channel' to e.g. '<#CBEAWGAPJ|channel>'
	slack_channel_re := regexp.MustCompile(`(?i)<(#[A-Z0-9]+)\|([^>]+)>`)
	m := slack_channel_re.FindStringSubmatch(wanted)
	if len(m) > 0 {
		wanted = m[2]
		channelLookup = true
	}

	// Private channels may not be expanded by slack...
	if strings.HasPrefix(wanted, "#") {
		wanted = wanted[1:]
		channelLookup = true
	}

	if channelLookup {
		r.MentionName = "*"
		result = getUserCountableByChannel(which, wanted, r)
		return
	}

	chName = "*"
	if len(wanted) > 0 {
		expandedUser := expandSlackUser(wanted)
		if expandedUser != nil && expandedUser.ID != "" {
			r.MentionName = expandedUser.Name
		} else {
			r.MentionName = wanted
		}
	}

	result = getUserCountableByChannel(which, chName, r)
	return
}

func getUserCountableByChannel(countable, channel string, r Recipient) (result string) {
	verbose(3, "Getting %s count by channel for %s in %s...", countable, r.MentionName, channel)

	count := 0
	if channel == "*" {
		userCurses := map[string]int{}
		for _, ch := range CHANNELS {
			users := getUsersFromChannel(ch.Name, r.ChatType)
			if uinfo, found := users[r.MentionName]; found {
				if countable == "yubifail" {
					count += uinfo.Yubifail
				} else if countable == "curses" {
					for cw, count := range uinfo.CurseWords {
						userCurses[cw] += count
					}
				}
			}
		}

		if countable == "curses" {
			curseRanks := []string{}
			curses := getSortedKeys(userCurses, true)
			for _, c := range curses {
				curseRanks = append(curseRanks, fmt.Sprintf("%s (%d)", c, userCurses[c]))
			}
			if len(curseRanks) < 1 {
				result = fmt.Sprintf("Looks like %s has been behaving since I started paying attention...", r.MentionName)
			} else {
				result = strings.Join(curseRanks, ", ")
			}
			return
		}
	} else {
		_, found := CHANNELS[channel]
		if !found {
			fmt.Fprintf(os.Stderr, ":: usercountable : channel %s not found!\n", channel)
			result = fmt.Sprintf("I don't know anything about #%s.", channel)
			return
		}

		users := getUsersFromChannel(channel, r.ChatType)

		if r.MentionName == "*" {
			for _, info := range users {
				if countable == "yubifail" {
					count += info.Yubifail
				} else if countable == "curses" {
					curseRanks := []string{}
					curses := getSortedKeys(info.CurseWords, true)
					for _, c := range curses {
						curseRanks = append(curseRanks, fmt.Sprintf("%s (%d)", c, info.CurseWords[c]))
					}
					if len(curseRanks) < 1 {
						result = fmt.Sprintf("Looks like %s has been behaving (at least in #%s) since I started paying attention...", channel, r.MentionName)
					} else {
						result = strings.Join(curseRanks, ", ")
					}
					return
				}
			}
		} else {
			uinfo, found := users[r.MentionName]
			if !found {
				result = fmt.Sprintf("I don't think %s is in #%s.", r.MentionName, channel)
				return
			}
			if countable == "yubifail" {
				count += uinfo.Yubifail
			} else if countable == "curses" {
				count += uinfo.Curses
			}
		}
	}

	result = fmt.Sprintf("%d\n", count)
	return
}

func getUsersFromChannel(channel, chatType string) (users map[string]UserInfo) {
	verbose(3, "Getting users for #%s...", channel)
	users = map[string]UserInfo{}
	ch, found := CHANNELS[channel]
	if found {
		if chatType == "slack" {
			users = ch.SlackUsers
		} else {
			for hc, u := range ch.HipChatUsers {
				users[hc.MentionName] = u
			}
		}
	} else if slackChannel, found := SLACK_CHANNELS[channel]; found {
		for _, uid := range getAllMembersInChannel(slackChannel.ID) {
			var userInfo UserInfo
			uinfo, found := SLACK_USERS_BY_ID[uid]
			if !found {
				i, err := SLACK_CLIENT.GetUserInfo(uid)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Unable to get user info for '%s': %s\n", uid, err)
					continue
				}
				SLACK_USERS_BY_ID[uid] = *i

				userInfo.Id = i.ID
				users[i.Name] = userInfo
			} else {
				userInfo.Id = uid
				users[uinfo.Name] = userInfo
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "Unable to get users for unknown channel '%s'.\n", channel)
	}
	return
}

func incrementCounter(category, counter string) {
	if categoryCounters, ok := COUNTERS[category]; ok {
		if ccount, ok := categoryCounters[counter]; ok {
			categoryCounters[counter] = ccount + 1
		} else {
			categoryCounters[counter] = 1
		}
		COUNTERS[category] = categoryCounters
	} else {
		COUNTERS[category] = map[string]int{counter: 1}
	}
}

func isWorkspaceUser(uname string) (yesno bool) {
	u := getSlackUser(uname)
	if u == nil {
		return false
	}

	return !u.IsRestricted && !u.IsUltraRestricted && !u.IsStranger && !u.IsBot
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
			msg := "Bots can't leave Slack channels - you'd have to find a Slack admin to kick me out.\n"
			msg += "But I'm going to ignore everything in this channel going forward.\n"
			msg += "If you do miss me terribly much, @-mention me and I'll start paying attention in here again, ok?\n\n"
			rand.Seed(time.Now().UnixNano())
			msg += cursiveText(GOODBYE[rand.Intn(len(GOODBYE))])
			ch, found := getChannel(r.ChatType, r.ReplyTo)
			if found {
				ch.Settings["ignored"] = "true"
				msg += fmt.Sprintf("\n_pretends to have left #%s._", ch.Name)
			}
			reply(r, msg)
		}
	} else {
		reply(r, "Try again from a channel I'm in.")
	}
	return
}

func listContains(list []string, item string) bool {
	for _, i := range list {
		if i == item {
			return true
		}
	}
	return false
}

func locationToTZ(l string) (result string, success bool) {
	success = false

	apikey := CONFIG["timezonedbApiKey"]
	if len(apikey) < 1 {
		result = "Missing 'timezonedbApiKey'."
		return
	}

	lat := "0.0"
	lng := "0.0"

	latlon := cmdLatLong(Recipient{}, "", []string{l})
	if !strings.Contains(latlon, ",") {
		result = "Unknown location."
		return
	}

	ll := strings.SplitN(latlon, ",", 2)
	lat = ll[0]
	lng = ll[1]

	theURL := fmt.Sprintf("http://api.timezonedb.com/v2.1/get-time-zone?key=%s&format=json&by=position&lat=%s&lng=%s",
		apikey, lat, lng)
	data := getURLContents(theURL, nil)

	type TZData struct {
		Abbreviation string
		CountryCode  string
		CountryName  string
		Dst          string
		Formatted    string
		GmtOFfset    int
		Status       string
		ZoneName     string
	}

	var t TZData

	err := json.Unmarshal(data, &t)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal tz data: %s\n", err)
		return
	}

	result = t.ZoneName
	success = true

	return
}

func newSlackChannel(name, id, inviter string) (ch Channel) {
	verbose(2, "Creating new channel '#%s'...", name)

	ch.Toggles = map[string]bool{}
	ch.Throttles = map[string]time.Time{}
	ch.Settings = map[string]string{}
	ch.Type = "slack"
	ch.Id = id
	ch.SlackUsers = make(map[string]UserInfo, 0)
	ch.Inviter = "Nobody"
	ch.Name = name
	ch.Phishy = &PhishCount{0, 0, time.Now(), time.Unix(0, 0)}

	if len(inviter) > 0 {
		user, err := SLACK_CLIENT.GetUserInfo(inviter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to find user information for '%s'.\n", inviter)
		} else {
			ch.Inviter = user.Name
		}
	}

	for t, v := range TOGGLES {
		ch.Toggles[t] = v
	}

	return
}

func parseConfig() {
	fname := CONFIG["configFile"]
	verbose(1, "Parsing config file '%s'...", fname)
	fd, err := os.Open(fname)
	if err != nil {
		fail("Unable to open '%s': %v\n", fname, err)
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
			fail("Invalid line in configuration file '%s', line %d.",
				fname, n)
		} else {
			key := strings.TrimSpace(keyval[0])
			val := strings.TrimSpace(keyval[1])
			printval := val
			for _, s := range SECRETS {
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
			jbotDebug(fmt.Sprintf("%v", ch))
			CHANNELS[ch.Name] = &ch
		}
	}

	if len(CONFIG["slackService"]) > 0 {
		if len(CONFIG["mentionName"]) < 1 || len(CONFIG["slackToken"]) < 0 {
			fail("Please set 'mentionName' and 'slackToken'.")
		}
	}

	fileOptions := []string{"x509Cert", "x509Key"}
	for _, f := range fileOptions {
		if len(CONFIG[f]) > 0 {
			fh, err := os.Open(CONFIG[f])
			if err != nil {
				fail("Unable to open %s file '%s': %q\n", f, CONFIG[f], err)
			}
			fh.Close()
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

func printCommandHelp(cmd string, c *Command) (help string) {
	help = fmt.Sprintf("%s: %s. Usage:\n%s", cmd, c.Help, c.Usage)
	if len(c.Aliases) > 0 {
		help += "\nThis command can also be invoked as: '!"
		help += strings.Join(c.Aliases, "', '!")
		help += "'."
	}

	return
}

func printVersion() {
	fmt.Printf("%v version %v\n", PROGNAME, VERSION)
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

	line = replaceFancyQuotes(line)

	args, err := shlex.Split(line)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to shlex.Split(%s): %s\n", line, err)
		args = strings.Fields(line)
	}
	if len(args) < 1 {
		rand.Seed(time.Now().UnixNano())
		replies := []string{
			"Yes?",
			"Yeeeeees?",
			"How can I help you?",
			"You sound like you need help. Call a friend.",
			"What do you want?",
			"I can't help you unless you tell me what you want.",
			"Go on, don't be shy, ask me something.",
			"At your service!",
			"Ready to serve!",
			"Uhuh, sure.",
			"_looks at you expectantly._",
			"_chuckles._",
			"Go on...",
			"?",
			fmt.Sprintf("!%s", r.MentionName),
		}
		reply(r, replies[rand.Intn(len(replies))])
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

	/* '!leave' does not have a callback, so needs
	 * to be processed first. */
	if cmd == "leave" {
		leave(r, channelFound, line, true)
		return
	}

	/* People sometimes use e.g., "!oncall, do something" etc. */
	cmd = strings.TrimRight(cmd, ",;:")

	var response string
	_, commandFound := COMMANDS[cmd]

	if !commandFound {
		cm_re := regexp.MustCompile(`(?i)^cmr?([0-9]+)$`)
		inc_re := regexp.MustCompile(`(?i)^inc([0-9]+)$`)
		jira_re := regexp.MustCompile(`(?i)^([a-z]+-[0-9]+)$`)

		alias := findCommandAlias(cmd)
		if len(alias) > 1 {
			cmd = alias
			commandFound = true
		} else if m := cm_re.FindStringSubmatch(cmd); len(m) > 0 {
			cmd = "cm"
			args = []string{m[1]}
			commandFound = true
		} else if m := jira_re.FindStringSubmatch(cmd); len(m) > 0 {
			cmd = "jira"
			args = []string{m[1]}
			commandFound = true
		} else if m := inc_re.FindStringSubmatch(cmd); len(m) > 0 {
			cmd = "sn"
			args = []string{m[1]}
			commandFound = true
		} else if strings.HasPrefix(invocation, "!") {
			/* people get excited and say e.g. '!!' or '!!!'; ignore that */
			rex := regexp.MustCompile(`^[[:punct:]]+$`)
			if rex.MatchString(cmd) {
				return
			}
			response = cmdHelp(r, r.ReplyTo, []string{cmd})
		} else if channelFound {
			processChatter(r, line, true)
			return
		}
	}

	if commandFound {
		incrementCounter("commands", cmd)
		if COMMANDS[cmd].Call != nil {
			chName := r.ReplyTo
			if ch, found := getChannel(r.ChatType, r.ReplyTo); found {
				chName = ch.Name
			}
			response = COMMANDS[cmd].Call(r, chName, args)
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
	p := fmt.Sprintf("^(?i)(!|[@/]%s [/!]?", CONFIG["mentionName"])

	if r.ChatType == "slack" {
		p += "|<@" + CONFIG["slackID"] + "> [/!]?"
	}
	p += ")"

	command_re := regexp.MustCompile(p)
	if command_re.MatchString(msg) {
		matchEnd := command_re.FindStringIndex(msg)[1]
		processCommands(r, msg[0:matchEnd], msg[matchEnd:])
	} else {
		if !processAutoReplies(r, msg) {
			processChatter(r, msg, false)
		}
	}
}

func processSlackChannelJoin(ev *slack.ChannelJoinedEvent) {
	jbotDebug(fmt.Sprintf("Join: %v\n", ev))
}

func processSlackChannelRename(ev *slack.ChannelRenameEvent) {
	newName := ev.Channel.Name
	id := ev.Channel.ID
	verbose(1, "Renaming '<%s>' to '#%s'...", id, newName)
	if _, found := CHANNELS[newName]; found {
		fmt.Fprintf(os.Stderr, "Renamed channel '%s' already found?\n", newName)
		return
	}

	for oldName, chInfo := range CHANNELS {
		if chInfo.Id == id {
			verbose(2, "Renaming '#%s' to '#%s'...", oldName, newName)
			delete(CHANNELS, oldName)
			chInfo.Name = newName
			CHANNELS[newName] = chInfo
			break
		}
	}
}

func processSlackInvite(r Recipient, name string, msg *slack.MessageEvent) {
	if strings.Contains(msg.Text, "<@"+CONFIG["slackID"]+">") {
		slackChannel, err := SLACK_CLIENT.GetConversationInfo(msg.Channel, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to SLACK_CLIENT.GetConversationInfo(%s): %s\n",
				msg.Channel, err)
			return
		}
		if slackChannel.IsExtShared {
			fmt.Fprintf(os.Stderr, "Refusing to join externally shared channel '%s' (%s).\n",
				slackChannel.Name, msg.Channel)
			return
		}
		ch := newSlackChannel(name, msg.Channel, msg.User)
		verbose(2, "I was invited into Slack '%s' (%s) by '%s'.", ch.Name, ch.Id, ch.Inviter)
		CHANNELS[ch.Name] = &ch
		CHANNELS_BY_ID[ch.Id] = &ch

		rand.Seed(time.Now().UnixNano())
		reply(r, HELLO[rand.Intn(len(HELLO))])
	}
}

func processSlackMessage(msg *slack.MessageEvent) {
	jbotDebug(fmt.Sprintf("\nMessage: |%v|", msg))

	LAST_SLACK_MESSAGE_TIME = time.Now()

	info := SLACK_RTM.GetInfo()

	var channelName string

	channel, err := SLACK_CLIENT.GetConversationInfo(msg.Channel, false)
	if err == nil {
		channelName = channel.Name
	}

	r := getRecipientFromMessage(fmt.Sprintf("%s@%s", msg.User, msg.Channel), "slack", msg.Timestamp, msg.ThreadTimestamp)

	ch, found := CHANNELS[channelName]
	if !found {
		/* Hey, let's just pretend that any
		 * message we get in a channel that
		 * we don't know about is effectively
		 * an invite. */
		processSlackInvite(r, channelName, msg)
		return
	} else {
		ignored := ch.Settings["ignored"]
		atMention := fmt.Sprintf("<@" + CONFIG["slackID"] + ">")
		if strings.EqualFold(ignored, "true") {
			if strings.Contains(msg.Text, atMention) {
				ch.Settings["ignored"] = "false"
			} else {
				return
			}
		}
	}

	if msg.User == info.User.ID {
		/* Ignore our own messages. */
		return
	}

	txt := msg.Text
	if msg.SubType == "message_changed" {
		/* When unfirling a link, Slack effectively updates
		 * the original message, so it shows up here again
		 * with an attachment.  Let's simply ignore any
		 * edited messages with attachments to avoid processing
		 * it twice. */
		if len(msg.SubMessage.Attachments) > 0 {
			return
		}

		txt = msg.SubMessage.Text
		/* Edited messages come from the channel only,
		 * so we need to reconstruct the recipient from
		 * the submessage.  That will yield a no-channel
		 * recipient, however, so we reuse the original
		 * channel to avoid sending a privmsg. */
		r = getRecipientFromMessage(fmt.Sprintf("%s@%s", msg.SubMessage.User, msg.Channel), "slack", msg.Timestamp, msg.ThreadTimestamp)
	}

	/* E.g. threads and replies get a dupe event with
	 * an empty text.  Let's ignore those right
	 * away. */
	if len(txt) < 1 {
		return
	}

	updateSeen(r, txt)

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
	txt = SLACK_UNLINK_RE1.ReplaceAllString(txt, "${3}")
	txt = SLACK_UNLINK_RE2.ReplaceAllString(txt, "${1}")
	processMessage(r, txt)
}

func processSlackRateLimit(ev *slack.RateLimitEvent) {
	fmt.Fprintf(os.Stderr, "%v\n", ev)
}

func processSlackUserChangeEvent(ev *slack.UserChangeEvent) {
	if !ev.User.IsBot {
		return
	}

	newName := ev.User.Name
	oldReal := ev.User.Profile.RealName

	if oldReal == CONFIG["fullName"] {
		if newName != oldReal {
			verbose(1, "Bot was renamed from '%s' to '%s'!", oldReal, newName)
		}

		from := CONFIG["fullName"] + "@" + CONFIG["emailDomain"]
		to := []string{CONFIG["botOwner"] + "@" + CONFIG["emailDomain"]}
		subject := CONFIG["fullName"] + " bot change"
		body := fmt.Sprintf("New User Info:\n\n"+
			"ID: %s\n"+
			"TeamID: %s\n"+
			"Name: %s\n"+
			"Deleted: %v\n"+
			"RealName: %s\n"+
			"Profile:\n"+
			"  FirstName: %s\n"+
			"  LastName: %s\n"+
			"  RealName: %s\n"+
			"  Email: %s\n",
			ev.User.ID,
			ev.User.TeamID,
			ev.User.Name,
			ev.User.Deleted,
			ev.User.RealName,
			ev.User.Profile.FirstName,
			ev.User.Profile.LastName,
			ev.User.Profile.RealName,
			ev.User.Profile.Email)

		err := sendMailSMTP(from, to, []string{""}, subject, body)
		if len(err) > 0 {
			fmt.Fprintf(os.Stderr, "Unable to send bot change mail: %s\n", err)
			fmt.Fprintf(os.Stderr, "%v\n", ev)
		}
	}
}

func randomLineFromUrl(theUrl string) (line string) {
	rand.Seed(time.Now().UnixNano())
	data := getURLContents(theUrl, nil)
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
		fail("Error %s: %q\n", CONFIG["channelsFile"], err)
	}

	buf := bytes.Buffer{}
	buf.Write(b)

	d := gob.NewDecoder(&buf)
	if err := d.Decode(&CHANNELS); err != nil {
		fail("Unable to decode data: %s\n", err)
	}

	verbose(2, "Reading saved data from: %s", CONFIG["countersFile"])
	if _, err := os.Stat(CONFIG["countersFile"]); err != nil {
		return
	}

	b, err = ioutil.ReadFile(CONFIG["countersFile"])
	if err != nil {
		fail("Error %s: %q\n", CONFIG["countersFile"], err)
	}

	buf = bytes.Buffer{}
	buf.Write(b)

	d = gob.NewDecoder(&buf)
	if err := d.Decode(&COUNTERS); err != nil {
		fail("Unable to decode data: %s\n", err)
	}
}

func replaceFancyQuotes(in string) (out string) {
	out = in

	/* The Slack App replaces regular ascii quotes
	 * (0x22 and 0x27) with fancy unicode quotes,
	 * presumably based on locale.  This messes up
	 * our own shlex splitting later on, so undo
	 * that mess here. */
	out = strings.Replace(out, "“", "\"", -1)
	out = strings.Replace(out, "”", "\"", -1)
	out = strings.Replace(out, "‟", "\"", -1)
	out = strings.Replace(out, "„", "\"", -1)
	out = strings.Replace(out, "‘", "'", -1)
	out = strings.Replace(out, "’", "'", -1)
	out = strings.Replace(out, "‛", "'", -1)
	out = strings.Replace(out, "‚", "'", -1)

	/* Similarly, replace any no-break spaces etc. */
	out = strings.Replace(out, "\u00a0", " ", -1)
	out = strings.Replace(out, "\u202f", " ", -1)
	return
}

func reply(r Recipient, msg string) {
	incrementCounter("replies", msg)
	if r.ChatType == "hipchat" {
		if _, found := CHANNELS[r.ReplyTo]; found {
			HIPCHAT_CLIENT.Say(r.Id, CONFIG["fullName"], msg)
		} else {
			HIPCHAT_CLIENT.PrivSay(r.Id, CONFIG["fullName"], msg)
		}
	} else if r.ChatType == "slack" {
		recipient := r.ReplyTo
		channelName := "#"
		var options []slack.RTMsgOption
		if len(r.ThreadTs) > 0 {
			options = append(options, slack.RTMsgOptionTS(r.ThreadTs))
		}

		chfound := false
		for _, ch := range CHANNELS {
			if ch.Id == r.ReplyTo {
				channelName = ch.Name
				chfound = true
				break
			}
		}

		if !chfound {
			_, _, id, err := SLACK_RTM.OpenIMChannel(r.Id)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Unable to open private channel: %s\n%v\n", err, r)
				return
			}
			recipient = id
		}

		for len(msg) > SLACK_MAX_LENGTH {
			verbose(3, "Message length %d > limit %d, chunking...\n", len(msg), SLACK_MAX_LENGTH)
			m1 := msg[:SLACK_MAX_LENGTH-1]

			last_index := strings.LastIndex(m1, "\n")
			if last_index == 0 {
				last_index = strings.LastIndex(m1, " ")
			}
			if last_index == 0 {
				last_index = strings.LastIndex(m1, ",")
			}
			if last_index > 0 {
				m1 = msg[:last_index-1]
				msg = msg[last_index+1:]

				m1 = fontFormat(channelName, m1)
				SLACK_RTM.SendMessage(SLACK_RTM.NewOutgoingMessage(m1, recipient, options...))
			} else {
				SLACK_RTM.SendMessage(SLACK_RTM.NewOutgoingMessage("Message too long, truncating/chunking...\n", recipient, options...))
				SLACK_RTM.SendMessage(SLACK_RTM.NewOutgoingMessage(msg[:SLACK_MAX_LENGTH-1], recipient, options...))
				msg = msg[SLACK_MAX_LENGTH:]
			}
		}
		msg = fontFormat(channelName, msg)
		SLACK_RTM.SendMessage(SLACK_RTM.NewOutgoingMessage(msg, recipient, options...))
	}
}

func runCommand(cmd ...string) (out []byte, rval int) {
	var argv []string

	if len(cmd) == 0 {
		return
	}

	if len(cmd) == 1 {
		argv = strings.Split(dehtmlify(cmd[0]), " ")
	} else {
		for _, word := range cmd {
			argv = append(argv, dehtmlify(word))
		}
	}
	command := exec.Command(argv[0], argv[1:]...)

	rval = 0
	verbose(3, "Exec'ing '%s'...", argv)

	go func() {
		time.Sleep(30 * time.Second)
		if command != nil && command.ProcessState != nil &&
			command.ProcessState.Exited() != true {
			response := fmt.Sprintf("Sorry, I had to kill your '%s' command.\n", cmd)
			fmt.Fprintf(os.Stderr, ":: |%v|\n", command)
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

	gob.Register(map[string]int{})
	b = bytes.Buffer{}
	e = gob.NewEncoder(&b)
	if err := e.Encode(COUNTERS); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to encode counters: %s\n", err)
		return
	}

	err = ioutil.WriteFile(CONFIG["countersFile"], b.Bytes(), 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to write data to '%s': %s\n",
			CONFIG["countersFile"], err)
		return
	}

	memfile := CONFIG["memfile"]
	if len(memfile) > 0 {
		f, err := os.Create(memfile)
		if err != nil {
			fail("Unable to create memory profile: %s\n", err)
		}
		defer f.Close()
		runtime.GC()
		verbose(2, "Writing memory profile...")
		if err := pprof.WriteHeapProfile(f); err != nil {
			fail("Unable to write memory profile: %s\n", err)
		}
	}
}

func sendMailSMTP(from string, to, cc []string, subject, body string) (errstr string) {
	verbose(3, "Sending email from '%s' to '%s' with subject '%s'...", from, strings.Join(to, ", "), subject)

	msg := []byte(fmt.Sprintf("From: %s\r\n", from) +
		fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")) +
		fmt.Sprintf("Cc: %s\r\n", strings.Join(cc, ", ")) +
		fmt.Sprintf("Subject: %s\r\n", subject) +
		"X-Slack-Bot: jbot\r\n" +
		"\r\n" +
		body + "\r\n")

	err := smtp.SendMail(CONFIG["SMTP"], nil, from, to, msg)
	if err != nil {
		return fmt.Sprintf("%s", err)
	}

	return
}

func slackChannelPeriodics() {
	verbose(2, "Running slack channel periodics...")
	for _, chInfo := range CHANNELS {
		cveAlert(*chInfo)
		snowAlerts(*chInfo)
		jiraAlert(*chInfo, false)
	}
}

func slackLiveCheck() {
	verbose(2, "Checking if Slack is still sending me messages...")

	threshold := SLACK_LIVE_CHECK * PERIODICS * time.Second

	diff := time.Now().Sub(LAST_SLACK_MESSAGE_TIME)
	if diff.Seconds() > threshold.Seconds() {
		verbose(2, "Uhoh, I haven't seen any messages in %s seconds. Restarting...", threshold)
		serializeData()
		err := syscall.Exec(os.Args[0], os.Args, os.Environ())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to restart: %s\n", err)
		}
	}
}

func slackPeriodics() {
	ticks := PERIODICS * time.Second

	n := 0
	for _ = range time.Tick(ticks) {
		verbose(1, "Running slack periodics...")

		go serializeData()
		go slackChannelPeriodics()

		if (n % SLACK_CHANNEL_UPDATE_INTERVAL) == 0 {
			go updateSlackChannels()
		}
		if (n % CVE_FEED_UPDATE_INTERVAL) == 0 {
			updateCVEData()
		}

		if (n % SLACK_LIVE_CHECK) == 0 {
			slackLiveCheck()
		}
		n++
	}
}

func updateHipChatRooms(rooms []*hipchat.Room) {
	for _, room := range rooms {
		HIPCHAT_ROOMS[room.Id] = room
	}
}

func updateSlackChannels() {
	if CURRENTLY_UPDATING_CHANNELS {
		verbose(2, "Already busy updating channels...")
		return
	}

	verbose(2, "Updating all channels...")

	CURRENTLY_UPDATING_CHANNELS = true
	params := slack.GetConversationsParameters{
		Limit: 1000,
	}

	for {
	AGAIN:
		channels, cursor, err := SLACK_CLIENT.GetConversations(&params)
		if err != nil {
			if rateLimitedError, ok := err.(*slack.RateLimitedError); ok {
				seconds := rateLimitedError.RetryAfter / time.Millisecond / time.Microsecond
				verbose(3, "GetConversations rate limited.")
				verbose(3, "Sleeping for rate limit %d * 10 seconds...", seconds)
				time.Sleep(seconds * time.Second * 10)
				goto AGAIN
			} else {
				fmt.Fprintf(os.Stderr, "Unable to get conversations: %s\n", err)
				break
			}
		}
		for _, c := range channels {
			/* Let's not try to keep in
			 * memory a map of all users
			 * in all channels... */
			c.Members = []string{}
			SLACK_CHANNELS[c.Name] = c
		}
		if len(cursor) > 0 {
			params.Cursor = cursor
		} else {
			break
		}
	}

	CURRENTLY_UPDATING_CHANNELS = false
	verbose(3, "Fetched %d channels.", len(SLACK_CHANNELS))
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

	curses_re := regexp.MustCompile(`(shit|motherfucker|piss|f+u+c+k+|cunt|cocksucker|tits)`)
	curses_match := curses_re.FindAllString(msg, -1)

	yubifail_re := regexp.MustCompile(`eiddcc[a-z]{38}`)
	yubifail_match := yubifail_re.FindAllString(msg, -1)

	/* We don't keep track of priv messages, only public groupchat. */
	if ch, chfound := getChannel(r.ChatType, r.ReplyTo); chfound {
		var uInfo UserInfo

		uInfo.Seen = fmt.Sprintf(time.Now().Format(time.UnixDate))
		uInfo.Count = 1
		uInfo.Curses = 0
		uInfo.CurseWords = map[string]int{}
		uInfo.Yubifail = 0
		uInfo.Id = r.Id

		for _, curse := range curses_match {
			incrementCounter("curses", curse)
			incrementCounter("cursers", r.MentionName)
			count, found := uInfo.CurseWords[curse]
			if !found {
				uInfo.CurseWords[curse] = 1
			} else {
				uInfo.CurseWords[curse] = count + 1
			}
		}
		for _ = range yubifail_match {
			incrementCounter("yubifail", r.MentionName)
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
				uInfo.Yubifail = t.Yubifail + len(yubifail_match)
				uInfo.Curses = t.Curses + len(curses_match)
				uInfo.Count = t.Count + count

				/* Need to remember other counters here,
				 * lest they be reset. */
				for c, n := range t.CurseWords {
					uInfo.CurseWords[c] += n
				}
			}
			ch.HipChatUsers[*u] = uInfo
		} else if r.ChatType == "slack" {
			if len(ch.SlackUsers) < 1 {
				ch.SlackUsers = make(map[string]UserInfo, 0)
			}
			if t, found := ch.SlackUsers[r.MentionName]; found {
				uInfo.Yubifail = t.Yubifail + len(yubifail_match)
				uInfo.Curses = t.Curses + len(curses_match)
				uInfo.Count = t.Count + count

				/* Need to remember other counters here,
				 * lest they be reset. */
				for c, n := range t.CurseWords {
					uInfo.CurseWords[c] += n
				}
			}
			ch.SlackUsers[r.MentionName] = uInfo
		}
		CHANNELS[ch.Name] = ch
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

func updateChannels() {
	for n, ch := range CHANNELS {
		/* Note: this function populates the
		 * CHANNELS_BY_ID map so that we don't need to
		 * serialize that. */
		CHANNELS_BY_ID[ch.Id] = ch

		verbose(2, "Updating channel info for channel %s (#%s)...", ch.Id, n)
		if n != ch.Name {
			fmt.Fprintf(os.Stderr, "+++ dupe: %s (#%s)\n", ch.Id, n)
			delete(CHANNELS, n)
			continue
		}

		if !ch.Verified {
		RATE_LIMIT_LOOP:
			verbose(3, "Trying to get info on %s (%s)...\n", n, ch.Id)
			slackChannel, err := SLACK_CLIENT.GetConversationInfo(ch.Id, false)
			if err != nil {
				if rateLimitedError, ok := err.(*slack.RateLimitedError); ok {
					seconds := rateLimitedError.RetryAfter / time.Millisecond / time.Microsecond
					verbose(3, "Sleeping for rate limit %d seconds...", rateLimitedError.RetryAfter)
					time.Sleep(seconds * time.Second)
					goto RATE_LIMIT_LOOP
				}
				fmt.Fprintf(os.Stderr, "Unable to SLACK_CLIENT.GetConversationInfo(%s): %s\n",
					ch.Id, err)
				if fmt.Sprintf("%s", err) == "channel_not_found" {
					fmt.Fprintf(os.Stderr, "Removing myself from no-longer found channel '%s' (%s).\n",
						n, ch.Id)
					delete(CHANNELS, n)
				}
				continue
			}
			if slackChannel.IsExtShared {
				fmt.Fprintf(os.Stderr, "Removing myself from externally shared channel '%s' (%s).\n",
					slackChannel.Name, ch.Id)
				delete(CHANNELS, n)
				continue
			}
			ch.Verified = true
		}

		for t, v := range TOGGLES {
			if len(ch.Toggles) == 0 {
				ch.Toggles = map[string]bool{}
			}
			if _, found := ch.Toggles[t]; !found {
				ch.Toggles[t] = v
			}
		}

		if ch.Phishy == nil {
			ch.Phishy = &PhishCount{0, 0, time.Now(), time.Unix(0, 0)}
		}

		if ch.CVEs == nil {
			ch.CVEs = map[string]CVEItem{}
		}

		if ch.AutoReplies == nil {
			ch.AutoReplies = map[string]AutoReply{}
		}
	}
}

func verbose(level int, format string, v ...interface{}) {
	if level <= VERBOSITY {
		fmt.Fprintf(os.Stderr, "%s ", time.Now().Format("2006-01-02 15:04:05"))
		for i := 0; i < level; i++ {
			fmt.Fprintf(os.Stderr, "=")
		}
		fmt.Fprintf(os.Stderr, "> "+format+"\n", v...)
	}
}

/*
 * Main
 */

func main() {

	if err := os.Setenv("PATH", "/bin:/usr/bin:/sbin:/usr/sbin:/usr/local/bin:/home/y/bin:/home/y/sbin:/home/y/bin64"); err != nil {
		fail("Unable to set PATH: %s\n", err)
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
