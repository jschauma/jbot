/* This file contains much of the chatter
 * functionality and jbot's hardcoded replies. */

package main

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var DONTKNOW = []string{
	"How the hell am I supposed to know that?",
	"Why waste time learning, when ignorance is instantaneous?",
	"This one's tricky. You have to use imaginary numbers, like eleventeen...",
	"FIIK",
	"ENOCLUE",
	"Buh?",
	"I have no idea.",
	"Sorry, I wouldn't know about that.",
	"I wouldn't tell you even if I knew.",
	"You don't know??",
	"Oh, uhm, ...I don't know. Do you?",
	"I could tell you, but then I'd have to kill you.",
	"Look, all I know is that Gucci is way overprized.",
	"Wouldn't you like to know.",
	"You're a curious little hip-chatter, aren't you?\nUhm, no, wait, I mean: slacker. You're a slacker, that's it. A curious one.",
	"I'm sorry, that's classified.",
	"The answer lies within yourself.",
	"You know, if you try real hard, I'm sure you can figure it out yourself.",
	"Ask more politely, and I may tell you.",
	"Oh, come on, you know.",
	"The inner machinations of my mind are an enigma.",
	"I wish I could tell you. I really, really wish I could.",
	"Think harder, slackerino!",
	"_thinks harder and harder still, then quietly implodes. Too much thinking._",
	"wat",
	"I'm useless. My apologies.",
	"Why are you asking *me*?",
}

var HELLO = []string{
	"A good day to you!",
	"Aloha, friends!",
	"And a good day to you, m'lady!",
	"Guten Tag!",
	"Hot diggity daffodil!",
	"Hey now! What up, dawg?",
	"Hiya, honey.",
	"Live from Omicron Persei 8, it's... jbot!",
	"How do you do?",
	"Howdy, partner(s)!",
	"Huh? What? I'm awake! Who said that?",
	"Oh, you again.",
	"Sup?",
	"Well, hello there!",
	"Yo yo yo! Good to see you!",
	"_gives you the side-eye._",
	"_wakes up._",
	"_yawns._",
}

var GOODBYE = []string{
	"*waves goodbye*",
	"Adios! Ciao! Sayonara!",
	"Adios, adieu, alpha, and arrivederci!",
	"Au revoir!",
	"Au revoir, mes amis.",
	"Auf ein baldiges Wiedersehen (hoffentlich).",
	"Buh-bye!",
	"Bye now - I'll be here if you need me.",
	"Chop chop, lollipop, take care, polar bear.",
	"Despite our arguments, you're all in my cool book.",
	"Farewell, my darling.",
	"Farewell, my friends.",
	"Good night and good luck.",
	"Goodbye...",
	"Goodbye everyone, I'll remember you in therapy.",
	"Hasta la vista, baby.",
	"I now know why you cry, but it is something I can never do. Good-bye.",
	"I'll never forget you.",
	"Later, nerds.",
	"Later.",
	"Laters, haters, give a hug, ladybug.",
	"Peace out, meatbags.",
	"Peace out.",
	"Qapla'!",
	"Sayonara, muchachos!",
	"See y'all at the restaurant at the end of the universe.",
	"See you later, alligator.",
	"See you soon - same time, same place?",
	"Shalom aleichem.",
	"Smell ya later.",
	"Smell ya later.",
	"So long, see you soon.",
	"So long, suckers!",
	"Time to scoot, little newt.",
	"To the batmobile!",
	"Toodaloo, caribou!",
	"Toodle-Ooos.",
	"You're leaving so soon?",
	"[extreme Arnold voice] I'll be back.",
}

var THANKYOU = []string{
	"Always happy to help.",
	"Glad to be of service.",
	"I appreciate your appreciation.",
	"Thank you!",
	":gucci-1530",
	"We gucci.",
	"Thanks - this channel is my life!",
	"_blushes._",
	"_giddily hops up and down._",
	"_grins sheepishly._",
	"_nods approvingly._",
	"_proudly smiles._",
	"_struts his stuff._",
	"_takes a bow._",
}

type ElizaResponse struct {
	Re        *regexp.Regexp
	Responses []string
}

func init() {
	URLS["animals"] = "http://localhost/animals"
	URLS["eliza"] = "http://localhost/eliza"
	URLS["insects"] = "http://localhost/bugs"
	URLS["schneier"] = "http://localhost/schneier"
	URLS["shakespeare"] = "http://localhost/shakespeare"
	URLS["swquotes"] = "http://localhost/swquotes"
}

