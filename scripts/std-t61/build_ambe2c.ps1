#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$OutDir,
    [string]$SourceZip,
    [switch]$Force,
    [switch]$NoToolchainInstall,
    [switch]$NoPause
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$ProgressPreference = 'SilentlyContinue'
try {
    [Net.ServicePointManager]::SecurityProtocol =
        [Net.SecurityProtocolType]::Tls12 -bor [Net.SecurityProtocolType]::Tls11
} catch { }

$Repo = 'https://github.com/symysak/ambe2-c'
$Commit = '70e32cbfc1cf5a08f3e9a3fbe41f6a9873cc4ca6'

$Cli = @'
/* ambe2-c を使った AMBE 系デコーダ CLI。引数でレートを選ぶ（AMBE-3000R の RATET）:
     0   … AMBE+ 2400 bps、誤り訂正なし（48 ビット = 6 バイト）
     22  … AMBE+ 2400 bps + 誤り訂正 1600 bps（80 ビット = 10 バイト）
     17  … AMBE+ 4000 bps、誤り訂正なし（80 ビット = 10 バイト。インタリーブ済みのチャネルの並び）
     26  … AMBE+ 4000 bps + 誤り訂正 2400 bps（128 ビット = 16 バイト）
     31  … AMBE+ 2000 bps（40 ビット = 5 バイト）
     33  … AMBE+2 3600x2450（72 ビット = 9 バイト。誤り訂正込み、AMBE-3000R のチャネルの並び）
     34  … AMBE+2 2450 bps、誤り訂正なし（49 ビット = 7 バイト。AMBE-3000R / ThumbDV の並び）
   印 1 バイト + フレーム（MSB 先頭）を受け取り、8kHz int16 LE を毎フレーム flush して返す。印が 1 なら消失。 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "blip25.h"
#ifdef _WIN32
#include <fcntl.h>
#include <io.h>
#endif

int main (int argc, char **argv)
{
#ifdef _WIN32
  /* 標準入出力はバイナリ。テキストのままだと PCM の 0x0A が 0x0D 0x0A に、入力の 0x1A が EOF になる */
  _setmode (_fileno (stdin), _O_BINARY);
  _setmode (_fileno (stdout), _O_BINARY);
#endif
  const char *arg = argc > 1 ? argv[1] : "31";
  int rate = atoi (arg);
  size_t nb = rate == 0 && strcmp (arg, "0") == 0 ? 6 : rate == 22 || rate == 17 ? 10 : rate == 26 ? 16
            : rate == 31 ? 5 : rate == 33 ? 9 : rate == 34 ? 7 : 0;
  if (nb == 0) {
    fprintf (stderr, "usage: ambe2c_decode 0|22|17|26|31|33|34\n");
    return 2;
  }
  static blip25_decoder dec;
  unsigned char in[1 + 16];
  int16_t pcm[160];
  int errs;
  blip25_init (&dec);
  while (fread (in, 1, 1 + nb, stdin) == 1 + nb) {
    if (in[0] == 1) blip25_decode_lost (&dec, pcm);
    else if (rate == 0) blip25_decode_ambeplus_r0 (&dec, in + 1, pcm);
    else if (rate == 22) blip25_decode_ambeplus_r22 (&dec, in + 1, pcm, &errs);
    else if (rate == 17) blip25_decode_ambeplus_r17 (&dec, in + 1, pcm);
    else if (rate == 26) blip25_decode_ambeplus_r26 (&dec, in + 1, pcm, &errs);
    else if (rate == 33) blip25_decode_3600 (&dec, in + 1, pcm, &errs);
    else if (rate == 34) blip25_decode_2450_thumbdv (&dec, in + 1, pcm);
    else blip25_decode_ambeplus_r31 (&dec, in + 1, pcm);
    fwrite (pcm, sizeof (int16_t), 160, stdout);
    fflush (stdout);            /* 毎フレーム流さないと呼び出し側が待ち続ける */
  }
  return 0;
}
'@

$Sources = @(
    'src\b25_dsp_a.c', 'src\b25_dsp_b.c', 'src\b25_decode_c.c', 'src\b25_decode_d.c',
    'src\b25_tables.c', 'src\b25_golay.c', 'src\blip25.c', 'src\mbelib_compat.c',
    'src\ambeplus.c', 'src\ambeplus_tables.c', 'src\ambeplus_fec.c', 'src\ambeplus4000_tables.c',
    'src\ambeplus_layer2.c'
)

