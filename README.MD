# 水群统计小助手

- 执行`make`命令打包，会构建`wechat-assistant`的docker镜像

## 部署

参数懒得写了，参照`docker-compose.yaml`吧

```yaml
version: '3'
services:
  wechat-assistant:
    image: wechat-assistant
    container_name: wechat-assistant
    restart: always
    volumes:
      - $(pwd)/data:/data
    environment:
      - DATA=/data
      - DB=mysql
      - SECRET=base64格式的secret，用于生成totp动态验证码
      - MYSQL_HOST=localhost
      - MYSQL_PORT=3306
      - MYSQL_DATABASE=assistant
      - MYSQL_PASSWORD=assistant
      - MYSQL_USERNAME=assistant
      - MYSQL_PARAMETERS=charset=utf8mb4&collation=utf8mb4_unicode_ci&parseTime=true&loc=Asia%2FShanghai
```

## 使用

1. 先把小助手加进目标群
2. 启动项目，扫码登录
3. 通过 `@小助手 命令`与小助手交互
   - `@小助手 龙王`：获取今日龙王
   - `@小助手 龙王`：获取前10名水王