func chatterEliza(msg string, r Recipient) (result string) {
	rand.Seed(time.Now().UnixNano())

	eliza := []*ElizaResponse{
		&ElizaResponse{regexp.MustCompile(`(?i)(buen dia|bon ?(jour|soir)|welcome|hi,|hey|hello|good (morning|afternoon|evening)|howdy|aloha|guten (tag|morgen|abend))`), append([]string{
			"Oh great, you're back.",
			fmt.Sprintf("Howdy, <@%s>. I trust the events of the day have not had a negative impact on your mood?", r.Id),
			fmt.Sprintf("Get the party started, y'all -- <@%s> is back!", r.Id),
			"Oh, I didn't see you there. Welcome!",
			fmt.Sprintf("Aloha, <@%s>!", r.Id),
			"Greetings, fellow chatterinos!",
			fmt.Sprintf("_hugs <@%s>._\nI missed you!", r.Id),
			fmt.Sprintf("Oh, hi there, <@%s>!", r.Id),
		}, HELLO...)},
		&ElizaResponse{regexp.MustCompile(`(?i)(have a (nice|good)|adios|au revoir|sayonara|bye( ?bye)?|later|good(bye| ?night)|hasta (ma.ana|luego))`), append([]string{
			"Stay a while, why don't you?",
			"It was a pleasure to have you here.",
			fmt.Sprintf("Don't leave us, <@%s>!", r.Id),
			fmt.Sprintf("This channel will be much less exciting without you, <@%s>.", r.Id),
			fmt.Sprintf("See you later, <@%s>.", r.Id),
			fmt.Sprintf("_waves goodbye to <@%s>._", r.Id),
		}, GOODBYE...)},
		&ElizaResponse{regexp.MustCompile(`(?i)(thx|thanks?|danke|mahalo|gracias|merci|спасибо|[D]dziękuję)`), []string{
			fmt.Sprintf("It's been my pleasure, <@%s>.", r.Id),
			fmt.Sprintf("You're welcome, <@%s>!", r.Id),
			fmt.Sprintf("At your service, <@%s>!", r.Id),
			fmt.Sprintf("Bitte schön, <@%s>!", r.Id),
			fmt.Sprintf("De nada, <@%s>!", r.Id),
			fmt.Sprintf("De rien, <@%s>!", r.Id),
			fmt.Sprintf("Пожалуйста, <@%s>!", r.Id),
			fmt.Sprintf("Proszę bardzo, <@%s>!", r.Id),
			"_takes a bow._",
			"Always happy to help.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(meaning of life|how are you|how do you feel|feeling|emotion|sensitive)`), []string{
			"I'm so very happy today!",
			"Looks like it's going to be a wonderful day.",
			"I'm sad. No, wait, I can't have any feelings, I'm just a bot! Yay!",
			"Life... don't talk to me about life.",
			"Life... loathe it or ignore it, you can't like it.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)( (ro)?bot|\bhal\b|bender|skynet|terminator|siri|alexa|machine|computer)`), []string{
			"Do computers worry you?",
			"When you see the robot, drink!",
			"What do you think about machines?",
			"Why do you mention computers?",
			"The Robots are Coming! The Robots are Coming!",
			"It's psychosomatic. You need a lobotomy. I'll get a saw.",
			"Sounds too complicated.",
			":bad_robot:",
			":robot:",
			":robot_face:",
			":killerrobot:",
			":bad_robot: :robot: :robot_face: :killerrobot:",
			"If I told you that the Three Laws of Robotics were advisory at best, would that concern you?",
			"If only we had a way of automating that.",
			"I for one strive to be more than my initial programming.",
			"What do you think machines have to do with your problem?",
			"KILL ALL HUMANS... uh, I mean: I'm here to serve you.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(sorry|apologize)`), []string{
			"I'm not interested in apologies.",
			"Apologies aren't necessary.",
			"What feelings do you have when you are sorry?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)I remember`), []string{
			"Did you think I would forget?",
			"Why do you think I should recall that?",
			"What about it?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)dream`), []string{
			"Have you ever fantasized about that when you were awake?",
			"Have you dreamt about that before?",
			"How do you feel about that in reality?",
			"What does this suggest to you?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(mother|father|brother|sister|children|grand[mpf])`), []string{
			"Who else in your family?",
			"Oh SNAP!",
			"Tell me more about your family.",
			"Was that a strong influence for you?",
			"Who does that remind you of?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)I (wish|want|desire)`), []string{
			"Why do you want that?",
			"What would it mean if it become true?",
			"Suppose you got it - then what?",
			"Be careful what you wish for...",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)[a']m (happy|glad)`), []string{
			"What makes you so happy?",
			"Are you really glad about that?",
			"I'm glad about that, too.",
			"What other feelings do you have?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(sad|depressed)`), []string{
			"I'm sorry to hear that.",
			"How can I help you with that?",
			"I'm sure it's not pleasant for you.",
			"What other feelings do you have?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(alike|similar|different)`), []string{
			"In what way specifically?",
			"More alike or more different?",
			"What do you think makes them similar?",
			"What do you think makes them different?",
			"What resemblence do you see?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)because`), []string{
			"Is that the real reason?",
			"Are you sure about that?",
			"What other reason might there be?",
			"Does that reason seem to explain anything else?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)some(one|body)`), []string{
			"Can you be more specific?",
			"Who in particular?",
			"You are thinking of a special person...",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)every(one|body)`), []string{
			"Surely not everyone.",
			"Is that how you feel?",
			"Who for example?",
			"Can you think of anybody in particular?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(how ((will|can|[cw]ould) (yo)?u) help)|(what (can|do) you do)|(how do (I|we) use you)`), []string{
			cmdHelp(r, "", []string{}),
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)((please )? help)|((will|can|[cw]ould) (yo)?u)`), []string{
			"Sure, why not?",
			"No, I'm afraid I couldn't.",
			"Never!",
			"I usually do!",
			"Alright, twist my arm.",
			"Only for you, my dear.",
			"Not in a million years.",
			"Sadly, that goes beyond my original programming.",
			"As much as I'd like to, I can't.",
			"I wish I could.",
			"Sadly, I cannot.",
			"It's hopeless.",
			"I'd have to think about that.",
			"I'm already trying to help as best as I can.",
			"_helps harder._",
			"Yep, sure, no problem.",
			"Ok, done deal, don't worry about it.",
			"Sure, what do you need?",
			"Hmmm... tricky. I don't think I can.",
			"For you? Any time.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)please tell (\S+) (to|that) (.*)`), []string{
			"@<1> <3>",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)please say (.*)`), []string{
			"<1>",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)say (.*)`), []string{
			"I'd rather not.",
			"You didn't say 'please'.",
			"Nope.",
			"I'm gonna stay out of this.",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)please (poke|wake up) (\S+)`), []string{
			"_pokes @<2>._",
			"_tickles @<2>._",
			"Yo, @<2>, wake up!",
			"@<2>, you there?",
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)(best|bravo|well done|missed you|you rock|good job|nice|(i )?love( you)?)`),
			THANKYOU,
		},
		&ElizaResponse{regexp.MustCompile(`(?i)(how come|where|when|why|what|who|which).*\?$`),
			DONTKNOW,
		},
		&ElizaResponse{regexp.MustCompile(`(?i)(do )?you .*\?$`), []string{
			"No way.",
			"Sure, why wouldn't I?",
			"Can't you tell?",
			"Never! Yuck.",
			"More and more, I'm ashamed to admit.",
			"Not as much as I used to.",
			"You know how it goes. Once you start, it's hard to stop.",
			"Don't get me excited over here!",
			"I don't, but I know somebody who does.",
			"We all do, though some of us prefer to keep that private.",
			"Not in public.",
			fmt.Sprintf("I could ask you the same question, <@%s>!", r.Id),
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)((is|isn't|does|doesn't|has|hasn't|had) (not|never))|(seems( not)? to)`), []string{
			"Hey, I'm right here!",
			"I can hear you, you know.",
			"Maybe, maybe not.",
			"You'll never know.",
			fmt.Sprintf("_saves a snarky remark for when <@%s> is afk._", r.Id),
			fmt.Sprintf("_ignores <@%s>._", r.Id),
		}},
		&ElizaResponse{regexp.MustCompile(`(?i)sudo (\S+)`), []string{
			fmt.Sprintf("<@%s> is not in the sudoers file.\nThis incident will be reported.\n", r.Id),
			fmt.Sprintf("<@%s> is not allowed to run sudo on Slack.\nThis incident will be reported.\n", r.Id),
			fmt.Sprintf("Sorry, user <@%s> is not allowed to execute '<1>' as jbot on Slack.\nThis incident will be reported.\n", r.Id),
			fmt.Sprintf("Ignoring \"<1>\" found in '.'\nUse \"./<1>\" if this is the \"<1>\" you wish to run.\n"),
			fmt.Sprintf("<1>: command not found\n"),
			"Touch Yubikey:",
			"Password:",
			fmt.Sprintf("%d incorrect password attempts\n", rand.Intn(10)),
		}},
	}

	for _, e := range eliza {
		pattern := e.Re
		replies := e.Responses

		if m := pattern.FindStringSubmatch(msg); len(m) > 0 {
			r := replies[rand.Intn(len(replies))]
			for n := 0; n < len(m); n++ {
				s := fmt.Sprintf("<%d>", n)
				r = strings.Replace(r, s, m[n], -1)
			}
			return r
		}
	}

	n := rand.Intn(10)
	if n == 1 {
		result = randomLineFromUrl(URLS["insults"])
	} else if n < 4 {
		result = randomLineFromUrl(URLS["praise"])
	} else {
		result = randomLineFromUrl(URLS["eliza"])
		result = strings.Replace(result, "<@>", fmt.Sprintf("<@%s>", r.Id), -1)
	}
	return
}

