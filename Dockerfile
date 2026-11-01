# 钱迹 CLI 的 MCP server 容器镜像；配置与缓存持久化到挂载卷 /data。
#   docker build -t qianji-mcp .
#   # 先起 server（未登录也不崩）：
#   docker run -d --name qianji -p 8787:8787 -v qianji-data:/data -e QIANJI_MCP_PATH=密钥 qianji-mcp
#   # 再登录（写入卷，下次调用即生效）：
#   docker exec -it -e QIANJI_PASSWORD=密码 qianji qianji login --account 账号
# MCP 端点： POST http://<host>:8787/mcp/<QIANJI_MCP_PATH>
# 备选：不挂卷，直接 -e QIANJI_UID=<uid> -e QIANJI_TOKEN=<token> 注入登录态。

# --- build ---
FROM golang:1.26 AS build
WORKDIR /src
COPY cli/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /qianji .

# --- runtime (alpine: 体积小且带 shell，便于 docker exec 进容器 login / 排查) ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /qianji /usr/local/bin/qianji
# 配置 + 账单/分类缓存都落在 /data，挂卷即持久化
ENV QIANJI_CONFIG_DIR=/data
VOLUME ["/data"]
EXPOSE 8787
# entrypoint 拆开，便于 `docker run ... login` 或 `docker exec -it <容器> sh` 进去
ENTRYPOINT ["qianji"]
CMD ["mcp", "--addr", ":8787"]
