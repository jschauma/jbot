This is a Slack version of the 'jbot' IRC bot,
originally "developed" at Yahoo! in 2007, and which
can be found in the 'old/irc' subdir.

This variant was created as a rewrite in Go for
HipChat in July 2016 by Jan Schaumann (@jschauma /
jschauma@netmeister.org).  Support for Slack was added
in July 2017.  Many thanks to Yahoo for letting me
play around with nonsense like this.

You should be able to run the bot by populating a
configuration file with suitable values.  The
following configuration values are required:

```
slackService  = the Slack service name, e.g. <foo>.slack.com
slackToken    = the authentication token for your bot
```

You may optionally also set the following
configuration values:

```
    channelFile = pathname where to store a state file
    debug = whether to enable debugging output
    opsgenieApiKey = an API key to access OpsGenie
```

This bot has a bunch of features that are company
internal; those features have been removed from
this public version.

Some day this should be extended into a pluggable
bot, so that internal code can more easily be kept
apart, I suppose.  Pull requests welcome etc.

Some of the URLs used by the bot reference simple text
documents hosted on an internal server.  This is so as
to not rely on outside resources and their flimsy
markups as well as to control the contents somewhat.
You can update the URLS map in jbot.go.

---

Interacting with the bot:

Getting jbot to join your channel:

```
/invite jbot
```

Getting jbot to leave your channel:

```
!leave
```

(Note: on Slack, bots cannot leave a channel and
require an admin to kick them.)


jbot responds to:
- commands invoked via '!command' -- see '!help'
- commands invoked via @-mentioning the bot:
  !command     => "@jbot command"
  !command arg => "@jbot command arg"

If 'chatter' is not toggled off (it is on by default),
jbot will also:
- reply to any statement that is addressed to it:
  How are you today, jbot?
  jbot, you suck

- chime in with certain semi-random replies (sure, you
  got the source, but the fun part is figuring out
  what triggers what type of response); some of these
  responses are throttled to avoid too repetitive
  annoyances

You may wish to '!toggle chatter' to turn all that
off.

All private messages to jbot are interpreted as
commands. You cannot engage in a private conversation
with jbot.

### Supported commands

The full list of public features can be seen via the
"!help" command.

The following is a list of commands supported as of
2016-09-09 with examples.

#### !8ball &lt;question&gt; -- ask the magic 8-ball

Ask the magic 8-ball. You get the expected reply.
E.g.:

```
16:16 <jschauma> !8ball is this a useful feature?
16:16 <jbot> My sources say no.
```

#### !asn &lt;hostname|ip|asn&gt; -- display information about ASN

```
16:20 <jschauma> !asn www.yahoo.com
16:20 <jbot> 36646   | 98.138.49.66     | YAHOO-NE1 - Yahoo, US
16:20 <jschauma> !asn 98.138.49.66
16:20 <jbot> 36646   | 98.138.49.66     | YAHOO-NE1 - Yahoo, US
16:20 <jschauma> !asn 36646
16:20 <jbot> YAHOO-NE1 - Yahoo, US
```

#### !bacon -- when you just need some more meat in your life

```
12:42 <jschauma> !bacon
12:42 <jbot> Strip steak burgdoggen pork chop prosciutto tenderloin, brisket doner porchetta jowl pork ham hock meatloaf.
```

#### !beer -- obey your thirst

```
20:57 <@jschauma> !beer
20:57 < jbot> AleSmith Speedway Stout by AleSmith Brewing Company - 4.37
20:57 < jbot> American Double / Imperial Stout (12.00%)
20:57 < jbot> https://www.beeradvocate.com/beer/profile/396/3833/
20:58 <@jschauma> !beer bacon
20:58 < jbot> Maple Bacon Coffee Porter by Funky Buddha Brewery - 4.45
20:58 < jbot> American Porter (6.40%)
20:58 < jbot> https://www.beeradvocate.com/beer/profile/31805/62761/
```

#### !bs -- Corporate B.S. Generator

```
16:18 <jans> !bs
16:18 <jbot> energistically reconceptualize real-time intellectual capital
```

#### !cert -- display information about the x509 cert found at the given hostname

This command allows you to display information about
the x509 certificate from the given host.</p>

