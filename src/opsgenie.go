/* This file contains functionality around the
 * OpsGenie parts of the '!oncall' command.
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func init() {
	URLS["opsgenie"] = "https://api.opsgenie.com/v2/"
}

type OpsGenieOncallData struct {
	Data struct {
		OnCallParticipants []struct {
			Id   string
			Name string
			Type string
		}
	}
}

type OpsGenieUser struct {
	Data struct {
		FullName     string
		UserContacts []struct {
			ContactMethod string
			To            string
		}
	}
}

type OpsGenieTeam struct {
	Data struct {
		Members []struct {
			Role string
			User struct {
				Id       string
				Username string
			}
		}
		Name string
	}
}

type OpsGenieSchedules struct {
	Data []struct {
		Id        string
		Name      string
		OwnerTeam struct {
			Id   string
			Name string
		}
	}
}

func cmdOncallOpsGenie(r Recipient, chName, args string, allowRecursion bool) (result string) {
	schedule_found := false
	wantedName := args
	key := CONFIG["opsgenieApiKey"]

	if len(key) < 1 {
		result = "Unable to query OpsGenie -- no API key in config file."
		return
	}

	theUrl := URLS["opsgenie"] + "schedules"
	urlArgs := map[string]string{"Authorization": "GenieKey " + key}
	data := getURLContents(theUrl, urlArgs)

	var candidates []string
	scheduleURL := "https://app.opsgenie.com/schedule#/"

	var schedules OpsGenieSchedules

	err := json.Unmarshal(data, &schedules)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal opsgenie data: %s\n", err)
		return
	}

	for _, s := range schedules.Data {
		id := s.Id
		ownerTeam := s.OwnerTeam
		if len(ownerTeam.Name) < 0 {
			continue
		}
		tname := ownerTeam.Name
		sname := s.Name
		_sname := sname
		if strings.HasSuffix(sname, "_schedule") {
			sname = sname[0:strings.Index(sname, "_schedule")]
		}
		tid := ownerTeam.Id

		if strings.EqualFold(_sname, wantedName) || strings.EqualFold(tname, wantedName) {
			theUrl := URLS["opsgenie"] + "schedules/" + id + "/on-calls"
			data := getURLContents(theUrl, urlArgs)

			var ogOncalls OpsGenieOncallData

			err := json.Unmarshal(data, &ogOncalls)
			if err != nil {
				result = fmt.Sprintf("Unable to unmarshal opsgenie data: %s\n", err)
				return
			}

			if len(ogOncalls.Data.OnCallParticipants) > 0 {
				schedule_found = true
				result += fmt.Sprintf("<%s%s|%s>:\n", scheduleURL, id, sname)
			}

			for _, participant := range ogOncalls.Data.OnCallParticipants {
				result += fmt.Sprintf("%s\n", opsgenieUserDetails(participant.Name))
			}

			if !schedule_found {
				result = fmt.Sprintf("Schedule(s) found in OpsGenie for '%s', but nobody's currently oncall.\n", sname)
				result += fmt.Sprintf("%s%s\n", scheduleURL, id)

				theUrl = URLS["opsgenie"] + "teams/" + tid
				data := getURLContents(theUrl, urlArgs)

				var ogt OpsGenieTeam

				err = json.Unmarshal(data, &ogt)
				if err != nil {
					result += fmt.Sprintf("Unable to unmarshal opsgenie data: %s\n", err)
					return
				}

				var members []string
				for _, m := range ogt.Data.Members {
					members = append(members, m.User.Username)
				}

				if len(members) > 0 {
					result += fmt.Sprintf("You can try contacting the members of owning team '%s':\n", tname)
					result += strings.Join(members, ", ")
					result += "\n"
				}
			}
		} else if strings.Contains(strings.ToLower(sname), strings.ToLower(wantedName)) {
			candidates = append(candidates, sname)
		} else if strings.Contains(strings.ToLower(tname), strings.ToLower(wantedName)) {
			candidates = append(candidates, tname)
		}
	}

	if !schedule_found && len(candidates) > 0 {
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

func opsgenieUserDetails(u string) (details string) {
	theURL := fmt.Sprintf("%susers/%s?expand=contact", URLS["opsgenie"], u)
	urlArgs := map[string]string{"Authorization": "GenieKey " + CONFIG["opsgenieApiKey"]}
	data := getURLContents(theURL, urlArgs)

	var ogu OpsGenieUser
	err := json.Unmarshal(data, &ogu)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to unmarshal json from '%s': %s\n",
			theURL, err)
		return
	}

	details = fmt.Sprintf("%s (%s", ogu.Data.FullName, u)
	for _, c := range ogu.Data.UserContacts {
		if c.ContactMethod == "voice" {
			details += fmt.Sprintf(", %s", c.To)
			break
		}
	}
	details += ")"

	return
}
