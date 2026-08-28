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

$Stub = @'
/* STD-T115 QPSK ナロー方式デコーダ用のスタブ。
 *
 * 3GP コンテナ読み書きは Windows の er-libisomedia.dll 側にしか実装が無い。
 * こちらは生ビットストリーム（-ff raw）だけを使うので、呼ばれたら明示的に
 * 失敗させる。信号処理には関与しない。
 */
#include <stdio.h>
#include <stdlib.h>
#include "include/amr_plus.h"

static void amrwbp_no_3gp(const char *fn)
{
   fprintf(stderr, "%s: 3GP container is not supported in this build; use -ff raw\n", fn);
   exit(EXIT_FAILURE);
}

int Create3GPAMRWBPlus(void) { amrwbp_no_3gp("Create3GPAMRWBPlus"); return 0; }
int Create3GPAMRWB(void) { amrwbp_no_3gp("Create3GPAMRWB"); return 0; }
int WriteSamplesAMRWBPlus(EncoderConfig conf, void *Serial, int length)
{
   (void) conf; (void) Serial; (void) length;
   amrwbp_no_3gp("WriteSamplesAMRWBPlus"); return 0;
}
int Close3GP(char *filename) { (void) filename; amrwbp_no_3gp("Close3GP"); return 0; }
int GetNextFrame3GP(short *tfi, int *bfi, short *extension, short *mode,
                    short *st_mode, short *fst, void *serial, int init)
{
   (void) tfi; (void) bfi; (void) extension; (void) mode;
   (void) st_mode; (void) fst; (void) serial; (void) init;
   amrwbp_no_3gp("GetNextFrame3GP"); return 0;
}
int Open3GP(short *tfi, int *bfi, char *filename, int verbose, DecoderConfig *conf)
{
   (void) tfi; (void) bfi; (void) filename; (void) verbose; (void) conf;
   amrwbp_no_3gp("Open3GP"); return 0;
}
'@