You can specify a hostname or an IP address followed
optionally by a port.  If a port is not provided, jbot
will try port 443.  You can also provide an optional
SNI name as well as ask for the full
chain:
```
16:22 <@jans> !cert www.yahoo.com
16:22 < jbot> Serial Number: 08:88:b1:ad:2a:59:33:10:59:3f:47:56:5a:5a:5a:4a
Subject      : CN=*.www.yahoo.com,O=Yahoo Holdings\, Inc.,L=Sunnyvale,ST=California,C=US
Issuer       : CN=DigiCert SHA2 High Assurance Server CA,OU=www.digicert.com,O=DigiCert Inc,C=US
Validity     :
   Not Before: 2018-08-13 00:00:00 +0000 UTC
   Not After : 2019-02-14 12:00:00 +0000 UTC
44 SANs:
...
16:22 <@jans> !cert www.yahoo.com chain
16:22 < jbot> Certificate 0:
Serial Number: 08:88:b1:ad:2a:59:33:10:59:3f:47:56:5a:5a:5a:4a
Subject      : CN=*.www.yahoo.com,O=Yahoo Holdings\, Inc.,L=Sunnyvale,ST=California,C=US
...
Certificate 1:
Serial Number: 04:e1:e7:a4:dc:5c:f2:f3:6d:c0:2b:42:b8:5d:15:9f
Subject      : CN=DigiCert SHA2 High Assurance Server CA,OU=www.digicert.com,O=DigiCert Inc,C=US
...
16:22 <@jans> !cert badssl.com extended-validation.badssl.com
16:22 < jbot> Serial Number: 03:6a:f1:d4:8f:7e:5f:22:2a:8c:45:25:f0:12:c9:e1
Subject      : SERIALNUMBER=C2543436,CN=extended-validation.badssl.com,O=Mozilla Foundation,L=Mountain View,ST=California,C=US
...

```

#### !channels -- display channels I'm in

```
16:22 <jschauma> !channels
16:22 <jbot> I'm in the following 2 channels:
16:22 <jbot> foo, bar
```

#### !clear [num] -- clear the screen / backlog

Suppose you had a NSFW comment in your room, or wish
to scroll an annoying gif off screen by e.g. 15 lines.
You can run:

```
16:23 <jschauma> !clear 15
16:23 <jbot> /code ...............
16:23 <jbot> ..............
16:23 <jbot> .............
16:23 <jbot> ............
16:23 <jbot> ...........
16:23 <jbot> ..........
16:23 <jbot> .........
16:23 <jbot>  ______
16:23 <jbot> < clear >
16:23 <jbot>  -------
16:23 <jbot>         \   ^__^
16:23 <jbot>          \  (oo)\_______
16:23 <jbot>             (__)\       )\/\
16:23 <jbot>                 ||----w |
16:23 <jbot>                 ||     ||
```

#### !cowsay &lt;something&gt; -- cowsay(1) something

```
16:25 <jschauma> !cowsay moo
16:25 <jbot> /code  _____
16:25 <jbot> < moo >
16:25 <jbot>  -----
16:25 <jbot>         \   ^__^
16:25 <jbot>          \  (oo)\_______
16:25 <jbot>             (__)\       )\/\
16:25 <jbot>                 ||----w |
16:25 <jbot>                 ||     ||
```

#### !curses [&lt;user&gt;] -- check your curse count

```
16:26 <jschauma> !curses
16:26 <jbot> shit (2), fuck (1)
16:26 <jschauma> !curses lord
16:26 <jbot> Looks like lord has been behaving so
far.
```

#### !cve &lt;cve-id&gt; -- display vulnerability description

```
16:27 <jschauma> !cve CVE-2016-5385
16:27 <jbot> PHP through 7.0.8 does not attempt to address RFC 3875 section 4.1.18 namespace
              conflicts and therefore does not protect applications from the presence of untrusted
              client data in the HTTP_PROXY environment variable, which might allow remote
              attackers to redirect an application's outbound HTTP traffic to an arbitrary proxy
              server via a crafted Proxy header in an HTTP request, as demonstrated by (1) an
              application that
16:27 <jbot> makes a getenv('HTTP_PROXY') call or (2) a CGI configuration of PHP, aka an "httpoxy"
              issue.
16:27 <jbot> https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2016-5385
```

#### !fml -- display an FML quote

Note: possibly NSFW.
```
16:28 <jschauma> !fml
16:28 <jbot> Today, I asked my mom why she drinks. She said she only drinks when she's depressed.
              My step-dad said she only drinks on the weekend. Those are the days I'm at her house.
              FML
```

#### !fortune -- print a random, hopefully interesting, adage