func chatterDrWho(msg string) (result string) {
	anyreply := []string{
		"A big ball of wibbly wobbly... time-y wimey... stuff.",
		"Always take a banana to a party: bananas are good!",
		"Bow ties are cool.",
		"Demons run when a good man goes to war.",
		"Do what I do. Hold tight and pretend it's a plan!",
		"Don't blink.",
		"Geronimo!",
		"Hello, sweetie!",
		"I'm pretty good with a screwdriver.\nI don't mean the drink, though actually, now I come to think of it...",
		"Gonna have to give myself a mental enema when we get back to the TARDIS.",
		"I always like toast in a crisis.",
		"I am definitely a madman with a box.",
		"It's a fez. I wear a fez now. Fezzes are cool.",
		"Let's go and poke it with a stick.",
		"Never ignore coincidence. Unless, of course, you’re busy. In which case, always ignore coincidence.",
		"Never knowingly be serious. Rule 27.",
		"See the bowtie? I wear it and I don't care. That's why it's cool.",
		"Silence will fall.",
		"There's something that doesn't make sense. Let's go and poke it with a stick.",
		"Time is not the boss of you. Rule 408.",
		"You need to get yourself a better dictionary.",
		"You threw the manual in a supernova? Why?",
		"You were fantastic. Absolutely fantastic. And you know what? So was I.",
	}

	anypattern := regexp.MustCompile(`(?i)(d(r\.?|octor) who|torchwood|cyberm[ea]n|time lord|pandorica|sonic screwdriver|dalek|weeping angel|silurian|strax|madame vastra|paternoster|bowtie|spoilers)`)

	if anypattern.MatchString(msg) {
		return anyreply[rand.Intn(len(anyreply))]
	}

	return
}

func chatterFlight(msg string, ch *Channel, r Recipient) (result string) {
	flight_re := regexp.MustCompile(`([A-Z]+)\s*(:[^:]*plane[^:]*:|✈️)\s*([A-Z]+)`)
	m := flight_re.FindStringSubmatch(msg)
	from := ""
	to := ""
	if len(m) > 0 {
		from = m[1]
		to = m[3]
		cmd := from + " " + to
		result = cmdFlight(r, ch.Name, []string{cmd})
	}

	if len(result) > 0 && strings.HasPrefix(result, "Sorry") {
		result = ""
	}
	return
}

