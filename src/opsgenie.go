/* This file contains functionality around the
 * OpsGenie parts of the '!oncall' command.
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SLEEP_TIME = 5

func init() {
	URLS["opsgenie"] = "https://api.opsgenie.com/v2/"
	URLS["directory"] = "https://XXXdirectoryURLXXX"

	COMMANDS["oncall"] = &Command{cmdOncall,
		"show who's oncall",
		"Service Now & OpsGenie",
		"!oncall [<group>]\nIf <group> is not specified, this uses the channel name.\nUse '!set oncall=<rotation-name>' to change the default.\nIf your <rotation name> contains spaces, you have to quote the argument ('!set oncall=\"<rotation name>\"').\nYou can also specify multiple rotations by separating them with a comma (','); this works for both explicit invocations as well as for channel settings.\n\nIf you invoke the command and follow it with multiple arguments ('!oncall please look at ticket 12345'), then I will @-mention the current oncall for the rotation set in the channel and point them to your message.\n\nIf your channel does not have an oncall rotation and you want to have me reply to users with some other message, use '!set noncall=\"your message here\", and I will give people \"your message here\" when they run '!oncall'.\n\n",
		[]string{"on_call", "on-call"}}
}

type OpsGenieScheduleInfo struct {
	Candidates   []string
	ScheduleId   string
	ScheduleName string
	TeamId       string
	TeamName     string
}

type OpsGenieApiData struct {
	Message string
	Data    interface{}
}

func cmdOncall(r Recipient, chName string, args []string) (result string) {
	input := strings.Join(args, " ")

	var oncall string
	var noncall string
	if len(args) == 1 {
		oncall = args[0]
	}
	oncall_source := "user input"

	atMention := false
	uparrow_re := regexp.MustCompile(`(?i)^(-*\^+|https?://)`)
	if uparrow_re.Match([]byte(input)) || len(args) > 1 {
		atMention = true
		oncall = ""
	}

	if len(oncall) < 1 {
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
			noncall, _ = ch.Settings["noncall"]
		} else if !atMention {
			result = "Usage: " + COMMANDS["oncall"].Usage
			return
		}
	}

	for n, rot := range strings.Split(oncall, ",") {

		oncallFound := true
		if n > 0 {
			result += "\n---\n"
		}
		result += cmdOncallOpsGenie(r, chName, rot, true)
		if len(result) < 1 {
			result = fmt.Sprintf("No oncall information found for '%s'.\n", oncall)
			oncallFound = false
		}
		if strings.HasPrefix(result, "No OpsGenie schedule found for") {
			oncallFound = false
		}

		if !oncallFound {
			if len(noncall) > 0 && oncall_source != "user input" {
				result = noncall
				continue
			}
			switch oncall_source {
			case "channel name":
				result += fmt.Sprintf("\nIf your oncall rotation does not match your channel name (%s), use '!set oncall=<rotation_name>'.\n", chName)
			case "channel setting":
				result += fmt.Sprintf("\nIs your 'oncall' channel setting (%s) correct?\n", oncall)
				result += "If not, use '!set oncall=<rotation_name>' to fix that.\n"
			}
		}

		if atMention {
			user_re := regexp.MustCompile(fmt.Sprintf("(?i)%s/([^|]+)", URLS["directory"])
			if m := user_re.FindAllStringSubmatch(result, -1); len(m) > 0 {
				users := map[string]bool{}
				for _, u := range m {
					user, err := SLACK_CLIENT.GetUserByEmail(u[1] + "@" + CONFIG["emailDomain"])
					if err == nil {
						users[fmt.Sprintf("<@%s>", user.ID)] = true
					}
				}
				if len(users) > 0 {
					var keys []string
					for k, _ := range users {
						keys = append(keys, k)
					}
					sort.Strings(keys)

					result = ""
					for _, k := range keys {
						result += k + " "
					}
					result += " --^"
				}
			}
		}

	}
	return
}

func cmdOncallOpsGenie(r Recipient, chName, args string, allowRecursion bool) (result string) {
	var candidates []string
	scheduleFound := false
	wantedName := args
	scheduleURL := "https://app.opsgenie.com/schedule#/"

	if len(CONFIG["opsgenieApiKey"]) < 1 {
		result = "Unable to query OpsGenie -- no API key in config file."
		return
	}

	theUrl := URLS["opsgenie"] + "schedules"
	schedules := getOpsgenieAPIData(theUrl)
	if len(schedules.Message) > 0 {
		result = schedules.Message
		return
	}

LabelLookup:
	sinfo := opsGenieIds(schedules, wantedName)

	for _, sched := range sinfo {
		sid := sched.ScheduleId
		tid := sched.TeamId
		sname := sched.ScheduleName
		tname := sched.TeamName

		if len(sched.Candidates) > 0 {
			candidates = append(candidates, sched.Candidates...)
			continue
		}

		theUrl = URLS["opsgenie"] + "schedules/" + sid + "/on-calls"
		ogOncalls := getOpsgenieAPIData(theUrl)
		if len(ogOncalls.Message) > 0 {
			result = schedules.Message
			return
		}

		participants := ogOncalls.Data.(map[string]interface{})["onCallParticipants"].([]interface{})
		if len(participants) > 0 {
			scheduleFound = true
			if tname != sname {
				result += fmt.Sprintf("Team %s: Schedule ", tname)
			}
			result += fmt.Sprintf("<%s%s|%s>:\n", scheduleURL, sid, sname)
		}

		for _, participant := range participants {
			if participant != nil {
				result += fmt.Sprintf("%s\n", opsgenieUserDetails(participant.(map[string]interface{})["name"].(string)))
			}
		}

		rotationTeams := getOpsgenieRotations(sid)
		for _, t := range rotationTeams {
			wantedName = t
			goto LabelLookup
		}

		if !scheduleFound {
			result = fmt.Sprintf("Team '%s' schedule '%s' found in OpsGenie, but nobody's currently oncall.\n", tname, sname)
			result += fmt.Sprintf("%s%s\n", scheduleURL, sid)

			theUrl = URLS["opsgenie"] + "teams/" + tid
			ogt := getOpsgenieAPIData(theUrl)
			if len(ogt.Message) > 0 {
				result = schedules.Message
				return
			}

			var members []string
			jsonMembers := ogOncalls.Data.(map[string]interface{})["members"]
			if jsonMembers != nil {
				for _, m := range jsonMembers.([]interface{}) {
					u := m.(map[string]interface{})["user"].(map[string]interface{})["username"].(string)
					members = append(members, u)
				}
			}

			if len(members) > 0 {
				result += fmt.Sprintf("You can try contacting the members of owning team '%s':\n", tname)
				result += strings.Join(members, ", ")
				result += "\n"
			}
		}
	}

	if !scheduleFound && len(candidates) > 0 {
		if len(candidates) == 1 && strings.EqualFold(wantedName, candidates[0]) &&
			allowRecursion {
			return cmdOncallOpsGenie(r, chName, candidates[0], false)
		}
		result += fmt.Sprintf("No OpsGenie schedule found for rotation '%s'.\n", wantedName)
		result += "\nPossible candidates:\n"
		result += strings.Join(candidates, ", ")
	}
	return
}

func getOpsgenieAPIData(url string) (ogData OpsGenieApiData) {
	key := CONFIG["opsgenieApiKey"]
	urlArgs := map[string]string{"Authorization": "GenieKey " + key}
LabelOncalls:
	data := getURLContents(url, urlArgs)
	sleepCount := 1

	err := json.Unmarshal(data, &ogData)
	if err != nil {
		ogData.Message = fmt.Sprintf("Unable to unmarshal opsgenie data: %s\n", err)
		return
	}

	if len(ogData.Message) > 0 && strings.Contains(ogData.Message, "You are making too many requests!") {
		if sleepCount > 4 {
			ogData.Message = "I'm rate limited by OpsGenie. Please try again later."
			return
		}
		time.Sleep(time.Duration(sleepCount*SLEEP_TIME) * time.Second)
		sleepCount++
		goto LabelOncalls
	}

	return
}

func getOpsgenieRotations(id string) (rnames []string) {
	theUrl := URLS["opsgenie"] + "schedules/" + id + "/rotations"
	rotations := getOpsgenieAPIData(theUrl)

	data := rotations.Data.([]interface{})
	for _, r := range data {
		participants := r.(map[string]interface{})["participants"].([]interface{})
		for _, p := range participants {
			if p.(map[string]interface{})["type"].(string) == "team" {
				rnames = append(rnames, p.(map[string]interface{})["name"].(string))
			}
		}
	}

	return
}

func opsGenieIds(schedules OpsGenieApiData, wantedName string) (info []OpsGenieScheduleInfo) {
	data := schedules.Data.([]interface{})
	for _, s := range data {
		var i OpsGenieScheduleInfo

		i.ScheduleId = s.(map[string]interface{})["id"].(string)
		ownerTeam := s.(map[string]interface{})["ownerTeam"]
		if ownerTeam == nil || len(ownerTeam.(map[string]interface{})["name"].(string)) < 0 {
			continue
		}
		i.TeamName = ownerTeam.(map[string]interface{})["name"].(string)
		sname := s.(map[string]interface{})["name"].(string)
		_sname := sname
		if strings.HasSuffix(sname, "_schedule") {
			sname = sname[0:strings.Index(sname, "_schedule")]
		}
		i.TeamId = ownerTeam.(map[string]interface{})["id"].(string)

		if strings.EqualFold(_sname, wantedName) || strings.EqualFold(i.TeamName, wantedName) {
			i.ScheduleName = sname
			info = append(info, i)
		} else if strings.Contains(strings.ToLower(sname), strings.ToLower(wantedName)) {
			i.Candidates = append(i.Candidates, sname)
			info = append(info, i)
		} else if strings.Contains(strings.ToLower(i.TeamName), strings.ToLower(wantedName)) {
			i.Candidates = append(i.Candidates, i.TeamName)
			info = append(info, i)
		}
	}
	return
}

func opsgenieUserDetails(u string) (details string) {
	theURL := fmt.Sprintf("%susers/%s?expand=contact", URLS["opsgenie"], u)
	urlArgs := map[string]string{"Authorization": "GenieKey " + CONFIG["opsgenieApiKey"]}
	data := getURLContents(theURL, urlArgs)

	var ogu OpsGenieApiData
	err := json.Unmarshal(data, &ogu)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to unmarshal json from '%s': %s\n",
			theURL, err)
		return
	}

	i := strings.Index(u, "@")
	if i < 0 {
		i = len(u)
	}
	if ogu.Data == nil {
		return
	}

	details = fmt.Sprintf("<%s/%s|%s> (%s", URLS["directory"], u[:i], ogu.Data.(map[string]interface{})["fullName"].(string), u)
	for _, c := range ogu.Data.(map[string]interface{})["userContacts"].([]interface{}) {
		if c.(map[string]interface{})["contactMethod"].(string) == "voice" {
			details += fmt.Sprintf(", %s", c.(map[string]interface{})["to"].(string))
			break
		}
	}
	details += ")"

	return
}
