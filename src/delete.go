/* This file contains functionality around the
 * '!delete' command, letting the user ask the bot to
 * delete one of its messages.
 *
 * Note: only the botowner can ask the bot to delete
 * its own messages.
 *
 * Usage:
 * !delete URL
 */

package main

import (
	"fmt"
	"regexp"
)

func init() {
	COMMANDS["delete"] = &Command{cmdDelete,
		"delete a slack message (only available to the bot owner)",
		"Slack API",
		"!delete URL",
		nil}
}

func cmdDelete(r Recipient, chName string, args []string) (result string) {

	/* If this command fails, somehow we get the
	 * same command delivered to us by the API as
	 * if coming from the channel.  No idea why,
	 * but the channel has no "MentionName", so we
	 * can ignore that. */
	if len(r.MentionName) < 1 {
		return
	}
	if CONFIG["botOwner"] != r.MentionName {
		result = fmt.Sprintf("Sorry, %s is not allowed to run this command.", r.MentionName)
		return
	}

	if len(args) < 1 {
		result = "Usage: " + COMMANDS["delete"].Usage
		return
	}
	input := args[0]

	/* The URL given should be of the format:
	 * https://foo.slack.com/archives/<channelID>/p[0-9]+
	 * The numbers are seconds since epoch + 6
	 * digits milliseconds or whatever granularity
	 * the API uses here. */
	url_re := regexp.MustCompile(`(?i)https://.*slack.com/archives/(.*)/p([0-9]+)([0-9]{6})$`)
	m := url_re.FindStringSubmatch(input)
	if len(m) < 1 {
		result = "Invalid input. Your URL should look like this:\n"
		result += "https://foo.slack.com/archives/<channelID>/p[0-9]+\n"
		return
	}
	ch := m[1]
	ts := m[2] + "." + m[3]
	if _, _, err := SLACK_CLIENT.DeleteMessage(ch, ts); err != nil {
		result = "Unable to delete the given message: "
		result += fmt.Sprintf("%s\n", err)
		return
	}

	result = "Message deleted."

	return
}
