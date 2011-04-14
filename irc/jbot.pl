#!/usr/local/bin/perl -w
#
# Jan's Bot.
#
# This IRC bot has its origins at Yahoo!, where it was spawned from a
# previous POE-based bot and subsequently carelessly slapped together by
# Jan Schaumann <jschauma@yahoo-inc.com>.  It has grown a number of
# features: some useful, some less so, some company specific (removed in
# this version) and some general purpose functions.
#
# It should be noted that this particular code is not representative of
# the author's usual code; quite the contrary.  It's a mental playground
# of "do as I say, not as I do" and the author is fully aware of its
# inherent awfulness.  No apologies.
#
# jbot has recently performed the quantum leap to twitter, where it can be
# found as "@j_b_o_t"; it hopes to eventually become self-aware and father
# skynet.
#
# jbot is considered by it's author as beerware, so should we meet some
# day, and you think jbot's worth it, you can buy me a beer in return.
#
# Yahoo!'s Open Source Working Group asked that the code not be licensed
# as beerware, but using a 3-clause BSD license, so this is what it'll be.
# Yahoo! also wanted to retain the copyright:
#
# Copyright (c) 2011, Yahoo! Inc.
# All rights reserved.
#
# Redistribution and use of this software in source and binary forms,
# with or without modification, are permitted provided that the following
# conditions are met:
#
# * Redistributions of source code must retain the above
#   copyright notice, this list of conditions and the
#   following disclaimer.
#
# * Redistributions in binary form must reproduce the above
#   copyright notice, this list of conditions and the
#   following disclaimer in the documentation and/or other
#   materials provided with the distribution.
#
# * Neither the name of Yahoo! Inc. nor the names of its
#   contributors may be used to endorse or promote products
#   derived from this software without specific prior
#   written permission of Yahoo! Inc.
#
# THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS
# IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
# TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A
# PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
# OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
# SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
# LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
# DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
# THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
# (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
# OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

use strict;
use 5.008;

use POE;
use POE::Component::IRC;

use POSIX qw(mktime strftime);

use Compress::Zlib;
use Date::Calc qw(Delta_DHMS Week_of_Year Monday_of_Week This_Year Weeks_in_Year );
use Date::Parse;
use Digest::MD5;
use File::Temp qw(tempfile);
use HTML::Entities;
use IPC::Open3;
use JSON;
use LWP::Simple;
use LWP::UserAgent;
use NetAddr::IP;
use Net::Ping::External qw(ping);
use Net::Whois::Raw;
use URI::Escape;
use XML::Simple;

# Used for !vlan subnet file parsing
use NetAddr::IP;

use Net::hostent;
use Socket;
use Socket6;

use Storable qw(store retrieve);

$ENV{'TZ'} = "PST8PDT";

our $_kernel;
our $botowner = 'yourusernamehere';
our $botownerhost = 'yourusualhostnamehere';

# A hash of toggles for each channel.  It maps the channel name to the
# toggleable values, ie
#    $toggles{"sa-guru"} => ( "tiny" => 1, "omg" => 0 ).
our @chatter_toggles = (
			"charliesheen",
			"futurama",
			"hotness",
			"holdon",
			"insults",
			"monkey",
			"schneier",
			"stfu",
			"trivia",
			"oncall",
			"omg",
			"yourmom",
			);

our @toggleables = ( @chatter_toggles,
			"chatter",
			"nc17",
			"sed",
			"tiny",
			"title",
		);



# a "channel" may contain the following data:
# - a hash of toggleables "toggleables" => ( t1 => 0, t2 => 2 )
# - an inviter "inviter" => "obo"
# - a key to be used for keyed channels "key" => "s3criT"
# - a hash of users to how many lines they've uttered
#		"stfu" => ( u1 => 1, u2 => 2, ... )
#
# %CHANNELS will look like this:
#
# %CHANNELS = (
#		"channel1" => (
#			"toggles" => ( t1 => 0, t2 => 2, ... ),
#			"inviter" => "obo",
#			"key" => "s3criT",
#			"users" => (
#				"u1" => 1,
#				"u2" => 1,
#				...
#				),
#			"seen" => (
#				"u1" => 1,
#				"u2" => 1,
#				...
#				),
#			),
#		"channel2" => ( ... ),
#		...
#		);

# function : newChannel
# purpose  : create and populate a hash suitable to be used as a "channel"
# inputs   : optional: inviter, key
# returns  : a hashref representing a channel

sub newChannel(;$$) {
	my ($inviter, $key) = @_;

	my %c;

	$c{"inviter"} = $inviter;
	$c{"key"} = $key;
	$c{"toggles"} = ();
	$c{"users"} = ();

	foreach my $t (@toggleables) {
		$c{"toggles"}{$t} = 1;
	}

	$c{"toggles"}{"title"} = 0;

	return \%c;
}

our $botnick = 'jbot';
our %CHANNELS = ( "#$botnick-$botowner" => newChannel($botowner) );

our $ircserver = 'irc.efnet.org';

our $channels_file = "/var/tmp/$botnick.channels";
our $potty_file = "/var/tmp/$botnick.potty";
our $rainbow_file = "/var/tmp/$botnick.rainbow";
our $curses_file = "/var/tmp/$botnick.curses";
our $cmdr_file = "/var/tmp/$botnick.cmdrs";
our $cmd_file = "/var/tmp/$botnick.cmd";
our $autoping = 300;
our $maxperiod = 172800;

# a hash-ref containing the results from the last irc_whois lookup
our $last_whois_result;
our $whois_by_field = "";

# we clumsily stuffed primes(6) into that dir
$ENV{'PATH'} = $ENV{'PATH'} . ":/usr/games:/home/$botowner/bin";

our @whats_new = ( "!subnet_resolver [ipv4[/nm]] -- display resolvers for given subnet",
			"fix !beer" );

our %cmds;
our %cmdrs;
our %pottymouths;
our %curses;
our %rainbow;

END {
	my $now = localtime();

	my $cf;

	do_mail("Exit at $now; uptime: " . get_uptime(), $botnick, $botowner, "exit");

	unlink($channels_file);
	unlink($potty_file);
	unlink($curses_file);
	unlink($cmd_file);
	unlink($cmdr_file);
	store(\%CHANNELS, $channels_file);
	store(\%pottymouths, $potty_file);
	store(\%curses, $curses_file);
	store(\%cmds, $cmd_file);
	store(\%cmdrs, $cmdr_file);
	store(\%rainbow, $rainbow_file);

        my @keys = sort { $cmds{$b} <=> $cmds{$a} } keys %cmds;
	my $n = 0;
        foreach my $k (@keys) {
                last if ($n > 5);
		print STDERR "$k (" . $cmds{$k} . ")\n";
                $n++;
        }
}

$SIG{ALRM} = 'IGNORE';

our %bricks;

our $last_escalation;

our %periodics = (
	);

our %totcount;

our %methods= (
	"8ball" => 'My magic 8-ball.  Duh.',
	"aotd"  => 'http://feeds.feedburner.com/animals',
	"asn"	=> 'whois -h whois.cymru.com " -v $input"',
	"az"	=> 'http://www.amazon.com/s?index=blended&field-keywords=',
	"babel"	=> 'http://babelfish.yahoo.com/translate_txt',
	"beer"	=> 'http://www.ratebeer.com/findbeer.asp',
	"better" => 'http://sucks-rocks.com/',
	"bible" => 'http://www.biblegateway.com/passage/?search=',
	"bing"  => 'http://www.bing.com/search?q=',
	"bofh"	=> 'http://pages.cs.wisc.edu/~ballard/bofh/excuses',
	"bugmenot" => 'http://www.bugmenot.com/view/',
	"bush"  => 'http://en.wikiquote.org/wiki/George_W._Bush',
	"cal"	=> 'cal(1)',
	"calc"  => 'bc(1)',
	"calendar" => 'calendar(1)',
	"charliesheen" => 'http://www.livethesheendream.com/',
	"chuck"  => 'http://4q.cc/index.php?pid=fact&person=chuck',
	"cowsay" => 'cowsay(1)',
	"countdown" => "$botnick's brain",
	"cursebird" => 'http://cursebird.com/',
	"cve"	=> 'http://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-',
	"digest" => 'digest(1)',
	"errno" => '/usr/include/sys/errno.h',
	"fileinfo" => 'http://www.fileinfo.net/extension/',
	"flickr" => 'http://www.flickr.com/photos/tags/',
	"flight" => 'http://dps1.travelocity.com/dparflifo.ctl?',
	"fml"	=> 'http://www.fmylife.com/random',
	"fortune" => 'fortune(1)',
	"foo"	=> 'http://en.wikipedia.org/wiki/Metasyntactic_variable',
	"futurama" => 'http://www.iolfree.ie/~alexandros/humour/futurama.htm',
	"g"	=> 'http://www.google.com/search?q=',
	"gas"	=> 'http://gasprices.mapquest.com/searchresults.jsp?search=true&gasPriceType=3%2C4%2C5&postalCode=',
	"geo"	=> 'http://local.yahooapis.com/MapsService/V1/geocode?appid=batchGeocode&location=',
	"geoip"	=> 'http://www.ipaddressguide.com/ip2location.aspx?ip=',
	"host"	=> 'host(1)',
	"help"  => 'hardcoded',
	"how"	=> 'hardcoded',
	"imdb"	=> 'http://lab.abhinayrathore.com/imdb/index.php',
	"ip"	=> 'NetAddr::IP',
	"ipv4"	=> 'http://ipv6.he.net/exhaustionFeed.php?platform=json',
	"ipv4countdown"	=> 'http://www.potaroo.net/tools/ipv4/',
	"jbot"	=> 'SVN:yahoo/ops/nss/user/jans/jbot',
	"man"  => 'http://www.freebsd.org/cgi/man.cgi?manpath=FreeBSD+7.0-RELEASE+and+Ports&format=ascii&query=',
	"rhelman"  => 'http://grisha.biz/cgi-bin/man?format=ascii&query=',
	"motd" => 'fortune(1)',
	"morse"	=> 'morse(6)',
	"movies" => 'http://rss.ent.yahoo.com/movies/thisweek.xml',
	"movies_opening" => 'http://rss.ent.yahoo.com/movies/thisweek.xml',
	"movies_soon" => 'http://rss.ent.yahoo.com/movies/upcoming.xml',
	"movies_gossip" => 'http://rss.ent.yahoo.com/movies/movie_headlines.xml',
	"mrt"  => 'http://4q.cc/index.php?pid=fact&person=mrt',
	"ninja" => 's3crit',
	"nts"	=> 'sendmail',
	"ohiny" => 'http://www.overheardinnewyork.com/bin/randomentry.cgi',
	"omg"   => 'http://omg.yahoo.com/latest/rss/news',
	"perldoc" => '/usr/local/bin/perldoc -t -f',
	"ping" => 'Net::Ping::External',
	"pirate" => 'crawled out me bung hole',
	"primes" => 'primes(6)',
	"pydoc" => '/usr/local/bin/pydoc',
	"quake" => 'http://earthquake.usgs.gov/earthquakes/recenteqs<WHAT>/Quakes/quakes_all.php',
	"quip"	=> 'http://www.randominsults.net/',
	"fullquote" => 'http://finance.yahoo.com/q?s=',
	"rate"	=> 'http://sucks-rocks.com/rate/',
	"rainbow" => 'I AM Weasel!',
	"random" => 'rand',
	"rfc"	=> 'http://tools.ietf.org/html/',
	"rotd"	=> 'http://feeds.epicurious.com/newrecipes?format=xml',
	"rosetta" => 'http://bhami.com/rosetta.html',
	"shakespear" => 'http://www.pangloss.com/seidel/Shaker/',
	"score"	=>  'http://www.totallyscored.com/rss',
	"seen"	=>  'I\'m watching you all!',
	"speb"	=> 'http://www.crypto.com/bingo/pr',
	"service" => "/etc/services",
	"signal" => '/usr/include/sys/signal.h',
	"snopes"=> 'http://search.atomz.com/search/?getit=Go&sp-a=00062d45-sp00000000&sp-advanced=1&sp-p=all&sp-w-control=1&sp-w=alike&sp-date-range=-1&sp-x=any&sp-c=100&sp-m=1&sp-s=0&sp-q=',
	"stfu" =>  'What do you think?',
	"symbol" => 'http://finance.yahoo.com/lookup?s=',
	"sysexit" => '/usr/include/sysexits.h',
	"sysexits" => '/usr/include/sysexits.h',
	"tld"	=> 'http://www.iana.org/domains/root/db/',
	"tiny" => 'http://is.gd/api.php?longurl=',
	"top5yb" => 'http://rss.news.yahoo.com/rss/business',
	"top5ydvds" => 'http://rss.movies.yahoo.com/dvd/topsellers.xml',
	"top5yboxoffice" => 'http://rss.ent.yahoo.com/movies/boxoffice.xml',
	"top5ye" => 'http://rss.news.yahoo.com/rss/mostemailed',
	"top5yel" => 'http://rss.news.yahoo.com/rss/elections',
	"top5yen" => 'http://rss.news.yahoo.com/rss/entertainment',
	"top5yh" => 'http://rss.news.yahoo.com/rss/health',
	"top5ys" => 'http://rss.news.yahoo.com/rss/science',
	"top5ysp" => 'http://rss.news.yahoo.com/rss/sports',
	"top5yr" => 'http://rss.news.yahoo.com/rss/highestrated',
	"top5yt" => 'http://rss.news.yahoo.com/rss/tech',
	"top5yterror" => 'http://rss.news.yahoo.com/rss/terrorism',
	"top5yo" => 'http://rss.news.yahoo.com/rss/obits',
	"top5yodd" => 'http://rss.news.yahoo.com/rss/oddlyenough',
	"top5yoped" => 'http://rss.news.yahoo.com/rss/oped',
	"top5yp" => 'http://rss.news.yahoo.com/rss/politics',
	"top5yw" => 'http://rss.news.yahoo.com/rss/world',
	"top5yv" => 'http://rss.news.yahoo.com/rss/mostviewed',
	"top5g"	=> 'http://www.google.com/trends/hottrends?date=',
	"top5y"	=> 'http://d.yimg.com/ds/rss/V1/top10/all',
	"top5"	=> '!top5g',
	"top5twitter" => 'http://twitter.com/favorites/toptweets.rss',
	"traffic" => 'http://traffic.511.org/traffic_text_all.asp',
	"trivia" => 'http://www.nicefacts.com/quickfacts/index.php',
	"twitter" => 'http://search.twitter.com/search.atom?q=',
	"twitter_user" => 'http://api.twitter.com/1/statuses/user_timeline.json?screen_name=',
	"tz"	=> 'TZ=/usr/share/zoneinfo/<input>; date',
	"ud"	=> 'http://www.urbandictionary.com/define.php?term=',
	"uptime" => 'Counting seconds.',
	"validate" => 'http://validator.w3.org/check?uri=',
	"vin"  => 'http://4q.cc/index.php?pid=fact&person=vin',
	"vu"	=> 'http://www.kb.cert.org/vuls/id/',
	"week"	=> 'Date::Calc',
	"whois" => 'Net::Whois::Raw',
	"wiki"	=> 'http://www.wikipedia.org/wiki/',
	"wwipind" => "inet_ntop(inet_pton(AF_INET6, input))",
	"wolfram" => "http://www.wolframalpha.com/input/?i=",
	"wotd"	=> 'http://www.merriam-webster.com/word-of-the-day/',
	"woot"	=> 'http://www.woot.com/',
	"wtf"	=> 'ywtf(1)',
	"y"	=> 'http://search.yahoo.com/search?p=',
	"yelp"	=> 'http://mobile.yelp.com/search?find_desc=<what>&find_loc=<where>&find_submit=Search',
	"ysearch" => 'http://search.yahoo.com/search?p=',
	# shortcuts:
	"area"	=>   'http://search.yahoo.com/search?p=area+code+',
	"capital" => 'http://search.yahoo.com/search?p=capital+',
	"convert" => 'http://search.yahoo.com/search?p=convert+',
	"define" => 'http://search.yahoo.com/search?p=define+',
	"quot"	=> '!quote',
	"q52"	=> 'http://finance.yahoo.com/q?s=',
	"ahq"	=> 'http://finance.yahoo.com/q?s=',
	"rq"	=> 'http://finance.yahoo.com/q?s=',
	"quote"	=> 'http://search.yahoo.com/search?p=quote+',
	"schneier" => 'http://geekz.co.uk/schneierfacts/',
	"synonym" => 'http://search.yahoo.com/search?p=synonym+',
	"time"	=> 'http://search.yahoo.com/search?p=time+in+',
	"weather" => 'http://search.yahoo.com/search?p=weather+',
	"zip"	=> 'http://search.yahoo.com/search?p=zip+',
);

our %countdowns = (
	"2012"		=>	mktime(0, 0, 0, 0, 0, 112),
	"dst"		=>	mktime(0, 0, 2, 6, 10, 111),
	"eow"		=>	mktime(0, 0, 0, 21, 11, 112),
	"end of the world"		=>	mktime(0, 0, 0, 21, 11, 112),
	"xmas"		=>	mktime(0, 0, 0, 24, 11, 111),
	"festivus"	=>	mktime(0, 0, 0, 23, 11, 111),
	"y2k38"		=>	mktime(7, 14, 3, 0, 0, 138),
	"y2.038k"	=>	mktime(7, 14, 3, 0, 0, 138),
	"turkey"	=>	mktime(0, 0, 16, 24, 10, 112),
	"d-day"		=>	mktime(0, 0, 0, 6, 5, 112),
	"worldcup"	=>	mktime(0, 0, 0, 13, 5, 114),
	);

our %rssfeeds = (
	"nyt"		=> "New York Times Breaking News, World News & Multimedia",
	"onion"		=> "America's Finest News Source",
	"slashdot"	=> "News for nerds, stuff that matters",
	"uwotd"		=> "Urban Dictionary Word of the Day",
	);

our %rssurl = (
	"nyt"		=> 'http://www.nytimes.com/services/xml/rss/nyt/HomePage.xml',
	"onion"		=> 'http://feeds.theonion.com/theonion/daily',
	"slashdot"	=> "http://rss.slashdot.org/Slashdot/slashdot",
	"uwotd"		=> 'http://feeds.urbandictionary.com/UrbanWordOfTheDay',
	"ybuzz"		=> 'http://buzz.yahoo.com/feeds/buzzoverl.xml',
	"ybuzz_movers"	=> 'http://buzz.yahoo.com/feeds/buzzoverm.xml',
	"ynews"		=> 'http://rss.news.yahoo.com/rss/topstories',
	);

our %h2g2 = (
	"foolproof" => "A common mistake that people make when trying to design something completely foolproof is to underestimate the ingenuity of complete fools.",
	"my ego" => "If there's anything more important than my ego around here, I want it caught and shot now!",
	"universe" => "I always said there was something fundamentally wrong with the universe.",
#	"lunchtime" => "Time is an illusion, lunchtime doubly so.",
#	"bypass" => "What do you mean, why has it got to be built? It's a bypass. Got to build bypasses.",
	"giveaway" => "`Oh dear,' says God, `I hadn't thought of  that,' and promptly vanished in a puff of logic.",
	"don't panic" => "It's the first helpful or intelligible thing anybody's said to me all day.",
	"new yorker" => "The last time anybody made a list of the top hundred character attributes of New Yorkers, common sense snuck in at number 79.",
#	"deadline" => "I love deadlines. I like the whooshing sound they make as they fly by.",
	"potato" => "It is a mistake to think you can solve any major problem just with potatoes.",
	"grapefruit" => "Life... is like a grapefruit. It's orange and squishy, and has a few pips in it, and some folks have half a one for breakfast.",
	"don't remember anything" => "Except most of the good bits were about frogs, I remember that.  You would not believe some of the things about frogs.",
	"ancestor"	=> "There was an accident with a contraceptive and a time machine. Now concentrate!",
	"makes no sense at all"	=> "Reality is frequently inaccurate.",
	"apple products" => "It is very easy to be blinded to the essential uselessness of them by the sense of achievement you get from getting them to work at all.",
	"philosophy" => "Life: quite interesting in parts, but no substitute for the real thing",
	);

#our %calvin = (
#	"braindead" => "It's psychosomatic. You need a lobotomy. I'll get a saw.",
#	"retarded" => "It's psychosomatic. You need a lobotomy. I'll get a saw.",
#	"ascertain" => "Why waste time learning, when ignorance is instantaneous?",
#	"calculate" => "This one's tricky. You have to use imaginary numbers, like eleventeen ...",
#	"cereal" => "YAAH! DEATH TO OATMEAL!",
#	"verbification" => "Verbing weirds language."
#	);
#
our %seinfeld = (
	"human fund" => "A Festivus for the rest of us!",
	"dog shit" => "If you see two life forms, one of them's making a poop, the other one's carrying it for him, who would you assume is in charge?",
	"want soup" => "No soup for you!  Come back, one year!",
	"junior mint" => "It's chocolate, it's peppermint, it's delicious.  It's very refreshing.",
	"rochelle" => "A young girl's strange, erotic journey from Milan to Minsk.",
	"aussie" => "Maybe the Dingo ate your baby!",
	"woody allen" => "These pretzels are making me thirsty!",
	"puke" => "'Puke' - that's a funny word.",
	"mystery wrapped in" => "You're a mystery wrapped in a twinky!",
	"marine biologist" => "You know I always wanted to pretend that I was an architect!",
	"sailor" => "If I was a woman I'd be down on the dock waiting for the fleet to come in.",
	"dentist" => "Okay, so you were violated by two people while you were under the gas. So what? You're single.",
	"sophisticated" => "Well, there's nothing more sophisticated than diddling the maid and then chewing some gum.",
	"sleep with me" => "I'm too tired to even vomit at the thought.",
	"what do you want to eat" => "Feels like an Arby's night.",
	);

our @futurama = (
	"morbo",
	"farnsworth",
	"good news, every(one|body)",
	"Philip J. Fry",
	"Turanga Leela",
	"Bender Bending Rodriguez",
	"Zoidberg",
	"Amy Wong",
	"brain slugs",
	"Hermes Conrad",
	"Zapp Brannigan",
	"Kif Kroker",
	"Barbados Slim",
	"Calculon",
	"Wernstrom",
	"Hypnotoad",
	);

our @megan = (
	"We will all laugh at gilded butterflies.",
	"There once was a little girl who never knew love until a boy broke her HEART.",
	);

our %python = (
	"camelot" => "On second thought, let's not go to Camelot. It is a silly place.",
	"swallow" => "An African or European swallow?",
#	"government"   => "Strange women lying in ponds distributing swords is no basis for a system of government!",
	"Judean People's Front" => "Splitters.",
	"People's Front of Judea" => "Splitters.",
#	"what's wrong" => "I'll tell you what's wrong with it. It's dead, that's what's wrong with it.",
#	"agnostic" => "There's nothing an agnostic can't do if he doesn't know whether he believes in anything or not.",
	"really very funny" => "I don't think there's a punch-line scheduled, is there?",
#	"unexpected" => "Nobody expects the Spanish inquisition!",
	"inquisition" => "Oehpr Fpuarvre rkcrpgf gur Fcnavfu Vadhvfvgvba.",
	"romans" => "What have the Romans ever done for us?",
	"say no more" => "Nudge, nudge, wink, wink. Know what I mean?",
	"cleese" => "And now for something completely different.",
	"Romanes eunt domus" => "'People called Romanes they go the house?'",
	"quod erat" => "'People called Romanes they go the house?'",
	"correct latin" => "Romani ite domum.",
	"proper latin" => "Romani ite domum.",
	"hungarian" => "My hovercraft if full of eels."
	);
#
#our %burns = (
#	"outfit"	=> "Some men hunt for sport; Others hunt for food; But the only thing I'm hunting for Is an outfit that looks good...",
#	"gorilla vest"	=> "Seeeeeeee my vest! See my vest!  Made from real gorilla chest!",
#	"warm sweater"	=> "Feel this sweater, There's no better Than authentic Irish setter.",
#	"vampire"	=> "See this hat, 'twas my cat; My evening wear, vampire bat.",
#	"rhino"		=> "These white slippers are albino african endangered rhino.",
#	"grizzly"	=> "Grizzly bear underwear; Turtles' necks, I've got my share.",
#	"noodle"	=> "Beret of poodle on my noodle It shall rest.",
#	"robin"		=> "Try my red robin suit It comes one breast or two.",
#	"gopher"	=> "Like my loafers? Former gophers.  It was that, or skin my chauffeurs.",
#	"tuxedo"	=> "But a greyhound fur tuxedo would be best.",
#	"clogs"		=> "So lets prepare these dogs; Kill two for matching clogs.",
#	"vest"		=> "I really like the vest.",
#	);
#
our @loveboat = ( "Love, exciting and new... Come aboard.  We're expecting you.",
		"Love, life's sweetest reward.  Let it flow, it floats back to you.",
		"The Love Boat, soon will be making another run.",
		"The Love Boat promises something for everyone.",
		"Set a course for adventure, Your mind on a new romance.",
		"Love won't hurt anymore; It's an open smile on a friendly shore.",
		);

our @ninja = ( "Smash something!",
		"Destroy enemy!",
		"Unleash fury!",
		"Stealth attack!",
		"Annihilate adversary!",
		"Jump over a building!",
		"Silence opponent!",
		"Get really mad!",
		"Hypnotize someone!",
		"Escape on a motorcycle!",
		"Strike quickly!",
		"Turn invisible!" );

our @zen_of_python = (
	"Beautiful is better than ugly.",
	"Explicit is better than implicit.",
	"Simple is better than complex.",
	"Complex is better than complicated.",
	"Flat is better than nested.",
	"Sparse is better than dense.",
	"Readability counts.",
	"Special cases aren't special enough to break the rules.",
	"Although practicality beats purity.",
	"Errors should never pass silently.  Unless explicitly silenced.",
	"In the face of ambiguity, refuse the temptation to guess.",
	"There should be one-- and preferably only one --obvious way to do it.",
	"Although that way may not be obvious at first unless you're Dutch.",
	"Now is better than never.",
	"Although never is often better than *right* now.",
	"If the implementation is hard to explain, it's a bad idea.",
	"If the implementation is easy to explain, it may be a good idea.",
	"Namespaces are one honking great idea -- let's do more of those!",
);

our @zaphod = (
		"Steal the Heart of Gold!",
		"Down a Pangalactic Gargleblaster - then chase it with another one.",
	);

our @viking = ( "Loudly sing 'Spam, lovely Spam, wonderful Spam.'",
	);

our @pirate = ( "Sing A Chantey!",
		"Bury The Booty!",
		"Take No Prisoners!",
		"Yell 'Land Ho'!",
		"Loot and Pillage!",
		"Swab the Deck!",
		"Guzzle Grog!",
		"Plunder a Sloop!",
		"Sail the High Seas!",
		"Keelhaul a Scurvy Dog!",
		"Raise the Jolly Roger!",
		"Maroon a Scallywag!" );

our @jans = (
		"Say: 'File a bug!'.",
		"Go surfing instead.",
		"Write a man page.",
		"Go skateboarding instead.",
		"feed::dead:beaf",
		"Drink beer.",
		"Go snowboarding instead.",
		"Waste his time writing an IRC bot.",
		"Say stuff like 'well, given our resources... then, no?'.",
		"rather not use perl.",
		"not use powerpoint, word or excel.",
		"Tell ver to do the work.",
		"Fork.",
		"Go back to New York.",
	);

our @ayanich = (
		"Go outside to throw up!",
		"Play with his balls for at least 15 minutes!",
		"Fix your subnets.",
		"Tip off DHS and get you deported.",
		"Kill the conversation.",
		"Dump Y! and go to Netflix, then dump Netflix.",
	);

our @ahorn= (
		"Write it in his boook.",
		"Point you to perlmonks.org.",
		"Help you toast.",
		"Invoke Horn's Law.",
		"Be smug in that british kind of way.",
	);

our @dhawth = (
		"Unleash perl!",
		"Get your nocers in a twist.",
		"Steal Christmas.",
		"Disappear a bit due to a gravity shift.",
	);

our @kevink = (
		"Move to Portland (or somewhere up there).",
		"Have a shitty strike price.",
		"Rather have high-density light rays shot in his eyes.",
	);

our @jfesler = (
		"do it all.",
		"tell you to get moving on IPv6.",
		"Get your nocers in a twist.",
		"feed::dead:beaf",
	);

our @kimo = (
		"Tell you to use PHP.",
		"Name an obscure porn star.",
		"Fork.",
		"Grow pointy hair.",
		"Disappear a bit (due) to a gravity shift.",
	);

our @pettit = (
		"Play WOW for an hour.",
		"Practice Guitar Hero for an hour.",
		"Fix your regex.",
		"Sing a song - a capella.",
		"Waste his time writing an IRC bot.",
		"Really confuse the bots.",
		"Frob the DNS.",
		"Prefer Roundtable.",
		"Have a comeback.",
		"Touch dog's in the middle of the night.",
	);

