# 阶段 1：编译
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/cs-proxy ./cmd/cs-proxy
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/cs ./cmd/cs

# 阶段 2：运行
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/cs-proxy /out/cs /usr/local/bin/
RUN mkdir -p /root/.claude_switch
EXPOSE 8787
ENTRYPOINT ["cs-proxy"]
