# chalklined 多阶段构建：静态编译，运行镜像只带二进制与 CA 证书。
# 基础镜像版本需 >= go.mod 的 go 指令（当前 1.26.0）。
FROM golang:1.26-alpine AS build
WORKDIR /src
# Go 模块代理（GOPROXY）：由 compose 从 .env 透传；留空 = Go 内置默认
# （https://proxy.golang.org,direct），国内网络可在 .env 设为 https://goproxy.cn,direct。
ARG GOPROXY=
ENV GOPROXY=${GOPROXY}
# 先拷贝依赖清单，利用层缓存加速重复构建。
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/yuylong/Chalkline/internal/server.Version=${VERSION}" \
    -o /out/chalklined ./cmd/chalklined

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -g 10001 chalk \
    && adduser -D -u 10001 -G chalk chalk
COPY --from=build /out/chalklined /usr/local/bin/chalklined
USER chalk
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/chalklined"]