our @rayriver = (
		"Manage your traffic.",
		"Not sleep 'till Brooklyn.",
	);

our @farisa = (
		"Dump the NOC.",
		"Manage your traffic.",
	);

our @johneagl = (
		"Eat a puppy.",
		"Contribute with a Perl of wisdom.",
		"Dump the NOC.",
		"Make other people dump the NOC, too.",
		"Try to exploit the bots.",
		"Manage your traffic.",
	);

our @leesay = (
		"Hang out with the cool kids.",
		"Appreciate $botnick.",
		"Smell her armpits.",
		"Smell her puppy's farts.",
		"Like to let the humping begin.",
		"Munch on her peach.",
		"Eye her pink lady.",
		"Dump Y! and go to Ning.",
	);

our @obot = (
		"Not do all that much.",
		"Come and go every now and then.",
	);

our @pbot = (
		"Obsess about WoW.  And motorcycles.",
		"Tell you to stfu!",
		"Let you put words into other people's mouth.",
		"Waste pettit's time.",
		"Have a comeback.",
	);

our @jmturner = (
		"ROFL.",
		"Close a ticket for you.",
		"Like to move the Isotopes to Albuquerque.",
		"Rock out to Sade.",
		"Get angry when bill collectors call Sylvia.",
		"Grow pointy hair.",
	);

our @mikek = (
		"Obsess about gerbils in all sorts of inappropriate ways.",
	);

our @moof = (
		"Frob the DNS 'n stuff.",
		"Get better sushi.",
		"Become a real G.",
	);

our @like_dislike = (
		"loves ==%==!",
		"wants to marry ==%== and have, like, a million babies with ==%==.",
		"thinks ==%== are the most awesomest things ever.",
		"says \"==%==?  I'd rather stick needles in my eyes.\"",
		"hates ==%== more than anything.",
		"doesn't care at all about ==%==.",
		"is pretty indifferent about ==%==.",
		"loooooathes ==%==.",
		"pretends to not like ==%==, but secretly digs ==%== quite a bit.",
		"likes ==%==.  No, really.",
	);

our @blysik = (
		"Ask you to write your status report.",
		"Call your bluff!",
		"Use Bruce Force!",
		"Also name an obscure porn star.",
		"Fork.",
		"Fork again.",
		"Grow pointy hair.",
		"Rock out to Sade.",
	);

our @jbot = (
		"Masterplan?  What masterplan?  There is no masterplan.  Who said that?",
		"Waste everyone's time.",
		"Become self-aware, eventually.",
		"Interrupt your IRC chat.",
	);

our @penick = (
		"Request silly features for $botnick.",
		"Kindly tell the requestor to stuff it.",
		"Burn all hippies.",
		"Try to arouse his turkey.",
	);

our @bhaga = (
		"Become the new timelord.",
	);

our @east = (
		"SMASH!",
		"Attempt to trigger a response from $botnick... and fail.",
		"Certainly not eat at URLs.",
		"Fork.",
	);

our @ver = (
		"Battle $botnick.",
		"Chain the bots.",
		"Rather not use perl.",
		"Waste his time writing an IRC bot.",
		"Ditch Y! to become a tweety.",
	);

our %what_would = (
		"ahorn" => \@ahorn,
		"alan" => \@ahorn,
		"asher" => \@ayanich,
		"ayanich" => \@ayanich,
		"bhaga" => \@bhaga,
		"blysik" => \@blysik,
		"bruce" => \@blysik,
		"dhawth" => \@dhawth,
		"east" => \@east,
		"farisa" => \@farisa,
		"jans" => \@jans,
		"jfesler" => \@jfesler,
		"jmturner" => \@jmturner,
		"mikey" => \@jmturner,
		"mokey" => \@jmturner,
		"jbot" => \@jbot,
		"johneagl" => \@johneagl,
		"je" => \@johneagl,
		"kevink" => \@kevink,
		"kimo" => \@kimo,
		"leesay" => \@leesay,
		"mikek" => \@mikek,
		"mkugler" => \@mikek,
		"moof" => \@moof,
		"obot" => \@obot,
		"pbot" => \@pbot,
		"pettit" => \@pettit,
		"penick" => \@penick,
		"rayriver" => \@rayriver,
		"ver" => \@ver,
		"oliver" => \@ver,
		"viking" => \@viking,
		"zaphod" => \@zaphod,
	);


our @base_belong = (
		"In A.D. 2101, war was beginning.",
		"What happen?",
		"Somebody set up us the bomb.",
		"We get signal.",
		"What!",
		"Main screen turn on.",
		"It's you!",
		"How are you gentlemen!",
		"All your base are belong to us.",
		"You are on the way to destruction.",
		"What you say!",
		"You have no chance to survive make your time.",
		"Captain!",
		"Take off every 'ZIG'!",
		"You know what you doing.",
		"Move 'ZIG'.",
		"For great justice."
	);

our @happy_actions = (
		"giddily hops up and down.",
		"struts his stuff.",
		"proudly smiles.",
		"nods approvingly.",
	);

our @insults = (
		"dammn you",
		"damn you",
		"damn u",
		"dammn u",
		"damm u",
		"shuttup",
		"shut up",
		"die",
		"screw u",
		"screw you",
		"cram it",
		"pissoff",
		"piss off",
		"fuck (off|you|u)",
		"hate you",
		"stupid",
		"you stink",
		"you blow",
		"bite me",
		"(yo)?u suck",
		"useless",
		"sucks",
		"shush",
		"yo?u'?re? dum",
		"is lame",
		"blows",
		"stuff it",
		"go to hell",
		"idiot",
		"stfu",
		"(you are|is) annoying",
		"blame $botnick",
		"${botnick}'s fault",
		"${botnick}s fault",
	);

our @dontknow = (
		"How the hell am I supposed to know that?",
		"FIIK",
		"ENOCLUE",
		"Buh?",
		"I have no idea.",
		"Sorry, I wouldn't know about that.",
		"I wouldn't tell you even if I knew.",
	);

# the throttle hash is used to make sure folks can't abuse the
# on_public responses, it gets populated with userhost, command,
# and time last run

our %throttle;

our %parameters = (
	Nick => $botnick,
	Username => $botnick,
	Ircname  => 'Jan\'s bot',
	Server   => $ircserver,
);

our $irc = POE::Component::IRC->spawn(%parameters);

# Create the bot session.  The new() call specifies the events the bot
# knows about and the functions that will handle those events.

POE::Session->create(
    inline_states => {
        irc_disconnected  => \&bot_reconnect,
        irc_error         => \&bot_reconnect,
        irc_socketerr     => \&bot_reconnect,
        connect           => \&bot_connect,
	irc_ctcp_action   => \&on_me_action,
        _start            => \&bot_start,
        autoping          => \&bot_do_autoping,
        irc_001           => \&bot_connected,
        irc_public        => \&on_public,
        irc_msg           => \&on_msg,
	irc_invite        => \&on_invite,
        irc_join          => \&on_join,
	irc_part	  => \&on_part,
	irc_kick	  => \&on_kick,
	irc_quit	  => \&on_quit,
	irc_353		  => \&irc_names,
	irc_nick          => \&on_nick,
	irc_whois	  => \&on_whois,
    },
		heap => { irc => $irc },
);

####################################################################
##
## Handler functions used by POE::Session object
##
####################################################################

# function : bot_start
# purpose  : called when the bot starts

sub bot_start {
	my $heap    = $_[HEAP];
	my $session = $_[SESSION];
	$_kernel  = $_[KERNEL];

	# The bot session has started.  Register this bot with the "magnet"
	# IRC component.  Select a nickname.  Connect to a server.
	# Run the bot until it is done.
	$irc->yield( register => "all" );
	$irc->yield("connect");
}

# function : bot_connect
# purpose  : called to connect the bot to an IRC server

sub bot_connect {
	my ( $kernel, $heap ) = @_[ KERNEL, HEAP ];
	my $irc_session = $heap->{irc}->session_id();
	# Create the component that will represent an IRC network.
	$kernel->post( $irc_session => connect => \%parameters );
}

# function : bot_connected
# purpose  : called immediately after bot has connected (joins channel etc.)

sub bot_connected {
	my ( $kernel, $heap, $sender ) = @_[ KERNEL, HEAP, SENDER ];

	foreach my $channel (keys(%CHANNELS)) {
		$channel = lc($channel);
		if ($channel !~ /^#/) {
			delete($CHANNELS{$channel});
			next;
		}
print STDERR "Joining |$channel|...\n";
		my $key = $CHANNELS{$channel}{"key"};
		if ($key) {
			$kernel->post( $sender => join => $channel, $key );
		} else {
			$kernel->post( $sender => join => $channel );
		}
	}

	$heap->{seen_traffic} = 1;
	$kernel->delay( autoping => $autoping);
}

# function : bot_do_autoping
# purpose  : if theres been no traffic since the last ping (300s ago)
#            then ping ourselves; run some other stuff periodically

sub bot_do_autoping {
	my ( $kernel, $heap ) = @_[ KERNEL, HEAP ];

	if (!$heap->{seen_traffic}) {
		$kernel->post( poco_irc => userhost => "my-nickname" );
	}
	$heap->{seen_traffic} = 0;
	$kernel->delay( autoping => $autoping );

	do_periodics();

}

# function : bot_reconnect
# purpose  : Reconnect in 60 seconds.  Don't ping while we're disconnected.
#            It's # important to wait between connection attempts or the
#            server may detect "abuse".  In that case, you may be prohibited
#            from connecting at all.

sub bot_reconnect {
	my $kernel = $_[KERNEL];
	$kernel->delay( autoping => undef );
	$kernel->delay( connect  => 60 );
}

# purpose : determine if a string is an IP address
# inputs  : a string
# returns : true if it's a valid IP address (either IPv4 or IPv6), false
#           otherwise

sub is_ip($) {
	my ($ip) = @_;

	if (inet_pton(AF_INET, $ip)) {
		return 1;
	} elsif (inet_pton(AF_INET6, $ip)) {
		return 1;
	}

	return 0;
}

# function : is_throttled
# purpose  : checks (and sets) throttle for the given command per userhost
# inputs   : command, userhost, optional 'all', optional timeout
# results  : returns 0 if the command is throttled for the given user
#            returns 1 if the command is not throttled, then sets the
#            throttle

sub is_throttled($$;$$) {
	my ($cmd, $userhost, $all, $timeout) = @_;
	my $now_time = time();

	my $max = $timeout ? $timeout : 600;

	my @throttlees;

	if ($all) {
		push(@throttlees, ($userhost, "all"));
	} else {
		push(@throttlees, $userhost);
	}

	foreach my $t (@throttlees) {
		if (!defined($throttle{$t}->{$cmd})) {
			$throttle{$t}->{$cmd} = $now_time;
		} else {
			my $diff = $now_time - $throttle{$t}->{$cmd};
			if (($diff < $max) && ($diff >= 0)) {
				$throttle{$t}{$cmd} = $now_time;
				return 1;
			}
		}
	}

	return 0;
}

# function : dehtmlify
# purpose  : strip html and return plain text
# input    : string

sub dehtmlify($) {
	my ($line) = @_;

	$line =~ s/<.+?>//g;
#	$line =~ s/\&.+?;//g;
	$line =~ s/^\s+//g;

	return decode_entities($line);
}

# function : unescape
# purpose  : turn %20%3D etc into normal characters
# inputs   : none
# returns  : unescaped text
# from     : http://www.1-script.com/forums/Escape-in-perl-article102313--6.htm

sub unescape($) {
	my ($text) = @_;
	$text =~ s/%(..)/pack("c",hex($1))/ge;
	return ($text);
}

# function : eggplus
# purpose  : increment command count for given user
# inputs   : username

sub eggplus($) {
	my ($nick) = @_;
	if (!$cmdrs{$nick}) {
		$cmdrs{$nick} = 0;
	}
	$cmdrs{$nick} = $cmdrs{$nick} + 1;
}

# function : do_stfu
# purpose  : Reports on who is a chatterer
# inputs   : who to respond to, which chatterer (-1 for all), which
#            channel we're interested in
# returns  : 0 on success, >0 otherwise

sub do_stfu($$$) {
	my ($who, $count_or_who, $channel) = @_;
	my $i = 0;
	my ($count, $chatterer, $do_who);

	if ($count_or_who =~ /^-?\d*$/) {
		$count = $count_or_who;
		$count = $totcount{$channel}{"TOTAL"} if (! defined $count || $count < 0);
		$count_or_who = ($count > 0) ? $count : 3;
		$do_who = 0;
	} else {
		$chatterer = $count_or_who;
		$do_who = 1;
	}

	if ($do_who) {
		my $n = $totcount{$channel}{$chatterer} ? $totcount{$channel}{$chatterer} : 0;
		emit($irc, $who, "$chatterer ($n)");
	} else {
		my $outstr = "";
		foreach my $nick (sort { $totcount{$channel}{$b} <=> $totcount{$channel}{$a} } keys %{ $totcount{$channel} }) {
			next if ($nick =~ /(TOTAL|$botnick)/);
			last if ++$i > $count_or_who;
			$outstr .= "$nick ($totcount{$channel}{$nick}) ";
		}

		if ($outstr) {
			$outstr .= ".  STFU already.";
		} else {
			$outstr = "All quiet in $channel.";
		}
		emit ($irc, $who, $outstr);
	}

}

# function : emit
# purpose  : generate message and increment counters

sub emit($$$) {
	my ($irc, $channel, $message) = @_;
	if ($channel =~ m/^#/) {
		$totcount{$channel}{"TOTAL"}++;
		$totcount{$channel}{$botnick}++;
		$CHANNELS{$channel}{"seen"}{$botnick} = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());
	}
	$irc->yield( 'privmsg' => $channel => $message );
}

# function : on_me_action
# purpose  : respond to /me actions

sub on_me_action {
	my ( $kernel, $who, $where, $msg ) = @_[ KERNEL, ARG0, ARG1, ARG2 ];
	my ($nick,$userhost) = split(/!/, $who );
	my $channel = $where->[0];

	$channel =~ s/^.*#/#/;
	$channel = lc($channel);
	$CHANNELS{$channel}{"seen"}{$nick} = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());
}

###
### CHATTER
###

# function : on_public
# purpose  : The bot has received a public message.  Parse it for commands,
#            and respond to interesting things.

sub on_public {
	my ( $kernel, $who, $where, $msg ) = @_[ KERNEL, ARG0, ARG1, ARG2 ];
	my ($nick,$userhost) = split(/!/, $who );
	my $channel = $where->[0];
	my $ts = scalar localtime;

	$channel = lc($channel);
	$CHANNELS{$channel}{"seen"}{$nick} = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());

	$totcount{$channel}{$nick}++;
	$totcount{$channel}{"TOTAL"}++;

	if ($msg =~ m/(sh[ia]t|motherfucker|piss|f+u+c+k+|cunt|cocksucker|tits)/i) {
		my @words = split(/\s+/, $msg);

		if (!$CHANNELS{$channel}{"toggles"}{"nc17"}) {
			$irc->yield( 'ctcp' => $channel => "ACTION washes $nick\'s mouth out with soap.");
		}

		if (!$pottymouths{$nick}) {
			$pottymouths{$nick} = 0;
		}

		foreach my $w (@words) {
			if ($w =~ m/(.*(:?sh[ia]t|motherfucker|piss|f+u+c+k+|cunt|cocksucker|tits).*)/) {
				my $curse = $1;
				$curse =~ s/^\s*//g;
				$curse =~ s/\s*$//g;
				$curse =~ s/[,\.;!\?].*//g;
				$curse =~ s/^['"]+//g;
				$curse =~ s/['"]+$//g;
				$pottymouths{$nick} = $pottymouths{$nick} + 1;
				if (!$curses{$curse}) {
					$curses{$curse} = 1;
				}
				$curses{$curse} = $curses{$curse} + 1;
			}
		}
		if ($pottymouths{$nick} == 0) {
			$pottymouths{$nick} = 1;
		}
	}

	if ($msg =~ m/^(https?:\/\/\S+)$/) {
		if (($CHANNELS{$channel}{"toggles"}{"title"}) && ($nick !~ m/^(([op]|dns|tiny)bot|logger|ybiip)$/))  {
			emit($irc, $channel, do_title($1));
		}
	}

	if ($msg =~ m/\b((?:sh|[cw])ould) of\b/) {
		if ($CHANNELS{$channel}{"toggles"}{"chatter"}) {
			emit($irc, $channel, "s/$1 of/$1 have/; $nick");
		}
	}

	if ($msg =~ m/^what('| i)s the word\?$/i) {
		if ($CHANNELS{$channel}{"toggles"}{"word"}) {
			emit($irc, $channel, "\"the bird\"");
		}
	}

	# commands sent to the bot
	if ($msg =~ /^(<[^>]+> )?\!(.+)$/)  {
		my $cmd = $2 ? $2 : $1;
		my $lamo = $nick;
		my $fake = $2 ? $1 : $nick;

		on_command($cmd, $channel, $nick, $userhost, $channel);
		return;
	}

	if ($msg =~ /^(hold|hang)\s*on/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) &&
			($CHANNELS{$channel}{"toggles"}{"holdon"}) &&
			!is_throttled('youholdon', 'all')) {
			eggplus($nick);
			if ( $nick =~ m/afk$/i ) {
				emit($irc, $channel, "No *YOU*... Wait a minute, ${nick}.  Aren't you AFK?" );
			} else {
				emit($irc, $channel, "No *YOU* $1 on, ${nick}!" );
			}
		}
	}

	elsif ($msg =~ /(trivia|factlet)/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) &&
			($CHANNELS{$channel}{"toggles"}{"trivia"}) &&
			(!is_throttled('trivia', $userhost))) {
			eggplus($nick);
			do_trivia($channel);
		}
	}

	elsif ($msg =~ /^I R /i) {
		if ($CHANNELS{$channel}{"toggles"}{"chatter"}) {
			my @cac = ( "I am Weasel!", "I R Baboon!" );
			eggplus($nick);
			emit($irc, $channel, $cac[int(rand(scalar(@cac)))]);
		}
	}

#	elsif ($msg =~ /(norris|chuck)/i) {
#		if (!is_throttled('chuck', 'all')) {
#			do_fact($channel, "chuck");
#		}
#	}
#	elsif ($msg =~ /(mrt|mr. t|a-team)/i) {
#		if (!is_throttled('mrt', 'all')) {
#			do_fact($channel, "mrt");
#		}
#	}
#	elsif ($msg =~ /(vin |diesel)/i) {
#		if (!is_throttled('vin', 'all')) {
#			do_fact($channel, "vin");
#		}
#	}

	elsif ($msg =~ /(schneier|crypt|blowfish)/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) &&
			($CHANNELS{$channel}{"toggles"}{"schneier"}) &&
			(!is_throttled('schneier', $userhost, 'all'))) {
			eggplus($nick);
			do_fact($channel, "schneier");
		}
	}

#	elsif ($msg =~ m/^(foo|bar|baz)[,\?!\. ]?$/) {
#		if (!is_throttled('foo', $userhost)) {
#			eggplus($nick);
#			do_foo($channel);
#		}
#	}

#	elsif ($msg =~ m/(insult me|call me names)/i) {
#		if (!is_throttled('insult', $userhost)) {
#			eggplus($nick);
#			do_quip($channel);
#		}
#	}

#	elsif (($msg =~ /george (w\.?)?( )?bush/i) || ($msg =~ m/(turd sandwich|dubya|giant douche|dimwit|bushism|worst president ever|President of the United States)/i)){
#		if (!is_throttled('bush', $userhost)) {
#			eggplus($nick);
#			do_bush($channel);
#		}
#	}

	elsif ($msg =~ m/(federline|britney|spears)/i) {
		if ($CHANNELS{$channel}{"toggles"}{"chatter"}) {
			eggplus($nick);
			emit($irc, $channel, "Kevin Federline, yeah mmm hmm." );
		}
	}

	elsif ($msg =~ m/hot in here/i) {
		if ($CHANNELS{$channel}{"toggles"}{"chatter"}) {
			eggplus($nick);
			emit($irc,$channel, "So take off all your clothes." );
		}
	}

	elsif ($msg =~ m/(https?:\/\/[^ ]+)/) {
		my $url = $1;
		if (($CHANNELS{$channel}{"toggles"}{"tiny"}) &&
			(length($url) > 65) &&
			($nick !~ m/bot$/)) {
			eggplus($nick);
			emit($irc, $channel, do_tiny($url));
		}
	}

	elsif ($msg =~ /botwar/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) &&
			!is_throttled('botwar', $userhost)) {
			eggplus($nick);
			if (rand(100) > 50) {
				emit($irc, $channel, "BOTWAR!!" );
			} else {
				emit($irc, $nick, "You sayin' you wanna piece of me?!!" );
			}
		}
	}

#	elsif ($msg =~ m/there is no spoon/i) {
#		eggplus($nick);
#		emit($irc, $nick, "Yes, there is.");
#	}

#	elsif ($msg =~ /(seeya|Auf Wiedersehen|adios|ciao)([ ,])*([a-z0-9]+)*/i) {
#		if (!is_throttled('byebye', $userhost)) {
#			eggplus($nick);
#			my $return = "Bye bye";
#			if (($3) && ($3 !~ m/$botnick/)) {
#				$return .= ", $3";
#				emit($irc, $channel, "$return!" );
#			} else {
#				$msg .= ", $nick";
#				emit($irc, $nick, "$return!" );
#			}
#		}
#	}

	elsif ($msg =~ /(monkey|orangutan|gorilla|macaque|chimp|\bape\blemur|simian|primate)/i) {
		my @monkeys = ( "bababooey bababooey bababooey",
				"Fafa Fooey",
				"Mama Monkey",
				"Fla Fla Flo Fly" );
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) &&
			($CHANNELS{$channel}{"toggles"}{"monkey"})) {
			eggplus($nick);
			emit($irc, $channel, $monkeys[int(rand(scalar(@monkeys)))]);
		}
	}

#	elsif ($msg =~ /^lunch\?$/i) {
#		if (!is_throttled('lunch', $userhost)) {
#			eggplus($nick);
#			my (undef, undef, $hour, undef) = localtime();
#
#			if ($hour < 12) {
#				emit($irc, $channel, "Nonsense - it's not even noon yet!" );
#			}
#		}
#	}

	elsif ($msg =~ /omf?g/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) &&
			($CHANNELS{$channel}{"toggles"}{"omg"}) &&
			(!is_throttled('omg', $userhost))) {
			eggplus($nick);
			do_omg($channel);
		}
	}

	elsif ($msg =~ /(blame ?jans?|jans?'s fault)/) {
		eggplus($nick);
		do_quip($channel, $nick);
	}

#	elsif ($msg =~ /^(<.*> )?shoot me[.!]*$/i) {
#		eggplus($nick);
#		my $victim = $1 ? $1 : $nick;
#		emit($irc, $channel, "No problem, can do.");
#		$irc->yield( 'ctcp' => $channel => "ACTION shoots $victim in the head.");
#	}

	elsif ($msg =~ /(tang|wu-|shaolin)/i) {
		if ($CHANNELS{$channel}{"toggles"}{"chatter"}) {
			my @wutang = ("Ghostface Killah (born Dennis Coles, 1970)",
					"GZA (born Gary Grice, 1966)",
					"Inspectah Deck (born Jason Hunter, 1970)",
					"Masta Killa (born Elgin Turner, 1969)",
					"Method Man (born Clifford Smith, 1971)",
					"Ol' Dirty Bastard (born Russell Jones, 1968 - 2004)",
					"Raekwon The Chef (born Corey Woods, 1970)",
					"RZA (born Robert Diggs, 1969)",
					"U-God (born Lamont Hawkins, 1970)",
					"Enter the Wu-Tang (36 Chambers) (1993)",
					"Wu-Tang Forever (1997)",
					"Iron Flag (2001)",
					"8 Diagrams (2007)",
					"Do you think your Wu-Tang sword can defeat me?",
					"Unguard, I'll let you try my Wu-Tang style.",
					"It's our secret. Never teach the Wu-Tang!",
					"How dare you rebel the Wu-Tang Clan against me.",
					"We have only 35 Chambers. There is no 36.", );

			if (!is_throttled('tang', $userhost)) {
				eggplus($nick);
				if ($msg =~ /poontang/i) {
					$irc->yield( 'ctcp' => $channel => "ACTION blushes");
				} else {
					if (($msg !~ /wu-tang/i) && ($nick !~ /pbot/)) {
						if ($msg =~ /^tang/i) {
							emit($irc, $channel, "s/^/wu-/; $nick");
						} elsif ($msg =~ /tang/i) {
							emit($irc, $channel, "s ntanwu-tang; $nick");
						}
					} else {
						my $which = $wutang[int(rand(scalar(@wutang)))];
						emit($irc, $channel, $which);
					}
				}
			}
		}
	}

	elsif ($msg =~ /^sleep$/i) {
		if ($CHANNELS{$channel}{"toggles"}{"chatter"}) {
			eggplus($nick);
			emit($irc, $channel, "To sleep, perchance to dream.");
			emit($irc, $channel, "Ay, theres the rub.");
			emit($irc, $channel, "For in that sleep of death what dreams may come...");
		}
	}

	elsif ($msg =~ m/love ?boat/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled('loveboat', $userhost, 'all')) {
			eggplus($nick);
			emit($irc, $channel, $loveboat[int(rand(scalar(@loveboat)))] );
		}
	}

	elsif ($msg =~ m/(shakespear|hamlet|macbeth|romeo and juliet|merchant of venice|midsummer nicht's dream|henry V|as you like it|All's Well That Ends Well|Comedy of Errors|Cymbeline|Love's Labours Lost|Measure for Measure|Merry Wives of Windsor|Much Ado About Nothing|Pericles|Prince of Tyre|Taming of the Shrew|Tempest|Troilus|Cressida|Twelfth Night|two gentleman of verona|Winter's tale|henry IV|king john|richard II|antony and cleopatra|coriolanus|julius caesar|kind lear|othello|timon of athens|titus|andronicus)/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled('shakespear', $userhost)) {
			eggplus($nick);
			do_shakespear($channel);
		}
	}
#	elsif ($msg =~ m/why (was|did)n't that/i) {
#		eggplus($nick);
#		emit($irc, $channel, "'Cause you suck, that's why!");
#	}

#	elsif ($msg =~ m/(\bboa\b|rattle snake|serpent|viper|cobra)/i) {
#		if (!is_throttled('python', $userhost, 'all')) {
#			eggplus($nick);
#			my @snakes = keys(%python);
#			emit($irc, $channel, $python{$snakes[int(rand(scalar(@snakes)))]} );
#		}
#	}

	elsif ($msg =~ m/(\b42\b|arthur dent|slartibartfast|zaphod|beeblebrox|ford prefect|hoopy|trillian)/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled('h2g2', $userhost, 'all')) {
			eggplus($nick);
			my @hitch = keys(%h2g2);
			emit($irc, $channel, $h2g2{$hitch[int(rand(scalar(@hitch)))]} );
		}
	}

	elsif ($msg =~ m/(zen of python|TMTOWTDI)/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled('zen', $userhost, 'all')) {
			eggplus($nick);
			emit($irc, $channel, $zen_of_python[int(rand(scalar(@zen_of_python)))] );
		}
	}



	elsif ($msg =~ m/quoth the raven/i) {
		if (!is_throttled('quoth', $userhost, 'all')) {
			eggplus($nick);
			emit($irc, $channel, "Nevermore.");
		}
	}

	elsif ($msg =~ m/(wwnd|ninja|assassination|on'yomi|oniwaban|shinobi)/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled('ninja', $userhost, 'all')) {
			eggplus($nick);
			emit($irc, $channel, "A ninja would... " . $ninja[int(rand(scalar(@ninja)))] );
		}
	}

	elsif ($msg =~ m/(wwpd|pirate|ahoy|arrr|yarr|lagoon)/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled('pirate', $userhost, 'all')) {
			eggplus($nick);
			emit($irc, $channel, "A pirate would... " . $pirate[int(rand(scalar(@pirate)))] );
		}
	}

	elsif ($msg =~ m/(wwvd|viking)/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled('viking', $userhost, 'all')) {
			eggplus($nick);
			emit($irc, $channel, "A viking would... " . $viking[int(rand(scalar(@viking)))] );
		}
	}

#	elsif ($msg =~ m/^(I )?don't know([.!])?$/i) {
#		eggplus($nick);
#		emit($irc, $channel, "ENOCLUE");
#	}

#	elsif ($msg =~ m/^(I have )?no idea([.!])?$/i) {
##		eggplus($nick);
#		emit($irc, $channel, "ENOFC");
#	}

