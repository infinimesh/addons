FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/app ./mqttbridge

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/app /app
USER 65532:65532
ENTRYPOINT ["/app"]
