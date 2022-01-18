FROM golang:1.17 AS builder

ARG hugo_version=0.92.0
ARG hugo_dl_url=https://github.com/gohugoio/hugo/releases/download/v"$hugo_version"/hugo_extended_"$hugo_version"_Linux-64bit.tar.gz

WORKDIR /hugo
RUN wget -O hugo.tar.gz $hugo_dl_url && tar -xzf hugo.tar.gz && rm hugo.tar.gz

WORKDIR /app
COPY . .
RUN go build -o hugo-ci .

FROM golang:1.17

WORKDIR /app
COPY --from=builder /hugo/hugo /app
COPY --from=builder /app/hugo-ci /app

VOLUME /data
VOLUME /live
VOLUME /beta

ENV REPO_URL https://github.com/buterland-beckerhook/buterland-beckerhook.git
ENV LIVE_BRANCH main
ENV LIVE_BASE_URL https://buterland-beckerhook.de/
ENV BETA_BRANCH beta
ENV BETA_BASE_URL https://beta.buterland-beckerhook.de/
ENV LIVE_BUILD_CRON "15 2 * * *"
ENV BETA_BUILD_CRON "10 2 * * *"
ENV BIND_ADDRESS ":80"
ENV GITHUB_SEC_TOKEN ""
ENV MAIL_SMTP_SERVER ""
ENV MAIL_SMTP_PORT 587
ENV MAIL_SMTP_USERNAME ""
ENV MAIL_SMTP_PASSWORD ""
ENV MAIL_RECIPIENTS ""
ENV MAIL_SENDER ""
ENV MAIL_PUSH_SUCCESS true
ENV MAIL_CRON_SUCCESS false


EXPOSE 80

ENTRYPOINT ["/app/hugo-ci"]