function Write-Step([string]$Message) { Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Note([string]$Message) { Write-Host "    $Message" }

function Resolve-Root([string]$ScriptDir) {
    $parent = Split-Path -Leaf $ScriptDir
    $grand = Split-Path -Leaf (Split-Path -Parent $ScriptDir)
    if ($parent -eq 'std-t61' -and $grand -eq 'scripts') {
        return (Split-Path -Parent (Split-Path -Parent $ScriptDir))
    }
    return $ScriptDir
}

$script:WinLibsRepo = 'brechtsanders/winlibs_mingw'
$script:WingetIds = @(
    'BrechtSanders.WinLibs.POSIX.UCRT',
    'BrechtSanders.WinLibs.POSIX.MSVCRT',
    'BrechtSanders.WinLibs.MCF.UCRT',
    'BrechtSanders.WinLibs.MCF.MSVCRT'
)

function Expand-ZipFast([string]$Zip, [string]$Destination) {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    if (Test-Path -LiteralPath $Destination) { Remove-Item -Recurse -Force $Destination }
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    [IO.Compression.ZipFile]::ExtractToDirectory(
        (Resolve-Path -LiteralPath $Zip).Path,
        (Resolve-Path -LiteralPath $Destination).Path)
}

function Sync-PathFromEnvironment {
    $parts = @()
    foreach ($scope in @('Machine', 'User')) {
        $v = [Environment]::GetEnvironmentVariable('Path', $scope)
        if ($v) { $parts += $v }
    }
    $links = Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Links'
    if (Test-Path -LiteralPath $links) { $parts += $links }
    if ($parts.Count -gt 0) { $env:Path = ($parts -join ';') }
}

function Find-CCompiler {
    if ($env:CC) {
        $cmd = Get-Command $env:CC -ErrorAction SilentlyContinue
        if ($cmd) { return $cmd.Source }
        if (Test-Path -LiteralPath $env:CC) { return (Resolve-Path -LiteralPath $env:CC).Path }
        throw "CC に指定されたコンパイラが見つかりません: $env:CC"
    }
    foreach ($name in @('gcc', 'clang')) {
        $cmd = Get-Command $name -ErrorAction SilentlyContinue
        if ($cmd) { return $cmd.Source }
    }
    return $null
}

function Find-GccUnderWinGet {
    $packages = Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Packages'
    if (-not (Test-Path -LiteralPath $packages)) { return $null }
    $dirs = Get-ChildItem -LiteralPath $packages -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -like '*WinLibs*' -or $_.Name -like '*mingw*' }
    foreach ($d in $dirs) {
        $hit = Get-ChildItem -LiteralPath $d.FullName -Filter 'gcc.exe' -Recurse -File -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($hit) { return $hit.FullName }
    }
    return $null
}

function Install-GccWithWinget {
    $cc = Find-GccUnderWinGet
    if ($cc) {
        Write-Host "    winget で入れた gcc がありました: $cc"
        return $cc
    }
    $winget = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $winget) {
        Write-Host '    winget がありません（Windows 10 1809 未満など）。ポータブル版へ切り替えます。'
        return $null
    }
    foreach ($id in $script:WingetIds) {
        Write-Host "    winget install $id"
        Write-Host '    （約 0.9GB を展開するので数分かかります。止まって見えても閉じずに待ってください）'
        $p = Start-Process -FilePath $winget.Source -NoNewWindow -Wait -PassThru -ArgumentList @(
            'install', '--id', $id, '--exact', '--source', 'winget',
            '--accept-source-agreements', '--accept-package-agreements',
            '--disable-interactivity')
        $installed = ($p.ExitCode -eq 0)
        Sync-PathFromEnvironment
        $cc = Find-CCompiler
        if (-not $cc) { $cc = Find-GccUnderWinGet }
        if ($cc) { return $cc }
        if ($installed) {
            Write-Host '    インストールは成功しましたが gcc.exe を見つけられませんでした。'
        }
    }
    return $null
}

function Install-GccPortable([string]$ToolchainDir) {
    $api = "https://api.github.com/repos/$script:WinLibsRepo/releases/latest"
    Write-Host "    WinLibs の最新 Release を照会: $api"
    $release = Invoke-RestMethod -Uri $api -UserAgent 'arib-build' -Headers @{ Accept = 'application/vnd.github+json' }
    $asset = $release.assets |
        Where-Object { $_.name -like '*.zip' } |
        Where-Object { $_.name -like '*x86_64*' } |
        Where-Object { $_.name -like '*posix*' } |
        Where-Object { $_.name -like '*ucrt*' } |
        Where-Object { $_.name -notlike '*llvm*' } |
        Sort-Object -Property size |
        Select-Object -First 1
    if (-not $asset) { throw "WinLibs の zip が Release に見つかりません（$api）" }

    New-Item -ItemType Directory -Force -Path $ToolchainDir | Out-Null
    $zip = Join-Path $ToolchainDir $asset.name
    if (-not (Test-Path -LiteralPath $zip)) {
        Write-Host ("    ダウンロード: {0} （{1:N0} MB）" -f $asset.name, ($asset.size / 1MB))
        Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zip -UserAgent 'arib-build'
    }
    $dest = Join-Path $ToolchainDir 'winlibs'
    Write-Host '    展開（数分かかることがあります）'
    Expand-ZipFast -Zip $zip -Destination $dest

    $hit = Get-ChildItem -LiteralPath $dest -Filter 'gcc.exe' -Recurse -File -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if (-not $hit) { throw "展開しましたが gcc.exe が見つかりません: $dest" }
    $env:Path = (Split-Path -Parent $hit.FullName) + ';' + $env:Path
    return $hit.FullName
}

