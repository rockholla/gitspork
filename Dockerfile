FROM alpine:3.24.1
COPY gitspork /usr/local/bin/gitspork
RUN apk update && apk upgrade --no-cache && apk add --no-cache bash git
ENTRYPOINT ["gitspork"]
