# 构建阶段
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /bin/kiln-api .

# 运行阶段
FROM alpine:3.21
RUN adduser -D -u 10001 kiln && mkdir -p /data && chown kiln /data
USER kiln
COPY --from=build /bin/kiln-api /bin/kiln-api
ENV ADDR=:8080 \
    DB_PATH=/data/kiln.db
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/bin/kiln-api"]
