
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$root = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path))
$work = Join-Path $root 'build\amrwbplus'
$src  = Join-Path $work 'src'
$zip  = Join-Path $work '26304-c00.zip'
$url  = 'https://www.3gpp.org/ftp/Specs/archive/26_series/26.304/26304-c00.zip'

New-Item -ItemType Directory -Force -Path $work | Out-Null

if (-not (Test-Path $zip)) {
  Write-Host "==> 3GPP TS 26.304 (Rel-12) を取得: $url"
  Invoke-WebRequest -Uri $url -OutFile $zip -UserAgent 'Mozilla/5.0'
}
Write-Host ("==> zip: {0} bytes" -f (Get-Item $zip).Length)

if (-not (Test-Path $src)) {
  Write-Host '==> 展開'
  $tmp = Join-Path $work 'unzip'
  if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp }
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null
  Expand-Archive -Path $zip -DestinationPath $tmp -Force
  $inner = Get-ChildItem -Path $tmp -Recurse -Filter '*ANSI-C_source_code.zip' |
           Select-Object -First 1
  if (-not $inner) { throw 'ANSI-C ソースの zip が見つかりません' }
  Expand-Archive -Path $inner.FullName -DestinationPath (Join-Path $tmp 'code') -Force
  $cdir = Get-ChildItem -Path (Join-Path $tmp 'code') -Recurse -Directory -Filter 'c-code' |
          Select-Object -First 1
  if (-not $cdir) { throw 'c-code ディレクトリが見つかりません' }
  New-Item -ItemType Directory -Force -Path $src | Out-Null
  Copy-Item -Path (Join-Path $cdir.FullName '*') -Destination $src -Recurse -Force
  Remove-Item -Recurse -Force $tmp
}

Write-Host '==> パッチ'
$py = $null
foreach ($cand in @('python3', 'python', 'py')) {
  if (Get-Command $cand -ErrorAction SilentlyContinue) { $py = $cand; break }
}
if (-not $py) { throw 'Python 3 が見つかりません（patch_amrwbplus.py の実行に必要）' }
& $py (Join-Path $root 'scripts\std-t115\patch_amrwbplus.py') $src
if ($LASTEXITCODE -ne 0) { throw 'パッチに失敗しました' }

Write-Host '==> ビルド'
$cc = $env:CC
if (-not $cc) {
  foreach ($cand in @('gcc', 'clang', 'cc')) {
    if (Get-Command $cand -ErrorAction SilentlyContinue) { $cc = $cand; break }
  }
}
if (-not $cc) {
  throw 'gcc / clang が見つかりません。MinGW-w64（例: MSYS2 の mingw-w64-x86_64-gcc）を入れてください。MSVC の cl は非対応です。'
}
Write-Host "    コンパイラ: $cc"

$cflags = if ($env:CFLAGS) { $env:CFLAGS -split ' ' } else { @('-O2', '-w') }

$sources = @()
foreach ($d in @('common', 'decoder', 'lib_amr')) {
  Get-ChildItem -Path (Join-Path $src $d) -Filter '*.c' | ForEach-Object {
    if ($_.Name -ne '3gpp_mod.c') { $sources += $_.FullName }
  }
}
$sources += (Join-Path $src 'stub3gp.c')

$obj = Join-Path $work 'obj'
if (Test-Path $obj) { Remove-Item -Recurse -Force $obj }
New-Item -ItemType Directory -Force -Path $obj | Out-Null

Push-Location $obj
try {
  $args = @() + $cflags + @('-c') + $sources +
          @('-I', (Join-Path $src 'include'), '-I', (Join-Path $src 'lib_amr'))
  & $cc @args
  if ($LASTEXITCODE -ne 0) { throw 'コンパイルに失敗しました' }
} finally {
  Pop-Location
}

$objs = (Get-ChildItem -Path $obj -Filter '*.o').FullName
$exe = Join-Path $work 'amrwbp_decoder.exe'
$linkArgs = @() + $cflags + $objs + @('-lm', '-o', $exe)
& $cc @linkArgs
if ($LASTEXITCODE -ne 0) { throw 'リンクに失敗しました' }

Write-Host "==> 完成: $exe"
& $exe 2>&1 | Select-Object -First 3