function Write-Step([string]$Message) { Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Note([string]$Message) { Write-Host "    $Message" }

function Resolve-Root([string]$ScriptDir) {
    $parent = Split-Path -Leaf $ScriptDir
    $grand = Split-Path -Leaf (Split-Path -Parent $ScriptDir)
    if ($parent -eq 'std-t115' -and $grand -eq 'scripts') {
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

function Invoke-AmrwbplusPatch([string]$Src) {
    $latin1 = [Text.Encoding]::GetEncoding(28591)
    $evaluator = [Text.RegularExpressions.MatchEvaluator] {
        param($m)
        '#include "' + $m.Groups[1].Value.Replace('\', '/') + '"'
    }
    $re = [regex]'#\s*include\s+"([^"]*)"'
    $changed = 0
    foreach ($f in Get-ChildItem -LiteralPath $Src -Include '*.c', '*.h' -Recurse -File) {
        $text = [IO.File]::ReadAllText($f.FullName, $latin1)
        $new = $re.Replace($text, $evaluator)
        if ($new -ne $text) {
            [IO.File]::WriteAllText($f.FullName, $new, $latin1)
            $changed++
        }
    }
    Write-Note "include のパス区切りを直したファイル: $changed"

    $encIf = Join-Path $Src 'lib_amr\enc_if.c'
    if (Test-Path -LiteralPath $encIf) {
        $text = [IO.File]::ReadAllText($encIf, $latin1)
        $fixed = $text -replace '(?m)^(const\s+Word16\s*\*\s*dhf\s*\[\s*10\s*\]\s*;[ \t]*\r?)$', 'extern $1'
        if ($fixed -ne $text) {
            [IO.File]::WriteAllText($encIf, $fixed, $latin1)
            Write-Note 'enc_if.c の dhf 仮定義を extern 宣言に直した'
        } else {
            Write-Note 'enc_if.c の dhf は宣言済み'
        }
    }

    $stub = $Stub -replace "`r`n", "`n"
    if (-not $stub.EndsWith("`n")) { $stub += "`n" }
    $utf8 = New-Object Text.UTF8Encoding($false)
    $path = Join-Path $Src 'stub3gp.c'
    $current = if (Test-Path -LiteralPath $path) { [IO.File]::ReadAllText($path, $utf8) } else { $null }
    if ($current -ne $stub) {
        [IO.File]::WriteAllText($path, $stub, $utf8)
        Write-Note '3GP スタブを書き出し: stub3gp.c'
    } else {
        Write-Note '3GP スタブは最新'
    }
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
    $work = Join-Path $root 'build\amrwbplus'
    $src = Join-Path $work 'src'
    $exe = Join-Path $work 'amrwbp_decoder.exe'
    $zip = Join-Path $work '26304-c00.zip'
    $url = 'https://www.3gpp.org/ftp/Specs/archive/26_series/26.304/26304-c00.zip'

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
        Write-Step "3GPP TS 26.304 (Rel-12) を取得: $url"
        Invoke-WebRequest -Uri $url -OutFile $zip -UserAgent 'Mozilla/5.0'
    }
    Write-Step ("参照ソース zip: {0} ({1:N0} bytes)" -f (Split-Path -Leaf $zip), (Get-Item -LiteralPath $zip).Length)

    if (-not (Test-Path -LiteralPath $src)) {
        Write-Step '展開'
        $tmp = Join-Path $work 'unzip'
        Expand-ZipFast -Zip $zip -Destination $tmp
        $inner = Get-ChildItem -LiteralPath $tmp -Recurse -File -Filter '*ANSI-C_source_code.zip' |
            Select-Object -First 1
        if (-not $inner) { throw 'ANSI-C ソースの zip が見つかりません' }
        Expand-ZipFast -Zip $inner.FullName -Destination (Join-Path $tmp 'code')
        $cdir = Get-ChildItem -LiteralPath (Join-Path $tmp 'code') -Recurse -Directory -Filter 'c-code' |
            Select-Object -First 1
        if (-not $cdir) { throw 'c-code ディレクトリが見つかりません' }
        New-Item -ItemType Directory -Force -Path $src | Out-Null
        Copy-Item -Path (Join-Path $cdir.FullName '*') -Destination $src -Recurse -Force
        Remove-Item -Recurse -Force $tmp
    }

    Write-Step 'パッチ'
    Invoke-AmrwbplusPatch $src

    Write-Step 'ビルド'
    $cflags = if ($env:CFLAGS) { $env:CFLAGS -split ' ' } else { @('-O2', '-w') }

    $sources = @()
    foreach ($d in @('common', 'decoder', 'lib_amr')) {
        Get-ChildItem -LiteralPath (Join-Path $src $d) -Filter '*.c' -File | ForEach-Object {
            if ($_.Name -ne '3gpp_mod.c') { $sources += $_.FullName }
        }
    }
    $sources += (Join-Path $src 'stub3gp.c')

    $obj = Join-Path $work 'obj'
    if (Test-Path -LiteralPath $obj) { Remove-Item -Recurse -Force $obj }
    New-Item -ItemType Directory -Force -Path $obj | Out-Null

    Push-Location $obj
    try {
        $ccArgs = @() + $cflags + @('-c') + $sources +
            @('-I', (Join-Path $src 'include'), '-I', (Join-Path $src 'lib_amr'))
        & $cc @ccArgs
        if ($LASTEXITCODE -ne 0) { throw 'コンパイルに失敗しました' }
    } finally {
        Pop-Location
    }

    $objs = (Get-ChildItem -LiteralPath $obj -Filter '*.o' -File).FullName
    $linkArgs = @() + $cflags + $objs + @('-lm', '-o', $exe)
    & $cc @linkArgs
    if ($LASTEXITCODE -ne 0) { throw 'リンクに失敗しました' }

    Write-Step "完成: $exe"
    & $exe 2>&1 | Select-Object -First 3 | ForEach-Object { Write-Note $_ }
    Write-Host ''
    Write-Host "std-t115-qpsknarrow.exe を $root に置いてあれば、このまま音声が鳴ります。" -ForegroundColor Green
    Write-Host "別の場所で使うなら build\amrwbplus\ を実行ファイルの隣へコピーしてください。"
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
