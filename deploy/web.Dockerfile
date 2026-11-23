# 控制台构建与托管（console 模块）：
# node 构建 Vite 产物 → nginx 托管静态文件并处理 SPA 路由回退。
# 本地开发用 `cd web && pnpm dev`（Vite 代理 /api 到 localhost:8080），不经本文件。

FROM node:24-alpine AS build
WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm build

FROM nginx:1.31-alpine
COPY deploy/nginx/web.conf /etc/nginx/conf.d/default.conf
COPY --from=build /web/dist /usr/share/nginx/html
