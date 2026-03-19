#!/bin/bash
cd /root/file-server

# 等待旧进程退出（最多 10 秒）
OLD_PID=$1
if [ -n "$OLD_PID" ]; then
  for i in $(seq 1 20); do
    kill -0 "$OLD_PID" 2>/dev/null || break
    sleep 0.5
  done
fi

# Build
export PATH=$PATH:/usr/local/go/bin
go build -o fileserver . >> fileserver.log 2>&1
if [ $? -ne 0 ]; then
  echo "$(date) [RESTART] Build failed!" >> fileserver.log
  exit 1
fi

echo "$(date) [RESTART] Build succeeded, starting server..." >> fileserver.log

# 启动（参数与 run_fileserver.sh 一致）
shift  # remove PID arg
nohup ./fileserver "$@" >> fileserver.log 2>&1 &

echo "$(date) [RESTART] Server started, PID=$!" >> fileserver.log