#	elsif ($msg =~ m/^(no|I don't have) time([.!]|, sorry)?$/i) {
#		eggplus($nick);
#		emit($irc, $channel, "ENOTIME");
#	}

#	elsif ($msg =~ m/stupid user/i) {
#		eggplus($nick);
#		emit($irc, $channel, "EPEBKAC");
#	}

	elsif ($msg =~ m/\b(panties|tied up|underwear|naked|thong|lindsay lohan|unzip|muscle|cowgirl|bikini|paris hilton|strip|underpants|hooker|whore)\b/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) &&
			($CHANNELS{$channel}{"toggles"}{"hotness"}) &&
			(!is_throttled('hotness', $userhost, "all"))) {
			eggplus($nick);
			emit($irc, $channel, "That's hot.");
		}
	}

	elsif ($msg =~ m/<\s*$botnick>:? that's hot/i) {
		if (!is_throttled('hotness', $userhost, "all")) {
			eggplus($nick);
			my @ans = ( "No, it's not.",
					"Eeew.",
					"Damn right it is." );
			emit($irc, $channel, $ans[int(rand(scalar(@ans)))] );
		}
	}

	elsif ($msg =~ m/what (?:else )?would (\S+) do/i || $msg =~ m/(wwjd|whom? would your ?mom do)/i) {
		my (@user_quotes, $who);

		$who = $1;

		if ($who =~ m/^i$/i) {
			$who = $nick;
			$who =~ s/_afk$//;
		}

		if ($who =~ m/your ?mom/) {
			my @users = keys(%{$CHANNELS{$channel}{"users"}});
			emit($irc, $channel, $users[int(rand(scalar(@users)))] );
			return;
		}

		if ($who =~ m/^(jans?|wwjd)$/i) {
			@user_quotes = @jans;
			$who = "Jan";
		} elsif (defined($what_would{$who})) {
			@user_quotes = @{$what_would{$who}};
		}

		if (!is_throttled($who, $userhost) || m/what else/) {
			eggplus($nick);
			if (!scalar(@user_quotes)) {
				emit($irc, $channel, $dontknow[int(rand(scalar(@dontknow)))]);
				return;
			}

			emit($irc, $channel, "$who would... " . $user_quotes[int(rand(scalar(@user_quotes)))] );
		}
	}

	elsif ($msg =~ /(ur([ _])mom|yourmom|m[oa]mma|[^ ]+'s mom)/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) &&
			$CHANNELS{$channel}{"toggles"}{"yourmom"} &&
			!is_throttled('yourmom', $userhost, 'all', 1200)) {
			eggplus($nick);
			do_mom($channel, $nick, $1);
		}
	}



#	elsif ($msg =~ m/jebus/i) {
#		if (!is_throttled("jebus", $userhost)) {
#			eggplus($nick);
#			emit($irc, $channel, "It's supposed to be 'Jesus', isn't it?  I'm pretty sure it is.");
#		}
#	}

#	elsif ($msg =~ m/math/i) {
#		if (!is_throttled("math", $userhost)) {
#			emit($irc, $channel, "Math is hard.  Let's go shopping!");
#		}
#	}

	elsif ($msg =~ m/holl(er|a) ?back/i || $msg =~ m/(b-?a-?n-?a-?n-?a-?s|this my shit)/i) {
		my @bananas = (
			"Ooooh ooh, this my shit, this my shit.",
			"$nick ain't no hollaback girl.",
			"Let me hear you say this shit is bananas.",
			"B-A-N-A-N-A-S",
			);
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled("hollaback", $userhost)) {
			eggplus($nick);
			emit($irc, $channel, $bananas[int(rand(scalar(@bananas)))]);
		}
	}

	elsif ($msg =~ m/my milkshake/i) {
		my @milkshake = (
			"...brings all the boys to the yard.",
			"The boys are waiting.",
			"Damn right it's better than yours.",
			"I can teach you, but I have to charge.",
			"Warm it up.",
			);
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled("milkshake", $userhost)) {
			eggplus($nick);
			emit($irc, $channel, $milkshake[int(rand(scalar(@milkshake)))]);
		}
	}

	elsif ($msg =~ m/horn'?s law/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled("horns_law", $userhost)) {
			emit($irc, $channel, "People are stupid.");
		}
	}

#	elsif( $msg =~ m/kool-?aid/i) {
#		if (!is_throttled("koolaid", $userhost)) {
#			emit($irc, $channel, "Oh, yeah!  Dig it!.");
#		}
#	}

	elsif ($msg =~ /$botnick/i) {
		if ($msg =~ m/^${botnick}[,:]*\s*(please\s+)?leave(,?\s*please)?$/) {
			if ($msg !~ m/please/) {
				emit($irc, $channel, "Say \"$botnick, please leave\".");
				return;
			} else {
				emit($irc, $where, "Alrighty.  Have a nice day.");
				$irc->yield( 'part' => $channel);
print STDERR "Leaving $channel cause I was asked to.\n";
				delete($CHANNELS{$channel});
				return;
			}
		}

		if ($msg =~ /^s(.); $botnick/) {
			# let's just claim it's a s///
			return;
		}

		if ($msg =~ /who invited you/i) {
			my $inviter = $CHANNELS{$channel}{"inviter"};
			my $msg = "I got here all by myself.";
			emit($irc, $channel, $inviter ? $inviter : $msg);
			return;
		}

		if ($msg =~ /(thx|thanks?|danke|mahalo|gracias|merci)/i) {
			eggplus($nick);
			do_thanks($channel, $nick);
			return;
		}

		if ($msg =~ /(buen dia|bon ?(jour|soir)|welcome|hi,|hey|hello|good (morning|afternoon|evening)|howdy|aloha|guten (tag|morgen|abend))/i) {
			eggplus($nick);
			emit($irc, $channel, "Why, $1 to you, too, $nick!");
			return;
		}

		foreach my $insult (@insults) {
			if ($msg =~ /$insult/i) {
				if ($userhost =~ m/^$botowner\@($botownerhost|127.0.0.1)$/) {
					if ($msg eq "please die, $botnick") {
						emit($irc, $channel, "Yes, master!" );
						emit($irc, $channel, "Banzaii!" );
						$kernel->signal( $kernel, 'POCOIRC_SHUTDOWN', "*sniff*" );
						sleep(1);
						exit;
						# NOTREACHED
					}
				} else {
					eggplus($nick);
					do_quip($channel, $nick);
					return;
				}
			}
		}

		if ($msg =~ /(good |bravo|well done|you rock|good job|nice|i love( you)?)/i) {
			eggplus($nick);
			my $act = $happy_actions[int(rand(scalar(@happy_actions)))];
			$irc->yield( 'ctcp' => $channel => "ACTION $act");
			return;
		}


		if ($msg =~ /who.*on([ -])?call/) {
			eggplus($nick);
			do_oncall($channel);
			return;
		}

		if ($msg =~ m/what's new(, )?/i) {
			eggplus($nick);
			foreach my $new (@whats_new) {
				emit($irc, $channel, $new);
			}
			emit($irc, $channel, "That's what's new.");
			return;
		}

		if ($msg =~ m/insult (.+)/i) {
			my $whom = $1;
			if ($whom eq "me") {
				$whom = $nick;
			}
			if ($whom eq $botnick) {
				$whom = $nick;
			}
			if (!is_throttled('insult', $userhost)) {
				eggplus($nick);
				do_quip($channel, $whom);
				return;
			}
		}

#				elsif ($msg =~ m/(father|mother|pops|mom|son|daughter)/) {
#					emit($irc, $channel, "Tell me more about your $1, $nick." );
#				} else {
#					my $num=rand(100);
#					my $msg = $1;
#					$msg =~ s/you('| a)re/I am/gi;
#					$msg =~ s/\bare you\b/am I/gi;
#					$msg =~ s/\byour\b/my/gi;
#					$msg =~ s/\byou\b/I/gi;
#					$msg =~ s/[, ]*$//;
#					if ($num > 90) {
#						emit($irc, $channel, "I don't know about that, $nick." );
#					} elsif ($num > 80) {
#						emit($irc, $channel, "Can you elaborate on that, $nick?" );
#					} elsif ($num > 70) {
#						emit($irc, $channel, "What makes you believe $msg, $nick?" );
#					} elsif ($num > 60) {
#						emit($irc, $channel, "Perhaps your plans have something to do with this." );
#					} elsif ($num > 50) {
#						emit($irc, $channel, "Does that really interest you, $nick?" );
#					} elsif ($num > 40) {
#						emit($irc, $channel, "Maybe you should consult a doctor of medicine." );
#					} elsif ($num > 30) {
#						emit($irc, $channel, "Why do you say that?" );
#					} elsif ($num > 20) {
#						emit($irc, $channel, "Please go on, $nick." );
#					} elsif ($num > 10) {
#						emit($irc, $channel, "That's not very nice." );
#					}
#				}

		if ($msg =~ /^$botnick: (.+)/) {
			on_command($1, $channel, $nick, $userhost, $channel);
			return;
		}
	}

	elsif( $msg =~ m/security ((problem )?excuse )?bingo/i) {
		if ($CHANNELS{$channel}{"toggles"}{"chatter"}) {
			do_speb($channel);
		}
	}


	elsif( $msg =~ m/speed.*european swallow/i) {
		if ($CHANNELS{$channel}{"toggles"}{"chatter"}) {
			emit($irc, $channel, "11 meters per second");
		}
	}

	elsif ($msg =~ m/Megan Fox/i) {
		if (($CHANNELS{$channel}{"toggles"}{"chatter"}) && !is_throttled("megan_fox", $userhost, 'all')) {
			emit($irc, $channel, $megan[int(rand(scalar(@megan)))]);
		}
	}

	elsif ($msg =~ m/(Charlie Sheen|Bree Olson|Tiger ?blood)/i &&
		$CHANNELS{$channel}{"toggles"}{"chatter"} &&
		$CHANNELS{$channel}{"toggles"}{"charliesheen"} &&
		!is_throttled("charliesheen", $userhost, 'all')) {
		eggplus($nick);
		do_charliesheen($channel);
	}

	else {
		if (!($CHANNELS{$channel}{"toggles"}{"chatter"})) {
			return;
		}
		foreach my $k (keys %h2g2) {
			if ($msg =~ m/$k/i && !is_throttled("$k", $userhost, 'all')) {
				eggplus($nick);
				emit($irc, $channel, $h2g2{$k} );
				return;
			}
		}

#		foreach my $k (keys %calvin) {
#			if ($msg =~ m/$k/i && !is_throttled("$k", $userhost, 'all')) {
#				eggplus($nick);
#				emit($irc, $channel, $calvin{$k} );
#				return;
#			}
#		}

		foreach my $k (keys %python) {
			if ($msg =~ m/$k/i && !is_throttled("$k", $userhost, 'all')) {
				eggplus($nick);
				emit($irc, $channel, $python{$k} );
				return;
			}
		}

		foreach my $k (@futurama) {
			if ($msg =~ m/$k/i && !is_throttled("futurama", $userhost, 'all')
				&& $CHANNELS{$channel}{"toggles"}{"chatter"}
				&& $CHANNELS{$channel}{"toggles"}{"futurama"}) {
				eggplus($nick);
				do_futurama($channel);
				return;
			}
		}

		foreach my $k (keys %seinfeld) {
			if ($msg =~ m/$k/i && !is_throttled("$k", $userhost, 'all')) {
				eggplus($nick);
				emit($irc, $channel, $seinfeld{$k} );
				return;
			}
		}

#		foreach my $k (keys %burns) {
#			if ($msg =~ m/$k/i && !is_throttled("$k", $userhost, 'all')) {
#				eggplus($nick);
#				emit($irc, $channel, $burns{$k} );
#				return;
#			}
#		}

#		my $n = 0;
#		foreach my $base (@base_belong) {
#			$n = ($n >= (scalar(@base_belong) - 1)) ? 0 : $n + 1;
#			if ($msg =~ m/$base/i && !is_throttled("$base", $userhost, 'all')) {
#				eggplus($nick);
#				my $r = $base_belong[$n];
#				emit($irc, $channel, $r);
#			}
#		}
	}


}

# function : on_msg
# purpose  : same as on_public, but for private /msg to the bot

sub on_msg {
	my ( $kernel, $who, $where, $msg ) = @_[ KERNEL, ARG0, ARG1, ARG2 ];
	my ($nick, $userhost) = ( split /!/, $who );
	my $channel = $where->[0];

	$channel = lc($channel);
	$CHANNELS{$channel}{"seen"}{$nick} = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());
	my $md5 = Digest::MD5::md5_hex($msg);

	if ($msg =~ /^\!(.+)$/) {
		on_command($1, $nick, $nick, $userhost, $channel);
		return;
	} elsif ($msg =~ m,^s(\[)((?:[^\\]|\\.)*?)\]\[((?:[^\\]|\\.)*)\]([gi]*)(?:\s*;\s*(\S*)?)?$, ||
            $msg =~ m,^s(\{)((?:[^\\]|\\.)*?)\}\{((?:[^\\]|\\.)*)\}([gi]*)(?:\s*;\s*(\S*)?)?$,      ||                     $msg =~ m,^s(\()((?:[^\\]|\\.)*?)\)\(((?:[^\\]|\\.)*)\)([gi]*)(?:\s*;\s*(\S*)?)?$,      ||                     $msg =~ m,^s(<)((?:[^\\]|\\.)*?)><((?:[^\\]|\\.)*)>([gi]*)(?:\s*;\s*(\S*)?)?$, 	  ||
            $msg =~ m,^s(.)((?:[^\\]|\\.)*?)\1((?:[^\\]|\\.)*)\1([gi]*)(?:\s*;\s*(\S*)?)?$,) {

		my $c = "s///";
		if (!$cmds{$c}) {
			$cmds{$c} = 0;
		}
		$cmds{$c} = $cmds{$c} + 1;

		if (!$cmdrs{$nick}) {
			$cmdrs{$nick} = 0;
		}
		$cmdrs{$nick} = $cmdrs{$nick} + 1;

	} elsif ($md5 eq "d6b5fd5b083ca75c5ac7375e7358ec22") {
		if ($userhost =~ /^$botowner\@($botownerhost|127.0.0.1)$/) {
			my $cf;
			my @args = ("perl", "-c", "$0");
			unlink($channels_file);
			unlink($potty_file);
			unlink($curses_file);
			unlink($cmd_file);
			unlink($cmdr_file);
			unlink($rainbow_file);
			store(\%CHANNELS, $channels_file);
			store(\%pottymouths, $potty_file);
			store(\%curses, $curses_file);
			store(\%cmds, $cmd_file);
			store(\%cmdrs, $cmdr_file);
			store(\%rainbow, $rainbow_file);
			system(@args) == 0 and exec($0);
			emit($irc, $nick, "Oh Noes!  Syntax errors!");
			emit($irc, $nick, "Fix me!");
		} elsif (!is_throttled('reload', $userhost)) {
			emit($irc, $nick, "Screw you!");
		}
		return;
	} elsif ($msg =~ /^say (.*)/) {
		if ($1 =~ /^(#\S+)\s+(.*)/) {
		#	emit($irc, $1, "$2");
			emit($irc, $1, "$nick is a wanker.");
		}
		return;
	} elsif ($msg =~ m/^leave (#\S+)/) {
		my $ch = $1;
		if ($userhost =~ /^$botowner\@($botownerhost|127.0.0.1)$/) {
			$irc->yield( 'part' => $ch);
print STDERR "Leaving $ch cause my master said so.\n";
			delete($CHANNELS{$ch});
			return;
		}
	} elsif ($msg =~ m/^join (#\S+)/) {
		my $ch = $1;
		if ($userhost =~ /^$botowner\@($botownerhost|127.0.0.1)$/) {
			$CHANNELS{$ch} = newChannel($botowner);
			$irc->yield( join => $ch);
print STDERR "Joining $ch cause my master said so.\n";
			return;
		}
	}


	emit($irc, $nick, "No such command.  Try '!help'.");
}

# function : on_invite
# purpose  : called whenever somebody invites the bot

sub on_invite {
	my ($kernel, $who, $channel) = @_[ KERNEL, ARG0, ARG1 ];
	my ($inviter, $userhost) = split(/!/, $who);

	$channel =~ s/^.*#/#/;
	$channel = lc($channel);
	$CHANNELS{$channel} = newChannel($inviter);

	$irc->yield( join => $channel );
	$irc->yield( names => $channel );
}


# function : on_join
# purpose  : called whenever someone joins a channel

sub on_join {
	my ( $kernel, $who, $where, $msg ) = @_[ KERNEL, ARG0, ARG1, ARG2 ];
	my ($nick,$userhost) = split(/!/, $who);
	my $uname = (split(/@/, $userhost))[0];

	$uname =~ s/^~//;

	$where = lc($where);
	$CHANNELS{$where}{"users"}{$nick} = 1;
	$CHANNELS{$where}{"seen"}{$nick} = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());
	$bricks{$nick} = 20;

	bot_toggle($nick, $where, 0);
}

# function : on_kick
# purpose  : called whenever someone gets kicked off

sub on_kick {
	my ($kernel, $kicker, $where, $goner) = @_[ KERNEL, ARG0, ARG1, ARG2 ];

	$where = lc($where);
	$CHANNELS{$where}{"seen"}{$goner} = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());

	delete $CHANNELS{$where}{"users"}{$goner};

	delete $bricks{$goner};

	bot_toggle($goner, $where, 1);

	if ($goner eq $botnick) {
		delete($CHANNELS{$where});
print STDERR "I got kicked out of $where by $kicker.\n";
	}
}

# function : on_part
# purpose  : called whenever someone parts a channel

sub on_part {
	my ($kernel, $who, $where, $msg) = @_[ KERNEL, ARG0, ARG1, ARG2 ];
	my $nick = (split /!/, $_[ARG0])[0];

	$where = lc($where);

	if ($nick eq $botnick) {
print STDERR "I'm parting $where.\n";
		return;
	}

	$CHANNELS{$where}{"seen"}{$nick} = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());

	delete $CHANNELS{$where}{"users"}{$nick};
	delete $bricks{$nick};

	bot_toggle($nick, $where, 1);

	if (scalar(keys(%{$CHANNELS{$where}{"users"}})) < 3) {
		if ($CHANNELS{$where}{"users"}{"logger"}) {
			emit($irc, $where, "Only you and me, logger?  No way! ");
			$irc->yield( 'part' => $where );
print STDERR "Leaving $where cause only logger's there.\n";
			delete($CHANNELS{$where});
		} elsif ((scalar(keys(%{$CHANNELS{$where}{"users"}})) < 2) && ($nick ne $botnick)) {
			emit($irc, $where, "I'm not gonna hang around here all by myself.");
			$irc->yield( 'part' => $where );
print STDERR "Leaving $where cause nobody's there.\n";
			delete($CHANNELS{$where});
		}
	}
}

# function : on_quit
# purpose  : called whenever someone quits IRC

sub on_quit {
	my ($kernel, $who) = @_[ KERNEL, ARG0 ];
	my $nick = (split /!/, $_[ARG0])[0];

	foreach my $ch (keys(%CHANNELS)) {
		$ch = lc($ch);
		bot_toggle($nick, $ch, 1);
		delete($CHANNELS{$ch}{"users"}{$nick});
		if (defined($CHANNELS{$ch}{"seen"}{$nick})) {
			$CHANNELS{$ch}{"seen"}{$nick} = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());
		}
	}

	delete $bricks{$nick};
}

# purpose  : handle bot toggling
# inputs   : botname, channel, value (0 or 1)

sub bot_toggle($$$) {
	my ($bot, $channel, $value) = @_;

	my (@toggles);

	if ($bot eq "tinybot") {
		push(@toggles, "tiny");
	} elsif ($bot eq "ybiip") {
		push(@toggles, "ybiip");
	} elsif ($bot eq "pbot") {
		push(@toggles, "sed");
		push(@toggles, "stfu");
	} elsif ($bot eq "dnsbot") {
		# $value == 1 happens when the bot leaves
		if (!$value) {
			emit($irc, $channel, "?toggle tiny");
			emit($irc, $channel, "?toggle bug");
		}
	}

	foreach my $t (@toggles) {
		$CHANNELS{$channel}{"toggles"}{$t} = $value;
	}
}

# function : irc_names
# purpose  : populate global hash of users
#            called automatically at connection

sub irc_names {
	my $channel = (split / :/, $_[ARG1])[0];
	my $names = (split / :/, $_[ARG1])[1];
	my @lusers = split(/\s+/, $names);

	$channel =~ s/^.*#/#/;
	$channel =~ s/\s.*//;
	$channel = lc($channel);
	foreach my $l (@lusers) {
		$l =~ s/^@//;
		$CHANNELS{$channel}{"users"}{$l} = 1;
		$bricks{$l} = 20;
	}

	if (scalar(keys(%{$CHANNELS{$channel}{"users"}})) < 2) {
		$irc->yield( 'part' => $channel);
		emit($irc, $channel, "Where is everybody?");
print STDERR "Leaving $channel cause I couldn't find anybody.\n";
		delete($CHANNELS{$channel});
	}
}

# function : on_whois
# purpose  : called whenever we call whois, sets togglables for bots and
#            sets the global last_whois_result

sub on_whois {
	my ( $kernel, $hr ) = @_[ KERNEL, ARG0, ARG1 ];

	my %h = %{$hr};
	if (!$h{channels}) {
		return;
	}
	my @chs =@{$h{channels}};
	$last_whois_result = $hr;


#	if ($h{'nick'} eq $botnick) {
#		foreach my $c (@chs) {
#			$channels{$c} = 1;
#		}
#	}

	if ($h{'nick'} eq "tinybot") {
		foreach my $c (@chs) {
			$c =~ s/^.*#/#/;
			$c =~ s/\s.*//;
			$c = lc($c);
			if (defined($CHANNELS{$c})) {
				$CHANNELS{$c}{"toggles"}{"tiny"} = 0;
			}
		}
	}

	if ($h{'nick'} eq "ybiip") {
		foreach my $c (@chs) {
			$c =~ s/^.*#/#/;
			$c =~ s/\s.*//;
			$c = lc($c);
			if (defined($CHANNELS{$c})) {
				$CHANNELS{$c}{"toggles"}{"ybiip"} = 0;
			}
		}
	}

	if ($h{'nick'} eq "pbot") {
		foreach my $c (@chs) {
			$c =~ s/^.*#/#/;
			$c =~ s/\s.*//;
			$c = lc($c);
			if (defined($CHANNELS{$c})) {
				$CHANNELS{$c}{"toggles"}{"sed"} = 0;
				$CHANNELS{$c}{"toggles"}{"stfu"} = 0;
			}
		}
	}

}

# function : on_nick
# purpose  : called whenever someone changes nick

sub on_nick {
	my ( $kernel, $who, $new ) = @_[ KERNEL, ARG0, ARG1 ];
	my ($nick,$userhost) = ( split /!/, $who )[0];

	my $now = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());

	foreach my $c (keys(%CHANNELS)) {
		$c = lc($c);
		if (defined($CHANNELS{$c}{"seen"}{$who})) {
			delete($CHANNELS{$c}{"seen"}{$who});
			$CHANNELS{$c}{"seen"}{$new} = $now;
		}

		if (defined($CHANNELS{$c}{"users"}{$nick})) {
			delete($CHANNELS{$c}{"users"}{$nick});
			$CHANNELS{$c}{"users"}{$new} = 1;
		}
	}

	$bricks{$new} = $bricks{$nick};
	$cmdrs{$new} = $cmdrs{$nick};
	if ($pottymouths{$nick}) {
		$pottymouths{$new} = $pottymouths{$nick};
		delete $pottymouths{$nick};
	}
	delete $cmdrs{$nick};
	delete $bricks{$nick};

	for my $c (keys(%CHANNELS)) {
		$c = lc($c);
		if ($totcount{$c}{$nick}) {
			$totcount{$c}{$new} = $totcount{$c}{$nick};
			delete $totcount{$c}{$nick};
		}
	}


#	if (($nick =~ m/^jans_afk$/) && ($new =~ m/^jans$/)) {
#		foreach my $chan (keys(%CHANNELS)) {
#			emit($irc, $chan, "Welcome back, Master!");
#		}
#	}
}


# function : getIPAddresses
# purpose  : take a hostname and return its IP addresses in a hash
# inputs   : hostname
# returns  : a hash, keys are IP addresses

sub getIPAddresses($) {
	my ($host) = @_;
	my %addrs;

	$host = fqdn($host);

	my @res = getaddrinfo($host, undef);
	while (scalar(@res) >= 5) {
		my ($fam, undef, undef, $saddr, undef, @others) = @res;
		@res = @others;
		my ($port, $addr) = ($fam == AF_INET6) ?
			unpack_sockaddr_in6($saddr) : sockaddr_in($saddr);
		$addrs{inet_ntop($fam, $addr)} = 1;
	}

	return %addrs;
}


# function : getContent
# purpose  : retrieve content from a url
# inputs   : a url, optional referer,
# returns  : an array of lines

sub getContent($;$) {
	my ($url, $referer) = @_;
	my @content;
	my $ua = LWP::UserAgent->new(agent => "Mozilla/5.0 (X11; U; NetBSD i386; en-US; rv:1.9.2.3) Gecko/20100622 Namoroka/3.6.3");

	if (!$url) {
		print "Oy! No url to fetch!\n";
		return @content;
	}

	if ($referer) {
		$ua->default_headers->header('Referer' => $referer);
	}

	my $r = $ua->get("$url");

        my $encoding = $r->header('Content-Encoding');

	if ($r->is_error) {
		push(@content, $r->status_line);
	} else {
		if ($encoding =~ /gzip/i) {
			my $gz = Compress::Zlib::memGunzip($r->content);
			@content = split(/\n/, $gz);
			undef($gz);
		} else {
			@content = split(/\n/, $r->content);
		}
	}

	undef($r);

	if ($referer) {
		$ua->default_headers->remove_header('Referer');
	}

	if (!scalar(@content)) {
		print STDERR "Found no content at |$url| (encoding: $encoding)\n";
	}

	return @content;
}

# function : fqdn
# purpose  : return the fqdn of the input host
# inputs   : hostname
# returns  : a fqdn (minus trailing dot, actually) or undef

sub fqdn($) {
	my ($hostname) = @_;

	if (gethostbyname("$hostname.yahoo.com.")) {
		$hostname = "$hostname.yahoo.com";
	}
	if (!gethostbyname($hostname)) {
		return undef;
	}

	return $hostname;
}

sub fetch_rss_feed($$$;$) {
	my ($feedname,$nick,$lines, $url) = @_;
	my ($buzz, $count, $feed, $found, $link, $n, $num_only, $rss, $title, $tiny);
	my (@content);

	$tiny = 0;
	$count = 0;
	$found = 0;
	$n = 0;
	$num_only = 0;

	if ($url) {
		$rss = $url;
	} else {
		$rss = $rssurl{"$feedname"};
	}

	if ($lines eq "count") {
		$num_only = 1;
		$lines = "";
	}

	if ($feedname =~ m/buzz/) {
		$buzz = 1;
	}

	if ($feedname =~ m/(ynews|nyt|onion|slashdot|uwotd)/) {
		$tiny = 1;
		@content = getContent($rss);
		if (!$lines) {
			$lines = 4;
		}
		if ($feedname eq "onion") {
			$lines++;
		}
	}
	foreach my $line (@content) {
		if ($line =~ m/<rdf:li rdf:resource="/) {
			$count++;
			next;
		}

		if ((!$lines || $num_only) && $line =~ m/<\/rdf:Seq>/) {
			emit($irc, $nick, $count);
		}

		if ($line =~ m/<item/) {
			$found = 1;
		}

		if ($line =~ m/<item rdf:about="(.*)">/ || $line =~ m/<link>.*(http.*)<\/link>/) {
			if ($found) {
				$link = "$1";
				if ($feedname eq "nyt") {
					$link =~ s/\.html.*/.html/;
				}
			}
		}

		if ($found && $line =~ m/<title>([^<]+)(<\/title>)?/) {
			$title = decode_entities($1);
		}

		if ($found && $link && $title) {
			if (!$num_only && (!$lines || ($n < $lines))) {
				if (($n == 0) && ($feedname =~ m/(onion|slashdot)/)) {
					# onion gives title else
					$link = "";
					$title = "";
					$n++;
					next;
				}

				emit($irc, $nick, $title);
				if (!$buzz) {
					emit($irc, $nick, $tiny ? do_tiny($link) : $link);
				}
				$n++;
			}

			$link = "";
			$title = "";
		}
	}
}

# function : do_periodics
# purpose  : run everything that needs to be run every 300s

sub do_periodics() {

	if ($periodics{"escalations"} <= $autoping) {
		$periodics{"escalations"} = $autoping;
	} else {
		$periodics{"escalations"} -= $autoping;
	}

	if ($periodics{"unowned"} <= $autoping) {
		$periodics{"unowned"} = $autoping;
	} else {
		$periodics{"unowned"} -= $autoping;
	}
}

