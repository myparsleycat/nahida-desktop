param(
  [Parameter(Mandatory = $true)][string]$Group,
  [int]$Throttle = 8,
  [string]$Work = (Join-Path $PSScriptRoot 'work\upload')
)

$ErrorActionPreference = 'Stop'
$root = Join-Path $Work $Group
$prefix = "nhd-public/desktop/character-$Group"
$wrangler = (Get-Command npx).Source

$files = Get-ChildItem -Path $root -Recurse -File
Write-Output "uploading $($files.Count) files from $root"

$results = $files | ForEach-Object -Parallel {
  $file = $_
  $relative = [System.IO.Path]::GetRelativePath($using:root, $file.FullName).Replace('\', '/')
  $key = "$($using:prefix)/$relative"
  $type = switch -Wildcard ($file.Name) {
    '*.json' { 'application/json' }
    '*.webp' { 'image/webp' }
    '*.glb.gz' { 'application/gzip' }
    default { 'application/octet-stream' }
  }
  $ok = $false
  $output = ''
  for ($attempt = 0; $attempt -lt 3 -and -not $ok; $attempt++) {
    $output = & npx --no-install wrangler r2 object put $key --file $file.FullName --content-type $type --remote 2>&1 | Out-String
    $ok = $LASTEXITCODE -eq 0
  }
  [pscustomobject]@{ Key = $key; Bytes = $file.Length; Ok = $ok; Output = if ($ok) { '' } else { $output.Trim() } }
} -ThrottleLimit $Throttle

$failed = $results | Where-Object { -not $_.Ok }
$total = ($results | Measure-Object Bytes -Sum).Sum
Write-Output "uploaded $($results.Count - $failed.Count)/$($results.Count) files, $([math]::Round($total / 1MB, 1)) MB"
if ($failed.Count -gt 0) {
  $failed | ForEach-Object { Write-Output ("FAILED " + $_.Key + " :: " + $_.Output) }
  exit 1
}
