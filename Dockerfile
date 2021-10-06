# syntax = docker/dockerfile:experimental
FROM golang:1.16.2 as builder

ENV GO111MODULE on
ENV GOPRIVATE "bitbucket.org/xxxxxx"
WORKDIR /go/src/bitbucket.org/xxxxxx

COPY go.mod .

RUN git config --global url."git@bitbucket.org:".insteadOf "https://bitbucket.org/"
RUN mkdir /root/.ssh/ && touch /root/.ssh/known_hosts && ssh-keyscan -t rsa bitbucket.org >> /root/.ssh/known_hosts
RUN --mount=type=secret,id=ssh,target=/root/.ssh/id_rsa go mod download

COPY . .

RUN go build -o ui-backend-for-omotebako-site-controller ui-backend-for-omotebako-site-controller/app/

# Runtime Container
FROM alpine:3.12

RUN apk add --no-cache libc6-compat tzdata

COPY --from=builder /go/src/bitbucket.org/latonaio/ui-backend-for-omotebako-site-controller .

CMD ["./ui-backend-for-omotebako-site-controller"]