```
16:28 <jschauma> !fortune
16:28 <jbot> Denver, n.:
16:28 <jbot> A smallish city located just below the `O' in Colorado.
```

#### !help [all|&lt;command&gt;] -- show help

```
10:49 <jans> !help
10:49 <jbot> I know 32 commands.
10:49 <jbot> Use '!help all' to show all commands.
10:49 <jbot> Ask me about a specific command via '!help <cmd>'.
10:49 <jbot> If you find me annoyingly chatty, just '!toggle chatter'.
10:49 <jbot> To ask me to leave a channel, say '!leave'.
10:50 <jans> !help help
10:50 <jbot> !help [all|<command>] -- display this help
16:28 <jans> !help all
16:28 <jbot> These are commands I know:
16:28 <jbot> 8ball, asn, by, channels, clear, cowsay, curses, cve, fml, fortune,
              help, host, info, insult, jira, leave, ping, quote, rfc,
              room, seen, set, speb, stfu, tfln, throttle, tld, toggle, trivia, ud,
              unset, user, vu, weather, wtf
```

#### !host &lt;host&gt; -- host lookup

```
Just like host(1).

16:29 <jschauma> !host www.yahoo.com
16:29 <jbot> www.yahoo.com is an alias for fd-fp3.wg1.b.yahoo.com.
16:29 <jbot> fd-fp3.wg1.b.yahoo.com has address 98.138.49.66
16:29 <jbot> fd-fp3.wg1.b.yahoo.com has IPv6 address 2001:4998:44:204::a8
```

#### !how &lt;command&gt; -- show how a command is implemented

```
11:38 <jschauma> !how tld
11:38 <jbot> whois -h whois.iana.org
```

#### !img &lt;something&gt; -- fetch a link to an image
Note: possibly NSFW

```
12:49 <jschauma> !img avocado
12:49 <jbot> http://www.buyfruit.com.au/images/P/iStock_000002972468Small_%28avocado_-_shepard%29__31894.jpg
```


#### !info &lt;channel&gt; -- display info about a channel

```
16:29 <jschauma> !info
16:29 <jbot> I was invited into #jtest by jans.
16:29 <jbot> These are the users I've seen in #jtest:
16:29 <jbot> jbot, jans
16:29 <jbot> Top 10 channel chatterers for #jtest:
16:29 <jbot> jans (69), jbot (2)
16:29 <jbot> These are the toggles for this channel:
16:29 <jbot> chatter => true, python => true, trivia => true
16:29 <jbot> This channel is currently unthrottled.
```

#### !insult &lt;somebody&gt; -- insult somebody

```
16:30 <jschauma> !insult Donald Trump
16:30 <jbot> Donald Trump: Thou saucy unchin-snouted lout!
```

#### !jira &lt;ticket&gt; -- display info about a jira ticket

```
16:02 <jschauma> !jira foo-123
16:02 <jbot> Summary : do the foo thing
16:02 <jbot> Status  : Done
16:02 <jbot> Created : 2016-07-30T00:06:03.000+0000
16:02 <jbot> Assignee: foo
16:02 <jbot> Reporter: jschauma
16:02 <jbot> Link    : https://jira-url/browse/foo-123
```

#### !leave -- cause me to leave the current channel

Self-explanatory, hopefully.

#### !oid &lt;oid&gt; -- display OID information

```
16:21 <jans> !oid 1.2.840.113549.1.1.1
16:21 <jbot> ASN.1 notation: {iso(1) member-body(2) us(840) rsadsi(113549) pkcs(1) pkcs-1(1)
             rsaEncryption(1)}
16:21 <jbot> Description: Rivest, Shamir and Adleman (RSA) encryption (and signing)
16:21 <jbot> Information: Defined in IETF RFC 2313, IETF RFC 2437. See also IETF RFC 3370.
16:21 <jbot> See also the equivalent but deprecated OID {joint-iso-itu-t(2) ds(5) algorithm(8)
             encryptionAlgorithm(1) rsa(1)}.
