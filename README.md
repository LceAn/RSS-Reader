# 简述

实时展示rss订阅最新消息

## 特性

***在原有代码调整了以下功能***

* 增加钉钉通知
* 修改前端展示UI，参考(copy) [https://www.nodeseek.com/post-117926-1](https://www.nodeseek.com/post-117926-1) 这个大佬
* 根据系统配置自动切换黑夜白天
* 配置文件增加了自定义端口（没什么用，主要我本地开发80）

2023年7月28日，进行了界面改版和升级

## TODO

* [ ] 页面自定义提醒

## 预览

![](pc.png)

![](pc_night.png)

![](mobile.png)

# 配置文件

已提供 docker-compose 方式，可以一键完成安装启动运行 ***注意⚠：docker-compose.yml 中端口默认是9898***

部署前请先配置，配置都在 config.json 中修改，使用前请先去 config.json 中增加自己飞书机器人的webhook地址 或 Telegram 的 token 和 chat_id，注意⚠️ TG api 后面的地址不要改！ https://api.telegram.org/bot${token}/sendMessage ，也就是这个${token}保持原样别动

config.json 中的 refresh 单位为分钟，表示多少分钟请求一次所需的 rss 订阅源

TG机器人创建和权限赋予教程请看 https://www.telegramhcn.com/article/161.html

配置文件位于config.json，sources是RSS订阅链接，示例如下

```json
{
    "port": 8080,
    "values": [
        "https://linux.do/latest.rss",
        "https://rss.nodeseek.com",
        "https://hostloc.com/forum.php?mod=rss&fid=45&auth=389ec3vtQanmEuRoghE%2FpZPWnYCPmvwWgSa7RsfjbQ%2BJpA%2F6y6eHAx%2FKqtmPOg",
        "https://v2ex.com/feed/tab/tech.xml",
        "https://www.dalao.net/feed.htm"
    ],
    "refresh": 5,
    "autoUpdatePush": 7,
    "listHeight": 600,
    "webTitle": "Hello MJJ",
    "webDes": "MJJ station",
    "keywords": ["cc","cloudcone","rn","racknerd","咸鱼","4837","jpp","hk2p"],
    "notify": {
        "feishu": {
            "api": ""
        },
        "dingtalk": {
            "webhook": "",
            "sign": ""
        },
        "telegram": {
            "api": "https://api.telegram.org/bot${token}/sendMessage",
            "chat_id": "",
            "token": ""
        }
    },
    "archives": "archives.txt"
}
```

| 名称           | 说明                                                                                              |
| -------------- | ------------------------------------------------------------------------------------------------- |
| values         | rss订阅链接（必填）                                                                               |
| refresh        | rss订阅更新时间间隔，单位分钟（必填）                                                             |
| autoUpdatePush | 自动刷新间隔，默认为0，不开启。效果为前端每autoUpdatePush分钟自动更新页面信息，单位分钟（非必填） |

# 使用方式

## Docker部署

环境要求：Git、Docker、Docker-Compose

克隆项目

```bash
git clone https://github.com/okhanyu/rss-reader
```

进入rss-reader文件夹，运行项目

```bash
docker-compose up -d
```

国内服务器将Dockerfile中取消下面注释使用 go mod 镜像

```dockerfile
#RUN go env -w GO111MODULE=on && \
#    go env -w GOPROXY=https://goproxy.cn,direct
```

部署成功后，通过ip+端口号访问

# nginx反代

这里需要注意/ws，若不设置proxy_read_timeout参数，则默认1分钟断开。静态文件增加gzip可以大幅压缩网络传输数据

```conf
server {
    listen 443 ssl;
    server_name 域名;
    ssl_certificate  域名证书.cer;
    ssl_certificate_key 域名证书.key;
    gzip on;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript;
    location / {
        proxy_pass  http://localhost:9898;
    }
    location /ws {
        proxy_pass http://localhost:9898/ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "Upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 300s;
    }
}

server {
    listen 80;
    server_name 域名;
    rewrite ^(.*)$ https://$host$1 permanent;
}
```

