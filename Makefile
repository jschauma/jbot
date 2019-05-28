NAME=jbot

clean::
	@rm -fr ${NAME}

SOURCES= src/jbot.go		\
	src/chatter.go		\
	src/ct.go		\
	src/cve.go		\
	src/fonts.go		\
	src/opsgenie.go		\
	src/secheaders.go	\
	src/ssllabs.go

${NAME}: ${SOURCES}
	env GOOS=linux go build ${SOURCES}