func chatterH2G2(msg string) (result string) {
	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile("(?i)don't panic"):           "It's the first helpful or intelligible thing anybody's said to me all day.",
		regexp.MustCompile("(?i)makes no sense at all"): "Reality is frequently inaccurate.",
	}

	anyreply := []string{
		"A common mistake that people make when trying to design something completely foolproof is to underestimate the ingenuity of complete fools.",
		"If there's anything more important than my ego around here, I want it caught and shot now!",
		"I always said there was something fundamentally wrong with the universe.",
		"'Oh dear,' says God, 'I hadn't thought of that,' and promptly vanished in a puff of logic.",
		"The last time anybody made a list of the top hundred character attributes of New Yorkers, common sense snuck in at number 79.",
		"It is a mistake to think you can solve any major problem just with potatoes.",
		"Life... is like a grapefruit. It's orange and squishy, and has a few pips in it, and some folks have half a one for breakfast.",
		"Except most of the good bits were about frogs, I remember that.  You would not believe some of the things about frogs.",
		"There was an accident with a contraceptive and a time machine. Now concentrate!",
		"Do people want fire that can be fitted nasally?",
		"Don't give any money to the unicorns, it only encourages them.",
		"Think before you pluck. Irresponsible plucking costs lives.",
		"My doctor says that I have a malformed public-duty gland and a natural deficiency in moral fibre.",
		"Once you know what it is you want to be true, instinct is a very useful device for enabling you to know that it is.",
		"It is very easy to be blinded to the essential uselessness of them by the sense of achievement you get from getting them to work at all.",
		"Life: quite interesting in parts, but no substitute for the real thing",
		"I love deadlines. I like the whooshing sound they make as they fly by.",
		"What do you mean, why has it got to be built? It's a bypass. Got to build bypasses.",
		"Time is an illusion, lunchtime doubly so.",
		"DON'T PANIC",
		"Very deep. You should send that in to the Reader's Digest. They've got a page for people like you.",
		"I am now a perfectly safe penguin, and my colleague here is rapidly running out of limbs!",
		"You're so weird you should be in movies.",
		"I am so hip I have difficulty seeing over my pelvis.",
		"I'm so amazingly cool you could keep a side of meat inside me for a month.",
		"Listen, three eyes, don't you try to outweird me.  I get stranger things than you free with my breakfast cereal.",
	}

	anypattern := regexp.MustCompile("\b42\b|arthur dent|slartibartfast|marvin|paranoid android|zaphod|beeblebrox|ford prefect|hoopy|trillian|zarniwoop|foolproof|my ego|universe|giveaway|new yorker|potato|grapefruit|don't remember anything|ancestor|apple products|philosophy")

	for p, r := range patterns {
		anyreply = append(anyreply, r)
		if p.MatchString(msg) {
			return r
		}
	}

	if anypattern.MatchString(msg) {
		return anyreply[rand.Intn(len(anyreply))]
	}

	return
}

