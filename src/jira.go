/* This file contains functionality around the
 * various Jira commands, including "!jira"
 * and the Jira alert.
 */

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

var JIRA_REST = "/rest/api/latest"

type JiraFilterResult struct {
	ErrorMessages []string
	Jql           string
	Name          string
	ViewUrl       string
}

type JiraSearchResult struct {
	Issues []struct {
		Fields struct {
			Created  string
			Reporter struct {
				Name string
			}
			Status struct {
				Name string
			}
			Summary string
		}
		Key string
	}
}

func init() {
	ALERTS["jira-alert"] = "true"
	URLS["jira"] = "https://jira.XXXYOURDOMAINXXX.com"

	COMMANDS["jira"] = &Command{cmdJira,
		"display info about a jira ticket",
		URLS["jira"] + JIRA_REST,
		"!jira <ticket>",
		nil}
}

func cmdJira(r Recipient, chName string, args []string) (result string) {
	if len(args) != 1 {
		result = "Usage: " + COMMANDS["jira"].Usage
		return
	}

	urlArgs := map[string]string{
		"basic-auth-user":     CONFIG["jiraUser"],
		"basic-auth-password": CONFIG["jiraPassword"],
	}
	ticket := strings.TrimPrefix(args[0], URLS["jira"]+"/browse/")
	jiraUrl := fmt.Sprintf("%s/issue/%s", COMMANDS["jira"].How, ticket)
	data := getURLContents(jiraUrl, urlArgs)

	var jiraJson map[string]interface{}
	err := json.Unmarshal(data, &jiraJson)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal jira data: %s\n", err)
		return
	}

	if _, found := jiraJson["fields"]; !found {
		if errmsg, found := jiraJson["errorMessages"]; found {
			result = fmt.Sprintf("Unable to fetch data for %s: %s",
				ticket, errmsg.([]interface{})[0].(string))
			return
		}
		fmt.Fprintf(os.Stderr, "+++ jira fail for %s: %v\n", ticket, jiraJson)
		result = fmt.Sprintf("No data found for ticket %s", ticket)
		return
	}

	fields := jiraJson["fields"]
	status := fields.(map[string]interface{})["status"].(map[string]interface{})["name"]
	created := fields.(map[string]interface{})["created"]
	summary := fields.(map[string]interface{})["summary"]
	reporter := fields.(map[string]interface{})["reporter"].(map[string]interface{})["name"]

	result = fmt.Sprintf("```Summary : %s\n", summary)
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

	result += fmt.Sprintf("Reporter: %s```\n", reporter)
	result += fmt.Sprintf("%s/browse/%s", URLS["jira"], ticket)
	return
}

func jiraAlert(chInfo Channel, printFilter bool) {
	alertSettings, found := chInfo.Settings["jira-alert"]
	if !found {
		return
	}

	r := getRecipientFromMessage(fmt.Sprintf("%s@%s", CONFIG["mentionName"], chInfo.Id), "slack")

	for i, alert := range strings.Split(alertSettings, ";") {
		setval := strings.SplitN(alert, ",", 2)
		counter_num := 0
		alertCounter := fmt.Sprintf("jira-alert-counter%d", i)
		counter, found := chInfo.Settings[alertCounter]
		if found {
			c, err := strconv.Atoi(counter)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid '%s' for %s: %d", alertCounter, chInfo.Name, counter_num)
				counter_num = 1
			} else {
				counter_num = c
			}
		}

		if len(setval) != 2 {
			result := "Invalid setting for 'jira-alert'. See '!alerts jira-alert' for help.\n"
			reply(r, result)
			return
		}

		num := setval[0]
		filter := setval[1]

		alert_num, err := strconv.Atoi(num)
		if err != nil {
			msg := fmt.Sprintf("jira-alert interval '%s' invalid.\n", num)
			msg += "Please change via '!set jira-alert=<num,filterId>'.\n"
			reply(r, msg)
			return
		}

		filterId, err := strconv.Atoi(filter)
		if err != nil {
			msg := fmt.Sprintf("jira-alert filter '%s' invalid.\n", filter)
			msg += "Please change via '!set jira-alert=<num,filterId>'.\n"
			reply(r, msg)
			return
		}

		if printFilter {
			jiraFilter(chInfo, filterId, true)
		} else if counter_num == 0 || counter_num >= alert_num {
			jiraFilter(chInfo, filterId, false)
			counter_num = 0
		}
		counter_num += 1
		chInfo.Settings[alertCounter] = fmt.Sprintf("%d", counter_num)
	}
}

func jiraFilter(chInfo Channel, filterId int, printFilter bool) {
	verbose(4, "Running jira-alert in '%s'...", chInfo.Name)

	r := getRecipientFromMessage(fmt.Sprintf("%s@%s", CONFIG["mentionName"], chInfo.Id), "slack")
	theURL := fmt.Sprintf("%s%s/filter/%d", URLS["jira"], JIRA_REST, filterId)
	urlArgs := map[string]string{
		"basic-auth-user":     CONFIG["jiraUser"],
		"basic-auth-password": CONFIG["jiraPassword"],
	}
	data := getURLContents(theURL, urlArgs)

	var filter JiraFilterResult
	err := json.Unmarshal(data, &filter)
	if err != nil {
		reply(r, fmt.Sprintf("Unable to unmarshal jira data: %s\n", err))
		return
	}

	if len(filter.ErrorMessages) > 0 {
		msg := strings.Join(filter.ErrorMessages, "\n")
		msg += fmt.Sprintf("Review filter settings at %ssecure/EditFilter!default.jspa?filterId=%d", URLS["jira"], filterId)
		reply(r, msg)
		return
	}

	result := ""
	if printFilter {
		result = fmt.Sprintf("Filter %d is called '%s': %s\n", filterId, filter.Name, filter.ViewUrl)
	} else {
		result = jiraSearch(filter.Jql)
		if len(result) > 0 {
			result = fmt.Sprintf("Results for filter '<%s|%s>':\n", filter.ViewUrl, filter.Name) + result
		}
	}
	reply(r, result)
	return
}

func jiraSearch(jql string) (result string) {
	verbose(4, "Running jira search '%s'...", jql)

	theURL := fmt.Sprintf("%s%s/search?jql=%s", URLS["jira"], JIRA_REST, url.QueryEscape(jql))
	urlArgs := map[string]string{
		"basic-auth-user":     CONFIG["jiraUser"],
		"basic-auth-password": CONFIG["jiraPassword"],
	}
	data := getURLContents(theURL, urlArgs)

	var jiraJson JiraSearchResult
	err := json.Unmarshal(data, &jiraJson)
	if err != nil {
		result = fmt.Sprintf("Unable to unmarshal jira data: %s\n", err)
		return
	}

	if len(jiraJson.Issues) < 1 {
		return
	}

	for _, ticket := range jiraJson.Issues {
		summary := strings.ReplaceAll(ticket.Fields.Summary, "&", "&amp;")
		summary = strings.ReplaceAll(summary, ">", "&gt;")
		summary = strings.ReplaceAll(summary, "<", "&lt;")
		result += fmt.Sprintf("<%s/browse/%s|%s: %s> (Reporter: %s, Opened at: %s)\n",
			URLS["jira"], ticket.Key,
			ticket.Key, summary,
			ticket.Fields.Reporter,
			ticket.Fields.Created)
	}

	return
}
