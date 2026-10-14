FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /hookdesk .

FROM alpine:3.22
RUN addgroup -g 10001 hookdesk && adduser -D -u 10001 -G hookdesk hookdesk && mkdir /data && chown hookdesk:hookdesk /data
COPY --from=build /hookdesk /usr/local/bin/hookdesk
USER hookdesk
ENV PORT=8080 DB_PATH=/data/webhooks.db
EXPOSE 8080
VOLUME ["/data"]
CMD ["hookdesk"]