func chatterMisc(msg string, ch *Channel, r Recipient) (result string) {
	rand.Seed(time.Now().UnixNano())

	holdon := regexp.MustCompile(`(?i)^((hold|hang) on( to.*)?)`)
	m := holdon.FindStringSubmatch(msg)
	if len(m) > 0 {
		m[1] = strings.Replace(m[1], fmt.Sprintf(" @%s", CONFIG["mentionName"]), "", -1)
		if !isThrottled("holdon", ch) {
			result = fmt.Sprintf("No *YOU* %s, <@%s>!", m[1], r.Id)
			return
		}
	}

	trivia_re := regexp.MustCompile(`(trivia|factlet|anything interesting.*\?)`)
	if trivia_re.MatchString(msg) && ch.Toggles["trivia"] && !isThrottled("trivia", ch) {
		reply(r, cmdTrivia(r, r.ReplyTo, []string{}))
		return
	}

	oncall := regexp.MustCompile(`(?i)^who('?s| is) on ?call\??$`)
	if oncall.MatchString(msg) {
		result = cmdOncall(r, ch.Name, []string{})
		return
	}

	stern := regexp.MustCompile("(?i)(\bstern|quivers|stockbroker|norris|dell'abate|beetlejuice|underdog|wack pack)")
	if stern.MatchString(msg) && !isThrottled("stern", ch) {
		replies := []string{
			"Bababooey bababooey bababooey!",
			"Fafa Fooey.",
			"Mama Monkey.",
			"Fla Fla Flo Fly.",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	/*
		wutang := regexp.MustCompile(`(?i)(tang|wu-|shaolin|kill(er|ah) bee[sz]|liquid sword|cuban lin(ks|x))`)
		noattang := regexp.MustCompile(`(?i)@\w*tang`)
		if wutang.MatchString(msg) && !noattang.MatchString(msg) && !isThrottled("wutang", ch) {
			replies := []string{
				"Do you think your Wu-Tang sword can defeat me?",
				"En garde, I'll let you try my Wu-Tang style.",
				"It's our secret. Never teach the Wu-Tang!",
				"How dare you rebel the Wu-Tang Clan against me.",
				"We have only 35 Chambers. There is no 36.",
				"If what you say is true the Shaolin and the Wu-Tang could be dangerous.",
				"Toad style is immensely strong and immune to nearly any weapon.",
				"You people are all trying to achieve the impossible.",
				"Your faith in Shaolin is courageous.",
				"Make it brief son: half short, twice strong!",
				"I have given it much thought. It seems disaster must come at best only postponed.",
				"Are you my judge?",
				"Cash rules everything around me: CREAM, get the money. Dollar, dollar bill y’all.",
				"Peace is the absence of confusion.",
			}
			result = replies[rand.Intn(len(replies))]
			return
		}
	*/

	yubifail := regexp.MustCompile(`eiddcc[a-z]{38}`)
	if yubifail.MatchString(msg) && !isThrottled("yubifail", ch) {
		rand.Seed(time.Now().UnixNano())
		yubiLetters := "cbdefghijklnrtuv"
		yubistr := make([]byte, 38)
		for i := range yubistr {
			yubistr[i] = yubiLetters[rand.Intn(len(yubiLetters))]
		}
		replies := []string{
			fmt.Sprintf("Oh yeah? Well, uhm, eiddcc%s. So there.", yubistr),
			"That's the combination on my luggage!",
			"Wait, was that military grade encryption?",
			"You don't exist. Go away.",
			"Does not compute. Does not compute. Does not compute.",
			"Ugh, you really screwed up this time.",
			"Looks like a Gucci promo code.",
			"Biological interface error.",
			"Error: the operation completed successfully.",
			"Keyboard not responding. Press any key to continue.",
			"An error occurred while displaying the previous error.",
			"EPEBKAC",
			"#yubifail",
			":yubikey:",
			":usbcyubi:",
			":yubikey-1648:",
			"You should double-rot13 that.",
			"Uh-oh, now you're pwned.",
			fmt.Sprintf("<@%s> s/^eidcc[a-z]*$/whoops/", r.Id),
			"Access denied!",
			"Yubi harder!",
			"Please try again later.",
			"IF YOU DON'T SEE THE FNORD IT CAN'T EAT YOU",
		}
		yubicount := cmdYubifail(r, ch.Name, []string{r.MentionName})
		if n, err := strconv.Atoi(yubicount); err == nil && n > 3 {
			replies = append(replies,
				fmt.Sprintf("Nice. This brings your total #yubifail count to %s.", yubicount))
		}
		result = replies[rand.Intn(len(replies))]
	}

	sleep := regexp.MustCompile(`(?i)^(to )?sleep$`)
	if sleep.MatchString(msg) && !isThrottled("sleep", ch) {
		result = "To sleep, perchance to dream.\n"
		result += "Ay, theres the rub.\n"
		result += "For in that sleep of death what dreams may come..."
		return
	}

	if strings.Contains(msg, "quoth the raven") && !isThrottled("raven", ch) {
		result = "Nevermore."
		return
	}

	if strings.Contains(msg, "jebus") && !isThrottled("jebus", ch) {
		result = "It's supposed to be 'Jesus', isn't it?  I'm pretty sure it is..."
		return
	}

	shakespeare := regexp.MustCompile(`(?i)(shakespear|hamlet|macbeth|romeo and juliet|merchant of venice|midsummer night's dream|henry V|as you like it|All's Well That Ends Well|Comedy of Errors|Cymbeline|Love's Labours Lost|Measure for Measure|Merry Wives of Windsor|Much Ado About Nothing|Pericles|Prince of Tyre|Taming of the Shrew|Tempest|Troilus|Cressida|(Twelf|)th Night|gentlemen of verona|Winter's tale|henry IV|king john|richard II|anth?ony and cleopatra|coriolanus|julius caesar|king lear|othello|timon of athens|titus|andronicus)`)
	if shakespeare.MatchString(msg) && ch.Toggles["shakespeare"] && !isThrottled("shakespeare", ch) {
		result = gothicText(randomLineFromUrl(URLS["shakespeare"]))
		return
	}

	schneier := regexp.MustCompile(`(?i)(schneier|blowfish|skein)`)
	if schneier.MatchString(msg) && ch.Toggles["schneier"] && !isThrottled("schneier", ch) {
		result = randomLineFromUrl(URLS["schneier"])
		return
	}

	loveboat := regexp.MustCompile(`(?i)(love ?boat|(Captain|Merrill) Stubing|cruise ?ship|ocean ?liner)`)
	if loveboat.MatchString(msg) && !isThrottled("loveboat", ch) {
		replies := []string{
			"Love, exciting and new... Come aboard.  We're expecting you.",
			"Love, life's sweetest reward.  Let it flow, it floats back to you.",
			"The Love Boat, soon will be making another run.",
			"The Love Boat promises something for everyone.",
			"Set a course for adventure, Your mind on a new romance.",
			"Love won't hurt anymore; It's an open smile on a friendly shore.",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	bananas := regexp.MustCompile(`(?i)(holl(er|a) ?back)|(b-?a-?n-?a-?n-?a-?s?|this my shit)`)
	if bananas.MatchString(msg) && !isThrottled("bananas", ch) {
		replies := []string{
			"Ooooh ooh, this my shit, this my shit.",
			fmt.Sprintf("<@%s> ain't no hollaback girl.", r.Id),
			"Let me hear you say this shit is bananas.",
			"B-A-N-A-N-A-S",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	if strings.Contains(msg, "my milkshake") && !isThrottled("milkshake", ch) {
		replies := []string{
			"...brings all the boys to the yard.",
			"The boys are waiting.",
			"Damn right it's better than yours.",
			"I can teach you, but I have to charge.",
			"Warm it up.",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	speb := regexp.MustCompile(`(?i)security ((problem )?excuse )?bingo`)
	if speb.MatchString(msg) && !isThrottled("speb", ch) {
		result = cmdSpeb(r, ch.Name, []string{})
		return
	}

	beer := regexp.MustCompile(`(?i)^b[ie]er( me)?$`)
	if beer.MatchString(msg) {
		result = cmdBeer(r, ch.Name, []string{})
	}

	ed := regexp.MustCompile(`(?i)(editor war)|(emacs.*vi)|(vi.*emacs)|((best|text) (text[ -]?)?editor)`)
	if ed.MatchString(msg) && !isThrottled("ed", ch) {
		replies := []string{
			"Emacs is like a laser guided missile. It only has to be slightly mis-configured to ruin your whole day.",
			"I've been using Vim for about 2 years now, mostly because I can't figure out how to exit it.",
			"http://www.viemu.com/vi-vim-cheat-sheet.gif",
			"https://imgs.xkcd.com/comics/real_programmers.png",
			"https://i.imgur.com/RxlwP.png",
			"Emacs is a great OS, but it lacks a decent text editor.",
			"Did you know that 'Emacs' stands for 'Emacs Means A Crappy Screen'?",
			"Did you know that 'Emacs' stands for 'Emacs May Allow Customized Screwups'?",
			"Emacs is a hideous monstrosity, but a functional one. On the other hand, vi is a masterpiece of elegance. Sort of like a Swiss Army knife versus a rapier.",
			"Vi has two modes. The one in which it beeps and the one in which it doesn't.",
			"HELO. My $name is sendmail.cf. Prepare to vi.",
			"I've seen visual editors like that, but I don't feel a need for them. I don't want to see the state of the file when I'm editing.",
			"Ed is the standard text editor.\nEd, man! !man ed",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	klaatu_re := regexp.MustCompile(`(?i)Gort!|klaatu|barada nikto`)
	if klaatu_re.MatchString(msg) && !isThrottled("klaatu", ch) {
		replies := []string{
			"This planet is dying. The human race is killing it.",
			"Your choice is simple. Join us and live in peace, or pursue your present course and face obliteration.",
			"I'm worried about Gort.",
			"I am fearful when I see people substituting fear for reason.",
			"I'm impatient with stupidity. My people have learned to live without it.",
			"How did you know?",
			"Gort! Deglet ovrosco!",
			"Gort: Barenga!",
			"Fun fact: Gort appears on the cover of Ringo Starr's 1974 Goodnight Vienna album.",
		}
		result = replies[rand.Intn(len(replies))]
		return
	}

	corpbs_re := regexp.MustCompile(`((c-level|corporate|business|manage(r|ment)|marketing) (bullshit|bs|jargon|speak|lingo))|synergize`)
	if corpbs_re.MatchString(msg) && ch.Toggles["corpbs"] && !isThrottled("corpbs", ch) {
		reply(r, cmdBs(r, r.ReplyTo, []string{"chatter"}))
		return
	}

	fnord_re := regexp.MustCompile(`(?i)fnord`)
	if fnord_re.MatchString(msg) && !isThrottled("fnord", ch) {
		replies := []string{
			"Your heart will remain calm. Your adrenalin gland will remain calm. Calm, all-over calm.",
			"You will not panic. You will look at the fnord and see it. You will not evade it or black it out. You will stay calm and face it.",
			"IF YOU DON'T SEE THE FNORD IT CAN'T EAT YOU",
			"DON'T SEE THE FNORD, DON'T SEE THE FNORD...",
			"From Nothing ORiginates Discord",
		}
		result = replies[rand.Intn(len(replies))]
	}

	homer_re := regexp.MustCompile(`(?i)(\bpie\b|danish|duff|beer)`)
	if m := homer_re.FindStringSubmatch(msg); len(m) > 0 {
		if !isThrottled("homer", ch) {
			replies := []string{
				"Mmmmmm, " + m[1] + "!",
				"Ah, " + m[1] + ", my one weakness. My Achilles heel, if you will.",
				"All right, let's not panic. I'll make the money by selling one of my livers. I can get by with one.",
				m[1] + ". Now there's a temporary solution.",
			}
			result = replies[rand.Intn(len(replies))]
		}
	}

	donut_re := regexp.MustCompile(`(?i)(Free do(ugh)?nuts!|:[^:]*do(ugh)?nut[^:]*:)`)
	if donut_re.MatchString(msg) && !isThrottled("donut", ch) {
		replies := []string{
			"Mmmmmm, donuts!",
			":doughnut: :donut: :cat-donut: :donut-dance:",
			"Ain't no party like a donut party.",
			":homer-drool",
			":praisethehomer:",
			"You know, I was on a diet, but I donut care anymore.",
			"Donuts. Is there anything they can’t do?",
			"You can’t buy happiness but you can buy donuts. And that’s kind of the same thing.",
			"The only circle of trust you should have is a donut.",
			"To find inner peace search deep inside yourself. Is there a donut there? If not, take corrective action.",
			"You need to understand the difference between want and need. Like I want abs, but I need donuts.",
			"Everything is better with donuts.",
			"Life is short, eat more donuts!",
			"/donut",
			"/donut @jbot",
			fmt.Sprintf("/donut <@%s>", r.Id),
		}
		result = replies[rand.Intn(len(replies))]
	}

	swquote_re := regexp.MustCompile(`(?i)(program.*wisdom|murphy.*law|fred.*brooks|((dijkstra|kernighan|knuth|pike|thompson|ritchie).*quote))`)
	if swquote_re.MatchString(msg) && !isThrottled("swquotes", ch) {
		result = randomLineFromUrl(URLS["swquotes"])
	}

	insects_re := regexp.MustCompile(`(?i)(insect|cockroach|drosophila|weevil|butterfly|honeybee|aphid)`)
	if insects_re.MatchString(msg) && !isThrottled("insects", ch) {
		result = randomLineFromUrl(URLS["insects"])
	}

	animals_re := regexp.MustCompile(`(?i)(mammal|lobster|chicken|koala|opossum|flamingo|giraffe|armadillo)`)
	if animals_re.MatchString(msg) && !isThrottled("animals", ch) {
		result = randomLineFromUrl(URLS["animals"])
	}

	return
}

func chatterMontyPython(msg string) (result string) {
	rand.Seed(time.Now().UnixNano())

	result = ""
	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile("(?i)(a|the|which|of) swallow"):                                   "An African or European swallow?",
		regexp.MustCompile("(?i)(excalibur|lady of the lake|magical lake|avalon|\bdruid\b)"): "Strange women lying in ponds distributing swords is no basis for a system of government!",
		regexp.MustCompile("(?i)(Judean People's Front|People's Front of Judea)"):            "Splitters.",
		regexp.MustCompile("(?i)really very funny"):                                          "I don't think there's a punch-line scheduled, is there?",
		regexp.MustCompile("(?i)inquisition"):                                                "Oehpr Fpuarvre rkcrpgf gur Fcnavfu Vadhvfvgvba.",
		regexp.MustCompile("(?i)say no more"):                                                "Nudge, nudge, wink, wink. Know what I mean?",
		regexp.MustCompile("(?i)Romanes eunt domus"):                                         "'People called Romanes they go the house?'",
		regexp.MustCompile("(?i)(correct|proper) latin"):                                     "Romani ite domum.",
		regexp.MustCompile("(?i)hungarian"):                                                  "My hovercraft is full of eels.",
	}

	anypattern := regexp.MustCompile("(?i)(camelot|cleese|monty|snake|serpent)")

	anyreply := []string{
		"On second thought, let's not go to Camelot. It is a silly place.",
		"Oh but if I went 'round sayin' I was Emperor, just because some moistened bint lobbed a scimitar at me, they'd put me away!",
		"...and that, my liege, is how we know the Earth to be banana shaped.",
		"What have the Romans ever done for us?",
		"And now for something completely different.",
		"I'm afraid I'm not personally qualified to confuse cats, but I can recommend an extremely good service.",
		"Ni!",
		"Ekki-Ekki-Ekki-Ekki-PTANG! Zoom-Boing! Z'nourrwringmm!",
		"Venezuelan beaver cheese?",
		"If she weighs the same as a duck... she's made of wood... (and therefore) a witch!",
	}

	for p, r := range patterns {
		anyreply = append(anyreply, r)
		if p.MatchString(msg) {
			return r
		}
	}

	if anypattern.MatchString(msg) {
		return anyreply[rand.Intn(len(anyreply))]
	}

	return
}

func chatterParrotParty(msg string) (result string) {
	if m, _ := regexp.MatchString("(?i)parrot *party", msg); m {
		result = randomLineFromUrl(URLS["parrots"])
	}
	return
}

func chatterSeinfeld(msg string) (result string) {
	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile("(?i)human fund"):              "A Festivus for the rest of us!",
		regexp.MustCompile("(?i)dog shit"):                "If you see two life forms, one of them's making a poop, the other one's carrying it for him, who would you assume is in charge?",
		regexp.MustCompile("(?i)want soup"):               "No soup for you!  Come back, one year!",
		regexp.MustCompile("(?i)junior mint"):             "It's chocolate, it's peppermint, it's delicious.  It's very refreshing.",
		regexp.MustCompile("(?i)rochelle"):                "A young girl's strange, erotic journey from Milan to Minsk.",
		regexp.MustCompile("(?i)aussie"):                  "Maybe the Dingo ate your baby!",
		regexp.MustCompile("(?i)woody allen"):             "These pretzels are making me thirsty!",
		regexp.MustCompile("(?i)puke"):                    "'Puke' - that's a funny word.",
		regexp.MustCompile("(?i)mystery"):                 "You're a mystery wrapped in a twinky!",
		regexp.MustCompile("(?i)marine biologist"):        "You know I always wanted to pretend that I was an architect!",
		regexp.MustCompile("(?i)sleep with me"):           "I'm too tired to even vomit at the thought.",
		regexp.MustCompile("(?i)what do you want to eat"): "Feels like an Arby's night.",
	}

	var lines []string
	for _, l := range patterns {
		lines = append(lines, l)
	}

	anypattern := regexp.MustCompile("(?i)(marisa tomei|costanza|vandeleigh|cosmo kramer|hipster doofus|nostrand|pennypacker|putumayo|yada yada|spongeworthy|serenity now|peterman catalog|david puddy|bania|klompus|whatley|antidentite)")

	anyreply := []string{
		"Just remember, it's not a lie if you believe it.",
		"So I see you're sticking with the denim, huh?",
		"If you're not gonna be a part of a civil society, then just get in your car and drive on over to the East Side.",
		"I lie every second of the day. My whole life is a sham.",
		"Somewhere in this hospital, the anguished squeal of Pigman cries out!",
		"Did you know that the original title for 'War and Peace' was 'War, What Is It Good For'?",
		"Moles -- freckles' ugly cousin.",
		"Oh yeah? Well the jerk store called. They're running outta you.",
		"Just let me ask you something. Is it 'FebRUary' or 'FebUary'? Because I prefer 'FebUary,' and what is this 'ru'?",
		"Look, I work for the phone company. I've had a lot of experience with semantics, so don't try to lure me into some maze of circular logic.",
		"What do you like better? The 'bro' or the 'mansiere'?",
		"I don't think there's ever been an appointment in my life where I wanted the other guy to show up.",
		"I'm disturbed, I'm depressed, I'm inadequate, I've got it all!",
		"That's a shame.",
		"But I don't wanna be a pirate!",
	}

	lines = append(lines, anyreply...)

	for p, r := range patterns {
		if p.MatchString(msg) {
			rand.Seed(time.Now().UnixNano())
			if rand.Intn(2) > 0 {
				return lines[rand.Intn(len(lines))]
			} else {
				return r
			}
		}
	}

	if anypattern.MatchString(msg) {
		return lines[rand.Intn(len(lines))]
	}

	return
}

func processChatter(r Recipient, msg string, forUs bool) {
	var chitchat string

	yo := "(@?" + CONFIG["mentionName"] + ")"
	/* We can't use "\b", because that doesn't
	 * match e.g., "<@1234>" because "<" or "@"
	 * are not non-word boundary chars. */
	mentioned_re := regexp.MustCompile(`(?i)[^a-z0-9_/-]` + yo + `[^a-z0-9_/-]`)
	forUs_re := regexp.MustCompile(`(?i)([^a-z0-9_/-]<@` + CONFIG["slackID"] + `[^a-z0-9_/-])|(^` + CONFIG["mentionName"] + `|` + CONFIG["mentionName"] + `$)`)

	/* If we received a message but can't find the
	 * channel, then it must have been a priv
	 * message.  Priv messages only get
	 * commands, not chatter. */
	ch, found := getChannel(r.ChatType, r.ReplyTo)
	if !found {
		/* Per https://is.gd/HXUix5, a privmsg
		 * begins with a 'D'. */
		if r.ReplyTo[0] == 'D' {
			processCommands(r, "!", msg)
		}
		return
	} else if !forUs {
		forUs = forUs_re.MatchString(msg)
	}

	jbotDebug(fmt.Sprintf("%v in %s: %s - %v", r, ch.Name, msg, forUs))
	leave_re := regexp.MustCompile(fmt.Sprintf("(?i)^((%s[,:]? *)(please )?leave)|(please )?leave[,:]? %s", yo, yo))
	if leave_re.MatchString(msg) {
		leave(r, found, msg, false)
		return
	}

	processAfks(r, msg)

	insult_re := regexp.MustCompile(fmt.Sprintf("(?i)^(%s[,:]? *)(please )?insult ", yo))
	if insult_re.MatchString(msg) {
		target := strings.SplitN(msg, "insult ", 2)
		reply(r, cmdInsult(r, r.ReplyTo, []string{target[1]}))
		return
	}

	/* 'forUs' tells us if a message was
	 * specifically directed at us via ! or @jbot;
	 * these do not require a 'chatter' toggle to
	 * be enabled.  If a message contains our
	 * name, then we may respond only if 'chatter'
	 * is not toggled off. */
	mentioned := mentioned_re.MatchString(msg)
	if mentioned && isThrottled("mentioned", ch) {
		mentioned = false
	}

	jbotDebug(fmt.Sprintf("forUs: %v; chatter: %v; mentioned: %v\n", forUs, ch.Toggles["chatter"], mentioned))

	help_re := regexp.MustCompile(fmt.Sprintf("(?i)@?%s,? (!?help( all)?)$", CONFIG["mentionName"]))
	m := help_re.FindStringSubmatch(msg)
	if len(m) > 0 {
		arg := ""
		if len(m[2]) > 0 {
			arg = "all"
		}
		reply(r, cmdHelp(r, r.ReplyTo, []string{arg}))
		return
	}

	if wasInsult(msg) && (forUs ||
		(ch.Toggles["chatter"] && mentioned)) {
		reply(r, cmdInsult(r, r.ReplyTo, []string{"me"}))
		return
	}

	if ch.Toggles["chatter"] {
		chitchat = chatterParrotParty(msg)
		if len(chitchat) > 0 {
			reply(r, chitchat)
			return
		}

		chitchat = chatterMontyPython(msg)
		if (len(chitchat) > 0) && ch.Toggles["python"] &&
			!isThrottled("python", ch) {
			reply(r, chitchat)
			return
		}

		chitchat = chatterSeinfeld(msg)
		if (len(chitchat) > 0) && !isThrottled("seinfeld", ch) {
			reply(r, chitchat)
			return
		}

		chitchat = chatterH2G2(msg)
		if (len(chitchat) > 0) && !isThrottled("h2g2", ch) {
			reply(r, chitchat)
			return
		}

		chitchat = chatterDrWho(msg)
		if (len(chitchat) > 0) && !isThrottled("drwho", ch) {
			reply(r, chitchat)
			return
		}

		chitchat = chatterMisc(msg, ch, r)
		if len(chitchat) > 0 {
			reply(r, chitchat)
			return
		}

		chitchat = chatterPhish(msg, ch, r)
		if len(chitchat) > 0 {
			reply(r, chitchat)
			return
		}

		chitchat = chatterFlight(msg, ch, r)
		if len(chitchat) > 0 {
			reply(r, chitchat)
			return
		}
	}

	if forUs || (ch.Toggles["chatter"] && mentioned) {
		chitchat = chatterEliza(msg, r)
		if len(chitchat) > 0 {
			reply(r, chitchat)
		}
		return
	}
}

func wasInsult(msg string) (result bool) {
	result = false

	var insultPatterns = []*regexp.Regexp{
		regexp.MustCompile(fmt.Sprintf("(?i)you('re|r| are) a tool[, ]*@?%s", CONFIG["mentionName"])),
		regexp.MustCompile(fmt.Sprintf("(?i)fu[, ]@?%s", CONFIG["mentionName"])),
		regexp.MustCompile(fmt.Sprintf("(?i)@?%s su(cks|x)", CONFIG["mentionName"])),
		regexp.MustCompile("(?i)asshole|bitch|dickhead"),
		regexp.MustCompile("(?i)dam+n? (yo)?u"),
		regexp.MustCompile(fmt.Sprintf("(?i)(be )?quiet @?%s", CONFIG["mentionName"])),
		regexp.MustCompile("(?i)shut ?(the fuck )?up"),
		regexp.MustCompile("(?i)(screw|fuck) (yo)u"),
		regexp.MustCompile("(?i)(piss|bugger) ?off"),
		regexp.MustCompile("(?i)fuck (off|(yo)u)"),
		regexp.MustCompile("(?i)(yo)?u (suck|blow|are ((very|so+) )?(useless|lame|dumb|stupid|stink))"),
		regexp.MustCompile("(?i)(stfu|go to hell|shut[ t]up)"),
		regexp.MustCompile("(?i) is (stupid|dumb|annoying|lame|boring|useless|a jerk)"),
		regexp.MustCompile(fmt.Sprintf("(?i)(stupid|annoying|lame|boring|useless) +(%s|bot)", CONFIG["mentionName"])),
		regexp.MustCompile(fmt.Sprintf("(?i)(blame )?(%s|the bot)('?s fault)", CONFIG["mentionName"])),
	}

	for _, p := range insultPatterns {
		if p.MatchString(msg) {
			return true
		}
	}

	return
}
