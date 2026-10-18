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

$Repo = 'https://github.com/symysak/mbelib-neo'
$Commit = 'c37a2e60acbdf987bd9511a1a3523d8fe7fe8506'

$Cli = @'
/* mbelib-neo を使った AMBE+ 4000/2400 デコーダ CLI。
   優先順の 80 ビット（10 バイト、MSB 先頭）を受け取り、8kHz int16 LE を毎フレーム flush して返す。 */
#include <stdio.h>
#include <string.h>
#include <mbelib-neo/mbelib.h>
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
  int verbose = argc > 1 && !strcmp (argv[1], "-v");
  mbe_ambe4000_decoder *dec = mbe_ambe4000DecoderAlloc ();
  if (dec == NULL) return 1;
  unsigned char in[10];
  char bits[80];
  short pcm[160];
  long n = 0;
  while (fread (in, 1, 10, stdin) == 10) {
    for (int i = 0; i < 80; i++) bits[i] = (char) ((in[i >> 3] >> (7 - (i & 7))) & 1);
    mbe_process_result res;
    if (mbe_processAmbe4000Data (pcm, &res, bits, dec) < 0) memset (pcm, 0, sizeof pcm);
    fwrite (pcm, sizeof (short), 160, stdout);
    fflush (stdout);            /* 毎フレーム流さないと呼び出し側が待ち続ける */
    if (verbose) fprintf (stderr, "frame %ld flags=0x%x\n", n, (unsigned) res.flags);
    n++;
  }
  mbe_ambe4000DecoderFree (dec);
  return 0;
}
'@

$Sources = @(
    'src\ambe\ambe3600x2400.c', 'src\ambe\ambe6400x4000.c', 'src\ambe\ambe4000_decode.c',
    'src\ambe\ambe3600x2400_enc.c', 'src\ambe\dstar_frame.c', 'src\ambe\ambe3600x2450.c',
    'src\ambe\ambe_common.c', 'src\ecc\ecc.c', 'src\ecc\ecc_const.c',
    'src\imbe\imbe7100x4400.c', 'src\imbe\imbe7200x4400.c', 'src\core\mbelib.c',
    'src\core\mbe_pcm_convert.c', 'src\core\mbe_adaptive.c', 'src\core\mbe_unvoiced_fft.c',
    'src\external\pffft\pffft.c', 'src\external\pffft\fftpack.c'
)

function Write-Step([string]$Message) { Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Note([string]$Message) { Write-Host "    $Message" }

function Resolve-Root([string]$ScriptDir) {
    $parent = Split-Path -Leaf $ScriptDir
    $grand = Split-Path -Leaf (Split-Path -Parent $ScriptDir)
    if ($parent -eq 'std-t79' -and $grand -eq 'scripts') {
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
        & $winget.Source install --id $id --exact --source winget `
            --accept-source-agreements --accept-package-agreements `
            --disable-interactivity 2>&1 | Write-Host
        $installed = ($LASTEXITCODE -eq 0)
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
    $work = Join-Path $root 'build\mbelib-neo'
    $src = Join-Path $work 'src'
    $inc = Join-Path $work 'include'
    $exe = Join-Path $work 'ambeplus_decode.exe'
    $zip = Join-Path $work ("mbelib-neo-{0}.zip" -f $Commit)
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
        Write-Step "mbelib-neo を取得: $url"
        Invoke-WebRequest -Uri $url -OutFile $zip -UserAgent 'arib-build'
    }

    if (-not (Test-Path -LiteralPath (Join-Path $src 'src\core\mbelib.c'))) {
        Write-Step '展開'
        $tmp = Join-Path $work 'unzip'
        Expand-ZipFast -Zip $zip -Destination $tmp
        $top = Get-ChildItem -LiteralPath $tmp -Recurse -File -Filter 'version.h.in' |
            Where-Object { (Split-Path -Leaf $_.DirectoryName) -eq 'mbelib-neo' } | Select-Object -First 1
        if (-not $top) { throw 'include\mbelib-neo\version.h.in が見つかりません（zip の中身が想定と違う）' }
        $topDir = Split-Path -Parent (Split-Path -Parent $top.DirectoryName)
        if (-not (Test-Path -LiteralPath (Join-Path $topDir 'src\core\mbelib.c'))) { throw "src\core\mbelib.c が見つかりません: $topDir" }
        if (Test-Path -LiteralPath $src) { Remove-Item -Recurse -Force $src }
        New-Item -ItemType Directory -Force -Path $src | Out-Null
        Copy-Item -Path (Join-Path $topDir '*') -Destination $src -Recurse -Force
        Remove-Item -Recurse -Force $tmp
    }

    Write-Step 'version.h を作る'
    $incDir = Join-Path $inc 'mbelib-neo'
    New-Item -ItemType Directory -Force -Path $incDir | Out-Null
    $tmpl = [IO.File]::ReadAllText((Join-Path $src 'include\mbelib-neo\version.h.in'))
    [IO.File]::WriteAllText((Join-Path $incDir 'version.h'), ($tmpl -replace '@[A-Z_]*@', '0.0.0'), (New-Object Text.UTF8Encoding($false)))

    Write-Step 'ビルド'
    $cliPath = Join-Path $work 'ambeplus_decode.c'
    [IO.File]::WriteAllText($cliPath, ($Cli + "`n"), (New-Object Text.UTF8Encoding($false)))
    $cflags = if ($env:CFLAGS) { $env:CFLAGS -split ' ' } else { @('-O2', '-std=gnu11') }
    $defs = @('-DMBE_BUILDING=1', '-DMBE_STATIC=1')
    $incs = @('-I', (Join-Path $src 'include'), '-I', $inc, '-I', $src,
        '-I', (Join-Path $src 'src\internal'), '-I', (Join-Path $src 'src\external\pffft'))
    $files = @($cliPath) + ($Sources | ForEach-Object { Join-Path $src $_ })
    $ccArgs = @() + $cflags + $defs + $incs + @('-o', $exe) + $files + @('-lm')
    & $cc @ccArgs
    if ($LASTEXITCODE -ne 0) { throw 'コンパイルに失敗しました' }

    Write-Step "完成: $exe"
    Write-Host ''
    Write-Host "std-t79.exe を $root に置いてあれば、AMBE+ 4000/2400 の網の音声が鳴ります（-codec auto / ambeplus）。" -ForegroundColor Green
    Write-Host "別の場所で使うなら build\mbelib-neo\ を実行ファイルの隣へコピーしてください。"
    Write-Host ''
    Write-Host 'AMBE+ 4000/2400 の復号器は soltia48 さんが mbelib-neo に足したものです。ありがとうございます。' -ForegroundColor Yellow
    Write-Host '元のリポジトリにスターをお願いします: https://github.com/soltia48/mbelib-neo' -ForegroundColor Yellow
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