function Resolve-CCompiler([string]$ToolchainDir, [switch]$NoInstall) {
    $cc = Find-CCompiler
    if ($cc) {
        Write-Host "    コンパイラ: $cc"
        return $cc
    }
    if ($NoInstall) {
        throw 'gcc / clang が見つかりません。MinGW-w64 を入れるか $env:CC で指定してください（MSVC の cl は非対応）。'
    }
    Write-Host '    gcc が無いので MinGW-w64（WinLibs）を入れます。'
    $cc = Install-GccWithWinget
    if (-not $cc) { $cc = Install-GccPortable $ToolchainDir }
    Write-Host "    用意しました: $cc"
    return $cc
}

$exitCode = 0
try {
    $scriptDir = Split-Path -Parent $PSCommandPath
    if ($OutDir) {
        New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
        $root = (Resolve-Path -LiteralPath $OutDir).Path
    } else {
        $root = Resolve-Root $scriptDir
    }
    $work = Join-Path $root 'build\ambe2c'
    $src = Join-Path $work 'src'
    $exe = Join-Path $work 'ambe2c_decode.exe'
    $zip = Join-Path $work ("ambe2-c-{0}.zip" -f $Commit)
    $url = "$Repo/archive/$Commit.zip"

    Write-Step "出力先: $work"
    if ($Force -and (Test-Path -LiteralPath $src)) { Remove-Item -Recurse -Force $src }
    New-Item -ItemType Directory -Force -Path $work | Out-Null

    Write-Step 'C コンパイラを用意'
    $cc = Resolve-CCompiler -ToolchainDir (Join-Path $root 'build\toolchain') -NoInstall:$NoToolchainInstall
    & $cc --version | Select-Object -First 1 | ForEach-Object { Write-Note $_ }

    if ($SourceZip) {
        if (-not (Test-Path -LiteralPath $SourceZip)) { throw "-SourceZip が見つかりません: $SourceZip" }
        $zip = (Resolve-Path -LiteralPath $SourceZip).Path
    } elseif (-not (Test-Path -LiteralPath $zip)) {
        Write-Step "ambe2-c を取得: $url"
        Invoke-WebRequest -Uri $url -OutFile $zip -UserAgent 'arib-build'
    }

    if (-not (Test-Path -LiteralPath (Join-Path $src 'decoder\src\blip25.c'))) {
        Write-Step '展開'
        $tmp = Join-Path $work 'unzip'
        Expand-ZipFast -Zip $zip -Destination $tmp
        $top = Get-ChildItem -LiteralPath $tmp -Recurse -File -Filter 'blip25.c' |
            Where-Object { (Split-Path -Leaf $_.DirectoryName) -eq 'src' } | Select-Object -First 1
        if (-not $top) { throw 'decoder\src\blip25.c が見つかりません（zip の中身が想定と違う）' }
        $topDir = Split-Path -Parent (Split-Path -Parent $top.DirectoryName)
        if (Test-Path -LiteralPath $src) { Remove-Item -Recurse -Force $src }
        New-Item -ItemType Directory -Force -Path $src | Out-Null
        Copy-Item -Path (Join-Path $topDir '*') -Destination $src -Recurse -Force
        Remove-Item -Recurse -Force $tmp
    }

    Write-Step 'ビルド'
    $cliPath = Join-Path $work 'ambe2c_decode.c'
    [IO.File]::WriteAllText($cliPath, ($Cli + "`n"), (New-Object Text.UTF8Encoding($false)))
    $dec = Join-Path $src 'decoder'
    $cflags = if ($env:CFLAGS) { $env:CFLAGS -split ' ' } else { @('-O2', '-std=c99') }
    $incs = @('-I', (Join-Path $dec 'include'), '-I', (Join-Path $dec 'src'))
    $files = @($cliPath) + ($Sources | ForEach-Object { Join-Path $dec $_ })
    $ccArgs = @() + $cflags + $incs + @('-o', $exe) + $files + @('-lm')
    & $cc @ccArgs
    if ($LASTEXITCODE -ne 0) { throw 'コンパイルに失敗しました' }

    Write-Step "完成: $exe"
    Write-Host ''
    Write-Host "実行ファイル（std-t61.exe・std-t79.exe・arib.exe など）を $root に置いてあれば、AMBE 系の音声が鳴ります。" -ForegroundColor Green
    Write-Host "別の場所で使うなら build\ambe2c\ を実行ファイルの隣へコピーしてください。"
} catch {
    $exitCode = 1
    Write-Host ''
    Write-Host "失敗: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host $_.ScriptStackTrace
} finally {
    if (-not $NoPause -and -not $env:CI -and $Host.Name -eq 'ConsoleHost') {
        Write-Host ''
        Read-Host '終了するには Enter'
    }
}
exit $exitCode
