#! /bin/bash

# 服务器更新部署脚本

# 拉取最新代码
git pull origin main

# 停服务
sudo systemctl stop family-location.service

# 编译
make build && chmod 0755 bin bin/family-location

# 启动服务
sudo systemctl start family-location.service

