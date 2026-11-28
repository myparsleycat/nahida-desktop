$ErrorActionPreference = 'Stop'
$report = Get-Content (Join-Path $PSScriptRoot 'work\r2-orphans.json') -Raw | ConvertFrom-Json
$keys = @($report.motion) + @($report.original) + @($report.textures)
Write-Output "deleting $($keys.Count) unreferenced objects"

$ok = 0
$failed = @()
foreach ($key in $keys) {
  for ($attempt = 1; $attempt -le 3; $attempt++) {
    $out = & npx --no-install wrangler r2 object delete "nhd-public/$key" --remote 2>&1
    if ($LASTEXITCODE -eq 0) { $ok++; $out = $null; break }
    if ($attempt -eq 3) { $failed += "$key :: $($out -join ' ')" }
    Start-Sleep -Seconds 2
  }
}

Write-Output "deleted $ok/$($keys.Count)"
if ($failed.Count) {
  Write-Output "FAILED"
  $failed | ForEach-Object { Write-Output "  $_" }
  exit 1
}