```

#### !oncall &lt;group&gt; --- show who's oncall

This will attempt to look up an oncall schedule in
OpsGenie.  This option requires the 'opsgenieApiKey'
configuration option to be set.

jbot tries to be helpful and display possible groups
if it can't find the on you're looking for.

If you want to save yourself some typing, you can also
set a default oncall group for your channel via the
'set' command. E.g.:

```
16:06 <jschauma> !set
16:06 <jbot> There currently are no settings for #jtest.
16:07 <jschauma> !oncall
16:07 <jbot> No such group: jtest
16:07 <jschauma> !oncall jbot
16:07 <jbot> No OpsGenie schedule found for 'jbot'.
16:07 <jbot> Possible candidates:
16:07 <jbot> JBOT_Support, JBOT_ERMAGEHRD
16:08 <jschauma> !oncall JBOT_Support
16:08 <jbot> US: jschauma
16:08 <jbot> EU: jschauma
16:08 <jschauma> !oncall JBOT_ERMAGEHRD
16:08 <jbot> Schedule found in OpsGenie for 'JBOT_ERMAGEHRD', but nobody's currently oncall.
16:08 <jbot> You can try contacting the members of team 'JBOT_ERMAGEHRD':
16:08 <jbot> jschauma@netmeister.org
```

#### !ping &lt;hostname&gt; -- try to ping hostname

```
16:32 <jschauma> !ping www.yahoo.com
16:32 <jbot> www.yahoo.com is alive.
```

#### !praise [&lt;somebody&gt;] -- praise somebody

```
20:43 <jschauma> !praise jbot
20:43  * jbot blushes.
20:43 <jschauma> !praise somebody
20:43 <jbot> somebody: You're nicer than a day on the beach.
20:43 <jschauma> !praise
20:43 <jbot> jbot (18), somebody (1)
```

#### !quote &lt;symbol&gt; -- show stock price information

```
16:32 <jschauma> !quote yhoo
16:32 <jbot> yhoo: 44.30 (-0.36 - -0.81%)
```

#### !rfc &lt;rfc&gt; -- show RFC title and URL

```
16:33 <jschauma> !rfc 3514
16:33 <jbot> The Security Flag in the IPv4 Header
16:33 <jbot> https://tools.ietf.org/html/rfc3514
```

#### !room &lt;room&gt; -- show information about the given HipChat room

```
16:34 <jschauma> !room bot
16:34 <jbot> No room with that exact name found.
16:34 <jbot> Some possible candidates might be:
16:34 <jbot> bot bot bot - bot stuff
16:34 <jbot> jbot-test - test
16:34 <jbot> ...
16:34 <jschauma> !room bot bot bot
16:34 <jbot> 'bot bot bot' (public)
16:34 <jbot> Topic: bot stuff
16:34 <jbot> Owner: jschauma
16:34 <jbot> https://<company>.hipchat.com/history/room/2906069
```

#### !seen &lt;user&gt; [&lt;channel&gt;] -- show last time &lt;user&gt; was seen in &lt;channel&gt;

jbot can only see users in channels its in.

```
16:36 <jschauma> !seen alice
16:36 <jbot> I have not seen that user in #jtest.
16:36 <jschauma> !seen bob someroom
16:36 <jbot> I'm not currently in #someroom.
16:36 <jschauma> !seen jschauma
16:36 <jbot> Thu Sep  8 20:36:54 UTC 2016
```

#### !set [name=value] -- set 'name' to 'value'

Set a channel setting.

#### !speb -- show a security problem excuse bingo result

```
16:44 <jschauma> !speb
16:44 <jbot> You're just an academic.
```

#### !stfu [&lt;user&gt;] -- show channel chatterers

```
16:44 <jschauma> !stfu
16:44 <jbot> jans (88), bob (24), alice (13)
```

#### !tfln -- display a text from last night

Note: Possibly NSFW.

```
16:46 <jschauma> !tfln
16:46 <jbot> I either just heard my neighbors having sex or she really agreed with whatever he was
              talking about.
```

#### !throttle -- show or set throttles in this channel

Certain triggers only kick in unless it's previously
been triggered within a given time frame. You can
check which ones you're currently throttled for using
this command.

#### !tld [&lt;tld&gt;] -- display tld information

```
17:05 <jschauma> !tld de
17:05 <jbot> Organization: DENIC eG
17:05 <jbot> Contact     : vorstand@denic.de
17:05 <jbot> Whois       : whois.denic.de
17:05 <jbot> Status      : ACTIVE
17:05 <jbot> Created     : 1986-11-05
17:05 <jbot> URL         : http://www.denic.de/
```

#### !time [location] -- show the current time (in the given location)

```
16:59 <jschauma> !time
16:59 <jbot> Tue Nov  1 20:59:19 UTC 2016
16:59 <jbot> Tue Nov  1 16:59:19 EDT 2016
16:59 <jbot> Tue Nov  1 13:59:19 PDT 2016
16:59 <jschauma> !time new york
16:59 <jbot> Tue Nov  1 16:59:23 EDT 2016
16:59 <jschauma> !time taipei
16:59 <jbot> Wed Nov  2 04:59:48 CST 2016
17:01 <jschauma> !time Portugal
17:01 <jbot> Tue Nov  1 21:01:35 WET 2016
```

#### !tld [&lt;tld&gt;] -- display tld information

```
17:05 <jschauma> !tld de
17:05 <jbot> Organization: DENIC eG
17:05 <jbot> Contact     : vorstand@denic.de
17:05 <jbot> Whois       : whois.denic.de
17:05 <jbot> Status      : ACTIVE
17:05 <jbot> Created     : 1986-11-05
17:05 <jbot> URL         : http://www.denic.de/
```

#### !toggle [&lt;feature&gt;] -- toggle a feature

Turn on or off a feature. Most useful for turning off
'chatter' (see above).

#### !trivia -- show a random piece of trivia

```
16:48 <jschauma> !trivia
16:48 <jbot> Sleeping on the job is acceptable in Japan, as it is seen as exhaustion from working
              too hard.
