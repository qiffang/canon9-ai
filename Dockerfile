FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod ./
# COPY go.sum ./ # uncomment when external dependencies are added
# RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /engram9 ./cmd/engram9

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /engram9 /usr/local/bin/engram9

EXPOSE 9090
ENTRYPOINT ["engram9"]
CMD ["-addr", ":9090", "-data", "/data"]