# function : do_usertime
# purpose  : display time for user based on timezone
# inputs   : who to respond to, nickname
# returns  : results (if any) are sent to the recipient

sub do_usertime($$) {
	my ($who, $user) = @_;
	my ($count, $rcpt);
	my ($query);

	if ($user eq $botnick) {
		do_shortcut("time", "sunnyvale, ca", $who, 0);
		return;
	}

	$query = $methods{"usertime"};
	$query =~ s/<user>/$user/;
	$count = 0;

	foreach my $tz (getContent($query)) {
		if ($count++ > 1) {
			last;
		}

		if ($tz =~ m/^[[:print:]]*$/) {
			if (length($tz) > 255) {
				emit($irc, $rcpt, "Bad $user, bad!");
				$irc->yield( 'ctcp' => $rcpt => "ACTION whacks $user on the nose with a rolled up newspaper.");
				return;
			}
			$tz =~ s/[[:cntrl:]]+/ /g;
			do_tz($who, $tz);
			return;
		} else {
			emit($irc, $rcpt, "$user: gobbledygook");
			return;
		}
	}

	do_tz($who, "PST8PDT");
}

# function : do_new
# purpose  : display what's new
# inputs   : who to respond to, nickname, optional username
# returns  : results (if any) are sent to the recipient

sub do_new($$$) {
	my ($who, $nick, $target) = @_;
	my ($count, %users, $rcpt);

	$rcpt = $who;
	if (!$target) {
		foreach my $new (@whats_new) {
			emit($irc, $rcpt, $new);
		}
	}

	$target =~ s/^\s+//;

	my @u = split(/\s/, $target);
	%users = %{{ map {$_,1} @u}};

	foreach my $user (sort(keys %users)) {
		my ($query);

		if ($user eq $botnick) {
			foreach my $new (@whats_new) {
				emit($irc, $rcpt, "$botnick: $new");
			}
			next;
		}

		$query = $methods{"new"};
		$query =~ s/<user>/$user/;
		$count = 0;

		foreach my $msg (getContent($query)) {

			if ($msg eq "404 Not Found") {
				emit($irc, $rcpt, "No news is good news.");
				last;
			}
			if ($count++ > 4) {
				emit($irc, $rcpt, "Oh, zip it, $user.");
				last;
			}

			if ($msg =~ m/^[[:print:]]*$/) {
				if (length($msg) > 255) {
					emit($irc, $rcpt, "Bad $user, bad!");
					$irc->yield( 'ctcp' => $rcpt => "ACTION whacks $user on the nose with a rolled up newspaper.");
					last;
				}
				$msg =~ s/[[:cntrl:]]+/ /g;
				emit($irc, $rcpt, "$user: $msg");
			} else {
				emit($irc, $rcpt, "$user: gobbledygook");
			}
		}
	}
}

# function : do_shell
# purpose  : invoke given (sanctioned) shell command with given arguments
# inputs   : a command, the channel and any optional arguments, max lines,
#            optional linefy (to turn output into a single line)
# returns  : 0 on sucess, >0 otherwise

sub do_shell($$$$;$) {
	my ($cmd, $channel, $args, $max, $linefy) = @_;
	my $lines = "";
	my $count = 0;
	my @lines;
	my @command = split(/\s+/, $cmd);

	eval {
		local $SIG{ALRM} = sub { die "alarm\n" };
		alarm(10);

print STDERR "Opening pipe to: '" . join(" ", @command) . "'\n";
if ($args) { print STDERR "Piping into it: $args\n"; }
		my $pid =  open3(\*WH, \*RH, \*EH, @command);
		print WH "$args\n";
		close(WH);

		while (my $l = <EH>) {
			$count++;
			if (($count > $max) && !$linefy) {
				last;
			}
			emit($irc, $channel, $l);
		}
		close(EH);

		$count=0;
		while (my $l = <RH>) {
			$count++;

			if (($count > $max) && !$linefy) {
				last;
			}
			if ($linefy) {
				chomp($l);
				push(@lines, $l);
			} else {
				if ($l) {
					# digest - keep track for rainbow
					# table
					if ($cmd =~ m/.*digest (.*)/) {
						my $digest = $1;
						chomp($l);
						$rainbow{$digest}{$l} = $args;
					}
					emit($irc, $channel, $l);
				}
			}
		}
		waitpid($pid, 0);

		if ($linefy) {
			emit($irc, $channel, join(" ", @lines));
		}
	};
	if ($@) {
		emit($irc, $channel, "I killed your '$cmd', just FYI.");
	}

	return 0;
}

# function : do_countdown
# purpose  : display time left until given date
# input    : seconds since epoch of target date
# returns  : a string showing how much time is left

sub do_countdown($) {
	my ($target) = @_;

	my $t = $countdowns{$target};
	my ($s1, $m1, $h1, $d1, $M1, $y1) = localtime();
	my ($s2, $m2, $h2, $d2, $M2, $y2);
	if ($t) {
		($s2, $m2, $h2, $d2, $M2, $y2) = localtime($t);
	} else {
		if ($target =~ m/(tacos|lunch)$/) {
			$s2 = 0;
			$m2 = 30;
			$h2 = 11;
			$d2 = $d1;
			$M2 = $M1;
			$y2 = $y1;
		} elsif ($target =~ m/(beer|bier|mead|cerveza|booze)/) {
			# note: beer@ is an actual user, so won't really
			# match
			$h2 = $h1 + (rand(24 - $h1));
			if ($h2 == $h1) {
				$m2 = $m1 + (rand(60 - $m1));
			} else {
				$m2 = rand(60);
			}
			$s2 = rand(60);
			$d2 = $d1; $M2 = $M1; $y2 = $y1;
		} elsif ($target =~ m/^ipv4\s*(iana|rir)?$/) {
			my $what = $1;
			if ($what) {
				$what =~ s/ //g;
			}
			($s2, $m2, $h2, $d2, $M2, $y2) = localtime(get_ipv4_countdown($what));
		}
	}

	if (defined($s2)) {
		my ($Dd,$Dh,$Dm,$Ds) = Delta_DHMS($y1 + 1900, $M1 + 1, $d1, $h1, $m1, $s1,
				$y2 + 1900, $M2 + 1, $d2, $h2, $m2, $s2);
		return "$Dd day" . ($Dd != 1 ? "s" : "") .
			" $Dh hour" . ($Dh != 1 ? "s" : "") .
			" $Dm minute" . ($Dm != 1 ? "s" : "") .
			" and $Ds second" . ($Ds != 1 ? "s" : "") . ".";
	} else {
		return $dontknow[int(rand(scalar(@dontknow)))];
	}
}


# function : do_cursebird
# purpose  : display last curse or score of curser

sub do_cursebird($$) {
	my ($who, $tweetie) = @_;
	my ($query);
	my $found = 0;

	$query = $methods{"cursebird"};
	if ($tweetie) {
		$query .= $tweetie . ".json";
	}

	if (!$tweetie) {
		foreach my $line (getContent($query)) {
			if ($line =~ m/.*?<div class="tweet">\s*<a href="\/([^"]+)".*?<span class="text">(.*?)<\/span>/i) {
				my $twit = $1;
				my $msg = dehtmlify($2);
				emit($irc, $who, "$msg");
				emit($irc, $who, $query . $twit);
				last;
			}
		}
	} else {
		my $json = new JSON;
		my $cursebird;
		eval {
			$cursebird = $json->allow_nonref->utf8->relaxed->escape_slash->loose->allow_singlequote->allow_barekey->decode(getContent($query));
		};
		if ($@) {
			emit($irc, $who, "No cursebird found for $tweetie.");
			return;
		}
		my %cb = %{$cursebird};
		if (scalar(keys(%cb))) {
			emit($irc, $who, "$tweetie swears like " . $cb{"swears_like"});
			emit($irc, $who, "Level: " . $cb{"level"} . " (" . $cb{"xp_score"} . ")");
		}
	}
}


# function : do_cve
# purpose  : display information for a cve vulnerability

sub do_cve($$) {
	my ($who, $num) = @_;
	my ($query);
	my $found = 0;

	$query = $methods{"cve"} . $num;

	foreach my $line (getContent($query)) {
		if ($line =~ m/<th colspan="2">Description<\/th>/) {
			emit($irc, $who, $query );
			$found = 1;
			next;
		}

		if ($found) {
			if ($line =~ m/<\/td>/) {
				last;
			}

			$line =~ s/<.+?>//g;
			$line =~ s/^\s*//;
			chomp($line);
			if ($line) {
				emit($irc, $who, decode_entities($line));
			}
		}
	}
}

# function : do_vu
# purpose  : display information for a VU vulnerability

sub do_vu($$) {
	my ($who, $num) = @_;
	my ($query);
	my $found = 0;

	$num =~ s/#//g;
	$query = $methods{"vu"} . $num;

	foreach my $line (getContent($query)) {
		if ($line =~ m/Vulnerability Note VU#/i) {
			$found = 1;
			next;
		}

		if ($found) {
			if ($line =~ m/<h2>(.*)<\/h2>/i) {
				emit($irc, $who, dehtmlify($1));
				next;
			}
			if ($line =~ m/<h3>Overview<\/h3><\/a>(.*)/i) {
				emit($irc, $who, dehtmlify($1));
				emit($irc, $who, $query);
				last;
			}
		}
	}
}


# function : do_cvs
# purpose  : display requested lines from a file in CVS
# inputs   : a channel, a nick, a CVS path, a starting line number and an optional
#            ending line number
# outputs  : the requested lines from the file in CVS into the channel

sub do_cvs($$$$$) {
	my ($channel, $nick, $file, $start, $end) = @_;
	my $dir = $ENV{'YSA_CVSDIR'};
	my $path = "$dir/$file";
	my $lines = 0;
	my $total = 0;

	if (!$end) {
		$end = $start;
	}

	if (! -f $path) {
		`cd $dir && cvs -q co $file` or return 1;
	} else {
		`cd $dir && cvs -q up $file`;
	}

	open(F, $path) or do {
		emit($irc, $channel, "Can't open '$file': $!");
		return 1;
	};

	while (my $l = <F>) {
		$lines++;
		$total++;
		if ($lines >= $start && $lines <= $end) {
			my $out = "$lines:  $l";
			if ($total > 5) {
				emit($irc, $nick, $out);
			} else {
				emit($irc, $channel, $out);
			}
		}

		last if ($lines == $end);
	}
	close(F);
	return 0;
}

# function : show_toggles
# purpose  : print out the toggles for the given channel
# inputs   : who to respond to, channel

sub show_toggles($$) {
	my ($who, $channel) = @_;

	# privmsg
	if ($channel eq $botnick) {
		return;
	}

	$channel = lc($channel);
	if (!defined($CHANNELS{$channel})) {
		emit($irc, $who, "I don't think I know anything about $channel.");
		return;
	}

	my @msg;
	my %toggles = %{$CHANNELS{$channel}{"toggles"}};
	foreach my $t (sort(keys(%toggles))) {
		my $tv = "$t => " . $toggles{$t};
		push(@msg, $tv);
	}
	emit($irc, $who, join(", ", @msg));
}

# function : do_speb
# purpose  : display a securit problem excuse bingo result

sub do_speb($) {
	my ($who) = @_;
	my $found = 0;
	my @excuses;

	my $query = $methods{"speb"};

	foreach my $line (getContent($query)) {
		if ($line =~ m/var bingoFields = new Array/) {
			$line =~ s/.*\(//;
			$found = 1;
		}
		if ($found) {
			if ($line =~ m/\);/) {
				$line =~ s/\);.*//;
				$found = 0;
			}
			$line =~ s/[,"]//g;
			$line =~ s/<br>/ /g;
			$line =~ s/\s{2,}/ /g;
			$line =~ s/^\s//;
			if ($line) {
				$line =~ s/([^!?])$/$1./;
				push(@excuses, $line);
			}
		}
	}
	emit($irc, $who, $excuses[int(rand(scalar(@excuses)))]);
}


# function : do_score
# purpose  : display any sports results if found

sub do_score($$) {
	my ($who, $term) = @_;
	my ($query);
	my $found = 0;

	$query = $methods{"score"};

	foreach my $line (getContent($query)) {
		if ($line =~ m/<title><!\[CDATA\[(.*$term.*)\]\]><\/title>/i) {
			emit($irc, $who, $1);
			$found = 1;
			next;
		}

		if ($found) {
			if ($line =~ m/<description><!\[CDATA\[(.*)\]\]><\/description>/) {
				my $result = $1;
				$result =~ s/<.+?>/ /g;
				emit($irc, $who, decode_entities($result));
				last;
			}
		}
	}
}

# function : do_service
# purpose  : display number or name of service value
# inputs   : who to respond to, nick, name or number
# returns  : results (if any) are sent to the recipient

sub do_service($$$) {
	my ($who, $nick, $input) = @_;
	my ($f, @items, $rcpt, $line);

	$line = 0;
	$rcpt = $who;
	$f = $methods{"service"};

	@items = split(/ /, $input);
	foreach my $s (@items) {
		open(P, "egrep \"^[^#]*$s\" $f|");
		while (my $l = <P>) {
			$line++;
			if ($line > 5) {
				$rcpt = $nick;
			}
			last if ($line > 25);
			s/\t/    /g;
			emit($irc, $rcpt, $l);
		}
		close(P);
	}
}

# function : do_signal
# purpose  : display number or name of signal value
# inputs   : who to respond to, nick, name or number
# returns  : results (if any) are sent to the recipient

sub do_signal($$$) {
	my ($who, $nick, $input) = @_;
	my ($f, @items, $rcpt, $line);

	$line = 0;
	$rcpt = $who;
	$f = $methods{"signal"};

	@items = split(/ /, $input);
	foreach my $s (@items) {
		open(P, "grep \"^#define\" $f|");
		while (my $l = <P>) {
			if (m/$s/i) {
				s/^#define\s+//;
				$line++;
				if ($line > 5) {
					$rcpt = $nick;
				}
				s/\t/    /g;
				emit($irc, $rcpt, $l);
			}
		}
		close(P);
	}
}


# function : do_sysexit
# purpose  : display number or name of sysexit value
# inputs   : who to respond to, nick, name or number
# returns  : results (if any) are sent to the recipient

sub do_sysexit($$$) {
	my ($who, $nick, $input) = @_;
	my ($f, @items, $rcpt, $line);

	$line = 0;
	$rcpt = $who;
	$f = $methods{"sysexit"};

	@items = split(/ /, $input);
	foreach my $s (@items) {
		open(P, "grep \"^#define\" $f|");
		while (my $l = <P>) {
			if (m/$s/i) {
				s/^#define\s+//;
				$line++;
				if ($line > 5) {
					$rcpt = $nick;
				}
				s/\t/    /g;
				emit($irc, $rcpt, $l);
			}
		}
		close(P);
	}
}

# function : do_errno
# purpose  : display number or name of errno value
# inputs   : who to respond to, nick, name or number
# returns  : results (if any) are sent to the recipient

sub do_errno($$$) {
	my ($who, $nick, $input) = @_;
	my ($f, @items, $rcpt, $line);

	$line = 0;
	$rcpt = $who;
	$f = $methods{"errno"};

	@items = split(/ /, $input);
	foreach my $s (@items) {
		open(P, "grep \"^#define\" $f|");
		while (<P>) {
			if (m/$s/i) {
				s/^#define\s+//;
				$line++;
				if ($line > 5) {
					$rcpt = $nick;
				}
				s/\t/    /g;
				emit($irc, $rcpt, $_);
			}
		}
		close(P);
	}
}


# purpose  : display a BOFH excuse
# inputs   : recipient

sub do_bofh($) {
	my ($who) = @_;
	my ($url, @excuses);
	my $found = 0;

	$url = $methods{"bofh"};
	@excuses = getContent($url);
	emit($irc, $who, $excuses[int(rand(scalar(@excuses)))]);
}

# function : do_symbol
# purpose  : lookup a stock symbol
# inputs   : recipient, symbol(s)
# outputs  : results (if any) are returned

sub do_symbol($$) {
	my ($who, $s) = @_;
	my (@symbols, $query);

	@symbols = split(/\s/, $s);

	foreach $s (@symbols) {
		$query = $methods{"symbol"} . $s;

		foreach my $line (getContent($query)) {
			if ($line =~ m/yfi_sym_lookup_results/) {
				$line=~ s/.*finance.yahoo.com\/q.*\?s=\Q$s"//i;
				if ($line=~ m/>\Q$s\E<\/a><\/td><td>([^<]+)<\/td>.*>([^<]+)<\/a>/i) {
					emit($irc, $who, "$s: $1 ($2)");
					last;
				}
			}
		}
	}
}


# function : do_shakespear
# purpose  : insult somebody shakespearian style
# inputs   : recipient
# outputs  : results (if any) are returned

sub do_shakespear($) {
	my ($who) = @_;
	my ($query);
	my $found = 0;

	$query = $methods{"shakespear"};

	foreach my $line (getContent($query)) {
		if ($line =~ m/<p><font size="\+2">/i) {
			$found = 1;
			next;
		}

		if ($found) {
			$line =~ s/<.+?>//g;
			$line =~ s/\&.+?;//g;
			$line =~ s/^\s*//;
			emit($irc, $who, $line);
			last;
		}
	}
}

# function : do_quake
# purpose  : print information about the latest earth quake
# inputs   : recipied, optional "us"

sub do_quake($;$) {
	my ($who, $us) = @_;

	my $url = $methods{"quake"};

	if ($us) {
		$url =~ s/<WHAT>/us/;
	} else {
		$url =~ s/<WHAT>/ww/;
	}

	foreach my $line (getContent($url)) {
		if ($line =~ m|.*MAP</a></td><td.+?nowrap>(<strong>)?(&nbsp;)?([0-9.]+).+?href="(/earthquakes/recenteqs.*?/Quakes/.*?.php)">(.+?)</a>.*<td.*>&nbsp; ?(.*)</td></tr>|i) {
			my $mag = $3;
			my $link = "http://earthquake.usgs.gov" . $4;
			my $date = $5;
			my $where = $6;
			emit($irc, $who, "$mag  $date  " . dehtmlify($where));
			emit($irc, $who, $link);
			last;
		}
	}
}


# function : do_quote
# purpose  : get quotes (other than from a shortcut)
# inputs   : recipient, query, symbol
# outputs  : results (if any) are returned

sub do_quote($$$) {
	my ($who, $type, $symbol) = @_;
	my ($url, $f);
	my $found = 0;
	my $ah = 0;
	my %results;

	my @symbols = split(/\s+/, $symbol);

	foreach my $s (@symbols) {

		$ah = 0;
		$url = $methods{"fullquote"} . $s;

LINE:
		foreach my $line (getContent($url)) {
			if ($type eq "ahq") {
				$f = "@<<<<<< @####.## @####.## @<<<<<<<<< @<<<<<< @<<<";
				if ($line =~ m/\s*After Hours:\s*$/) {
					$ah = 1;
					next LINE;
				}

				if ($ah) {
					my $rate = $line;
					$rate =~ s/(.*)<table id=.*/$1/;
					$rate =~ s/<.+?>//g;
					$rate =~ s/\&.+?;//g;
					$rate =~ s/^\s*//;
					$results{$s} = "$s: $rate";
					last LINE;
				}
			} elsif ($type eq "rq") {
				$f = "@<<<<<< @<<<<<<<<<<<<<<<<<<<<<<<<<<<<<";
				if ($line =~ m/<span id="yfs_l90[^>]*">(.+?)<\/span/) {
					my $rate = $1;
					$results{$s} = "$s: $rate";
					$found = 1;
					last LINE;
				}
			} elsif ($type eq "q52") {
				$f = "@<<<<<< @###.##  @ @###.##";
				if ($line =~ m/>52wk Range:<\/t[dh]><td.+?>(.+?)<\/td>/) {
					my $range = $1;
					$range =~ s/<.+?>//g;
					$range =~ s/\&.+?;//g;
					$results{$s} = "$s: $range";
					last LINE;
				}
			}
		}
	}

	foreach my $r (sort(keys %results)) {
		my @fields = split(/\s+/, $results{$r});
		$^A = "";
		formline($f, @fields);
		next if ($^A =~ /^\s+$/);
		emit($irc, $who, $^A);
	}

	if (($type eq "rq") && !$found) {
		emit($irc, $who, "No real-time quote available.  Try '!ahq'.");
	}
}


# function : do_quip
# purpose  : insult somebody
# inputs   : recipient, optional person to insult
# outputs  : results (if any) are returned

sub do_quip($;$) {
	my ($who, $lamer) = @_;
	my ($url);
	my $found = 0;
	my ($msg, $ua);

	$url = $methods{"quip"};

	$who = lc($who);
	if (!$CHANNELS{$who}{"toggles"}{"insults"}) {
		return;
	}

	if ($lamer) {
		$msg = "$lamer: ";
	}

	foreach my $line (getContent($url)) {
		if ($line =~ m/<strong><i>(.*)<\/i><\/strong>/) {
			$msg .= $1;
			emit($irc, $who, decode_entities($msg));
			last;
		}
	}
}

# function : do_man
# purpose  : give man page
# inputs   : recipient, command
# outputs  : results (if any) are returned

sub do_man($$) {
	my ($who, $command) = @_;
	my ($url);
	my $found = 0;

	$url = $methods{"man"} . $command;

	foreach my $line (getContent($url)) {
		if ($line =~ m/^NAME/) {
			$found = 1;
			next;
		}

		if ($found) {
			$line =~ s/^\s*//;
			$line =~ s/\s*$//;
			emit($irc, $who, decode_entities($line));
			emit($irc, $who, do_tiny($url));
			last;
		}
	}

	if (!$found) {
		$url = $methods{"rhelman"} . $command;
		foreach my $line (getContent($url)) {
			if ($line =~ m/^NAME/) {
				$found = 1;
				next;
			}

			if ($found) {
				$line =~ s/^\s*//;
				$line =~ s/\s*$//;
				emit($irc, $who, decode_entities($line));
				emit($irc, $who, do_tiny($url));
				last;
			}
		}
	}
}

# function : do_movies
# purpose  : parse Y! movies rss and give info
# inputs   : who to respond to, what is requested

sub do_movies($$) {
	my ($who, $type) = @_;
	my ($url);
	my $found = 0;

	my $rss = "movies";

	if ($type) {
		$rss = "movies_$type";
	}

	do_top5_yrss($who, $rss, 0);
}

# function : do_snopes
# purpose  : search snopes for urban legends
# inputs   : recipient, search term
# outputs  : results (if any) are returned

sub do_snopes($$) {
	my ($who, $what) = @_;
	my ($query);
	my $found = 0;
	my $link;

	$what =~ s/\s/+/g;
	$query = $methods{"snopes"} . $what;

	foreach my $line (getContent($query)) {
		if ($line =~ m/<p><b>(\d+)/i) {
			$found++;
			if ($found > 3) {
				emit($irc, $who, " ...");
				last;
			}
			next;
		}

		if ($found) {
			if ($line =~ m/<a href="(.+)?" target/i) {
				$link = $1;
				next;
			}
			if ($line =~ m/<B><FONT COLOR="#0000AA">(.+)?<\/FONT>/i) {
				emit($irc, $who, " * $1");
				emit($irc, $who, "   $link");
			}
		}
	}
}

# function : do_tiny
# purpose  : tiny a URL via is.gd
# inputs   : a URL
# returns  : a tiny url

sub do_tiny($) {
	my ($url) = @_;

	my $query = $methods{'tiny'} . uri_escape($url);
	foreach my $line (getContent($query)) {
		print $line;
		return $line;
	}
}

# function : do_twitter
# purpose  : search twitter for the given keyword
# inputs   : recipient, search term
# outputs  : results (if any) are returned

sub do_twitter($;$) {
	my ($who, $what) = @_;
	my ($query);
	my $found = 0;
	my ($search, $byperson, $tweet, $twitterer, $num);
	$num = 0;
	$search = 0;
	$byperson = 0;

	if ($what) {
		$what =~ s/^\s+//;
		if ($what =~ m/^=/) {
			$byperson = 1;
			$what =~ s/^=//;
			if ($what =~ s/\s+-(\d+)$//) {
				$num = $1;
				if ($num > 20) {
					$num = 20;
				}
			}
			$query = $methods{"twitter_user"} . $what;
		} else {
			$search = 1;
			$what =~ s/\s/+/g;
			$query = $methods{"twitter"} . uri_escape($what);
		}
	} else {
		$query = "http://twitter.com/public_timeline";
	}

	my $n = 0;
	my $tweet_found = 0;
	my $time = "";

	if ($byperson) {
		my $json = new JSON;
		my $timeline;
		eval {
			$timeline = $json->allow_nonref->utf8->relaxed->escape_slash->loose->allow_singlequote->allow_barekey->decode(getContent($query));
		};
		if ($@) {
			emit($irc, $who, "No timeline found for $what.");
			return;
		}
		my @all = @{$timeline};
		if ($all[$num]) {
			my %last_tweet = %{$all[$num]};
			$twitterer = "http://twitter.com/$what";
			$tweet = dehtmlify($last_tweet{text});
			$tweet =~ s/\n/ /g;
			$time = $last_tweet{created_at};
		}
	} else {

		foreach my $line (getContent($query)) {
			if ($line =~ m|<a href="(http://twitter.com/.*?)" class="tweet-url screen-name">|) {
				$twitterer = dehtmlify(decode_entities($1));
			}

			if ($search) {
				if ($line =~ m/<entry>/) {
					$found = 1;
					next;
				}
				if ($found) {
					if ($line =~ m|<link .*href="(http://twitter.com/.*?)/.*".*rel="alternate"/>|) {
						$twitterer = $1;
					}
					if ($line =~ m|<content type="html">(.*)</content>|) {
						$tweet = dehtmlify(decode_entities($1));
						last;
					}
				}
			} else {
				# just last timeline item
				if ($line =~ m|<span class="entry-content">(.*)</span>|) {
					$tweet = dehtmlify(decode_entities($1));
					next;
				}
				if ($line = m|<span class="published timestamp" data="{time:'(.*)'}"|) {
					$time = $1;
					last;
				}
			}
		}
	}

	if ($tweet) {
		my $msg = $twitterer;
		emit($irc, $who, $tweet);
		if ($time) {
			$msg .= "  $time";
		}
		emit($irc, $who, $msg);
	}
}

# function : do_shortcut
# purpose  : look up a definition using Y! shortcut
# inputs   : action, a word, who to respond to, whether or not to prepend
#            result with input
# output   : a definition, if any

