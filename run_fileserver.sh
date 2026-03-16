#!/bin/bash

# 启动文件服务器
nohup ./fileserver -dir /root -port 3000 -password Welcome@202412 > fileserver.log 2>&1 &

# 打印服务启动信息
echo "File server is running in the background. Check fileserver.log for details."
