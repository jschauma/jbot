/* This file contains functionality around the
 * OpsGenie parts of the '!oncall' command.
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const SLEEP_TIME = 5

func init() {
	URLS["opsgenie"] = "https://api.opsgenie.com/v2/"
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

func cmdOncallOpsGenie(r Recipient, chName, args string, allowRecursion bool) (result string) {
	var candidates []string
	scheduleFound := false
	wantedName := args
	originalWantedName := wantedName
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
			if (tname != sname) {
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
			wantedName = originalWantedName
		}

		if (!scheduleFound) {
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
	sleepCount := 1;

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
	if (ogu.Data == nil) {
		return
	}

	details = fmt.Sprintf("<https://directory.vzbuilders.com/view/vzm/%s|%s> (%s", u[:i], ogu.Data.(map[string]interface{})["fullName"].(string), u)
	for _, c := range ogu.Data.(map[string]interface{})["userContacts"].([]interface{}) {
		if c.(map[string]interface{})["contactMethod"].(string) == "voice" {
			details += fmt.Sprintf(", %s", c.(map[string]interface{})["to"].(string))
			break
		}
	}
	details += ")"

	return
}