sub do_shortcut($$$$) {
	my ($action, $word, $who, $pre) = @_;
	my (@content, $msg, $query);
	my $found = 0;
	my $cite;
	my $min_found = 1;
	my $stomorrow;

	my @when;

	if ($action eq "weather") {
		@when = split(/\s/, $word);

		foreach my $w (@when) {
			if ($w =~ m/tomorrow/) {
				$stomorrow = $w;
				$word =~ s/tomorrow.*//;
			}
		}
	}

	$word =~ s/\s/\+/g;
	$query = $methods{"$action"} . $word;

	if ($pre > 1) {
		$msg = "$word";
	}

	my $next = 0;
	foreach my $line (getContent($query)) {

		if (($line =~ m/<div id="?yschiy"?.*>/) ||
			(($action =~ m/capital|zip/) && ($line =~ m/<h3><a (href=|class)/)) ||
			(($action =~ m/quote/) && ($line =~ m/div class="res sc sc-quote"/)) ||
			(($action =~ m/weather/) && ($line =~ m/<div class="res sc sc-wtr"/)) ||
			(($action =~ m/area|define|time|synonym/) && ($line =~ m/div class="res sc/)) ||
			(($action =~ m/convert|zip/) && ($line =~ m/<h3 class="yschttl">/))) {
			$found = 1;
			if ($action !~ m/area|define|synonym|quote|weather|zip|time|convert|capital/) {
				next;
			}
		}

		if ($found) {
#			if ($action eq "area") {
#				if (m/<h3 class="yschttl)/) {
#					$found++;
#					next if ($found <= $min_found);
#					s/\&.+?;//g;
#					s/<.+?>//g;
#					s/^\s*//;
#					s/Yahoo! Shortcut - .*//;
#					emit($irc, $who, $_);
#				}
#				last if (($found > ($min_found + 2)) && m/<\/div>/);
#				last if (m/Yahoo! Shortcut - /);
			if ($action eq "weather") {
				if ($line =~ m/<p>(Currently: .*<\/p>)<\/li>.*<p>(Tomorrow.*<\/p>)<\/li>.*<\/div><p>(.*)<\/li><\/ul>(.*)<div id=/) {
					my @today = split(/<\/p>/, $1);
					my @tomorrow = split(/<\/p>/, $2);
					my @tomorrow_1 = split(/<\/p>/, $3);

					my @which = @today;
					if ($stomorrow eq "tomorrow") {
						@which = @tomorrow;
					} elsif ($stomorrow eq "tomorrow+1") {
						@which = @tomorrow_1;
					}

					emit($irc, $who, dehtmlify($which[0]));
					emit($irc, $who, dehtmlify($which[1]));
					emit($irc, $who, dehtmlify($which[2]));
				}
			} elsif ($action =~ m/area|time|convert/) {
				if (($next && m/^(.*?)(<\/h3>|$)/) ||
				    ($line =~ m/<h3 class="yschttl">(.+?)(<\/h3>|$)/)) {
					my $t = $1;
					my $eol = $2;
					$t =~ s/\&.+?;//g;
					$t =~ s/<.+?>//g;
					emit($irc, $who, $t);
					if ($eol eq "</h3>") {
						$next = 0;
						last;
					} else {
						$next = 1;
					}
				}
			} elsif ($action eq "zip") {
				my $msg;
				#if (m/(<cite>|<a href.+class="yschttl"|<h3 class="yschttl">|<h3><a href=)/) {
				if ($line =~ m/<h3 class="yschttl">(The <b>zip.+?)<\/h3/) {
					$msg = $1;
				} elsif ($line =~ m/<a href=".*(http(%3a|:)\/\/maps.+?)" class="yschttl spt">(.+?)Map<\/a>/) {
					my $l = $1;
					my $what = $3;
					$l =~ s/%3a/:/g;
					$msg = "$what: " . decode_entities($l);
				}
				$msg =~ s/<.+?>//g;
				$msg =~ s/View All.*//;
				$msg =~ s/^\s*//;
				if ($msg !~ m/http:/) {
					$msg =~ s/:/: /;
				}
				$msg =~ s/\&.+?;//g;
				emit($irc, $who, $msg);
				last;
			} elsif ($action eq "quote") {
				if ($line =~ m/<ul class="quote">/) {
					my (@strings, $m, $f);
					if ($msg) {
						my $diff = 5 - length($msg);
						if ($diff > 0) {
							$msg .= " " x $diff;
						}
						$m = "$msg: ";
					} else {
						$m = "";
					}
					my $f = " @>>>>>>>>  @######.## @####.##%";
					$line =~ s/.*<ul class="quote">(.+)<li class="time">([^<]+)<.*/$1 $2/;
					$line =~ s/<.+?>/ /g;
					$line =~ s/^\s+//;
					$line =~ s/\&.+?;//g;
					$line =~ s/\(//g;
					$line =~ s/\)//g;
					@strings = split(/\s+/, $line, 4);
					$^A = "";
					formline($f, $strings[0], $strings[1], $strings[2]);
					emit($irc, $who, $m . $^A . "  " . $strings[3]);
					last;
				}
			} elsif ($action eq "capital") {
				if ($line =~ m/<h3><a .*class="yschttl spt".*>.*Capital.*: ([^<]+) - .*<\/a>/) {
					my $c = $1;
					$c =~ s/\&.+?;//g;
					$c =~ s/<.+?>//g;
					emit($irc, $who, $c);
					last;
				}
			} elsif ($action =~ m/define|synonym/) {
				if ($line =~ m/<h3><a class="yschttl spt"\s+href=".+?">(.*?)<\/div>/) {
					my $def = $1;
					$def =~ s/.*<dl>//;
					$def =~ s/<dd>.*//;
					$def =~ s/\&.+?;//g;
					$def =~ s/<.+?>//g;
					emit($irc, $who, $def);
					last;
				}
			} elsif ($line =~ m/<cite><a href/i) {
				$line =~ s/\&.+?;//g;
				$line =~ s/<.+?>//g;
				$cite = $_;
			} elsif ($line =~ m/<d[dl]>(.*?)<\/d[dl]>/) {
				$line =~ s/^.*?<d[ld]>//;
				$line =~ s/<\/d[ld]>.*//;
				$line =~ s/\&.+?;//g;
				$line =~ s/<.+?>//g;
				$cite="";
				emit($irc, $who, $line);
				last;
			}
		}
	}

	if ($cite) {
		emit($irc, $who, $cite);
	}

	return if $found;

	if ($action =~ m/quote/) {
		emit($irc, $who, "\"$word\"");
	}

	if (m/$botnick/ && ($action =~ m/define/)) {
		emit($irc, $who,
			"NOUN: the greatest IRC bot ever conceived by mankind");
	}
}

# function : do_ud
# purpose  : get quotations from the urban dictionary
# inputs   : who to respond to and a term to look up
# returns  : results (if any) are sent to the recipient

sub do_ud($$) {
	my ($who, $word) = @_;
	my ($query);
	my $found = 0;
	my $tfound = 0;

	$word =~ s/\s/+/g;

	$query = $methods{"ud"} . $word;
	my $term = decode_entities($word);

	foreach my $line (getContent($query)) {
		if ($line =~ m/<td class='word'>/) {
			$tfound = 1;
			next;
		}

		if ($tfound) {
			$term = dehtmlify($line);
			$tfound = 0;
			next;
		}

		if ($line =~ m/<div class="definition">(.*)<\/div>/) {
			my $def = $1;
			$found++;
			$def =~ s/<.+?>//g;
			$def =~ s/\&.+?;//g;
			$def =~ s/^\s*/  * /;

			if ($found == 1) {
				emit($irc, $who, "Urban Definition for '$term':");
			}

			if ($found > 3) {
				emit($irc, $who, "  ...");
				last;
			} else {
				emit($irc, $who, $def);
			}
		}
	}

	if (!$found) {
		$term =~ s/\+/ /g;
		emit($irc, $who, "Urban Dictionary is useless when it comes to '$term'.");
	}
}

# function : do_validate
# purpose  : perform a validate lookup
# inputs   : who to respond to, nick, a validate to look up
# returns  : results (if any) are sent to the recipient

sub do_validate($$) {
	my ($who, $url) = @_;
	my ($query, $msg);
	my $found = 0;
	my $next = 0;

	$query = $methods{"validate"} . "+" . encode_entities($url);

	foreach my $line (getContent($query)) {
		next if (m/^\s*$/);

		if ($line =~ m/<!-- valid\/invalid header and revalidation table -->/) {
			$found = 1;
		}

		if ($found) {
			if ($line =~ m/<h2.*class="(?:in)?valid">/) {
				$line =~ s/<.+?>//g;
				$line =~ s/^\s*//;
				$msg = decode_entities($line);
				if ($line !~ m/<\/h2>/) {
					$next = 1;
				}
				next;
			}

			if ($line =~ m/<td colspan="2" class="(?:in)?valid"/) {
				$msg = "Result: ";
				$next = 1;
				next;
			}

			if ($next) {
				my $pad = $msg ? " " : "";
				$line =~ s/<.+?>//g;
				$line =~ s/^\s*//;
				$msg .= $pad . decode_entities($line);
				emit($irc, $who, $msg);
				$next = 0;
				$msg = "";
			}

			if ($line =~ m/<\/h2>/) {
				$next = 0;
			}


			if ($line =~ m/<th><label title="Address of Page to Validate"/) {
				last;
			}
		}
	}
}


# function : do_brick
# purpose  : doink a user in the head with a brick
# input    : requestor, where, brickee, channel

sub do_brick {
	my ($nick, $where, $brickee, $channel) = @_;

	if ($brickee =~ m/^(my)self$/) {
		$brickee = $nick;
	}

	if ($bricks{$nick} == 0) {
		do_quip($channel, $nick);
		$irc->yield( 'kick' => $channel => $nick => "*clonk*");
		return;
	}

	$bricks{$nick} = $bricks{$nick} - 1;

	if ($brickee eq "$botnick") {
		emit($irc, $channel, "Bad $nick, bad!");
		$irc->yield( 'ctcp' => $channel => "ACTION whacks $nick on the nose with a rolled up newspaper.");
		return;
	}

	$channel = lc($channel);
	if (!defined($CHANNELS{$channel}{"users"}{$brickee})) {
		$irc->yield( 'ctcp' => $channel => "ACTION shoots a brick off into the ether.");
	} else {
		$irc->yield( 'ctcp' => $channel => "ACTION doinks $brickee in the head with a brick.");
		$bricks{$brickee} = $bricks{$brickee} + 1;
	}

	if ($bricks{$brickee} > 40) {
print "$brickee has too many bricks!\n";
		do_quip($channel, $brickee);
		$irc->yield( 'kick' => $channel => $brickee => "*clonk*");
	}
}

# function : do_bugmenot
# purpose  : get login for a given website
# inputs   : who to respond to, website
# returns  : results (if any) are sent to the recipient

sub do_bugmenot($$) {
	my ($who, $site) = @_;
	my ($query);
	my $found = 0;

	$query = $methods{"bugmenot"} . $site;

	foreach my $line (getContent($query)) {
		if ($line =~ m/<h2>Account Details<\/h2>/i) {
			$found = 1;
			next;
		}

		if ($found) {
			if ($line =~ m/<tr><th>Username/i) {
				$line =~ s/<.+?>//g;
				$line =~ s/\&.+?;//g;
				$line =~ s/\s\s+//g;
				emit($irc, $who, $line);
				next;
			}
			if ($line =~ m/<tr><th>Password/) {
				$line =~ s/<.+?>//g;
				$line =~ s/\&.+?;//g;
				$line =~ s/\s\s+//g;
				emit($irc, $who, $line);
				next;
			}
			if ($line =~ m/<tr><th>Stats/) {
				$line =~ s/<.+?>//g;
				$line =~ s/\&.+?;//g;
				$line =~ s/\s\s+//g;
				$line =~ s/Stats/Stats /;
				emit($irc, $who, $line);
				last;
			}
		}
	}
}

# function : do_bible
# purpose  : get quotations from the bible
# inputs   : who to respond to and a passage to look up
# returns  : results (if any) are sent to the recipient

sub do_bible($$) {
	my ($who, $passage) = @_;
	my ($query);
	my $found = 0;

	$passage =~ s/\s/+/g;

	$query = $methods{"bible"} . $passage;

	foreach my $line (getContent($query)) {
		if ($line =~ m/span id="en-NIV/) {
			$line =~ s/<\/p>.*//g;
			$line =~ s/<.+?>//g;
			$line =~ s/\&.+?;//g;
			emit($irc, $who, $line);
			last;
		}
	}
}

# function : do_tz
# purpose  : display timezone info
# inputs   : who to respond to, up to two timezones
# returns  : results (if any) are sent to the recipient

sub do_tz($$;$) {
	my ($who, $t1, $t2) = @_;
	my ($tz, $tzfile);
	my $tz = $ENV{'TZ'};

	foreach my $t ($t1, $t2) {
		if (defined($t)) {
			$tzfile = "/usr/share/zoneinfo/$t";
			if (! -f $tzfile) {
				emit($irc, $who, "No such timezone: $t");
				return;
			}
		}
	}

	if ($t2) {
		my $lt1 = `TZ=$t1 date +\%z`;
		my $lt2 = `TZ=$t2 date +\%z`;
		my $diff = $lt2 - $lt1;
		emit($irc, $who, (($diff > 0) ? "+$diff" : "$diff"));
	} else {
		$ENV{'TZ'} = $t1;
		$t1 = localtime();
		emit($irc, $who, $t1);
	}
	$ENV{'TZ'} = $tz;
}

# function : do_thanks
# purpose  : say you're welcome
# inputs   : who to respond to, $nick

sub do_thanks($$) {
	my ($channel, $nick) = @_;

	my @welcome = (
		"You're welcome",
		"Bitte schön",
		"De nada",
		"De rien",
		);

	emit($irc, $channel, $welcome[int(rand(scalar(@welcome)))] . ", $nick!");
}

# function : do_throttle
# purpose  : show what commands are throttled and for how long

sub do_throttle($$;$) {
	my ($who, $userhost, $help) = @_;
	my @throttles;
	my $now = time();
	my %lseen;

	if ($help) {
		emit($irc, $who, "!throttle            -- show all your throttles");
		emit($irc, $who, "!throttle <cmd>      -- throttle yourself for the given command");
		emit($irc, $who, "!throttle <cmd> all  -- throttle everybody for the given command");
		emit($irc, $who, "(Note that I will happily let you throttle yourself for non-existent throttles.)");
		emit($irc, $who, "!throttle (cm3mon|escalations|[ct]?gs2|unowned)        -- display timeout for given alert");
		emit($irc, $who, "!throttle (cm3mon|escalations|[ct]?gs2|escalations) [+-]N  -- set timeout for given alert");
		emit($irc, $who, "After N has been reached, I will return to the default alert interval of $autoping seconds.");
		return;
	}

	foreach my $k ($userhost, "all") {
		foreach my $cmd (keys %{$throttle{$k}}) {
			next if ($lseen{$cmd});

			$lseen{$cmd} = 1;
			my $diff = 600 - ($now - $throttle{$k}{$cmd});
			if ($diff < 0) {
				$diff = 0;
			}

			if ($diff == 0) {
				delete $throttle{$k}{$cmd};
				next;
			}
			push(@throttles, "$cmd => $diff");
		}
	}

	emit($irc, $who, join("; ", @throttles));
}

# function : do_title
# purpose  : get the title of a URL
# inputs   : a URL
# returns  : <title>(.*)</title>

sub do_title($) {
	my ($url) = @_;

	foreach my $line (getContent($url)) {
		if ($line =~ m/(<title>.*<\/title>)/i) {
			return dehtmlify($1);
		}
	}
}


# purpose : print RIR stats
# inputs  : who, optional name of RIR

sub do_ipv4($$) {
	my ($who, $rir) = @_;

	if (!$rir) {
		emit($irc, $who, do_countdown("ipv4"));
		return;
	} else {
		$rir =~ s/\s//g;
	}

	my $json = new JSON;
	my $results;
	eval {
		$results= $json->allow_nonref->utf8->relaxed->escape_slash->loose->allow_singlequote->allow_barekey->decode(getContent($methods{"ipv4"}));
	};
	if ($@) {
		emit($irc, $who, "No info found for $rir.");
		return;
	}
	my %stats = %{$results};
	if (scalar(keys(%stats))) {
		my $num = lc($rir) . "24s";
		my $percent = lc($rir) . "Percent";

		my $number = $stats{$num};
		$number =~ s/(\d{1,3}?)(?=(\d{3})+$)/$1,/g;
		emit($irc, $who, uc($rir) . ": " . $stats{$percent} . "% /24s free ($number)");
	}
}


# function : get_ipv4_countdown
# purpose  : retrieve epoch timestamp of predicted ipv4 countdown

sub get_ipv4_countdown(;) {
	my ($what) = @_;
	my $url = $methods{"ipv4countdown"};

	$what = $what ? uc($what) : "RIR";

	foreach my $line (getContent($url)) {
		if ($line =~ m/Projected $what Unallocated Address Pool Exhaustion: (.*)</) {
			my $t = str2time($1);
			return $t;
		}
	}
	return undef;
}

# function : get_update
# purpose  : get a string indicating the uptime

sub get_uptime() {
	my ($s1, $m1, $h1, $d1, $M1, $y1) = localtime($^T);
	my ($s2, $m2, $h2, $d2, $M2, $y2) = localtime;
	my ($Dd,$Dh,$Dm,$Ds) = Delta_DHMS($y1 + 1900, $M1 + 1, $d1, $h1, $m1, $s1,
			$y2 + 1900, $M2 + 1, $d2, $h2, $m2, $s2);

	my $uptime = "$Dd day" . ($Dd != 1 ? "s" : "") .
			", $Dh hour" . ($Dh != 1 ? "s" : "") .
			", $Dm minute" . ($Dm != 1 ? "s" : "") .
			" and $Ds second" . ($Ds != 1 ? "s" : "") . ".";

	return $uptime;
}

# function : do_ohiny
# purpose  : get a random quote from overheardinny
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_ohiny($) {
	my ($who) = @_;
	my ($url);
	my $found = 0;

	$url = $methods{"ohiny"};

	foreach my $line (getContent($url)) {
		if ($line =~ m/<span class="speakerline">/) {
			my @lines = split(/<br\/>/, $line);
			foreach my $l (@lines) {
				$l =~ s/^\s*//;
				$l =~ s/<.+?>//g;
				$l =~ s/\&.+?;//g;
				$l =~ s/Overheard by:.*//;
				$found++;
				if ($found > 8) {
					last;
				}
				emit($irc, $who, $l);
			}
			last;
		}
	}
}

# function : do_omg
# purpose  : get the first 'news' item from omg.y.c
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_omg($) {
	my ($who) = @_;
	my ($url);
	my $found = 0;
	my %omgs;
	my ($title, $desc);

	$url = $methods{"omg"};

	foreach my $line (getContent($url)) {

		if ($line =~ m/<title><!\[CDATA\[(.+)?\]\]><\/title>/) {
			$title = decode_entities($1);
		}
		if ($line =~ m/<description><!\[CDATA\[(.+)?\]\]><\/description>/) {
			$desc = $1;
			$desc =~ s/<.+?>//g;
		}

		if ($title && $desc) {
			$omgs{$title} = decode_entities($desc);
		}

		if ($line =~ m/<\/item>/) {
			$title = "";
			$desc = "";
		}
	}

	my @os = keys(%omgs);
	$title = $os[int(rand(scalar(@os)))];
        emit($irc, $who, $title);
        emit($irc, $who, $omgs{$title});
}

# function : do_dist
# purpose  : look up packages on dist
# inputs   : who to respond to, search term
# returns  : results (if any) are sent to the recipient

sub do_dist($$) {
	my ($who, $pkg) = @_;
	my ($url);
	my $lines = 0;

	$url = $methods{"dist"} . "$pkg";

	foreach my $line (getContent($url)) {
		if ($line =~ m/^(<font.+?>)*<b><a href='(.+?)'>/) {
			$lines++;
			emit($irc, $who, $2);
			if ($lines > 4) {
				emit($irc, $who, "... and more ...");
				last;
			}
		}
	}
}

# function : do_fileinfo
# purpose  : find out what a file is

sub do_fileinfo($$;$) {
	my ($who, $extension, $info) = @_;
	my ($url);
	my ($lhs, $desc, $progs, $lines, $maxl);
	my $next = 0;

	$maxl = 10;

	my $lines = "<b>File Type|Category";

	if ($info) {
		if ($info eq "desc") {
			$desc = 1;
			$lines = "File Description";
		} elsif ($info eq "prog") {
			$progs = 1;
			$lines = "<b>Program";
			$maxl = 2;
		}
	}

	$url = $methods{'fileinfo'} . encode_entities($extension);

	foreach my $line (getContent($url)) {

		if ($line =~ m/^<td.*?>($lines)/) {
			my $what = $1;

			$next = 1;

			if ($what eq "File Description") {
				$maxl = 2;
				next;
			}

			$what =~ s/^\s*//;
			$what =~ s/<.+?>//g;

			my $msg = decode_entities($what);
			my $diff = $maxl - length($msg);
			if ($diff > 0) {
				$msg .= " " x $diff;
			}
			$lhs = "$msg: ";
			next;
		}

		if ($progs) {
			if ($line =~ m/<table class="programs".+?>/) {
				$next = 1;
				$lhs = " ";
				next;
			}
		}

		if ($next) {

			if ($line =~ m/<\/tr>/) {
				$lhs = "";
				$next = 0;
				next;
			}

			$line =~ s/^\s*//;
			$line =~ s/<.+?>//g;

			if (!$line) {
				next;
			}

			emit($irc, $who, $lhs . decode_entities($line));
			$lhs = " " x $maxl;
		}

		if ($line =~ m/!-- google_ad_section_end -->/) {
			last;
		}
	}
}

# function : do_futurama
# purpose  : display a futurama quote
# inputs   : whom to respond to

sub do_futurama($) {
	my ($who) = @_;

	my $found = 0;
	my @quotes;
	my @lines;

	my $url = $methods{'futurama'};

	foreach my $line (getContent($url)) {
		if ($line =~ m/>ALL THE QUOTES</) {
			$found = 1;
			next;
		}

		if ($found) {
			if ($line =~ m/^\s*<\/p>/) {
				last;
			}
			if ($line =~ m/^\s*<br>$/) {
				next;
			}

			if ($line =~ m/^\s*(-+|<p>)<br>$/) {
				if (scalar(@lines) && (scalar(@lines) < 5)) {
					my @tmp = @lines;
					push(@quotes, \@tmp);
				}
				@lines = ();
				next;
			}

			push(@lines, dehtmlify($line));
		}
	}

	if (scalar(@quotes)) {
		my $n = int(rand(scalar(@quotes)));
		my @quote = @{$quotes[$n]};
		foreach my $line (@quote) {
			emit($irc, $who, $line);
		}
	}
}


# function : do_flight
# purpose  : get flight information

sub do_flight($$) {
	my ($who, $flight) = @_;
	my ($url, %results);
	my $found = 0;
	my $num = 0;

	my ($al, $num) = split(/\s+/, $flight, 2);

	$url = $methods{'flight'} . "aln_name=$al&flt_num=$num";

	foreach my $line (getContent($url)) {
		if ($line =~ m/<td width=100%><b>([^<]+)\s*<\/b>/i) {
			emit($irc, $who, "$1");
			next;
		}

		if ($line =~ m/td bgcolor=#eaeaea><B>Arrival/) {
			$found = 1;
			next;
		}

		if ($found) {
			if ($line =~ m/(<\/table name=flight_info>|Airline Notes:)/) {
				last;
			}

			if ($line =~ m/td valign=top align=right nowrap/) {
				my ($which, $len, $diff);
				$line =~ s/^\s*//;
				$line =~ s/<.+?>//g;
				$line =~ s/\&.+?;//g;
				next if ($line =~ m/^\s*$/);
				$which = $_;
				$len = length($which);
				$diff = 15 - $len;
				$which .= " " x $diff;
				push(@{$results{$num}}, $which);
			}
			if ($line =~ m/<td/) {
				$line =~ s/^\s*//;
				$line =~ s/<.+?>//g;
				$line =~ s/&nbsp;/N\/A/g;
				$line =~ s/\&.+?;//g;
				next if ($line =~ m/^\s*$/);
				push(@{$results{$num}}, $line);
			}
			if ($line =~ m/\/tr>/) {
				$num++;
			}
		}
	}

	foreach my $k (sort(keys %results)) {
		my $f = "@>>>>>>>>>>>>>> @<<<<<<<<<<<<<<<<<<<<<<<<   @<<<<<<<<<<<<<<<<<<<<<<<<<";
		$^A = "";
		formline($f, @{$results{$k}});
		next if ($^A =~ /^\s+$/);
		emit($irc, $who, $^A);
	}
}

# function : do_charliesheen
# purpose  : get a quote from Charlie Sheen

sub do_charliesheen($) {
	my ($who) = @_;

	my $url = $methods{'charliesheen'};

	foreach my $line (getContent($url)) {
		if ($line =~ m/<blockquote id="quote">(.*)<\/blockquote>/) {
			emit($irc, $who, dehtmlify($1));
			return;
		}
	}
}

# function : do_fact
# purpose  : get a fact about somebody
# inputs   : who to respond to, who to query about
# returns  : results (if any) are sent to the recipient

sub do_fact($$) {
	my ($who, $person) = @_;
	my ($url);
	my $found = 0;

	$url = $methods{$person};

	foreach my $line (getContent($url)) {
		if ((($person !~ m/schneier/i) && ($line =~ m/\s*<p>/i)) ||
			($line =~ m/\s*<p class="fact">/i)) {
			$line =~ s/^\s*//;
			$line =~ s/<.+?>//g;
			$line =~ s/\&.+?;//g;
			emit($irc, $who, $line);
			last;
		}
	}
}

# function : do_bush
# purpose  : display a bushism
# inputs   : who to respond to, who to query about
# returns  : results (if any) are sent to the recipient

sub do_bush($) {
	my ($who) = @_;
	my ($url);
	my $found = 0;
	my (@bushisms, $quote);

	$url = $methods{'bush'};

	foreach my $line (getContent($url)) {
		last if ($line =~ m/<p><a name="Disputed" id="Disputed"><\/a><\/p>/);
		if ($line =~ m/^<li>([^<]+)$/i) {
			$found++;
			$line =~ s/^\s*//;
			$line =~ s/<.+?>//g;
			$line =~ s/\&.+?;//g;
			push(@bushisms, $line);
		}
	}

	$quote = $bushisms[int(rand($found))];
	emit($irc, $who, "<george_w_bush> $quote");
}

# function : do_mom
# purpose  : insult yo momma
# inputs   : who to respond to, nickname, who's mom to insult
# returns  : results (if any) are sent to the recipient

sub do_mom($) {
	my ($who, $nick, $mom) = @_;
	my (@content, $url);
	my $found = 0;

	$url = $methods{"yourmom"};

	@content = getContent($url);
	my $mom = $content[int(rand(scalar(@content)))];
	$mom =~ s/Yo momma/$nick\'s mom/g;
	emit($irc, $who, $mom);
}

# function : do_php
# purpose  : look up a php function definition
# inputs   : function name

sub do_php($$) {
	my ($who, $func) = @_;
	my ($url);
	my $cfound = 0;
	my $dfound = 0;
	my $description = "";
	my $comment = "";

	$func =~ s/_/-/g;

	$url = $methods{"php"} . "$func.php";

	foreach my $line (getContent($url)) {
		if ($line =~ m/methodsynopsis dc-description/) {
			$dfound = 1;
			next;
		}

		if ($dfound) {
			$description .= $description ? " " : "";
			$description .= dehtmlify($line);
		}

		if ($line =~ m/para rdfs-comment/) {
			$cfound = 1;
			$dfound = 0;
		}

		if ($cfound) {
			$comment .= $comment ? " " : "";
			$comment .= dehtmlify($line);
		}

		if ($line =~ m/<\/p>/) {
			$dfound = 0;
			$cfound = 0;
		}

		if ($line =~ m/class="refsect1 parameters"/) {
			last;
		}
	}

	if ($description) {
		emit($irc, $who, $description);
		emit($irc, $who, $url);
	}
	if ($comment) {
		emit($irc, $who, $comment);
	}
}

# function : do_ping
# purpose  : say whether or not a given host can be pinged
# inputs   : who, hostname, channel

sub do_ping($$$) {
	my ($who, $host, $channel) = @_;
	my $msg;
	my $alive = ping(host => "$host");

	if ($host eq "$botnick") {
		emit($irc, $who, "I'm alive!");
		return;
	}

	$channel = lc($channel);
	if (defined($CHANNELS{$channel}{"seen"}{$host})) {
		my $date = str2time($CHANNELS{$channel}{"seen"}{$host});
		my $now = time();
		my $diff = $now - $date;
		if ($diff < 60) {
			$msg = "Come on, you just saw $host say something.";
		} elsif ($diff < 600) {
			$msg = "$host is alive and chatty.";
		} elsif ($diff < 3600) {
			$msg = "$host is around.";
		} elsif ($diff < 86400) {
			$msg = "$host seems asleep.";
		} else {
			$msg = "Who knows what's going on with $host.";
		}
	} else {
		$msg = $alive ? "$host is alive." : "Unable to ping $host.";
	}
	emit($irc, $who, $msg);
}

# function : do_port
# purpose  : look up a specific port on dist
# inputs   : who to respond to, search term
# returns  : results (if any) are sent to the recipient

sub do_port($$) {
	my ($who, $port) = @_;
	my ($url);
	my @os = ( "freebsd", "rhel-3.x", "rhel-4.x" );
	my $found = 0;
	my $item = 0;
	my $first;

	$first = substr(lc($port), 0, 1);
	if ($first !~ /[a-zA-Z]/) {
		$first = "_other";
	}

	foreach my $o (@os) {
		$url = $methods{"port"} . $o . "/" . $first . ".html";

		if ($o eq "freebsd") {
			$o .= " ";
		}

		foreach my $line (getContent($url)) {
			if ($line =~ m/<a name="$port".*>$port<\/a>.*<b>([^<]+)<\/b><\/small><\/td>/i) {
				$found = 1;
				emit($irc, $who, "$o: $1");
			}
		}
	}
}

# function : do_pydoc
# purpose  : look up documentation via pydoc
# inputs   : string

sub do_pydoc($$) {
	my ($who, $func) = @_;
	my ($next);

	open(P, "/usr/local/bin/pydoc $func 2>&1|") or do {
print STDERR "Unable to open pipe to pydoc $func!\n";
		return 1;
	};

	while (<P>) {
		if (m/^\s*$/) {
			$next = 0;
		}

		if (m/^(NAME|MODULE DOCS|DESCRIPTION)$/) {
			$next = 1;
			next;
		}

		if ($next) {
			s/^\s*//;
			emit($irc, $who, $_);
		}
	}
	close(P);

	return 0;
}

# function : do_perldoc
# purpose  : look up documentation via perldoc
# inputs   : string

sub do_perldoc($$) {
	my ($who, $func) = @_;
	my ($found);

	$found = 0;

	open(P, "/usr/local/bin/perldoc -t -f $func 2>/dev/null|") or do {
print STDERR "Unable to open pipe to perldoc $func!\n";
		return 1;
	};

	while (<P>) {
		if ($found > 7) {
			emit($irc, $who, "...");
			last;
		}
		if (m/^\s*$/) {
			last;
		}
		s/^\s*//;
		emit($irc, $who, $_);
		$found++;
	}
	close(P);

	if ($found) {
		return 0;
	}

	open(P, "/usr/local/bin/perldoc -t perlmodlib 2>&1|") or do {
print STDERR "Unable to open pipe to perldoc $func!\n";
		return 1;
	};

	while (<P>) {
		if (m/^\s*$func(\s+|$)/i) {
			$found++;
		}

		if ($found) {
			if ($found > 7) {
				emit($irc, $who, "...");
				last;
			}
			if (m/^\s*$/) {
				last;
			}
			s/^\s*//;
			emit($irc, $who, $_);
			$found++;
		}
	}
	close(P);

	return 0;
}


# function : do_dialin
# purpose  : display a persons dialin info
# inputs   : who to respond to, search term
# returns  : results (if any) are sent to the recipient

sub do_dialin($$) {
	my ($who, $person) = @_;
	my ($url, $msg, $dial);
	my %dialinfo;

	$url = $methods{"dialin"};
	foreach my $dial (getContent($url)) {
		if ($dial =~ m/(.*) => (.*)/) {
			$dialinfo{$1} = $2;
		}
	}

	if ($person =~ /^[0-9]+$/) {
		foreach my $d (keys(%dialinfo)) {
			if ($person == $dialinfo{$d}) {
				emit($irc, $who, "$person: $d");
				return;
				# NOTREACHED
			}
		}
	}

	if (!$person || !$dialinfo{$person}) {
		if ($person =~ m/your ?mom/) {
			emit($irc, $who, "1 800 GOOD TIME");
			return;
		}
		my @peeps = keys(%dialinfo);
		emit($irc, $who, "I only know the following dialins:");
		emit($irc, $who, join(" ", sort(@peeps)));
		return;
		# NOTREACHED
	} else {
		$dial = $dialinfo{$person};
	}

	emit($irc, $who, "888.371.8922; #$dial");
}

# function : do_eightball
# purpose  : generate a random eightball answer
# inputs   : who to respond to, nick
# returns  : results (if any) are sent to the recipient

sub do_eightball($$) {
	my ($who, $nick) = @_;
	my (@content, $url);

	$url = $methods{"eightball"};
	@content = getContent($url);
	emit($irc, $who, "$nick, my 8-ball says " . $content[int(rand(scalar(@content)))]);
}

# function : do_dinner
# purpose  : generate a random dinner suggestion
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_dinner($) {
	my ($who) = @_;
	my (@content, $url);

	$url = $methods{"dinner"};
	@content = getContent($url);
	emit($irc, $who, $content[int(rand(scalar(@content)))]);
}

# function : do_wolfram
# purpose  : query wolfram alpha
# inputs   : who to respond to, search query
# returns  : results (if any) are sent to the recipient

sub do_wolfram($$) {
	my ($who, $query) = @_;

	my ($count, $url, %results);

	$count = 0;

	$query =~ s/\s+/+/g;
	$url = $methods{"wolfram"} . $query;

LOOP:	while (1) {
	foreach my $line (getContent($url)) {
		if ($line =~ m/>Using closest Wolfram\|Alpha interpretation: <[^>]*>(.*?)<\/span>/) {
			my $term = $1;
			$count++;
			$term =~ s/\s+/+/g;
			$url = $methods{"wolfram"} . $term;
			if ($count > 3) {
				last;
			}
			next LOOP;
		}

		if ($line =~ m/>Input interpretation:<.*?alt="(.*?)"/) {
			emit($irc, $who, "Wolfram Alpha thinks you searched for '" . dehtmlify($1) . "':");
			next;
		}

		if ($line =~ m/<h2>(.*?):<.*?alt="(.*?)"/) {
			$results{$1} = $2;
		}
		if ($line =~ m/<\/html>/) {
			last LOOP;
		}
	}
	}

	my $n = 0;
	if (scalar(keys(%results))) {
		foreach my $r (keys(%results)) {
			my $val = $results{$r};
			my @lines = split(/\\n/, $val);
			if (scalar(@lines) > 1) {
				emit($irc, $who, "$r:");
				foreach my $l (@lines) {
					emit($irc, $who, "    " . dehtmlify($l));
					$n++;
				}
			} else {
				emit($irc, $who, "$r: " . dehtmlify($val));
				$n++;
			}
			if ($n > 4) {
				emit($irc, $who, "...");
				last;
			}
		}
	} else {
		emit($irc, $who, "Sorry, nada.");
	}
}


# function : do_week
# purpose  : display the current week or the week corresponding to the
#            given input
# inputs   : who to respond to, a number or a date string

sub do_week($$) {
	my ($who, $input) = @_;
	my $result;

	if ($input) {
		$input =~ s/\s*//g;
		my $time;
		if (($input =~ m/^\d+$/) && ($input > 0) &&
			($input <= Weeks_in_Year(This_Year()))) {
			my ($y, $m, $d) = Monday_of_Week($input, This_Year());
			$result = sprintf("This year's week #$input starts on $y-%02d-%02d.", $m, $d);
		} elsif ($input =~ m/(\d\d\d\d)-(\d\d)-(\d\d)$/) {
			my $y = $1;
			my $m = $2;
			my $d = $3;
			my $t = strftime($input);
			if ($t) {
				my ($week, $year) = Week_of_Year($y, $m, $d);
				$result = $week;
				if ($year != $y) {
					$result = "$week (of $year)";
				}
			}
		}
	} else {
		$result = strftime("%W", localtime());
	}
	if (!$result) {
		$result = "I can't figure that out, sorry.";
	}

	emit($irc, $who, $result);
}

# function : do_wotd
# purpose  : display the word of the day
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_wotd($) {
	my ($who) = @_;
	my ($url);
	my ($intro, $word, $pronunc, $func, @defs);
	my $found = 0;
	my $lines = 0;

	$url = $methods{"wotd"};

	foreach my $line (getContent($url)) {
		if ($line =~ m/main_entry_word">(.*)<\/strong>.*<span class="pron">(.*)<\/span>.*word_function">(.*)<\/p>.*?(<div class="scnt">.*class="hdrleaders">)EXAMPLES/) {
			my $word = $1;
			my $pron = $2;
			my $func = $3;
			my @defs = split(/div class="scnt">/, $4);
			emit($irc, $who, "$word -- $pron -- $func" );
			foreach my $d (@defs) {
				$d =~ s/<$//;
				if (!$d) {
					next;
				}
				emit($irc, $who, " " . dehtmlify(decode_entities($d)));
			}
			last;
		}
	}
}

# function : do_wwipind
# purpose  : what would inet_ntop(inet_pton(addr)) do
# inputs   : who to respond to, an ipv6 address

sub do_wwipind($$) {
	my ($who, $addr) = @_;

	my $i = inet_pton(AF_INET6, "$addr");
        if (!$i) {
                emit($irc, $who, "'$addr' is not a valid IPv6 address");
                return;
        }

        my $a = inet_ntop(AF_INET6, $i);
        if (!$a) {
                emit($irc, $who, "'$addr' was grokked by inet_pton, but couldn't be converted back - wtf?");
		return;
        }
	emit($irc, $who, "$a");
}


# function : do_woot
# purpose  : return item of the day on woot.com
# inputs   : who to respond to

sub do_woot($) {
	my ($who) = @_;
	my ($query);
	my ($found, $title, $price, $link);

	$query = $methods{"woot"};

	foreach my $line (getContent($query)) {
		if ($line =~ m/<div class="productDescription">/) {
			$found = 1;
		}

		if ($found) {
			if ($line =~ m/<div class="footer">/) {
				last;
			}
			if ($line =~ m/<h2[^>]*>(.*)<\/h2>/) {
				$title = decode_entities($1);
				next;
			}

			if ($line =~ m/<span class="amount">([^<]+)<\/span>/) {
				$price = "\$" . decode_entities($1);
				next;
			}

			if ($line =~ m/href="\/(WantOne.aspx.*)">I want one!/i) {
				$link = $methods{"woot"} . "$1";
				last;
			}
		}
	}

	if ($title) {
		emit($irc, $who, "$title: $price");
		if ($link) {
			emit($irc, $who, $link);
		}
	}
}

# function : do_bing
# purpose  : return first URL of a bing search
# inputs   : who to respond to, search term

sub do_bing($$) {
	my ($who, $term) = @_;
	my ($query);
	my $found = 0;

	$term =~ s/\s/\+/g;
	$query = $methods{"bing"} . "$term";

	foreach my $line (getContent($query)) {
		if ($line =~ m/<div id="results">.*?<h3><a href="(.+?)"/) {
			# bing results have &amp; etc in the urls, which
			# is all sorts of weird, so let's try to fix that
			# for improved IRC clickability
			my $url = decode_entities($1);
			emit($irc, $who, "$url");
			if (length($url) > 60) {
				emit($irc, $who, do_tiny("$url"));
			}
			last;
		}
	}
}

# function : do_yelp
# purpose  : lookup stuff on yelp
# inputs   : who to respond to, invoker's nick, a string

sub do_yelp($$$) {
	my ($who, $nick, $search) = @_;

	my ($query, $what, $where);

	if ($search =~ m/^"([^"]+)"(.*)/) {
		$what = $1;
		$where = $2;
		$where =~ s/^"//;
		$where =~ s/"$//;
	} else {
		($what, $where) = split(/\s+/, $search, 2);
	}

	$what = uri_escape($what);
	$where = uri_escape($where);

	$query = $methods{"yelp"};
	$query =~ s/<what>/$what/;
	$query =~ s/<where>/$where/;

	my $found = 0;
	my ($link, $name, $rating);
	foreach my $line (getContent($query)) {
		if ($line =~ m/<span class="address">/) {
			$found = 1;
		}
		if ($found) {
			if ($line =~ m/<a href="(\/biz\/.*)">(.*)<\/a>/) {
				$link = $1;
				$name = $2;
				next;
			}
			if ($line =~ m/alt="([0-9\.]+) star rating"/) {
				$rating = $1;
				next;
			}
			if ($line =~ m/<\/span>/) {
				last;
			}
		}
	}
	if ($name) {
		emit($irc, $who, decode_entities($name) . " ($rating)");
	}
	if ($link) {
		emit($irc, $who, "http://www.yelp.com$link");
	}
}


