# 编译go代码
FROM golang:1.20-alpine as build

# 更换阿里云Ubuntu apt 源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 安装时区和GIT
ENV TZ=Asia/Shanghai
RUN apk --no-cache add tzdata && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && echo '$TZ' >  /etc/timezone

ENV GO111MODULE=on \
    GOOS=linux \
    GOARCH=amd64 \
    CGO_ENABLED=0 \
	GOPROXY="https://goproxy.cn,https://goproxy.io,direct"

WORKDIR /go/cache
ADD go.mod .
ADD go.sum .
RUN go mod download

WORKDIR /go/release
ADD . .
RUN go build -ldflags="-s -w" -o app ./

# 构建运行镜像
FROM alpine as prod

# 更换阿里云Ubuntu apt 源
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 更换时区
ENV TZ=Asia/Shanghai \
    DATA=${DATA:-"/data"}
RUN apk --no-cache add tzdata && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && echo '$TZ' >  /etc/timezone

WORKDIR /app/dist
WORKDIR /data
COPY --from=build /go/release/app /app/dist/app

ENTRYPOINT /app/dist/app
