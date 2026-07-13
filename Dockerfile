# 多阶段构建（design D6/D7）：前端产物 go:embed 进 server；镜像只含运行与停机核验所需二进制。
# 凭证一律运行期经环境注入，构建产物与镜像不含任何凭证。

FROM node:24-alpine AS webui
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY backend/ ./backend/
COPY --from=webui /src/frontend/dist ./backend/internal/platform/webui/dist
RUN cd backend && CGO_ENABLED=0 go build -o /out/server ./cmd/server \
    && CGO_ENABLED=0 go build -o /out/avatar-manifest ./cmd/avatar-manifest

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D app \
    && mkdir -p /var/lib/crm/avatars \
    && chown -R app:app /var/lib/crm
USER app
COPY --from=backend /out/server /usr/local/bin/server
COPY --from=backend /out/avatar-manifest /usr/local/bin/avatar-manifest
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
