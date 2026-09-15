# 多阶段构建（design D6/D7）：前端产物 go:embed 进 server；镜像只含运行与停机核验所需二进制。
# 凭证一律运行期经环境注入，构建产物与镜像不含任何凭证。

FROM node:24-alpine AS webui
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend
ARG BUILD_REVISION=development
WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY backend/ ./backend/
COPY --from=webui /src/frontend/dist ./backend/internal/platform/webui/dist
RUN cd backend && CGO_ENABLED=0 go build -o /out/server ./cmd/server \
    && CGO_ENABLED=0 go build -o /out/avatar-manifest ./cmd/avatar-manifest \
    && CGO_ENABLED=0 go build -o /out/planning-media-manifest ./cmd/planning-media-manifest \
    && CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate \
    && CGO_ENABLED=0 go build -o /out/creativectl ./cmd/creativectl \
    && CGO_ENABLED=0 go build -ldflags "-X main.runtimeBuildRevision=${BUILD_REVISION}" -o /out/accountctl ./cmd/accountctl

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D app \
    && mkdir -p /var/lib/crm/avatars /var/lib/crm/planning-media \
    && chown -R app:app /var/lib/crm
USER app
COPY --from=backend /out/server /usr/local/bin/server
COPY --from=backend /out/avatar-manifest /usr/local/bin/avatar-manifest
COPY --from=backend /out/planning-media-manifest /usr/local/bin/planning-media-manifest
COPY --from=backend /out/migrate /usr/local/bin/migrate
COPY --from=backend /out/creativectl /usr/local/bin/creativectl
COPY --from=backend /out/accountctl /usr/local/bin/accountctl
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
