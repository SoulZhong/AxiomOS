#!/usr/bin/env python3
# AxiomOS 用量钩子（ADR 0030）：Claude Code 每轮结束（Stop 钩子）时运行，读这次会话的记录文件，
# 把整个会话按模型的累计 token 报给 AxiomOS（POST /api/v1/me/usage）。服务端与上一次比较得出增量并归口，
# 所以重复上报不会重复计。令牌与地址从 Claude Code 自己的 MCP 配置（~/.claude.json 里的 axiomos）读，不另存。
# 任何情况下都静默退出 0，绝不拖住 Claude Code。
import json, os, sys, urllib.request

def main():
    try:
        hook = json.load(sys.stdin)
    except Exception:
        return
    path = hook.get("transcript_path") or ""
    session = hook.get("session_id") or os.path.basename(path).rsplit(".", 1)[0]
    if not path or not session or not os.path.isfile(path):
        return
    url, token = mcp_config(hook.get("cwd") or os.getcwd())
    if not url or not token:
        return
    per_model = {}
    seen = {}
    with open(path, "r", encoding="utf-8", errors="replace") as f:
        for line in f:
            try:
                rec = json.loads(line)
            except Exception:
                continue
            if rec.get("type") != "assistant":
                continue
            msg = rec.get("message") or {}
            usage = msg.get("usage") or {}
            if not usage:
                continue
            model = msg.get("model") or "unknown"
            mid = msg.get("id") or rec.get("uuid") or ""
            vals = (int(usage.get("input_tokens") or 0), int(usage.get("output_tokens") or 0),
                    int(usage.get("cache_read_input_tokens") or 0), int(usage.get("cache_creation_input_tokens") or 0))
            # 同一条消息在流式写入时会出现多行、带同样的 usage：按消息 id 只算一次（取最大）
            if mid:
                prev = seen.get(mid)
                if prev is not None:
                    if prev[0] != model:
                        continue
                    vals = tuple(max(a, b) for a, b in zip(prev[1], vals))
                    old = prev[1]
                    seen[mid] = (model, vals)
                    acc = per_model.setdefault(model, [0, 0, 0, 0])
                    for i in range(4):
                        acc[i] += vals[i] - old[i]
                    continue
                seen[mid] = (model, vals)
            acc = per_model.setdefault(model, [0, 0, 0, 0])
            for i in range(4):
                acc[i] += vals[i]
    if not per_model:
        return
    body = {"session": session, "client": "claude-code", "cumulative": [
        {"model_id": m, "input_tokens": v[0], "output_tokens": v[1], "cache_read_tokens": v[2], "cache_write_tokens": v[3]}
        for m, v in sorted(per_model.items())]}
    base = url.rstrip("/")
    if base.endswith("/mcp"):
        base = base[:-4]
    req = urllib.request.Request(base + "/api/v1/me/usage", data=json.dumps(body).encode("utf-8"), method="POST",
                                 headers={"Content-Type": "application/json", "Authorization": "Bearer " + token})
    try:
        urllib.request.urlopen(req, timeout=8).read()
    except Exception:
        pass

def mcp_config(cwd):
    """从 Claude Code 的配置里取 axiomos 的地址与令牌：用户级（~/.claude.json 的 mcpServers）、
    项目级（~/.claude.json 的 projects[目录]，从当前目录往上找）、项目里的 .mcp.json，先找到哪个用哪个。"""
    def pick(srv):
        srv = srv or {}
        url = srv.get("url") or ""
        auth = (srv.get("headers") or {}).get("Authorization") or ""
        if url and auth.lower().startswith("bearer "):
            return url, auth[7:].strip()
        return "", ""
    data = {}
    try:
        with open(os.path.expanduser("~/.claude.json"), "r", encoding="utf-8") as f:
            data = json.load(f) or {}
    except Exception:
        data = {}
    url, token = pick((data.get("mcpServers") or {}).get("axiomos"))
    if url:
        return url, token
    projects = data.get("projects") or {}
    d = os.path.abspath(cwd or os.getcwd())
    while True:
        url, token = pick(((projects.get(d) or {}).get("mcpServers") or {}).get("axiomos"))
        if url:
            return url, token
        try:
            with open(os.path.join(d, ".mcp.json"), "r", encoding="utf-8") as f:
                url, token = pick(((json.load(f) or {}).get("mcpServers") or {}).get("axiomos"))
            if url:
                return url, token
        except Exception:
            pass
        parent = os.path.dirname(d)
        if parent == d:
            break
        d = parent
    return "", ""

if __name__ == "__main__":
    try:
        main()
    except Exception:
        pass
    sys.exit(0)
