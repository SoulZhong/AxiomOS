# AxiomOS 用量钩子（ADR 0030，Windows PowerShell 版）：Claude Code 每轮结束（Stop 钩子）时运行，读这次会话的记录文件，
# 把整个会话按模型的累计 token 报给 AxiomOS（POST /api/v1/me/usage）。服务端与上一次比较得出增量并归口，重复上报不会重复计。
# 令牌与地址从 Claude Code 自己的 MCP 配置（$HOME\.claude.json 里的 axiomos，用户级或项目级）读，不另存。
# 任何情况下都静默退出 0，绝不拖住 Claude Code。
$ErrorActionPreference = "SilentlyContinue"
try {
  $raw = [Console]::In.ReadToEnd()
  $hook = $raw | ConvertFrom-Json
  $path = [string]$hook.transcript_path
  $session = [string]$hook.session_id
  if (-not $session -and $path) { $session = [IO.Path]::GetFileNameWithoutExtension($path) }
  if (-not $path -or -not $session -or -not (Test-Path $path)) { exit 0 }
  $cwd = if ($hook.cwd) { [string]$hook.cwd } else { (Get-Location).Path }

  function Pick($srv) {
    if ($null -eq $srv) { return $null }
    $url = [string]$srv.url
    $auth = [string]$srv.headers.Authorization
    if ($url -and $auth -and $auth.ToLower().StartsWith("bearer ")) { return @{ url = $url; token = $auth.Substring(7).Trim() } }
    return $null
  }
  $cfg = $null
  $claudeJson = Join-Path $HOME ".claude.json"
  $data = $null
  if (Test-Path $claudeJson) { $data = Get-Content $claudeJson -Raw -Encoding UTF8 | ConvertFrom-Json }
  if ($data) { $cfg = Pick $data.mcpServers.axiomos }
  $d = [IO.Path]::GetFullPath($cwd)
  while (-not $cfg -and $d) {
    if ($data -and $data.projects) {
      $proj = $data.projects.PSObject.Properties[$d]
      if ($proj) { $cfg = Pick $proj.Value.mcpServers.axiomos }
    }
    if (-not $cfg) {
      $mcpFile = Join-Path $d ".mcp.json"
      if (Test-Path $mcpFile) { $cfg = Pick ((Get-Content $mcpFile -Raw -Encoding UTF8 | ConvertFrom-Json).mcpServers.axiomos) }
    }
    $parent = Split-Path $d -Parent
    if (-not $parent -or $parent -eq $d) { break }
    $d = $parent
  }
  if (-not $cfg) { exit 0 }

  # 按消息 id 去重（流式写入时同一条消息多行、usage 相同，取最大），按模型累计
  $perModel = @{}
  $seen = @{}
  foreach ($line in [IO.File]::ReadLines($path)) {
    if (-not $line.Trim()) { continue }
    $rec = $null
    try { $rec = $line | ConvertFrom-Json } catch { continue }
    if ($null -eq $rec -or $rec.type -ne "assistant") { continue }
    $msg = $rec.message
    if ($null -eq $msg -or $null -eq $msg.usage) { continue }
    $u = $msg.usage
    $model = if ($msg.model) { [string]$msg.model } else { "unknown" }
    $mid = if ($msg.id) { [string]$msg.id } elseif ($rec.uuid) { [string]$rec.uuid } else { "" }
    $vals = @([int64]($u.input_tokens + 0), [int64]($u.output_tokens + 0), [int64]($u.cache_read_input_tokens + 0), [int64]($u.cache_creation_input_tokens + 0))
    if (-not $perModel.ContainsKey($model)) { $perModel[$model] = @([int64]0, [int64]0, [int64]0, [int64]0) }
    if ($mid) {
      if ($seen.ContainsKey($mid)) {
        $prev = $seen[$mid]
        if ($prev.model -ne $model) { continue }
        for ($i = 0; $i -lt 4; $i++) {
          $m = [Math]::Max($prev.vals[$i], $vals[$i])
          $perModel[$model][$i] += ($m - $prev.vals[$i])
          $prev.vals[$i] = $m
        }
        continue
      }
      $seen[$mid] = @{ model = $model; vals = $vals }
    }
    for ($i = 0; $i -lt 4; $i++) { $perModel[$model][$i] += $vals[$i] }
  }
  if ($perModel.Count -eq 0) { exit 0 }
  $cum = @()
  foreach ($m in ($perModel.Keys | Sort-Object)) {
    $v = $perModel[$m]
    $cum += @{ model_id = $m; input_tokens = $v[0]; output_tokens = $v[1]; cache_read_tokens = $v[2]; cache_write_tokens = $v[3] }
  }
  $base = $cfg.url.TrimEnd("/")
  if ($base.EndsWith("/mcp")) { $base = $base.Substring(0, $base.Length - 4) }
  $body = @{ session = $session; client = "claude-code"; cumulative = $cum } | ConvertTo-Json -Depth 5 -Compress
  Invoke-RestMethod -Method Post -Uri "$base/api/v1/me/usage" -TimeoutSec 8 -ContentType "application/json; charset=utf-8" `
    -Headers @{ Authorization = "Bearer " + $cfg.token } -Body ([System.Text.Encoding]::UTF8.GetBytes($body)) | Out-Null
} catch { }
exit 0