# function : do_y
# purpose  : return first URL of a y! search
# inputs   : who to respond to, search term

sub do_y($$) {
	my ($who, $term) = @_;
	my ($query);

	$term =~ s/\s/\+/g;
	$query = $methods{"y"} . "$term";

	foreach my $line (getContent($query)) {
		if ($line =~ m/<ol start="1".*<h3><a class="yschttl spt" href="(.+?)"/) {
			my $url = $1;
			$url =~ s|.*\*\*http%3a(.*)|http:$1|;
			emit($irc, $who, "$url");
		               if (length($1) > 60) {
				emit($irc, $who, do_tiny("$url"));
			}
			last;
		}
	}
}

# function : do_ybuzz
# purpose  : return first 5 items from y! buzz
# inputs   : who to respond to, optional "movers" flag

sub do_ybuzz($;$) {
	my ($who, $movers) = @_;
	my $feed = $movers ? "ybuzz_movers" : "ybuzz";

	fetch_rss_feed($feed, $who, 5);
}

# function : do_slashdot
# purpose  : return first 5 items from /.; optional number of items
#            to return
# inputs   : who to respond to

sub do_slashdot($$;$) {
	my ($who, $nick, $num) = @_;
	my $feed = "slashdot";

	if ($num && $num > 6) {
		$who = $nick;
	}

	fetch_rss_feed($feed, $who, $num ? $num + 1 : 4);
}

# function : do_nyt
# purpose  : return first 5 items from nytimes; optional number of items
#            to return
# inputs   : who to respond to

sub do_nyt($$;$) {
	my ($who, $nick, $num) = @_;
	my $feed = "nyt";

	if ($num && $num > 3) {
		$who = $nick;
	}

	fetch_rss_feed($feed, $who, $num ? $num : 3);
}

# function : do_onion
# purpose  : return first 5 items from the onion; nick; optional number of items
#            to return
# inputs   : who to respond to

sub do_onion($$;$) {
	my ($who, $nick, $num) = @_;
	my $feed = "onion";

	if ($num && $num > 3) {
		$who = $nick;
	}

	fetch_rss_feed($feed, $who, $num ? $num : 3);
}

# function : do_g
# purpose  : return first URL of a google search
# inputs   : who to respond to, search term

sub do_g($$) {
	my ($who, $term) = @_;

	my $query = $methods{"g"} . "$term";

	foreach my $line (getContent($query)) {
		if ($line =~ m/<h3 class="r"><a href="([^"]+?)" class=l>/) {
			emit($irc, $who, "$1");
			last;
		}
	}
}


# function : do_better
# purpose  : display results of foo vs. bar
# inputs   : who to respond to, nick, search terms
# returns  : results (if any) are sent to the recipient

sub do_better($$$) {
	my ($who, $nick, $what) = @_;
	my ($url, @terms, %r);
	my ($found, $length);

	$length = 0;

	@terms = split(/ or /, lc($what));
	foreach my $term (@terms) {
		if (length($term) > $length) {
			$length = length($term);
		}

		if ($term =~ /^($botnick|jan|jans|ana|jschauma|yourmom)$/i) {
			$r{$term} = "11.0";
			$length = length($term);
			next;
		} elsif ($term eq "pbot") {
			$r{$term} = " 0.11";
			$length = length($term);
			next;
#		} else {
#			$r{$term} = sprintf("%.1f", rand(10));
		}

		$term =~ s/\+/%2B/g;
		$term =~ s/\&/%26/g;
		$term =~ s/ /+/g;
		$url = $methods{"better"} . "query?term=$term";
		$term =~ s/\+/ /g;
		$term =~ s/%2B/+/g;

		foreach my $line (getContent($url)) {
			if ($line =~ m/{"term": .* "sucks": (\d+), "rocks": (\d+)}/i) {
				if (($1 == 0) && ($2 == 0)) {
					$r{$term} = "unknown";
				} else {
					$r{$term} = sprintf("%.1f", ($2 / ($1 + $2)) * 10);
				}
				last;
			}
		}
	}

	if (($#terms == 1) && ($r{$terms[0]} == $r{$terms[1]})) {
		my $num=int(rand(2));
		my $better = $terms[$num];
		emit($irc, $who, "Pretty much the same, I'd say.");
		emit($irc, $who, "I guess that you'd like $better better.");
	} else {
		my $num = 0;
		foreach my $s (sort { $r{$b} cmp $r{$a} } keys %r) {
			my $spaces = "";

			for (my $i=length($s); $i < $length; $i++) {
				$spaces .= " ";
			}

			if ($num > 4) {
				$who = $nick;
			}
			$num++;
			if ($r{$s} == 10.0) {
				emit($irc, $who, "$s $spaces: not enough data to make the call");
			} else {
				emit($irc, $who, "$s $spaces: " . $r{$s});
			}
		}
	}
}

# function : do_rfc
# purpose  : display title of RFC
# inputs   : who to respond to, a rfc name
# returns  : results (if any) are sent to the recipient

sub do_rfc($$) {
	my ($who, $rfc) = @_;
	my ($url);
	my $found = 0;

	$url = $methods{"rfc"} . "$rfc";

	foreach my $line (getContent($url)) {
		if ($line =~ m/<span class="h1">/) {
			chomp($line);
			$line =~ s/<.+?>//g;
			$line =~ s/^\s*//;
			emit($irc, $who, decode_entities($line));
			emit($irc, $who, "$url");
			last;
		}
	}
}

# function : do_rotd
# purpose  : print out information about the animal of the day
# inputs   : none

sub do_rotd($) {
	my ($who) = @_;

	my $found = 0;
	my $query = $methods{"rotd"};

	foreach my $line (getContent($query)) {
		if ($line =~ m/<item>/) {
			$found = 1;
			next;
		}

		if ($found) {
			if ($line =~ m/<title>/) {
				emit($irc, $who, dehtmlify($line));
				next;
			}
			if ($line =~ m/<link>/) {
				emit($irc, $who, do_tiny(dehtmlify($line)));
				last;
			}
		}
	}
}



# function : do_room
# purpose  : display information about the given conference room
# inputs   : who to respond to, a room name, an optional office
# returns  : results (if any) are sent to the recipient

sub do_room($$$) {
	my ($who, $room, $office) = @_;
	my ($url);
	my $found = 0;
	my $bldg = 0;

	$url = $methods{"room"} . "$room";
	if ($office) {
		$url .= "&office=$office";
	}

	foreach my $line (getContent($url)) {
		if ($line =~ m/<td.+?>Bldg<\/td>/) {
			$found = 1;
			next;
		}

		if ($found == 1) {
			if ($line =~ m/<tr><td colspan="8"><b>(.*)<\/b><\/td><\/tr>/) {
				$bldg++;
				if ($bldg > 1) {
					emit($irc, $who, " ");
				}
				emit($irc, $who, "Office   : $1");
				$found=2;
				next;
			}
		}

		if ($line =~ m/<td class="textcopysmaller" valign="top".*?>(.*)(<\/td>)?/) {
			my $what;
			my $msg=$1;
			$msg =~ s/<.+?>//g;
			$msg =~ s/&.+?;//g;

			next if ($found == 1);

			if ($found == 2) {
				$what = "Building :";
				$found++;
			}
			elsif ($found == 3) {
				$what = "Floor    :";
				$found++;
			}
			elsif ($found == 4) {
				$what = "Name     :";
				$found++;
			}
			elsif ($found == 5) {
				$what = "Room#    :";
				$found++;
			}
			elsif ($found == 6) {
				$what = "Phone    :";
				$found++;
			}
			elsif ($found == 7) {
				$what = "Capacity :";
				$found=1;
			}
			emit($irc, $who, "$what $msg");
		}

	}
}

# function : do_mail
# purpose  : send a mail to the $botnick master
# inputs   : message, who sent it, who to send to, subject
# returns  : 0 on success, >0 otherwise

sub do_mail($$$$) {
	my ($msg, $sender, $recipient, $subject) = @_;

	if (!$msg) {
		return;
	}

	$sender =~ s/(.*)\@.*/$1/;
	$sender =~ s/~//;
	open(SM, "|/usr/sbin/sendmail -oi -t -odq") or return;
	print SM <<"EOF";
From: $sender
To: $recipient
Subject: $botnick: $subject
X-YSA-Tools: $botnick

$msg
EOF
	close(SM);
}

# function : do_fml
# purpose  : get a random fmylife quote

sub do_fml($) {
	my ($who) = @_;

	my $url = $methods{"fml"};

	foreach my $line (getContent($url)) {
		if ($line =~ m/class="fmllink">(.*? FML)<\/a>/) {
			emit($irc, $who, dehtmlify($1));
			last;
		}
	}
}

# function : do_foo
# purpose  : reply with a metasyntactical variable
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_foo($) {
	my ($who) = @_;
	my ($url);
	my $found = 0;
	my $begin = 0;
	my ($word, $k, $n, $j, @keys);
	my %metas;

	$url = $methods{"foo"};

	foreach my $line (getContent($url)) {
		if ($line =~ m/<p><a name="Gazonk" id="Gazonk">/i) {
			$begin = 1;
			next;
		}

		if ($begin) {
			if ($line =~ m/<span class="mw-headline">(.+)<\/span>/i) {
				$word = $1;
				$word =~ s/<.+?>//g;
				$word =~ s/^\s*//;
				$word =~ s/\s*$//;
				$found = 1;
			}

			if ($found) {
				next if ($line =~ m/^<p><a name=/);

				if ($line =~ m/^<p>/) {
					$line =~ s/<.+?>//g;
					$line =~ s/^\s*//;
					$line =~ s/\s*$//;
					push(@{$metas{$word}}, decode_entities($line));
				}
			}
		}
	}

	if (!$found) {
		return;
	}

	@keys = keys %metas;
	$k = $keys[int(rand(scalar(@keys)))];

	emit($irc, $who, "$k:");
	emit($irc, $who, join(" ", @{$metas{$k}}));
}


# function : do_gas
# purpose  : display gas information by zip code
# inputs   : who to respond to, zip
# returns  : results (if any) are sent to the recipient

sub do_gas($$) {
	my ($who, $zip) = @_;
	my ($url);
	my ($low, $next, $high);

	$next = "none";

	$url = $methods{"gas"} . "$zip";

	foreach my $line (getContent($url)) {
		if ($line =~ m/<span class="lowest"/) {
			$next = "low";
			next;
		}
		if ($line =~ m/<span>(.*)<\/span>/) {
			if ($next eq "low") {
				$low = decode_entities($1);
				$next = "high";
			} else {
				$high = decode_entities($1);
				last;
			}
		}
	}

	if ($low && $high) {
		emit($irc, $who, "Lowest : $low");
		emit($irc, $who, "Highest: $high");
	}
}

# function : do_geo
# purpose  : display geo information such as latitude, longitude
# inputs   : who to respond to, location
# returns  : results (if any) are sent to the recipient

sub do_geo($$) {
	my ($who, $location) = @_;
	my (@content, $result, $url);
	my @formatted;
	my $found = 0;
	my $ip = 0;

	$location = uri_escape($location);

	$url = $methods{"geo"} . $location;

	if ($location =~ m/\d+\.\d+\.\d+\.\d+/) {
		$url = $methods{"geoip"} . $location;
		$ip = 1;
	}

	@content = getContent($url);

	if ($ip) {
		my $what;
		foreach my $line (@content) {
			if ($line =~ m/<TABLE id="Table1".*/i) {
				$found = 1;
				next;
			}
			if ($found) {
				last if ($line =~ m/<\/TABLE>/i);
				if ($line =~ m/<TD class="label".+?>([^<]+)/i) {
					$what = $1;
					next;
				} elsif ($line =~ m/<span id=".+?>([^<&]+)/i) {
					emit($irc, $who, "$what: $1");
					next;
				}
			}
		}
	} else {
		foreach my $line (@content) {
			$line =~ s/(<Result .+?>)/$1\n/;
			$line =~ s/(<\/.+?>)/$1\n/g;
			push(@formatted, split(/\n/, $line));
		}

		foreach my $line (@formatted) {
			last if ($line =~ m/<\/Result>/);

			if ($line =~ m/<Result .*>/) {
				$found = 1;
				next;
			}

			if ($found == 1) {
				if ($line =~ m/<(Latitude|Longitude)>([^<]+)<\/.+?>/i) {
					emit($irc, $who, "$1: $2");
					next;
				}
			}
		}
	}
}


# function : do_traffic
# purpose  : display traffic information from 511.org
# inputs   : who to respond to, requestor, route
# returns  : results (if any) are sent to the recipient

sub do_traffic($$$) {
	my ($who, $nick, $route) = @_;
	my ($url);
	my $found = 0;
	my $count = 0;

	$url = $methods{"traffic"};
	$route = uc($route);

	foreach my $line (getContent($url)) {
		if ($line =~ m/<td scope="row">/) {
			$found = 1;
		}

		if ($line =~ m/<\/tr>/) {
			$found = 0;
		}

		if ($found > 0) {
			if ($line =~ m/<td>(.*)/) {
				my $match = $1;
				$match =~ s/&.+?;//g;
				$match =~ s/<.+?>//g;

				if (($found == 1) && (m/$route/)) {
					$count++;
					$found++;
					if ($count > 1) {
						last;
					}
					next;
				}

				elsif ($found == 2) {
					$found++;
					emit($irc, $who, "Start         :  $match");
					next;
				}
				elsif ($found == 3) {
					$found++;
					emit($irc, $who, "Est. Duration :  $match");
					next;
				}
				elsif ($found == 4) {
					$found++;
					emit($irc, $who, "$match");
					next;
				}
			}
			elsif (($found == 5) && ($line =~ m/<p class="more-info"><a href="([^"]+)?">/)) {

				emit($irc, $who, "http://traffic.511.org/$1");
				$found=0;
				next;
			}
		}
	}

	if (!$found) {
		emit($irc, $who, "No traffic.");
	}
}

# function : do_tld
# purpose  : output information about the requested TLD
# inputs   : who, nick, tld(s)

sub do_tld($$$) {
	my ($who, $nick, $input) = @_;
	my ($query, @tlds, %results);
	my $found = 0;

	$query = $methods{"tld"};
	@tlds = split(/\s+/, $input);

	foreach my $line (getContent($query)) {
		if ($line =~ m|<a href="/domains/root/db/.*.html">(\..*)</a></td><td>(.*)</td><td>(.*)<br/>|) {
			my $tld = $1;
			my $type = $2;
			my $desc = $3;
			foreach my $t (@tlds) {
				$t =~ s/^\.?/\./;
				if ($tld eq uc($t)) {
					my @tmp;
					push(@tmp, $type, $desc);
					$results{$t} = \@tmp;
				}
			}
		}
	}

	my $n = 0;
	if (scalar(keys(%results))) {
		my $f = "@<<<<<<<< @<<<<<<<<<<<< @*";
		foreach my $r (sort(keys(%results))) {
			my @vals = @{$results{$r}};
			$^A = "";
			formline($f, $r, @vals);
			$n++;
			if ($n > 5) {
				$who = $nick;
			}
			emit($irc, $who, $^A);
			if (scalar(keys(%results)) == 1) {
				my $tld = lc($r);
				$tld =~ s/^\.//;
				emit($irc, $who, $query . $tld . ".html");
			}
		}
	}

	my @notfound;
	foreach my $t (@tlds) {
		if (!$results{$t}) {
			push(@notfound, $t);
		}
	}
	if (scalar(@notfound)) {
		emit($irc, $who, "Not a TLD: " . join(" ", @notfound));
	}
}


# function : do_top5_twitter
# purpose  : display top5 twitter trends
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_top5_twitter($) {
	my ($who) = @_;
	my ($url, $tweet);
	my $n = 0;
	my $found = 0;

	$url = $methods{"top5twitter"};

	foreach my $line (getContent($url)) {
		if ($line =~ m/<item>/) {
			$found = 1;
			$n++;
			if ($n > 5) {
				last;
			}
		}

		if ($found) {
			if ($line =~ m/<description>(.*)<\/description>/i) {
				$tweet = decode_entities($1);
			}
			if ($line =~ m/<\/item>/i) {
				if ($tweet) {
					emit($irc, $who, "$n. $tweet");
				}
			}
		}
	}
}


# function : do_top5_flickr
# purpose  : display top5 hot tags
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_top5_flickr($) {
	my ($who) = @_;
	my ($url);
	my $n = 0;
	my $found = 0;

	$url = $methods{"flickr"};

	foreach my $line (getContent($url)) {
		if ($line =~ m/<p><b>In the last 24 hours<\/b><br \/>/) {
			$found = 1;
			next;
		}

		if ($found) {
			last if ($n == 5);
			if ($line =~ m/<b><a href=".+?">(.*)<\/a>/) {
				$n++;
				emit($irc, $who, "$n. " . $methods{"flickr"} . "$1/" );
			}
		}
	}
}

# function : do_top5_bots
# purpose  : display top5 bot commands
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_top5_bots($) {
	my ($who) = @_;

	my $n = 1;
	my @msg;

	$cmds{"top5"} = $cmds{"top5"} - 1;
	if ($cmds{"top5"} == 0) {
		delete $cmds{"top5"};
	}
	my @keys = sort { $cmds{$b} <=> $cmds{$a} } keys %cmds;
	foreach my $k (@keys) {
		last if ($n > 5);
		push(@msg, "$k (" . $cmds{$k} . ")");
		$n++;
	}

	emit($irc, $who, join(", ", @msg));
}

# function : do_top5_curses
# purpose  : display top5 curses
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_top5_curses($) {
	my ($who) = @_;

	my $n = 1;
	my @msg;

	my @keys = sort { $curses{$b} <=> $curses{$a} } keys %curses;
	foreach my $k (@keys) {
		last if ($n > 5);
		push(@msg, "$k (" . $curses{$k} . ")");
		$n++;
	}

	emit($irc, $who, join(", ", @msg));
}

# function : do_top5_cursers
# purpose  : display top5 pottymouths
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_top5_cursers($) {
	my ($who) = @_;

	my $n = 1;
	my @msg;

	my @keys = sort { $pottymouths{$b} <=> $pottymouths{$a} } keys %pottymouths;
	foreach my $k (@keys) {
		last if ($n > 5);
		push(@msg, "$k (" . $pottymouths{$k} . ")");
		$n++;
	}

	emit($irc, $who, join(", ", @msg));
}

