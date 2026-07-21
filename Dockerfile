ARG DISTROLESS_TAG=debug

FROM golang:1.26 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/komodo ./cmd/server

FROM gcr.io/distroless/base-debian12:${DISTROLESS_TAG}
COPY --from=build /bin/komodo /komodo
COPY --from=build /app/validation_rules.yaml /app/validation_rules.yaml
EXPOSE 7051 7052
ENTRYPOINT ["/komodo"]
