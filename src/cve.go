/* This file contains functionality around the
 * periodic parsing of NVD's CVE feed as well as the
 * '!cve' command.  Users may enable notifications
 * of CVE announcements in their channel via the
 * '!set cve-alert=true' setting. */

package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var ALL_CVES = map[string]CVEItem{}

const MAX_NEW_CVES = 30

type NvdCVSSV2 struct {
	BaseScore    float64
	Version      string
	VectorString string
}

type NvdCVSSV3 struct {
	BaseScore    float64
	Version      string
	VectorString string
}

type NvdCVEMetricV2 struct {
	CVSSv2                  NvdCVSSV2
	Severity                string
	ExploitabilityScore     float64
	ImpactScore             float64
	ObtainAllPrivilege      bool
	ObtainUserPrivilege     bool
	ObtainOtherPrivilege    bool
	UserInteractionRequired bool
}

type NvdCVEMetricV3 struct {
	CVSSv3              NvdCVSSV3
	ExploitabilityScore float64
	ImpactScore         float64
}

type NvdCVEImpact struct {
	BaseMetricV2 NvdCVEMetricV2
	BaseMetricV3 NvdCVEMetricV3
}

type NvdCVE_data_meta struct {
	ID string
}

type NvdCVEDescriptionData struct {
	Value string
}

type NvdCVEDescription struct {
	Description_data []NvdCVEDescriptionData
}

type NvdCVEReferenceData struct {
	Name      string
	Refsource string
	URL       string
	Tags      []string
}

type NvdCVEReferences struct {
	Reference_Data []NvdCVEReferenceData
}

type NvdCVE struct {
	CVE_data_meta NvdCVE_data_meta
	References    NvdCVEReferences
	Description   NvdCVEDescription
}

type CVEItem struct {
	CVE              NvdCVE
	Impact           NvdCVEImpact
	PublishedDate    string
	LastModifiedDate string
}

type NvdFeed struct {
	CVEItems         []CVEItem `json:"CVE_Items"`
	PublishedDate    string
	LastModifiedDate string
}

func init() {
	URLS["cvefeed"] = "https://nvd.nist.gov/feeds/json/cve/1.0/nvdcve-1.0-recent.json.gz"
	COMMANDS["cve"] = &Command{cmdCve,
		"display vulnerability description",
		"https://v1.cveapi.com/",
		"!cve <cve-id>",
		nil}
}

func cmdCve(r Recipient, chName string, args []string) (result string) {
	input := strings.Join(args, " ")
	cves := args
	if len(cves) != 1 {
		result = "Usage: " + COMMANDS["cve"].Usage
		return
	}

	cve := strings.TrimSpace(cves[0])

	if !strings.HasPrefix(cve, "CVE-") {
		cve = fmt.Sprintf("CVE-%s", cve)
	}

	if c, found := ALL_CVES[cve]; found {
		result = formatCVEData(c)
		return
	}

	theUrl := fmt.Sprintf("%s%s.json", COMMANDS["cve"].How, cve)
	data := getURLContents(theUrl, nil)

	var cveData CVEItem
	err := json.Unmarshal(data, &cveData)
	if err != nil {
		if strings.HasPrefix(err.Error(), "invalid character") {
			result = fmt.Sprintf("No CVE data found for '%s'.\n", input)
			result += "Perhaps that CVE is still not public in MITRE?\n"
			result += "https://cve.mitre.org/cgi-bin/cvename.cgi?name=" + input
		} else {
			result = fmt.Sprintf("Unable to unmarshal cveapi data: %s\n", err)
		}
		return
	}

	result = formatCVEData(cveData)
	return
}

func cveAlert(chInfo Channel) {
	cve_alert, found := chInfo.Settings["cve-alert"]
	if !found {
		return
	}

	/* Unset, e.g. "cve-alert=''" */
	if len(cve_alert) < 1 {
		return
	}

	r := getRecipientFromMessage(fmt.Sprintf("%s@%s", CONFIG["mentionName"], chInfo.Id), "slack")
	v, err := strconv.ParseBool(cve_alert)

	verbose(3, "Running cve-alert in '%s' (%v)...", chInfo.Name, v)
	if err != nil {
		msg := fmt.Sprintf("'cve-alert' setting '%s' invalid.\n", cve_alert)
		msg += "Please change via '!set cve-alert=<0|1|true|false>'."
		reply(r, msg)
		return
	} else if !v {
		return
	}

	silent := false
	count := 0
	for id, cve := range ALL_CVES {
		if _, found := chInfo.CVEs[id]; found {
			continue
		} else {
			chInfo.CVEs[id] = cve
		}

		count++
		if count > MAX_NEW_CVES {
			if !silent {
				reply(r, "...\n")
			}
			silent = true
		}

		msg := formatCVEData(cve)
		if !silent {
			reply(r, msg)
		}
	}
}

func formatCVEData(cve CVEItem) (msg string) {
	id := cve.CVE.CVE_data_meta.ID

	msg = "<https://cve.mitre.org/cgi-bin/cvename.cgi?name=" + id + "|" + id + ">\n"

	for _, d := range cve.CVE.Description.Description_data {
		msg += d.Value
	}

	baseMetricV3 := cve.Impact.BaseMetricV3
	msg += "```CVSSv3              : " + baseMetricV3.CVSSv3.VectorString + "\n"
	msg += fmt.Sprintf("Exploitability Score: %.1f\n", baseMetricV3.ExploitabilityScore)
	msg += fmt.Sprintf("Impact Score        : %.1f\n", baseMetricV3.ImpactScore)

	baseMetricV2 := cve.Impact.BaseMetricV2
	msg += "CVSSv2              : " + baseMetricV2.CVSSv2.VectorString + "\n"
	msg += "Severity            : " + baseMetricV2.Severity + "\n"
	msg += fmt.Sprintf("Exploitability Score: %.1f\n", baseMetricV2.ExploitabilityScore)
	msg += fmt.Sprintf("Impact         Score: %.1f\n", baseMetricV2.ImpactScore)

	msg += "Published Date      : " + cve.PublishedDate + "\n"
	msg += "Last Modified Date  : " + cve.LastModifiedDate + "\n"

	msg += "```\nReferences:\n"
	for _, r := range cve.CVE.References.Reference_Data {
		msg += r.URL
		if len(r.Tags) > 0 {
			msg += fmt.Sprintf(" (%s)\n", strings.Join(r.Tags, ", "))
		}
		msg += "\n"
	}
	return
}

func updateCVEData() {
	verbose(2, "Updating CVE Data...")

	data := getURLContents(URLS["cvefeed"], nil)

	b := bytes.NewReader(data)
	gz, err := gzip.NewReader(b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create a new gzip reader: %s\n",
			err)
		return
	}
	defer gz.Close()

	var nvdfeed NvdFeed
	err = json.NewDecoder(gz).Decode(&nvdfeed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to unmarshal NVD CVE Feed data: %s\n", err)
		return
	}

	for _, cve := range nvdfeed.CVEItems {
		id := cve.CVE.CVE_data_meta.ID
		if _, found := ALL_CVES[id]; !found {
			ALL_CVES[id] = cve
		}
	}
}
