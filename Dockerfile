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

# creative-worker 镜像：docker build --target creative-worker。图片编辑用 libvips CLI 子进程
# （V6-076，后端设计 §3/§8），版本在此固定，升级须重跑样张与资源穿刺
# （docs/dev/creative-image-edit.md）；choom 在 exec vips 前调高子进程的 OOM 倾向（§10）。
# ffmpeg 提供 ffprobe：音视频上传在 worker 里校验，API 按它是否存在开放音视频格式，两个镜像都要装。
# worker 不嵌前端产物，单独构建，不依赖 webui 阶段。
FROM golang:1.25-alpine AS worker-build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY backend/ ./backend/
RUN cd backend && CGO_ENABLED=0 go build -o /out/creative-worker ./cmd/creative-worker

FROM alpine:3.22 AS creative-worker
RUN apk add --no-cache ca-certificates tzdata vips-tools=8.16.1-r0 util-linux-misc ffmpeg=6.1.2-r2 \
    && adduser -D app
USER app
COPY --from=worker-build /out/creative-worker /usr/local/bin/creative-worker
ENTRYPOINT ["/usr/local/bin/creative-worker"]

# 默认目标（最后一个阶段）仍是 API server 镜像。
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata ffmpeg=6.1.2-r2 \
    && adduser -D app \
    && mkdir -p /var/lib/crm/avatars /var/lib/crm/planning-media \
    && chown -R app:app /var/lib/crm
USER app
COPY --from=backend /out/server /usr/local/bin/server
COPY --from=backend /out/avatar-manifest /usr/local/bin/avatar-manifest
COPY --from=backend /out/planning-media-manifest /usr/local/bin/planning-media-manifest
COPY --from=backend /out/migrate /usr/local/bin/migrate
COPY --from=backend /out/creativectl /usr/local/bin/creativectl
# Built-in skill packages travel as data, not as bytes inside a binary. The
# server syncs this directory into the platform catalog on every start, so the
# image has to actually carry it.
COPY deploy/creative-skills /usr/local/share/creative-skills
ENV CREATIVE_SKILL_PACKAGES_DIR=/usr/local/share/creative-skills
COPY --from=backend /out/accountctl /usr/local/bin/accountctl
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
