/* This file contains functionality around the
 * '!font' command, letting the user change
 * the "font" jbot users.
 *
 * Usage:
 * !font <fontname>
 *
 * !set font=<fontname>
 */

package main

import (
	"fmt"
	"sort"
)

type FontFunc func(string) string

var FONTS = map[string]FontFunc{
	"blocks":     blockText,
	"bubbles":    bubbleText,
	"cursive":    cursiveText,
	"double":     doubleText,
	"gothic":     gothicText,
	"normal":     normalText,
	"reverse":    reverseText,
	"rot13":      rot13Text,
	"upsidedown": upsideDownText,
}

func init() {
	COMMANDS["font"] = &Command{cmdFont,
		"change the font used by jbot",
		"builtin",
		"!font <fontname>",
		nil}
	COMMANDS["rot13"] = &Command{cmdRot13,
		"encrypt input using a military grade cipher",
		"builtin",
		"!rot13 <text>",
		nil}
}

func cmdFont(r Recipient, chName, args string) (result string) {
	result = "Usage: " + COMMANDS["font"].Usage + "\n"
	result += "I know the following fonts:\n"

	fontKeys := []string{}
	for font, _ := range FONTS {
		fontKeys = append(fontKeys, font)
	}

	sort.Strings(fontKeys)

	for _, font := range fontKeys {
		fontFunc := FONTS[font]
		result += fmt.Sprintf("%s: %s\n", font, fontFunc("The quick brown fox jumps over the lazy dog."))
	}

	if len(args) < 1 {
		return
	}

	if _, found := FONTS[args]; !found {
		return
	}

	result = cmdSet(r, chName, "font="+args)
	return
}

func cmdRot13(r Recipient, chName, args string) (result string) {
	if len(args) < 1 {
		result = "Usage: " + COMMANDS["rot13"].Usage + "\n"
		return
	}

	result = rot13Text(args)
	return
}

func fill(in, space string, fontMap map[int]rune) (out string) {
	for _, r := range in {
		if l, found := fontMap[int(r)]; found {
			out += string(l) + space
		} else {
			out += string(r) + space
		}
	}

	return
}

func fontFormat(channelName, msg string) (out string) {
	out = msg

	ch, found := CHANNELS[channelName]
	if !found {
		return
	}

	fontSetting, found := ch.Settings["font"]
	if !found {
		return
	}

	fontFunc, found := FONTS[fontSetting]
	if !found {
		return
	}

	out = fontFunc(msg)
	return
}

func blockText(in string) (out string) {
	fontMap := map[int]rune{}

	// lower case A-Z and upper case A-Z are the same here
	for i := 65; i <= 90; i++ {
		fontMap[i] = rune(i + 127215)
	}
	for i := 97; i <= 122; i++ {
		fontMap[i] = rune(i + 127183)
	}

	out = fill(in, " ", fontMap)

	return
}

func bubbleText(in string) (out string) {
	fontMap := map[int]rune{}

	// 0-9
	for i := 48; i <= 57; i++ {
		fontMap[i] = rune(i + 9264)
	}

	// lower case a-z
	for i := 97; i <= 122; i++ {
		fontMap[i] = rune(i + 9327)
	}

	// upper case A-Z
	for i := 65; i <= 90; i++ {
		fontMap[i] = rune(i + 9333)
	}

	out = fill(in, " ", fontMap)

	return
}

func cursiveText(in string) (out string) {
	fontMap := map[int]rune{}

	// and upper case A-Z
	for i := 65; i <= 90; i++ {
		fontMap[i] = rune(i + 119899)
	}

	// lower case a-z
	for i := 97; i <= 122; i++ {
		fontMap[i] = rune(i + 119893)
	}

	// outliers
	fontMap['e'] = 119890
	fontMap['g'] = 119892
	fontMap['o'] = 119900
	fontMap['B'] = 119861
	fontMap['E'] = 119864
	fontMap['F'] = 119865
	fontMap['H'] = 119867
	fontMap['I'] = 119868
	fontMap['L'] = 119871
	fontMap['M'] = 119872
	fontMap['R'] = 119877

	out = fill(in, "", fontMap)
	return
}

func doubleText(in string) (out string) {
	fontMap := map[int]rune{}

	// 0-9
	for i := 48; i <= 57; i++ {
		fontMap[i] = rune(i + 120744)
	}

	// and upper case A-Z
	for i := 65; i <= 90; i++ {
		fontMap[i] = rune(i + 120055)
	}

	// lower case a-z
	for i := 97; i <= 122; i++ {
		fontMap[i] = rune(i + 120049)
	}

	// outliers
	fontMap['C'] = 8450
	fontMap['H'] = 8461
	fontMap['N'] = 8469
	fontMap['P'] = 8473
	fontMap['Q'] = 8474
	fontMap['R'] = 8477
	fontMap['Z'] = 8484

	out = fill(in, "", fontMap)

	return
}

func gothicText(in string) (out string) {
	fontMap := map[int]rune{}

	// lower case a-z
	for i := 97; i <= 122; i++ {
		fontMap[i] = rune(i + 119997)
	}

	// upper case A-Z (but see outliers below)
	for i := 65; i <= 90; i++ {
		fontMap[i] = rune(i + 120003)
	}

	// oddballs
	fontMap[67] = rune(8493)
	fontMap[72] = rune(8460)
	fontMap[73] = rune(8465)
	fontMap[82] = rune(8476)
	fontMap[90] = rune(8488)

	out = fill(in, "", fontMap)

	return
}

func normalText(in string) (out string) {
	return in
}

func reverseText(in string) (out string) {
	runes := []rune(in)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	out = string(runes)
	return
}

func rot13Text(in string) (out string) {
	fontMap := map[int]rune{}

	// and upper case A-Z
	for i := 65; i <= 77; i++ {
		fontMap[i] = rune(i + 13)
	}
	for i := 78; i <= 90; i++ {
		fontMap[i] = rune(i - 13)
	}

	// lower case a-z
	for i := 97; i <= 109; i++ {
		fontMap[i] = rune(i + 13)
	}
	for i := 110; i <= 122; i++ {
		fontMap[i] = rune(i - 13)
	}

	out = fill(in, "", fontMap)
	return
}

func upsideDownText(in string) (out string) {
	fontMap := map[int]rune{
		'1': 406,
		'2': 4357,
		'3': 400,
		'4': 12579,
		'5': 987,
		'6': '9',
		'7': 12581,
		'9': '6',
		'a': 592,
		'b': 'q',
		'c': 596,
		'd': 'p',
		'e': 477,
		'f': 607,
		'g': 387,
		'h': 613,
		'i': 7433,
		'j': 638,
		'k': 670,
		'm': 623,
		'n': 'u',
		'p': 'd',
		'q': 'b',
		'r': 633,
		't': 647,
		'u': 'n',
		'v': 652,
		'w': 653,
		'y': 654,
		'A': 8704,
		'C': 390,
		'E': 398,
		'F': 8498,
		'G': 1508,
		'J': 383,
		'L': 741,
		'M': 'W',
		'P': 1280,
		'T': 9524,
		'U': 8745,
		'V': 923,
		'W': 'M',
		'Y': 8516,
		',': 39,
		'.': 729,
		'?': 191,
		'!': 161,
		39:  ',',
		'&': 8523,
		'_': 8254,
	}

	out = fill(in, " ", fontMap)
	out = reverseText(out)
	return
}
