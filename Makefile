NAME=jbot

clean::
	@rm -fr ${NAME}

SOURCES= \
        src/jbot.go             \
        src/afk.go           	\
        src/athere.go           \
        src/autoreply.go        \
        src/beer.go             \
        src/chatter.go          \
        src/ct.go               \
        src/cve.go              \
        src/delete.go           \
        src/doh.go              \
        src/flight.go           \
        src/fonts.go            \
        src/jira.go             \
        src/opsgenie.go         \
        src/secheaders.go       \
        src/ssllabs.go



${NAME}: ${SOURCES}
	go build ${SOURCES}
