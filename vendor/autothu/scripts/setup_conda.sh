#!/usr/bin/env bash
# 创建并配置 AutoThu conda 环境
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v conda &>/dev/null; then
  echo "ERROR: 未找到 conda，请先安装 Anaconda/Miniconda"
  exit 1
fi

# shellcheck source=/dev/null
source "$(conda info --base)/etc/profile.d/conda.sh"

if conda env list | grep -q '^autothu '; then
  echo "环境 autothu 已存在，跳过 create"
else
  conda env create -f environment.yml
fi

conda activate autothu
pip install -U thulearn2018 pdfplumber pymupdf markdown requests gmssl cryptography selenium 2>/dev/null || true

mkdir -p "$HOME/.config/autothu"
if [[ ! -f "$HOME/.config/autothu/session.json" ]]; then
  python "$ROOT/scripts/thu_learn_client.py" 2>/dev/null || true
  cat > "$HOME/.config/autothu/session.json.example" <<'EOF'
{
  "username": "你的学号",
  "cookies": {
    "JSESSIONID": "登录后从浏览器复制",
    "XSRF-TOKEN": "登录后从浏览器复制"
  }
}
EOF
  echo "已写入 ~/.config/autothu/session.json.example"
fi

echo ""
echo "完成。下一步:"
echo "  conda activate autothu"
echo "  python scripts/verify_learn.py --doc-only"
echo "  浏览器登录 learn.tsinghua.edu.cn 后配置 ~/.config/autothu/session.json"
echo "  python scripts/verify_learn.py --session ~/.config/autothu/session.json"
