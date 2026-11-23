# 控制台构建与托管（console 模块）：
# node 构建 Vite 产物 → nginx 托管静态文件并处理 SPA 路由回退。
# 本地开发用 `cd web && pnpm dev`（Vite 代理 /api 到 localhost:8080），不经本文件。

# 基础镜像可由 compose 从 .env 透传覆盖（如阿里云等仅支持 latest 或需走镜像源的环境）。
ARG NODE_IMAGE=node:24-alpine
FROM ${NODE_IMAGE} AS build
WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm build

ARG NGINX_IMAGE=nginx:1.31-alpine
FROM ${NGINX_IMAGE}
COPY deploy/nginx/web.conf /etc/nginx/conf.d/default.conf
COPY --from=build /web/dist /usr/share/nginx/html
