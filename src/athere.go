/* This file contains functionality around the
 * automated replies to '@here' and '@channel'.
 *
 * Settings for the auto-replies are:
 * !set athere|atchannel topic
 * !set athere|atchannel shame
 *
 *
 * For consistency's sake, this file also contains the
 * 'atnoyance' functionality, although that's more of
 * a chatter thing.
 */

package main

import (
	"fmt"
	"strings"
)

func athere(msg string, ch *Channel, r Recipient) (result string) {
	athereSetting := ch.Settings["athere"]
	atchannelSetting := ch.Settings["atchannel"]

	if !(strings.Contains(msg, "<!channel>") || strings.Contains(msg, "<!here>")) {
		return
	}

	if len(r.MentionName) > 0 {
		incrementCounter("atnoisers", r.MentionName)
	}

	if len(athereSetting) < 1 && len(atchannelSetting) < 1 {
		return
	}

	atChannel := (strings.Contains(msg, "<!channel>") && len(atchannelSetting) > 0)
	atHere := (strings.Contains(msg, "<!here>") && len(athereSetting) > 0)

	slackChannel, err := SLACK_CLIENT.GetConversationInfo(ch.Id, false)

	if (strings.EqualFold(atchannelSetting, "topic") && atChannel) ||
		(strings.EqualFold(athereSetting, "topic") && atHere) {
		if err != nil {
			result = fmt.Sprintf("Unable to get channel information for channel '%s' (%s): %s",
				ch.Name, ch.Id, err)
			return
		}

		if len(slackChannel.Topic.Value) > 0 {
			result += slackChannel.Topic.Value
			result += "\n\n"
		}
	}

	/*
		if ((strings.EqualFold(atchannelSetting, "pinned") && atChannel) ||
			(strings.EqualFold(athereSetting, "pinned") && atHere)) {
			pinnedItems, _, err := SLACK_CLIENT.ListPins(ch.Id)
			if err != nil {
				result = fmt.Sprintf("Unable to get pinned items for channel '%s' (%s): %s",
							ch.Name, ch.Id, err)
				return
			}
			for _, pin := range pinnedItems {
				if pin.Message != nil {
					result += pin.Message.Text
					result += "\n"
				}
			}
		}
	*/

	if (strings.EqualFold(atchannelSetting, "shame") && atChannel) ||
		(strings.EqualFold(athereSetting, "shame") && atHere) {
		num := len(getAllMembersInChannel(slackChannel.ID))
		trigger := "channel"
		if atHere {
			trigger = "here"
		}
		result += fmt.Sprintf("You just alerted %d users.\n", num)
		result += fmt.Sprintf("Please carefully consider if that is really necessary before you use `@%s` the next time.\n", trigger)
	}

	if (strings.EqualFold(atchannelSetting, "insult") && atChannel) ||
		(strings.EqualFold(athereSetting, "insult") && atHere) {
		result = cmdInsult(r, r.ReplyTo, []string{"me"})
	}

	return
}