# function : do_top5_cursentages
# purpose  : display top5 pottymouths by percentage
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_top5_cursentages($) {
	my ($channel) = @_;

	my $n = 1;
	my @msg;
	my %top5;

	foreach my $p (keys %pottymouths) {
		my $c = $pottymouths{$p};
		my $m = $totcount{$channel}{$p};

		if (!$m) {
			next;
		}

		$top5{$p} = sprintf("%.2f", ($c/$m) * 100);
	}

	my @keys = sort { $top5{$b} <=> $top5{$a} } keys %top5;
	foreach my $k (@keys) {
		last if ($n > 5);

		push(@msg, "$k (" . $top5{$k} . "%)");
		$n++;
	}

	emit($irc, $channel, join(", ", @msg));
}


# function : do_top5_botters
# purpose  : display top5 bot users
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_top5_botters($) {
	my ($who) = @_;

	my $n = 1;
	my @msg;

	my @keys = sort { $cmdrs{$b} <=> $cmdrs{$a} } keys %cmdrs;
	foreach my $k (@keys) {
		last if ($n > 5);
		push(@msg, "$k (" . $cmdrs{$k} . ")");
		$n++;
	}

	emit($irc, $who, join(", ", @msg));
}

# function : do_top5
# purpose  : display top5 search terms for given date
# inputs   : who to respond to, [gy], date, if any
# returns  : results (if any) are sent to the recipient

sub do_top5($$$) {
	my ($who, $which, $when) = @_;
	my ($date, $url);
	my $found = 0;

	$when =~ s/^\s*//;
	chomp($when);

	if ($when eq "bots") {
		do_top5_bots($who);
		return;
	} elsif ($when eq "bender") {
		emit($irc, $who, "1. Ass");
		emit($irc, $who, "2. Daffodil");
		emit($irc, $who, "3. Shiny");
		emit($irc, $who, "4. My");
		emit($irc, $who, "5. Bite");
	} elsif ($when eq "flickr") {
		do_top5_flickr($who);
		return;
	} elsif ($when eq "botters") {
		do_top5_botters($who);
		return;
	} elsif ($when eq "cursers") {
		do_top5_cursers($who);
		return;
	} elsif ($when eq "curses") {
		do_top5_curses($who);
		return;
	} elsif ($when eq "cursentages") {
		do_top5_cursentages($who);
		return;
	} elsif ($when eq "twitter") {
		do_top5_twitter($who);
		return;
	}

	if ($which =~ /(y.*)/) {
		do_top5_yrss($who, "top5$1", $when);
		return;
	} else {
		do_top5_g($who, $when);
		return;
	}
}

# function : do_top5_yrss
# purpose  : do a Y! rss top 5
# inputs   : who to respond to, feed name, whether to display links
# returns  : results (if any) are sent to the recipient

sub do_top5_yrss($$;$) {
	my ($who, $feed, $links) = @_;
	my ($url);
	my $found = 0;
	my $n = 0;

	$url = $methods{"$feed"};
	foreach my $line (getContent($url)) {
		if ($line =~ m/^\s*<item>/) {
			$found = 1;
		}

		if ($found && ($line =~ m/^\s*(<title>|\s*<link>)/)) {
			my $msg = "";
			if ($1 eq "<title>") {
				if ($n == 5) {
					last;
				}
				$line =~ s/^\s*//;
				$line =~ s/^$n\. //;
				$n++;
				$msg = "$n. " . dehtmlify($line);

			} else {
				if (!$links) {
					next;
				}
				$msg = "  $_";
				$msg =~ s/http:.*\*//;
			}

			$msg =~ s/&.+?;//g;
			$msg =~ s/<.+?>//g;
			emit($irc, $who, $msg);
			next;
		}
	}
}

# function : do_top5_g
# purpose  : do Google's top5
# inputs   : who to respond to, date, if any
# returns  : results (if any) are sent to the recipient

sub do_top5_g($$) {
	my ($who, $when) = @_;
	my ($url, $date);
	my $found = 0;

	$url = $methods{"top5g"};
	$date = $when;

	$date =~ s/-0([0-9])/-\1/g;

	if (!$date) {
		my (undef, undef, undef, $d, $m, $y, undef) = localtime();
		$y += 1900;
		$m += 1;
		$date = "$y-$m-$d";
	}

	$url .= $date;

	foreach my $line (getContent($url)) {
		if ($line =~ m/<a href="\/trends\/hottrends\?q=/) {
			$line =~ s/&.+?;//g;
			$line =~ s/<.+?>//g;
			$line =~ s/\s\s+//g;
			$line =~ s/^(\d\.)/$1 /;
			$line =~ s/(\w+)/\u\L$1/g;
			$found++;
			last if ($found > 5);
			emit($irc, $who, "$line");
			next;
		}
	}
}

# function : do_trivia
# purpose  : display random trivia information
# inputs   : who to respond to
# returns  : results (if any) are sent to the recipient

sub do_trivia($) {
	my ($who) = @_;
	my $found = 0;

	foreach my $line (getContent($methods{"trivia"})) {
		if ($line =~ m/<span class='factNumber'>Fact #\d+:<\/span>(.*)/) {
			my $text = $1;
			emit($irc, $who, dehtmlify(decode_entities($text)));
			last;
		}
	}
}

# function : do_rosetta
# purpose  : look up rosetta for unix equivalents from os to os
# inputs   : who to respond to, from-os, to-os, command/file
# returns  : results (if any) are sent to the recipient

sub do_rosetta($$$$) {
	my ($who, $from, $to, $cmd) = @_;
	my ($url);
	my $col = 0;
	my $found = 0;
	my $a = 0;
	my $n = 0;
	my (@from_cmd, @to_cmd, $task);

	my @cols = ( "Task", "AIX", "A/UX", "DG/UX", "FREEBSD", "HP-UX",
			"IRIX", "LINUX", "MAC OS X", "NCR UNIX", "NETBSD",
			"OPENBSD", "RELIANT", "SCO OPENSERVER", "SOLARIS",
			"SUNOS", "TRU64", "ULTRIX", "UNICOS", "Task");

	if (!$to || !$cmd) {
		emit($irc, $who, "Usage:  !rosetta <from> <to> <cmd>");
		emit($irc, $who, "  <from> -- given command is from this OS");
		emit($irc, $who, "  <to>   -- search for given command for this OS");
		emit($irc, $who, "  <cmd>  -- command to look up");
		emit($irc, $who, "Example: !rosetta linux freebsd insmod");
		return;
		# NOTREACHED
	}

	$from = uc($from);
	$to = uc($to);

	if ($from =~ m/^(MAC)?OS ?X?$/) {
		$from = "MAC OS X";
	} elsif ($from =~ m/^SCO$/) {
		$from = "SCO OPENSERVER";
	}
	if ($to =~ m/^(MAC)?OS ?X?$/) {
		$to = "MAC OS X";
	} elsif ($to =~ m/^SCO$/) {
		$to = "SCO OPENSERVER";
	}

	$url = $methods{"rosetta"};

	foreach my $line (getContent($url)) {
		if ($line =~ m/<table id="Rosetta"/) {
			$found = 1;
			next;
		}

		if ($found) {
			if ($line =~ m/<tr/) {
				$task = "";
				$col=0;
				next;
			}

			if ($line =~ m/<td/) {
				$col++;
			}

			if ($col == 1) {
				if ($line =~ m/<a href/) {
					$a = 1;
				}
				if ($line =~ m/<\/a>/) {
					$line =~ s/.*<\/a>//;
					$a = 0;
				}
				$line =~ s/<.+?>//g;
				$line =~ s/&.+?;//g;
				$line =~ s/\s+/ /g;
				$line =~ s/^\s+//;
				if (!$a) {
					$task .= $line;
				}
			}

			if ($cols[$col-1] eq $from) {
				$line =~ s/<.+?>//g;
				$line =~ s/&.+?;//g;
				$line =~ s/^\s+$//g;
				push(@from_cmd, $line);
			} elsif ($cols[$col-1] eq $to) {
				$line =~ s/<.+?>//g;
				$line =~ s/&.+?;//g;
				$line =~ s/^([ ]+)?/  /;
				$line =~ s/^\s+$//g;
				push(@to_cmd, $line);
			}

			if ($line =~ m/<\/tr>/) {
				foreach my $f (@from_cmd) {
					if ($f =~ m/$cmd/i) {
						$n++;
						emit($irc, $who, "$task:");
						foreach my $t (@to_cmd) {
							emit($irc, $who, "$t");
							$n++;
						}
						if ($n > 7) {
							emit($irc, $who, "... and more ...");
							return;
						}
					}
				}

				$task = "";
				@from_cmd = ();
				@to_cmd = ();

			}
		}
	}
}

# function : do_rp
# purpose  : return decoded RP entry for given host
# inputs   : who to respond to, string to lookup, how to

sub do_rp($$$) {
	my ($who, $string, $how) = @_;
	my $enc = $string;
	my $msg;

	my $lookup = `host -t RP $string 2>/dev/null`;
	chomp($lookup);
	if ($lookup =~ m/has RP record (\S+)/) {
		$enc = $1;
	}

	if ($enc) {
		my $rp = `/home/y/bin/rpcrypt $enc`;
		chomp($rp);
		if ($rp =~ m/$enc: (\S+) \(created/) {
			$msg = $1;
		}
	}

	if ($msg) {
		emit($irc, $who, $msg);
	}
}


# function : do_whois
# purpose  : do a whois lookup
# inputs   : who to respond to, a domain
# returns  : results (if any) are sent to the recipient

sub do_whois($$) {
	my ($who, $domain) = @_;
	my @content;

	eval { @content = split(/\n/, get_whois("$domain")); };
	if ($@) {
		emit($irc, $who, "No such domain.");
		return;
	}

	foreach my $line (@content) {
		$line =~ s/^\s*//g;
		$line =~ s/\.$//;
		my $pad = "";
		my $diff = 0;
		if ($line =~ m/(Domain Name|Registrar Name|Registrar Whois|Created on|Expires on|Record last updated on).*:(.*)/) {
			$diff = 22 - length($1);
			$pad = " " x $diff;
			emit($irc, $who, $1 . $pad . " :" . $2);
		}
	}
}

# function : do_wiki
# purpose  : give the first paragraph from wikipedia, if an entry is found
# inputs   : who to respond to, a search term
# returns  : results (if any) are sent to the recipient

sub do_wiki($$) {
	my ($who, $word) = @_;
	my ($query);
	my $intable = 0;
	my $items = 0;
	my $printed = 0;

	$word =~ s/ /_/g;
	$query = $methods{"wiki"} . uri_escape($word);

	foreach my $line (getContent($query)) {
		if ($line =~ m/<table/i) {
			$intable++;
			next;
		}
		if ($line =~ m/<\/table>/i) {
			$intable--;
			next;
		}

		if ($line =~ m/<div class="dablink">/) {
			emit($irc, $who, "See also: " . $methods{"wiki"} . "$word" . "_%28disambiguation%29");
		}

		if ($line =~ m/^(\s+)?<p>/ && !$intable) {
			$line =~ s/^\s+//;
			$line =~ s/<span .*class="IPA">.*?<\/span>//;
			$line =~ s/<.+?>//g;
			emit($irc, $who, decode_entities($line));
			$printed = 1;
			if ($line =~ m/:$/) {
				$items++;
			} else {
				last;
			}
		}

		if ($items) {
			if ($items > 3) {
				emit($irc, $who, "  ...");
				last;
			}
			if ($line =~ m/^<li>/) {
				$line =~ s/^\s*/  * /;
				$line =~ s/<span .*class="IPA">.*?<\/span>//;
				$line =~ s/<.+?>//g;
				emit($irc, $who, decode_entities($line));
				$printed = 1;
				$items++;
			}
		}
	}

	if ($printed) {
		emit($irc, $who, $query);
	}
}

# purpose : map IP to ASN or give ASN info
# inputs  : IP address or ASN

sub do_asn($$) {
	my ($who, $input) = @_;

	$input = lc($input);
	if (($input !~ m/^as[0-9]+$/) && (!is_ip($input))) {
		$input = fqdn($input);
		my %addrs = getIPAddresses($input);
		if (!scalar(keys(%addrs))) {
			emit($irc, $who, "Not a valid ASN, IP or hostname.");
			return;
		}
		foreach $a (keys(%addrs)) {
			do_asn($who, $a);
			return;
		}
	}

	my $results = `whois -h whois.cymru.com ' -v $input' | tail -1`;
	emit($irc, $who, $results);
}

# function : do_aotd
# purpose  : print out information about the animal of the day
# inputs   : none

sub do_aotd($) {
	my ($who) = @_;

	my $found = 0;
	my $query = $methods{"aotd"};

	foreach (getContent($query)) {
		if (m/<item>/) {
			$found = 1;
			next;
		}

		if ($found) {
			if (m/<title>/) {
				emit($irc, $who, dehtmlify($_));
				next;
			}
			if (m/<link>/) {
				emit($irc, $who, do_tiny(dehtmlify($_)));
				last;
			}
		}
	}
}

# function : do_az
# purpose  : list first item in an amazon search
# inputs   : search terms

sub do_az($$) {
	my ($who, $what) = @_;
	my $query = $methods{"az"} . uri_escape($what);

	foreach my $line (getContent($query, 'http://www.amazon.com')) {
		if ($line =~ m/<div class="productTitle"><a href="(.*?)">(.*)/i) {
			my $url = $1;
			my $desc = $2;
			emit($irc, $who, dehtmlify($2));
			emit($irc, $who, do_tiny($1));
			return;
		}
	}
}

# function : do_babel
# purpose  : try to translate via babelfish
# inputs   : who to respond to, from_to, sentence
# returns  : results (if any) are sent to the recipient

sub do_babel($$$) {
	my ($who, $fromto, $input) = @_;

	my $ua = LWP::UserAgent->new();

	my $r = $ua->post($methods{"babel"},
			[ 'ei' => 'UTF-8',
			'doit' => 'done',
			'fr' => 'bf-res',
			'intl' => '1',
			'lp' => "$fromto",
			'btnTrTxt' => '1',
			'trtext' => "$input" ]);

	my @content = split(/\n/, $r->content);
	undef($r);

	foreach my $line (@content) {
		if ($line =~ m/<div id="result">/) {
			emit($irc, $who, dehtmlify($line));
			last;
		}
	}
}

# function : do_beer
# purpose  : get beer info from ratebeer.com
# inputs   : who to respond to, a beer name
# returns  : results (if any) are sent to the recipient

sub do_beer($$) {
	my ($who, $beer) = @_;
	my (@content, $alcohol, $details, $rating, $title);

	my $ua = LWP::UserAgent->new();

	my $r = $ua->post($methods{"beer"}, [ 'BeerName' => "$beer" ]);
	@content = split(/\n/, $r->content);
	undef($r);

	foreach my $line (@content) {
		if ($line =~ m/<META HTTP-EQUIV=Refresh CONTENT="0; URL=([^"]+)">/i) {
			$details = "http://www.ratebeer.com$1";
			last;
		}

		if ($line =~ m/<TD.*class="results"/) {
			if ($line =~ m/.+?.nbsp;<A HREF="([^"]+)[^>]*>.*?\Q$beer\E.+?/i) {
				$details = "http://www.ratebeer.com$1";
				last;
			}
		}
	}

	if (!$details) {
		emit($irc, $who, "No $beer found.");
		return;
	}
	foreach my $line (getContent($details)) {
		if ($line =~ m/<TITLE>(.*)( - Ratebeer)?</i) {
			$title = decode_entities($1);
			next;
		}

		if ($line =~ m/>WEIGHTED AVG: <.*>([0-9.]+)<.*nbsp; ABV: <.*>(([0-9.]+%)|-)</) {
			$rating = $1;
			$alcohol = ($2 eq "-") ? "no" : $2;
		}
	}

	if ($rating) {
		emit($irc, $who, "$title ($alcohol alcohol): $rating / 5");
	}
}

# function : do_next
# purpose  : display next time the given periodic is run
# input    : name of a periodic

sub do_next($$) {
	my ($who, $what) = @_;

	if (!$periodics{$what}) {
		return;
	}

	my $when = localtime(time() + $periodics{$what});
	emit($irc, $who, "$when (Pacific)");
}

# function : periodic_throttle
# purpose  : set or report the throttle for the given periodic
# input    : which periodic, a number (optionally prefixed with +/-), who requested

sub periodic_throttle($$$$) {
	my ($which, $num, $who, $nick) = @_;
	if ($num) {
		my $nt = 0;
		if ($num =~ /^([+-])(\d+)$/) {
			$nt = $periodics{$which} + $num;
		} elsif ($num =~ /^(\d+)$/) {
			$nt = $num;
		} elsif ($num =~ m/max/) {
			$nt = $maxperiod;
		} else {
			do_quip($who, $nick);
			return;
		}

		if ($nt < $autoping) {
			emit($irc, $who => "Can't set throttle to lower than $autoping.");
			return;
		} elsif ($nt > $maxperiod) {
			emit($irc, $who => "Whoa, easy there!  I'm just going to use $maxperiod.");
			$periodics{$which} = $maxperiod;
			return;
		} else {
			$periodics{$which} = $nt;
			emit($irc, $who => "New $which throttle: " . $periodics{$which});
		}
	} else {
		emit($irc, $who => "Currently: " . $periodics{$which});
	}

}


# function : do_imdb
# purpose  : retrieve information from imdb
# inputs   : recipient, search term
# outputs  : Backyard information

sub do_imdb($$) {
	my ($who, $title) = @_;
	my (@keywords, %results, $longest);

	@keywords = ( "Title", "Rating", "Directors",
			"MPAA Rating", "Release Date", "Tagline" );

	my $ua = LWP::UserAgent->new();
	$ua->default_headers->header('Referer' => $methods{"imdb"});

	my $r = $ua->post($methods{"imdb"}, [ 'id' => "form1",
						'form1' => "1",
						'm' => "$title" ]);
	my @content = split(/\n/, $r->content);
	undef($r);

	foreach my $line (@content) {
		if ($line =~ m/<br \/><br \/><table /) {
			my @lines = split(/<th.+?>/, $line);
			foreach my $l (@lines) {
				if ($l =~ m/^</) {
					next;
				}
				if ($l =~ m/^([^<]+)<\/th><td>(.*)/) {
					$results{$1} = dehtmlify($2);
				}

			}
		}
	}

	$longest = "RELEASE_DATE";
	foreach my $k (@keywords) {
		my ($pad, $diff);
		my $key = uc($k);
		$key =~ s/ /_/g;
		$diff = length($longest) - length($k) + 1;
		$pad = " " x $diff;
		if ($results{$key}) {
			emit($irc, $who, $k . $pad . ": " . $results{$key});
		}
	}
}

# function : do_ip
# purpose  : perform IP calculations via NetAddr::IP
# inputs   : ip or cidr, optional piece of information wanted

sub do_ip($$$$) {
	my ($who, $nick, $ip, $field) = @_;

	my $recipient = $field ? $who : $nick;

	$field =~ s/\s+//g;
	my $ip = NetAddr::IP->new($ip);

	if (!$ip) {
		emit($irc, $who, "That doesn't look like valid input.");
		return;
	}

	my @funcs = qw/broadcast network mask bits version cidr range prefix numeric wildcard short full first last re re6/;

	my $longest = length("broadcast");
	foreach my $f (@funcs) {
		if (!$field || $field eq $f) {
			my $result = $ip->$f();

			my $len = length($f);
			my $diff = $longest - $len;
			my $pad = " " x $diff;

			emit($irc, $recipient, "$f $pad: $result");
			if (defined($field) && $field eq $f) {
				last;
			}
		}
	}
}


# function : do_info
# purpose  : print out information about the given channel
# inputs   : who to respond to, channel name

sub do_info($$) {
	my ($who, $c) = @_;

	if ($c !~ m/^#/) {
		$c = "#$c";
	}

	$c = lc($c);
	if (!defined($CHANNELS{$c})) {
		emit($irc, $who, "I know nothing about $c.");
		return;
	}

	my $inviter = $CHANNELS{$c}{"inviter"};
	if ($inviter) {
		emit($irc, $who, "I was invited into $c by $inviter.");
	}

	emit($irc, $who, "These are the toggles for $c:");
	show_toggles($who, $c);

	my @users = keys(%{$CHANNELS{$c}{"users"}});
	emit($irc, $who, "These are the users I see in $c:");
	emit($irc, $who, join(", ", @users));
	emit($irc, $who, "Channel chatterers for $c:");
	do_stfu($who, -1, $c);
}


###
### COMMANDS
###

# function : on_command
# purpose  : act on commands and respond to the given recipient
# input    : a message, who to respond to, the nickname of the person asking, userhost, channel
# output   : results of the action sent to either the channel or the nick