```

#### !ud &lt;term&gt; -- look up a term using the Urban Dictionary (NSFW)

Note: possibly NSFW

```
16:48 <jschauma> !ud food
16:48 <jbot> food: a substance you eat,then poop out.usually followed my a nap.
16:48 <jbot> Example: hungry.....need food....
```

#### !unset name -- unset a channel setting

Unset a per-channel setting.

#### !user &lt;user&gt;-- show information about the given HipChat user

```
16:49 <jschauma> !user alice
16:49 <jbot> No such user: alice
16:49 <jschauma> !user bob
16:49 <jbot> No user with that exact name found.
16:49 <jbot> Some possible candidates might be:
16:49 <jbot> Bob Marley <bobmarley@domain.com> (bobc)
16:49 <jbot> Bobby Tables <btables';drop table students;@domain.com>
16:49 <jbot> ...
16:52 <jschauma> !user 1234567
16:52 <jbot> Jan Schaumann <jschauma@netmeister.org> (jschauma)
```

#### !vu &lt;num&gt; -- display summary of a CERT vulnerability

```
16:57 <jschauma> !vu 797896
16:57 <jbot> CGI web servers assign Proxy header values from client requests to internal
              HTTP_PROXY environment variables
16:57 <jbot> Web servers running in a CGI or CGI-like context may assign client request Proxy
              header values to internal HTTP_PROXY environment variables. This vulnerability can be
              leveraged to conduct man-in-the-middle (MITM) attacks on internal subrequests or to
              direct the server to initiate connections to arbitrary hosts.
16:57 <jbot> https://www.kb.cert.org/vuls/id/797896
```

#### !weather &lt;location&gt; -- show weather information

```
16:57 <jschauma> !weather nyc
16:57 <jbot> Conditions for New York, NY, US at 04:00 PM EDT
16:57 <jbot> Today   : Partly Cloudy (Low: 72; High: 86)
16:57 <jbot> Tomorrow: Partly Cloudy (Low: 79; High: 90)
```

#### !whocyberedme -- show who cybered you

```
15:27 <jschauma> !whocyberedme
15:27 <jbot> Crowd Strike confirms: The NSA cybered you using a PEBKAC DoS.
```

#### !whois &lt;domain&gt; -- show whois information

```
14:37 <jschauma> !whois yahoo.com
14:37 <jbot> Registrar: MarkMonitor, Inc.
14:37 <jbot> Registrar URL: http://www.markmonitor.com
14:37 <jbot> Updated Date: 2017-06-29T11:36:33-0700
14:37 <jbot> Creation Date: 1995-01-18T00:00:00-0800
14:37 <jbot> Registrant Name: Domain Administrator
14:37 <jbot> Registrant Organization: Yahoo! Inc.
14:37 <jbot> Registrant Country: US
14:37 <jbot> Registrant Email: domainadmin@yahoo-inc.com
14:37 <jbot> Name Server: ns4.yahoo.com, ns2.yahoo.com, ns3.yahoo.com, ns5.yahoo.com, ns1.yahoo.com
14:37 <jbot> DNSSEC: unsigned
```

#### !wtf &lt;term&gt; -- decrypt acronyms

```
16:57 <jschauma> !wtf arp
16:57 <jbot> ARP: Address Resolution Protocol
```

---

### Requirements:
Go 1.3

### Installation:
```
make
install -c -m 755 jbot /somewhere/in/your/path/jbot
```

### Questions/comments:
jschauma@netmeister.org

https://twitter.com/jschauma
