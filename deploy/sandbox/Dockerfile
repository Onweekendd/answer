# 买不买 Agent 的工作台镜像（CubeSandbox 模板）。
#
# 改了以后这样更新模板（在装了 Cube 的机器上，本地是 WSL 的 Ubuntu-24.04）：
#   docker build -t answer-sandbox:v3 deploy/sandbox
#   docker tag answer-sandbox:v3 127.0.0.1:5000/answer-sandbox:v3
#   docker push 127.0.0.1:5000/answer-sandbox:v3      # Cube 只从镜像仓库拉，不读本机 docker；本地起的是 registry:2（answer-registry）
#   cubemastercli tpl create-from-image --image 127.0.0.1:5000/answer-sandbox:v3 --writable-layer-size 4G \
#     --expose-port 49983 --expose-port 49999 --probe 49983 --probe-path /health
# 拿到的 tpl-… 填进 CUBE_TEMPLATE_ID。2026-10-21 建的：v1 tpl-c9d6c8041d644d4684b55010，v2（加了 shot）tpl-b25625065f3b426990ede91e；2026-10-22 v3（shot 不靠 NODE_PATH）tpl-3dca5b85eb684c6ab662d534。
#
# 在 sandbox-code（Debian 12 + Python 3.12 + envd，启动脚本原样沿用）上加 node、Chromium、
# 中日韩字体和常用的 Python 包，让 Agent 不用每次现装。装包都走国内镜像：WSL 里的 docker 不走代理
FROM cube-sandbox-cn.tencentcloudcr.com/cube-sandbox/sandbox-code:latest

ARG NODE_VERSION=22.11.0

RUN sed -i 's#http://deb.debian.org#http://mirrors.aliyun.com#g' /etc/apt/sources.list.d/debian.sources \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      chromium fonts-noto-cjk fonts-noto-color-emoji fonts-liberation \
      xz-utils git jq unzip zip imagemagick ffmpeg \
 && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://registry.npmmirror.com/-/binary/node/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
      | tar -xJ -C /usr/local --strip-components=1 \
 && npm config set registry https://registry.npmmirror.com --location=global \
 && npm install -g pnpm playwright-core@1.48.2 \
 && npm cache clean --force

RUN pip config set global.index-url https://mirrors.aliyun.com/pypi/simple/ \
 && pip install --no-cache-dir --retries 10 --timeout 60 pillow numpy pandas matplotlib openpyxl python-pptx

# node 的 playwright-core 用系统的 chromium，不再下载自己的浏览器
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
    CHROME_PATH=/usr/bin/chromium \
    NODE_PATH=/usr/local/lib/node_modules

# shot 页面.html 输出.png：整页截图，Agent 做清单、行程卡用（说明在文件开头）
COPY shot /usr/local/bin/shot

RUN chmod +x /usr/local/bin/shot \
 && fc-cache -f >/dev/null \
 && node -v && npm -v && chromium --version && python3 -c "import PIL, pandas, pptx; print('py ok')"
