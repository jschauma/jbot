/* This file contains functionality around the
 * '!secheaders' command, letting the user
 * display a letter grade for their use of
 * Security Headers similar to securityheaders.com.
 *
 * Usage:
 * !secheaders <url>
 */

package main

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const HSTS_MINVAL = 2592000
const HSTS_RECOMMENDED = 31536000

type secheader struct {
	Wanted bool
	Check  func(string) (string, float64)
}

var SECURITY_HEADERS = map[string]secheader{
	"content-security-policy":     secheader{true, checkCSP},
	"expect-ct":                   secheader{true, checkExpectCT},
	"feature-policy":              secheader{true, nil},
	"public-key-pins-report-only": secheader{false, nil},
	"public-key-pins":             secheader{false, nil},
	"referrer-policy":             secheader{true, checkReferrerPolicy},
	"strict-transport-security":   secheader{true, checkSTS},
	"x-content-type-options":      secheader{true, checkContentTypeOptions},
	"x-frame-options":             secheader{true, checkFrameOptions},
	"x-xss-protection":            secheader{true, checkXSSProtection},
}

func checkCSP(hval string) (comment string, val float64) {
	findings := ""
	rval := 1.0
	if !strings.Contains(hval, "report-uri https://") {
		findings += "missing 'report-uri' directive;"
		rval -= 0.1
	}
	if !strings.Contains(hval, "default-src 'self'") {
		findings += "missing \"default-src 'self'\" directive;"
		rval -= 0.1
	}
	if strings.Contains(hval, "unsafe-eval") {
		findings += "avoid 'unsafe-eval';"
		rval -= 0.2
	}
	if strings.Contains(hval, "unsafe-inline") {
		findings += "avoid 'unsafe-inline';"
		rval -= 0.1
	}
	if strings.Contains(hval, "http://") {
		findings += "avoid loading resources over 'http://';"
		rval -= 0.1
	}
	return findings, rval
}

func checkExpectCT(hval string) (comment string, val float64) {
	/* for now we can just apply the same logic as for STS */
	return checkSTS(hval)
}

func checkSTS(hval string) (comment string, val float64) {
	maxage_re := regexp.MustCompile(`(?i)max-age=([0-9]+)([,;].*)?`)
	m := maxage_re.FindStringSubmatch(hval)
	if len(m) < 1 {
		return "Invalid header.", 0.0
	}

	v, err := strconv.Atoi(m[1])
	if err != nil {
		return fmt.Sprintf("Invalid number for 'max-age': '%s'.", m[1]), 0.0
	}

	if v < HSTS_MINVAL {
		return fmt.Sprintf("The 'max-age' value '%d' is too small. The minimum recommended value is %d, %d desired.", v, HSTS_MINVAL, HSTS_RECOMMENDED), 0.5
	} else if v < HSTS_RECOMMENDED {
		return fmt.Sprintf("The 'max-age' value should be %d, not %d.", HSTS_RECOMMENDED, v), 0.8
	}
	return "", 1.0
}

func checkContentTypeOptions(hval string) (comment string, val float64) {
	wanted := "nosniff"
	if strings.ToLower(hval) == wanted {
		return "", 1.0
	}
	return fmt.Sprintf("should be '%s' instead of '%s'", wanted, hval), 0.0
}

func checkFrameOptions(hval string) (comment string, val float64) {
	switch strings.ToLower(hval) {
	case "deny":
		return "", 1.0
	case "sameorigin":
		return "SAMEORIGIN accepted, DENY preferred", 0.8
	default:
		return fmt.Sprintf("Unexpected value '%s'.", hval), 0.0
	}
}

func checkReferrerPolicy(hval string) (comment string, val float64) {
	wanted := "strict-origin-when-cross-origin"
	accepted := "no-referrer-when-downgrade"
	if strings.ToLower(hval) == wanted {
		return "", 1.0
	} else if strings.ToLower(hval) == accepted {
		return fmt.Sprintf("'%s' acceptable, but '%s' preferred.", accepted, wanted), 0.8
	}
	return fmt.Sprintf("should be '%s' instead of '%s'", wanted, hval), 0.0
}

func checkXSSProtection(hval string) (comment string, val float64) {
	wanted := "1; mode=block"
	if strings.HasPrefix(hval, "1;") && strings.Contains(hval, "mode=block") {
		return "", 1.0
	} else if strings.HasPrefix(hval, "1; report=") {
		return "Report mode is a good start, but should move to 'mode=block'.", 0.8
	}
	return fmt.Sprintf("should be '%s' instead of '%s'", wanted, hval), 0.0
}

func init() {
	COMMANDS["secheaders"] = &Command{cmdSecheaders,
		"show securityheaders grade",
		"built-in",
		"!secheaders <url>",
		[]string{"sec-headers"}}
}

func cmdSecheaders(r Recipient, chName string, args []string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["secheaders"].Usage
		return
	}

	u := args[0]
	if !strings.HasPrefix(u, "http") {
		u = "https://" + u
	}

	_, err := url.Parse(u)
	if err != nil {
		result = fmt.Sprintf("Unable to parse url '%s': %s\n", u, err)
		return
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		result = fmt.Sprintf("Unable to create new request for '%s': %s\n", u, err)
		return
	}

	/* Without a browser UA, some sites don't
	 * return the full set of headers */
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/63.0.3239.132 Safari/537.36")

	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		result = fmt.Sprintf("Unable to make a request for '%s': %s\n", u, err)
		return
	}

	lcHeaders := (map[string][]string)(res.Header)
	for h, hvals := range res.Header {
		lcHeaders[strings.ToLower(h)] = hvals
	}

	sheaders := make([]string, len(SECURITY_HEADERS))
	i := 0
	for k, _ := range SECURITY_HEADERS {
		sheaders[i] = k
		i++
	}
	sort.Strings(sheaders)

	correct := 0.0
	for _, h := range sheaders {
		sech := SECURITY_HEADERS[h]
		if hvals, found := lcHeaders[h]; !found {
			if sech.Wanted && sech.Check != nil {
				result += ":x: missing '" + strings.Title(h) + "'\n"
			} else {
				correct++
			}
		} else if !sech.Wanted {
			result += ":x: '" + strings.Title(h) + "' should not be set\n"
		} else {
			comment := ""
			val := 1.0

			if len(hvals) > 1 {
				comment = "header set multiple times"
				val = 0.0
			} else if len(hvals) < 1 || len(hvals[0]) < 1 {
				comment = "header set but empty"
				val = 0.0
			}

			comment, val = sech.Check(hvals[0])
			slackmoji := ":white_check_mark:"
			if val < 1.0 {
				slackmoji = ":warning:"
			}
			result += fmt.Sprintf("%s %s", slackmoji, strings.Title(h))
			if len(comment) > 0 {
				result += fmt.Sprintf(" (%s)", comment)
			}
			result += "\n"
			correct += val
		}
	}

	grade := correct / float64(len(SECURITY_HEADERS))
	letterGrade := "F"
	if grade >= 0.95 {
		letterGrade = "A+"
	} else if grade >= 0.9 {
		letterGrade = "A"
	} else if grade >= 0.85 {
		letterGrade = "B+"
	} else if grade >= 0.8 {
		letterGrade = "B"
	} else if grade >= 0.75 {
		letterGrade = "C+"
	} else if grade >= 0.7 {
		letterGrade = "C"
	}
	result = fmt.Sprintf("Security Headers Grade for '%s': %s\n%s", u, letterGrade, result)
	return
}