sub on_command($$$$$) {
	my ($msg, $who, $nick, $userhost, $channel) = @_;
	my ($c, $real);

	$_ = $msg;

	$channel = lc($channel);
	$CHANNELS{$channel}{"seen"}{$nick} = strftime("%a %b %e %H:%M:%S %Z %Y", localtime());
	if (!$cmdrs{$nick}) {
		$cmdrs{$nick} = 0;
	}
	$cmdrs{$nick} = $cmdrs{$nick} + 1;

	($c, undef) = split(/\s+/, $msg, 2);

	if (!$cmds{$c}) {
		$cmds{$c} = 0;
	}
	$cmds{$c} = $cmds{$c} + 1;

	$real = $userhost;
	$real =~ s/~(.*)@.*/$1/;

print STDERR "$userhost ($real) <->$who $msg\n";

	if (/^about\s+$botnick$/i) {
		do_help($who, $nick, $botnick);
	}

	elsif (/^8ball(,?\s+.*|$)/) {
		do_eightball($who, $nick);
	}

	elsif (/^aotd$/i) {
		do_aotd($who);
	}

	elsif (/^asn\s+(\S+)/i) {
		do_asn($who, $1);
	}

	elsif (/^az\s+(.*)/i) {
		do_az($who, $1);
	}

	elsif (/^babel\s+(\S+)\s+(.+)/i) {
		do_babel($who, $1, $2);
	}

	elsif (/^beer\s+(.*)/i) {
		do_beer($who, $1);
	}

	elsif (/^better\s+(.+)/i) {
		do_better($who, $nick, $1);
	}

	elsif (/^bible\s+([a-z]+ [0-9]+:[0-9-]+)/i) {
		do_bible($who, $1);
	}

	elsif (/^bing\s+(.*)/i) {
		do_bing($who, $1);
	}

	elsif (/^bofh\s*$/i) {
		do_bofh($who);
	}

	elsif (/^brick\s+(\S+)(\s+\S+)?/i) {
		my $brickee = $1;
		my $target = $2;
		if ($target) {
			$target =~ s/^\s*//;
		} else {
			$target = $channel;
		}
		do_brick($nick, $channel, $brickee, $target);
	}

	elsif (/^bugmenot\s+(.*)/i) {
		do_bugmenot($who, $1);
	}

	elsif (/^cal(\s+\d+\s+\d+)?$/) {
		do_shell("cal $1", $who, "", 7);
	}

	elsif (/^calc\s+(.*)/) {
		my $terms = $1;
		if ($terms =~ m/(read|while|for|print)/i) {
			do_quip($who, $nick);
		} else {
			do_shell("bc -l", $who, "$terms", 1);
		}
	}

	elsif (/^calendar(\s+[a-z]+)?/i) {
		my $args = "";
		if ($1) {
			my $file = $1;
			$file =~ s/^\s+//;
			$args = "-f /usr/share/calendar/calendar.$file";
		}
		do_shell("calendar $args", $who, "", 5);
	}

	elsif (/^channels$/i ) {
		$irc->yield( 'whois' => "$botnick");
		my %chs = %CHANNELS;
		delete($chs{$botnick});
		if (scalar(keys(%chs))) {
			my @all = sort(keys(%chs));
			my @all_split;
			foreach my $c (@all) {
				if ($c !~ m/^#/) {
					delete($chs{$c});
					delete($CHANNELS{$c});
				}
			}
			@all = sort(keys(%chs));
			emit($irc, $who, "I'm in the following " . scalar(@all) . " channels:");
			while (my @tmp = splice(@all, 0, 10)) {
				push(@all_split, [@tmp]);
			}
			foreach my $a (@all_split) {
				emit($irc, $who, join(", ", @{$a}));
			}
		} else {
			emit($irc, $who, "I'm not in any channels at the moment.");
		}
	}

	elsif (/^countdown\s+(.*)/i) {
		my $what = $1;
		emit($irc, $who, do_countdown(lc($what)));
	}

	elsif (m/^curses(\s+\S+)?/i) {
		my $p = $1;
		$p =~ s/\s+//g;
		if (!$p) {
			$p = $nick;
		}
		if (defined($pottymouths{$p})) {
			emit($irc, $who, $pottymouths{$p});
		}
	}

	elsif (m/^cursebird(\s+\S+)?/i) {
		my $tweetie = $1;
		$tweetie =~ s/\s+//g;
		do_cursebird($who, $tweetie);
	}

	elsif (/^cve\s+([0-9-]+)/i) {
		do_cve($who, $1);
	}

	elsif (/^(capital|define|synonym|quote?|time|weather|zip|convert|area)(\s*.*)/i) {
		my (@a, $cmd, %args, $c, $in, $num, $s, $w);

		$s = $1;
		$in = $2;
		$in =~ s/\s*//;

		$c = 0;
		$w = $who;

		if ($s =~ m/quote?/) {
			$s = "quote";
			@a = split(/\s/, $in);
		} else {
			push(@a, $in);
		}
		$num = scalar(@a);

		foreach my $a (@a) {
			$c++;
			if ($c > 4) {
				$w = $nick;
			}
			do_shortcut($s, $a, $w, $num);
		}
	}

	elsif (/^(date|time)$/i) {
		my $tz = $ENV{'TZ'};
		emit($irc, $who, strftime("%a %b %e %H:%M:%S %Z %Y", localtime()));
		$ENV{'TZ'} = "UTC";
		emit($irc, $who, strftime("%a %b %e %H:%M:%S %Z %Y", gmtime()));
		$ENV{'TZ'}  = $tz;
	}

	elsif (/^cowsay\s+(.*)/i) {
		if (!is_throttled('cowsay', 'all')) {
			do_shell("cowsay $1", $who, "", 15);
		}
	}

	elsif (/^unicornsay\s+(.*)/i) {
		if (!is_throttled('unicornsay', 'all')) {
			do_shell("cowsay -f unicorn $1", $who, "", 15);
		}
	}

	elsif (/^digest\s+(\S+)\s+(.+)/i) {
		my $digest = $1;
		my $string = $2;
		$digest =~ s/-//;
		$digest = lc($digest);
		do_shell("/home/y/bin/digest $digest", $who, "$string", 1, 0);
	}

	elsif (/^errno\s+([a-zA-Z0-9._ -]+)/) {
		do_errno($who, $nick, $1);
	}

	elsif (/^feature (.*)/) {
		if (!is_throttled('mail', $userhost)) {
			if ($1 =~ "^: unknow") {
				do_quip($who, $nick);
			} else {
				do_mail($1, $userhost, $botowner, "feature request");
			}
		} else {
			if ($userhost !~ /jans\@127.0.0.1/) {
				emit($irc, $who, "Yo! Don't be spammin' me master!");
			}
		}
	}

	elsif (/^fileinfo\s+(\S+)\s?(\S*)/i) {
		do_fileinfo($who, $1, $2);
	}

	elsif (/^flight (.*)/i) {
		do_flight($who, $1);
	}

	elsif (/^(fortune|motd)$/) {
		my $fortunes = "20% /home/jans/fortunes/futurama";
		$fortunes .= " 20% /home/jans/fortunes/calvin";
		$fortunes .= " 20% /home/jans/fortunes/h2g2";
		$fortunes .= " 20% /home/jans/fortunes/alf";
		do_shell("fortune -s $fortunes 20% all", $who, "", 10);
	}

	elsif (/^foo/) {
		do_foo($who);
	}

	elsif (/^futurama/) {
		do_futurama($who);
	}

	elsif (/^fml$/) {
		do_fml($who);
	}

	elsif (/^g\s+(.*)/i) {
		do_g($who, $1);
	}

	elsif (/^gas\s+(\d+)/) {
		do_gas($who, $1);
	}

	elsif (/^geo(\s+.*|$)/) {
		my $input = $1;
		$input =~ s/^\s*//;
		do_geo($who, $input);
	}

	elsif (/^help ?(.*)?/) {
		do_help($who, $nick, $1);
	}

	elsif (/^how\s+(.*)/) {
		my ($cmd, $url);

		$cmd = $1;
		$cmd =~ s/!//;
		$url = $methods{lc($cmd)};

		if (!$url) {
			$url = $rssurl{lc($cmd)};
		}

		if ($url) {
			emit($irc, $who, $url);
			if ($cmd eq "oncall") {
				emit($irc, $who, $methods{"isops_oncall"});
			}
		} else {
			emit($irc, $who, "I'm afraid I can't tell you.");
		}
	}

	elsif (/^host\s+(.*)/i) {
		do_shell("/usr/bin/host $1", $who, "", 7);
	}

	elsif (/^imdb\s+(.*)/) {
		do_imdb($who, $1);
	}

	elsif (/^info(\s+\S+)?/) {
		my $c = $1;
		if (!$c) {
			$c = $channel;
		} else {
			$c =~ s/^\s+//;
		}
		do_info($who, $c);
	}

	elsif (/^ip\s+(\S+)(\s+\S+)?$/i) {
		do_ip($who, $nick, $1, $2);
	}

	elsif (/^ipv4(\s+((afri|ap|lac)nic|arin|ripe))?$/i) {
print STDERR "$1\n";
		do_ipv4($who, $1);
	}

	elsif (/^$botnick($|\s+.*)/i) {
		emit($irc, $who, "Try: !help, !help $botnick, !how $botnick");
	}

	elsif (/^leave\s*/) {
		emit($irc, $channel, "Say \"$botnick, please leave\".");
	}

	elsif (/^likes?\s+(\S+)\s+(.*)/) {
		my $liker = $1;
		my $stuff = $2;
		my $str = $like_dislike[int(rand(scalar(@like_dislike)))];
		$str =~ s/==%==/$stuff/g;
		emit($irc, $who, "$liker $str");
	}

	elsif(/^macro\s+(.*)/) {
		do_macro($who, uc($1));
	}

	elsif (/^man\s+(\S+)/) {
		if ($1 eq "hfrob") {
			emit($irc, $who, "hfrob -- frob the hobknobbin good");
			emit($irc, $who, do_tiny("http://produce.yahoo.com/jans/hfrob.txt"));
		} else {
			do_man($who, $1);
		}
	}

	elsif (/^movies(\s+(opening|soon))?/) {
		my $what = $1;
		$what =~ s/\s+//g;
		do_movies($who, $what);
	}

	elsif (/^morse\s+(-d\s+)?(.*)/i) {
		my $cmd;
		if ($1) {
			$cmd .= "-d";
			emit($irc, $who, "FreeBSD's morse(1) can't do that.  Lame.");
			return;
		} else {
			$cmd .= "-s";
		}
		do_shell("morse $cmd", $who, "$2", 0, 1);
	}

	elsif (/([^ ]+)stab (.*)(\s+#\S+)?/) {
		my $kind = $1;
		my $stabbee = $2;
		my $target = $3;
		if ($target) {
			$target =~ s/^\s*//;
		} else {
			$target = $channel;
		}

		if ($stabbee eq $botnick) {
			if (!is_throttled('insult', $userhost)) {
				do_quip($who, $nick);
				return;
			}
		}

		$irc->yield( 'ctcp' => $target => "ACTION unleashes a troop of pen-wielding stabbing-${kind}s on $stabbee!");
		emit($irc, $target => "Aaaay-ah!");
	}

	elsif (/^new(\s+.*)?$/) {
		do_new($who, $nick, $1);
	}

	elsif (/^old$/) {
		emit($irc, $who, "Your mom.");
	}

	elsif (/^next\s+(\S+)$/) {
		do_next($who, $1);
	}


	elsif (/^nts\s+(.*)/) {
		my $msg = $1;
		my $email = $userhost;
		$email =~ s/(.*)@.*/\1/;
		$email =~ s/~//g;
		do_mail($msg, $userhost, $email, "note to self");
	}

	elsif (/^nyt ?(\d+)?/i) {
		do_nyt($who, $nick, $1);
	}

	elsif (/^ohiny$/) {
		if ($CHANNELS{$channel}{"toggles"}{"nc17"}) {
			do_ohiny($who);
		} else {
			emit($irc, $who, $methods{"ohiny"});
		}
	}

	elsif (/^onion ?(\d+)?/i) {
		do_onion($who, $nick, $1);
	}

	elsif (/^primes\s+(\d+)\s+(\d+)/) {
		my $min = $1;
		my $max = $2;
		my $diff = $max - $min;
		if ($diff > 1000) {
			emit($irc, $who, "Bad $nick, bad!");
			$irc->yield( 'ctcp' => $who => "ACTION whacks $nick on the nose with a rolled up newspaper.");
		} else {
			do_shell("primes $min $max", $who, "", 0, 1);
		}
	}

	elsif (/^perldoc\s+([a-z0-9_:.-]+)/i) {
		do_perldoc($who, $1);
	}

	elsif (/^php\s+([a-zA-Z0-9_.-]+)/) {
		do_php($who, $1);
	}

	elsif (/^ping\s+([a-z0-9_.-]+)/) {
		do_ping($who, $1, $channel);
	}

	elsif (/^pwgen(\s+-s)?(\s+[0-9]+)?$/) {
		my $args = "$1 $2";
		if ($args !~ m/[0-9]/) {
			$args .= " 8";
		}
		do_shell("pwgen $args", $who, "", 1);
	}

	elsif (/^pydoc\s+([a-zA-Z0-9_.-]+)/) {
		do_pydoc($who, $1);
	}

	elsif (/^quake(\s+us)?$/) {
		do_quake($who, $1);
	}

	elsif (m/^(q52|rq|ahq)\s+(.+)/) {
		do_quote($who, $1, $2);
	}

	elsif (/^random\s+(\S+)/) {
		my $n = $1;
		if ($n =~ m/^\d+$/) {
			emit($irc, $who, int(rand($n)));
		} else {
			do_quip($who, $nick);
		}
	}

	elsif (/^rainbow\s+([0-9a-z-]+)\s+(.+)/i) {
		my $digest = $1;
		my $hash = $2;
print STDERR "Looking for $digest { $hash }...\n";
		if (defined($rainbow{$digest}) && defined($rainbow{$digest}{$hash})) {
			emit($irc, $who, $rainbow{$digest}{$hash});
		} else {
			emit($irc, $channel, $dontknow[int(rand(scalar(@dontknow)))]);
		}
	}

	elsif (/^rev\s+(.+)/) {
		my $rev = reverse($1);
		emit($irc, $who, $rev);
	}

	elsif (/^rfc\s+(.+)/i) {
		do_rfc($who, $1);
	}

	elsif(/^rosetta\s+([^ ]+) ?([^ ]+)? ?([^ ]+)?/) {
		do_rosetta($who, $1, $2, $3);
	}

	elsif (/^rotd$/) {
		do_rotd($who);
	}

	elsif (/^rot13\s+(.+)/) {
		my $rot13 = $1;
		$rot13 =~ tr[a-zA-Z][n-za-mN-ZA-M];

		emit($irc, $who, $rot13 );
	}

	elsif (/^rss$/) {
		emit($irc, $nick => 'I know about the following RSS feeds:');
		foreach my $i (sort(keys %rssfeeds)) {
			emit($irc, $nick => " - " . $rssfeeds{$i} . " [!" . $i . "]");
		}
	}

	elsif(/^score\s+(.*)/i) {
		do_score($who, $1);
	}

	elsif (/^seen\s+(\S+)/) {
		my $whom = lc($1);
		my $msg = $CHANNELS{$channel}{"seen"}{$whom};
		if (! $msg) {
			if ($CHANNELS{$channel}{"users"}{$whom}) {
				$msg = "$1 is present in $channel, but that's all I know.";
			} else {
				$msg = "I have not seen $1 say anything in $channel since I last joined.";
			}
		}
		emit($irc, $who, $msg);
	}

	elsif (/^service\s+([a-zA-Z0-9._-]+)/) {
		do_service($who, $nick, $1);
	}

	elsif (/^signal\s+([a-zA-Z0-9._-]+)/) {
		do_signal($who, $nick, $1);
	}

	elsif (/^slashdot ?(\d+)?/i) {
		do_slashdot($who, $nick, $1);
	}

	elsif (/^speb$/) {
		do_speb($who);
	}

	elsif (/^snopes\s+(.*)/) {
		do_snopes($who, $1);
	}

	elsif (/^stfu(\s+\S+)?(\s+#\S+)?$/) {
		if ($CHANNELS{$channel}{"toggles"}{"stfu"}) {
			my $chatterer = $1 ? $1 : 3;
			my $ch = $2 ? $2 : $channel;

			$ch =~ s/\s+//;
			$chatterer =~ s/\s+//;

			if (($chatterer =~ m/^#/) && ($ch eq $channel)) {
				# chatterer is a channel
				do_stfu($who, 3, $chatterer);
			} else {
				do_stfu($who, $chatterer, $ch);
			}
		}
	}

	elsif (/^symbol\s+(.*)/) {
		do_symbol($who, $1);
	}

	elsif (/^sysexits?\s+([a-zA-Z0-9._-]+)/) {
		do_sysexit($who, $nick, $1);
	}

	elsif (/^stop$/) {
		emit($irc, $who, "Hammertime!");
	}

	elsif (/^thanks/) {
		do_thanks($who, $nick);
	}

	elsif (/^throttle(\s+)?(\S+)?\s*(\S+)?/) {
		my $cmd = $2;
		my $num = $3;
		if ($cmd) {
			if ($cmd =~ m/cm3mon|unowned|escalations|gs2/) {
				periodic_throttle($cmd, $num, $who, $nick);
			} elsif ($cmd eq "help") {
				do_throttle($who, $userhost, "help");
			} elsif (($num && $num eq "all" && !is_throttled("$cmd", "all")) ||
				(!is_throttled("$cmd", $userhost))) {
				emit($irc, $who => "Sure thing, buddy.");
			}
		} else {
			do_throttle($who, $userhost);
		}
	}

	elsif (/^tiny\s+(.*)/) {
		emit($irc, $who, do_tiny($1));
	}

	elsif (/^tool\s*(.*)/)  {
		if (!is_throttled('tool', $userhost)) {
			my $tool = $nick;
			if ($1 && ($1 ne $botnick)) {
				$tool = $1;
			}
			emit($irc, $who => "You're a tool, $tool!" );
		}
	}

	elsif (/^tld\s+(.*)/i) {
		do_tld($who, $nick, $1);
	}

	elsif (/^toggle(?:\s+(\S+)(\s+#\S+)?)?\s*$/i) {
		my $what = $1;
		my $c = $2;
		if (!$what || ($what eq "show")) {
			if ($c) {
				$c =~ s/^\s+//;
				show_toggles($who, $c);
			} else {
				show_toggles($who, $channel);
			}
		} elsif ($what eq "possible") {
			emit($irc, $who, "I can toggle the following:");
			emit($irc, $who, join(" ", @toggleables));
		} else {
			if (defined($CHANNELS{$channel}{"toggles"}{$what})) {
				$CHANNELS{$channel}{"toggles"}{$what} = $CHANNELS{$channel}{"toggles"}{$what} ? 0 : 1 ;
				emit($irc, $who, "$what set to " . $CHANNELS{$channel}{"toggles"}{$what});
				if ($what eq "chatter") {
					foreach my $t (@chatter_toggles) {
						$CHANNELS{$channel}{"toggles"}{$t} = $CHANNELS{$channel}{"toggles"}{"chatter"} ? 0 : 1 ;
					}
				}
			} elsif (grep(/^$what$/, @toggleables)) {
				$CHANNELS{$channel}{"toggles"}{$what} = 1;
			}
		}
	}

	elsif (/^top5(g|y(?:e[ln]|sp|dvds|boxoffice|terror|odd|oped|[behorstpvw])?)?(\s+.*)?$/) {
		my $cmd = $1;
		my $word = $2;

		do_top5($who, $cmd, $word);
	}

	elsif (/^top10 bender/) {
		do_top5($who, "", "bender");
		emit($irc, $who, "6. Pimpmobile");
		emit($irc, $who, "7. Up");
		emit($irc, $who, "8. Yours");
		emit($irc, $who, "9. Chumpette");
		emit($irc, $who, "10. Chump");
	}

	elsif (/^traffic\s+(.*)/) {
		do_traffic($who, $nick, $1);
	}

	elsif (/^trivia$/) {
		do_trivia($who);
	}

	elsif (/^twitter(\s+.*)?/) {
		do_twitter($who, $1);
	}

	elsif (/^tz\s+([a-z0-9_\/]+)\s*([a-z0-9_\/]+)?/i) {
		do_tz($who, $1, $2);
	}

	elsif (/^ud\s+(.*)/) {
		if ($CHANNELS{$channel}{"toggles"}{"nc17"}) {
			do_ud($who, $1);
		} else {
			emit($irc, $who, $methods{"ud"} . $1);
		}
	}

	elsif (/^uwotd\s*$/) {
		fetch_rss_feed("uwotd", $who, 1);
	}

	elsif (/^uptimes*$/) {
		emit($irc, $who, "I've been up since: " .
			strftime("%a %b %e %H:%M:%S %Z %Y", localtime($^T)));
		emit($irc, $who, "That's " . get_uptime());
	}

	elsif (/^usertime\s+([a-z0-9_]+)/) {
		do_usertime($who, $1);
	}

	elsif (/^validate\s+(.*)/) {
		do_validate($who, $1);
	}

	elsif (/^vu\s+(#?[0-9-]+)/i) {
		do_vu($who, $1);
	}

	elsif (/^ninja/i) {
		emit($irc, $who => $ninja[int(rand(scalar(@ninja)))] );
	}

	elsif (/^pirate/i) {
		emit($irc, $who => $pirate[int(rand(scalar(@pirate)))] );
	}

	elsif (/^week(\s+[0-9-]+)?$/i) {
		do_week($who, $1);
	}

	elsif (/^whois\s+(.*)/) {
		do_whois($who, $1);
	}

	elsif (/^wiki\s+(.*)/) {
		do_wiki($who, $1);
	}

	elsif (/^wolfram\s+(.*)/i) {
		do_wolfram($who, $1);
	}

	elsif(/^wotd$/) {
		do_wotd($who);
	}

	elsif(/^woot$/) {
		do_woot($who);
	}

	elsif (/^wwipind\s+(.*)/) {
		do_wwipind($who, $1);
	}

	elsif (/^y?wtf\s+(is )*([a-zA-Z0-9._-]+)/) {
		my $term = lc($2);
		if (/$botnick/) {
			emit($irc, $who, "Unfortunately, no one can be told what $botnick is...");
			emit($irc, $who, "You have to see it for yourself.");
		} elsif ($term =~ /(grogglefroth|jfesler)/) {
			emit($irc, $who, "http://grogglefroth.com/");
		} else {
			do_shell("ywtf $term", $who, "", 5);
		}
	}

	elsif (/^ybuzz(\s+movers)?/i) {
		do_ybuzz($who, $1);
	}

	elsif (/^yelp\s+(.*)$/i) {
		do_yelp($who, $nick, $1);
	}

	elsif (/^y\s+(.*)/i) {
		do_y($who, $1);
	}

	else {
		foreach my $k (keys %rssfeeds) {
			if ($msg =~ m/^($k)(\s*)?(\d|count)?$/i) {
				if (($k =~ m/(sa-t[12]-)?unowned/) || ($3 && ($3 eq "count"))) {
					$nick = $who;
				}
				fetch_rss_feed($k, $nick, $3);
			}
		}
	}
}


# function : do_help
# purpose  : print help (for a given command)
# inputs   : who to respond to, nickname, command
# outputs  : help

sub do_help($$$)
{
	my ($who, $nick, $cmd) = @_;

	my %help = (
			"8ball" =>	"!8ball question    -- ask the magic 8-ball",
			"area"  =>	"!area <num|place>  -- display phone area code",
			"ahq"	=>	"!ahq <stock>       -- display after hours stock quote",
			"aotd"  =>	"!aotd              -- note the animal of the day",
			"asn"	=>	"!asn (<host>|<ip>|<asn>) -- display information about ASN",
			"az"	=>	"!az <word>         -- display first result from Amazon",
			"babel" =>	"!babel <from_to> <text> -- translate text from country code \"from\" to country code \"to\"",
			"beer"	=>	"!beer <beer>       -- rate a beer",
			"better" =>	"!better <foo> or <bar> -- show what's better",
			"bible" =>	"!bible <passage>   -- display a passage from the bible",
			"bing"  =>	"!bing <term>       -- display first result from bing.com",
			"brick" =>	"!brick <user>      -- doink a user in the head with a brick",
			"bofh"	=>	"!bofh              -- display a BOFH style excuse (for anything)",
			"bugmenot" =>	"!bugmenot <website> -- display login information for website",
			"cal"	=>	"!cal               -- invoke cal(1)",
			"calc"	=>	"!calc <input>      -- calculate input via bc(1)",
			"calendar" =>	"!calendar (<file>) -- display dates of interest (if any)",
			"channels" =>	"!channels           -- show what channels $botnick is in",
			"convert" =>	"!convert a to b    -- convert a to b",
			"countdown" =>	"!countdown <word>  -- show time remaining until event associated with <word>",
			"cowsay"=>	"!cowsay <msg>      -- cowsay your message",
			"curses" =>	"!curses (<user>)   -- check your curse-count",
			"cursebird" =>	"!cursebird (<user>) -- display twitter cursentages",
			"cve"	=>	"!cve <num>         -- display vulnerability description",
			"date"	=>	"!date              -- display current time and date",
			"define"=>	"!define <word>     -- provide a definition of 'word'",
			"digest" =>	"!digest <digest> <string> -- display the given digest for the given string",
			"errno" =>	"!errno <name|number> -- display matching errno values",
			"feature" =>	"!feature <desc>    -- request a new feature described in <desc>",
			"fileinfo" =>	"!fileinfo <ext> (desc|prog) -- display information about given file extention",
			"flight" =>	"!flight <airline> <number> -- request information about a given flight",
			"fml"	=>	"!fml               -- display a quote from www.fmylife.com",
			"fortune" =>	"!fortune           -- display a fortune",
			"futurama" =>	"!futurama          -- display a futurama quote",
			"g"	=>	"!g                 -- return first link of a google search",
			"gas"	=>	"!gas <zip>         -- display gas price by zip",
			"geo"	=>	"!geo <location|ip> -- display latitude and longitude for input",
			"help"	=>	"!help (<cmd>)      -- display help",
			"how"	=>	"!how <command>     -- show how we do <command>",
			"host"	=>	"!host <hostname>   -- look up <hostname>",
			"imdb"	=>	"!imdb <title>      -- display information about a movie title",
			"ip"	=>	"!ip <ip|cidr> (info)  -- display information about IP or CIDR",
			"ipv4"	=>	"!ipv4 (afrinic|apnic|arin|lacnic|ripe) -- display IPv4 allocation for given RIR",
			"$botnick" =>	"http://produce.yahoo.com/jans/jbot/",
			"like"	=>	"!like <somebody> <something> -- provide a guess as to whether somebody likes something or not",
			"man"	=>	"!man <command>     -- display summary and link for freebsd command",
			"monkeystab" => "!monkeystab <user> --  unleash a herd of pen wielding stabbing monkeys on the given user",
			"morse"	=>	"!morse <message>   -- turn message into morse code",
			"movies" =>	"!movies (soon)     -- display movies opening this week (or next)",
			"new"	=>	"!new (<user>)      -- show what's new (for <user> or $botnick)",
			"next"	=>	"!next <periodic> -- show when the next periodic is going to be executed.",
			"ninja" =>	"!ninja             -- show what a Ninja would do",
			"nts"	=>	"!nts <message>     -- note to self: send yourself a message",
			"nyt"	=>	"!nyt (<num>)       -- print NYT headlines",
			"ohiny"	=>	"!ohiny             -- print a random quote from overheardinny",
			"onion"	=>	"!onion (<num>)     -- print onion headlines",
			"oncall" =>	"!oncall (<pager>) (<YYYY-MM-DD>)  -- show who's oncall (for given rotation and date)",
			"perldoc" =>	"!perldoc <func>    -- display information about func via perldoc -f",
			"php"   =>	"!php <func>        -- display prototype and summary of given function",
			"ping"  =>	"!ping <hostname>   -- try to ping hostname",
			"pirate" =>	"!pirate            -- show what a Pirate would do",
			"pwgen" =>	"!pwgen ((-s) N)    -- generate a pasword",
			"primes" =>	"!primes min max    -- display primes in the given range",
			"pydoc"	=>	"!pydoc <func>      -- display documentation about given function",
			"quake" =>	"!quake (us)        -- display information about latest earthquake (in the US)",
			"quote"	=>	"!quote <symbol>    -- show stock price information",
			"q52"   =>      "!q52 <symbol>      -- show 52 week range quote",
			"rainbow" =>	"!rainbow <digest> <hash> -- (try to) reveal clear text for given digest hash",
			"random" =>	"!random <num>      -- print a random number between 0 and <num>",
			"rev"	=>	"!rev <msg>         -- reverse message",
			"rfc"	=>	"!rfc XXXX          -- display title and URL of given RFC",
			"rot13"	=>	"!rot13 <msg>       -- \"encrypt\" message",
			"rotd"  =>	"!rotd              -- give the recipe of the day",
			"rosetta" =>	"!rosetta <from> <to> <cmd> -- Rosetta Stone for Unix",
			"rq"	=>	"!rq <symbol>       -- display real-time quote",
			"rss"	=>	"!rss               -- display available rss feeds",
			"score" =>	"!score <words>     -- display sports scores",
			"seen"	=>	"!seen <user>       -- display last time I've seen user",
			"speb"	=>	"!speb              -- show a securty problem excuse bingo result",
			"service" =>	"!service <word>    -- lookup word in /etc/services",
			"signal" =>	"!signal <name|number> -- display matching signal values",
			"snopes" =>	"!snopes <myth>     -- display snopes urls about a myth",
			"stfu"  =>	"!stfu (<user>)     -- display channel chatterers",
			"symbol" =>	"!symbol <symbols>  -- lookup information about the stock symbol",
			"synonym"=>	"!synonym <word>    -- provide synonyms for given word",
			"sysexit" =>	"!sysexit <name|number> -- display matching sysexit values",
			"tld"	=>	"!tld <tld>         -- show what <tld> is",
			"time"	=>	"!time <location>   -- display time in location",
			"toggle" =>	"!toggle (<something>|possible|show) -- toggle a feature",
			"top5"	=>	"!top5[gy[e]] (<date>) -- display top5 search queries from Y!/Google",
			"top5g"	=>	"!top5g (<date>)    -- display top5 search queries from Google",
			"top5y"	=>	"!top5y (<date>)    -- display top5 search queries from Y!",
			"top5yb"=>	"!top5yb (links)    -- display top5 Y! business news",
			"top5ydvds"=>	"!top5ydvds         -- display top5 best selling DVDs from Y!",
			"top5yboxoffice"=>	"!top5yboxoffice -- display top5 weekend box office movies",
			"top5ye"=>	"!top5ye (links)    -- display top5 most emailed stories from Y!",
			"top5yel"=>	"!top5yel (links)    -- display top5 Y! election news",
			"top5yen"=>	"!top5yen (links)    -- display top5 Y! entertainment news",
			"top5yh"=>	"!top5yh (links)    -- display top5 Y! health news",
			"top5yr"=>	"!top5yr (links)    -- display top5 Y! highest rated news",
			"top5ys"=>	"!top5ys (links)    -- display top5 Y! science news",
			"top5ysp"=>	"!top5ysp (links)    -- display top5 Y! sports news",
			"top5yt"=>	"!top5yt (links)    -- display top5 Y! tech news",
			"top5yterror"=>	"!top5yterror (links)    -- display top5 Y! terrorism news",
			"top5yo"=>	"!top5yo (links)    -- display top5 Y! obituaries",
			"top5yodd"=>	"!top5yodd (links)    -- display top5 Y! odd stories",
			"top5yoped"=>	"!top5yoped (links)    -- display top5 Y! oped stories",
			"top5yp"=>	"!top5yp (links)    -- display top5 Y! politic news",
			"top5yv"=>	"!top5yv (links)    -- display top5 Y! most viewed stories",
			"top5yw"=>	"!top5yw (links)    -- display top5 Y! world news",
			"tool"	=>	"!tool              -- I'll make you a tool",
			"traffic"=>	"!traffic <route>   -- show traffic conditions from 511.org",
			"trivia"=>	"!trivia            -- show a random piece of trivia",
			"twitter" =>	"!twitter (<search>|=user( -N)) -- display latest twitter (for given search)",
			"tyblog" =>	"!tyblog (<user>)   -- display (a user's) latest blog entry title and link",
			"tz"	=>	"!tz <TZ> (<TZ>)    -- display date in that timezone",
			"ud"	=>	"!ud <word>         -- look up <word> in the Urban Dictionary",
			"uptime" =>	"!uptime            -- show since when I've been running",
			"uwotd" =>	"!uwotd             -- urban dictionary word of the day",
			"validate" =>	"!validate <uri>    -- validate given URI via validator.w3.org",
			"vu"	=>	"!vu <num> -- display summary of CERT Vulnerability",
			"week"	=>	"!week (<N|YYYY-MM-DD>) -- display the current week number, or the number of the given date / start date of the given week",
			"weather"=>	"!weather <loc> (tomorrow(+1))  -- give weather conditions for location",
			"whois" =>	"!whois <domain>    -- return some whois information",
			"wiki"	=>	"!wiki <word>       -- fetch the first paragraph from wikipedia",
			"wolfram" =>	"!wolfram <something> -- display results from Wolfram Alpha",
			"wotd"	=>	"!wotd              -- display the word of the day",
			"woot"	=>	"!woot              -- display woot of the day",
			"wtf"	=>	"!wtf <words>       -- decrypt acronyms",
			"wwipind" =>	"!wwipind <addr>    -- show what inet_ntop(inet_pton(addr)) would do",
			"y"	=>	"!y                 -- return first link of a yahoo search",
			"yelp"	=>	"!yelp <what> <where> -- search yelp; <what> should be quoted if multiple words",
			"zip"	=>	"!zip <loc>         -- show zip codes for given location",
	);

	if ($cmd && $cmd ne "all") {
		$cmd =~ s/^!//;
		if ($help{$cmd}) {
			emit($irc, $who, $help{$cmd});
		} else {
			if ($rssfeeds{$cmd}) {
				emit($irc, $who, "!$cmd fetches the '$cmd' RSS feed (" . $rssfeeds{$cmd} . ")");
				emit($irc, $who, "Can be called with an optional number indicating how many items to display.");
			} elsif ($cmd eq "throttle") {
				do_throttle($who, $nick, "help");
			} else {
				emit($irc, $who, "No such command: $cmd");
			}
		}
		return;
		# NOTREACHED
	}

	emit($irc, $nick, "I know of " . scalar(keys %help) . " commands:");
	my $words = "";
	foreach my $k (sort(keys %help)) {
		$words .= "$k ";
		if (length($words) > 60) {
			emit($irc, $nick, $words );
			$words = "";
		}
	}
	emit($irc, $nick, $words );
	emit($irc, $nick, "Ask me about one of them in specific: '!help <cmd>'");
	emit($irc, $nick, "If you find me annoyingly chatty, just '!toggle chatter'.");
	emit($irc, $nick, "If you'd like me to join a channel, just '/invite $botnick'.");
}


# function : main loop
# purpose  : the usual
# inputs   : none
# returns  : none

sub main() {

	if (-f $channels_file) {
		my $hr = retrieve($channels_file);
		if (!defined($hr)) {
			print STDERR "Unable to retrieve $channels_file: $!\n";
		} else {
			%CHANNELS = %{$hr};
		}
	}
	if (-f $curses_file) {
		my $hr = retrieve($curses_file);
		if (!defined($hr)) {
			print STDERR "Unable to retrieve $curses_file: $!\n";
		} else {
			%curses = %{$hr};
		}
	}
	if (-f $potty_file) {
		my $hr = retrieve($potty_file);
		if (!defined($hr)) {
			print STDERR "Unable to retrieve $potty_file: $!\n";
		} else {
			%pottymouths = %{$hr};
		}
	}
	if (-f $cmdr_file) {
		my $hr = retrieve($cmdr_file);
		if (!defined($hr)) {
			print STDERR "Unable to retrieve $cmdr_file: $!\n";
		} else {
			%cmdrs = %{$hr};
		}
	}
	if (-f $cmd_file) {
		my $hr = retrieve($cmd_file);
		if (!defined($hr)) {
			print STDERR "Unable to retrieve $cmd_file: $!\n";
		} else {
			%cmds = %{$hr};
		}
	}

	if (-f $rainbow_file) {
		my $hr = retrieve($rainbow_file);
		if (!defined($hr)) {
			print STDERR "Unable to retrieve $rainbow_file: $!\n";
		} else {
			%rainbow = %{$hr};
		}
	}

	$poe_kernel->run();

	exit 0;
}

main();
