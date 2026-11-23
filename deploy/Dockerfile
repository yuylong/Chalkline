# chalklined 多阶段构建：静态编译，运行镜像只带二进制与 CA 证书。
# 基础镜像可由 compose 从 .env 透传覆盖（如阿里云等仅支持 latest 或需走镜像源的环境）。
# 注意：供 FROM 插值的 ARG 必须声明在首个 FROM 之前（全局作用域）；
# 阶段内声明的 ARG 对后续 FROM 不可见，会导致 "base name should not be blank"。
# 构建镜像版本需 >= go.mod 的 go 指令（当前 1.26.0）。
ARG GO_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=alpine:3.23
FROM ${GO_IMAGE} AS build
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

FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -g 10001 chalk \
    && adduser -D -u 10001 -G chalk chalk
COPY --from=build /out/chalklined /usr/local/bin/chalklined
USER chalk
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/chalklined"]
