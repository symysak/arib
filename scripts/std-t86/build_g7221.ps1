#Requires -Version 5.1
$ErrorActionPreference = 'Stop'

$RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$Src = Join-Path $RepoRoot 'third_party\T-REC-G.722.1-200505-I!!SOFT-ZST-E\Software\Fixed-200505-Rel.2.1'
$OutDir = Join-Path $RepoRoot 'build\g7221'
$Work = Join-Path ([IO.Path]::GetTempPath()) ("g7221_build_" + [Guid]::NewGuid().ToString('N'))

$CC = $env:CC
if (-not $CC) {
    foreach ($c in 'gcc', 'clang', 'cc') {
        if (Get-Command $c -ErrorAction SilentlyContinue) { $CC = $c; break }
    }
}
if (-not $CC) {
    throw 'C コンパイラが見つかりません。MinGW-w64 の gcc/clang を PATH に入れるか $env:CC で指定してください。'
}

$Python = $null
foreach ($c in 'python', 'python3') {
    $cmd = Get-Command $c -ErrorAction SilentlyContinue
    if ($cmd -and $cmd.Source -notmatch 'WindowsApps') { $Python = @($cmd.Source); break }
}
if (-not $Python -and (Get-Command py -ErrorAction SilentlyContinue)) { $Python = @('py', '-3') }
if (-not $Python -and (Get-Command uv -ErrorAction SilentlyContinue)) { $Python = @('uv', 'run', 'python') }
if (-not $Python) { throw 'Python 3 が見つかりません。' }

$Latin1 = [Text.Encoding]::GetEncoding('iso-8859-1')

try {
    Write-Host '[1/5] ITU-T G.722.1 参考実装を展開...'
    if (-not (Test-Path -LiteralPath $Src)) { throw "ERROR: G.722.1 ソースが見つかりません: $Src" }
    $B = Join-Path $Work 'build'
    New-Item -ItemType Directory -Force $B | Out-Null
    foreach ($d in 'common', 'encode', 'decode') {
        Get-ChildItem -LiteralPath (Join-Path $Src $d) -File |
            Where-Object { $_.Extension -in '.c', '.h' } |
            Copy-Item -Destination $B
    }
    Expand-Archive -LiteralPath (Join-Path $Src 'common\stl-files.zip') -DestinationPath $B -Force

    Write-Host '[2/5] パッチ適用（modern C 互換）...'
    foreach ($f in Get-ChildItem $B -File | Where-Object { $_.Extension -in '.c', '.h' }) {
        $t = [IO.File]::ReadAllText($f.FullName, $Latin1)
        $t = $t.Replace("`r`n", "`n")
        $t = $t -creplace '\bround\b', 'g722_round'
        if ($f.Extension -eq '.c') {
            $t = $t -creplace '(?m)^\s*main\s*\(\s*Word16\s+argc\s*,\s*char\s*\*argv\[\]\s*\)', 'int main(int argc,char *argv[])'
        }
        if ($f.Name -eq 'typedef.h') {
            $t = $t.Replace('defined(_MSC_VER)', 'defined(_MSC_VER) || defined(_WIN32)')
        }
        [IO.File]::WriteAllText($f.FullName, $t, $Latin1)
    }

    Write-Host '[3/5] ビルド...'
    $Libs = 'basop32.c', 'common.c', 'dct4_a.c', 'dct4_s.c', 'huff_tab.c', 'tables.c',
            'coef2sam.c', 'sam2coef.c', 'decoder.c', 'encoder.c', 'count.c'
    New-Item -ItemType Directory -Force $OutDir | Out-Null
    Write-Host "    compiler: $CC"
    Push-Location $B
    try {
        & $CC -O2 -w -o (Join-Path $OutDir 'g7221_encode.exe') encode.c @Libs
        if ($LASTEXITCODE) { throw "g7221_encode のビルドに失敗しました (exit $LASTEXITCODE)" }
        & $CC -O2 -w -o (Join-Path $OutDir 'g7221_decode.exe') decode.c @Libs
        if ($LASTEXITCODE) { throw "g7221_decode のビルドに失敗しました (exit $LASTEXITCODE)" }

        Write-Host '[4/5] S-Codec 適応分離パッチ（ARIB STD-T86 §5.6）→ g7221_sep_decode...'
        $PatchCmd = @($Python) + @((Join-Path $RepoRoot 'scripts\std-t86\patch_g7221_scodec.py'), $B)
        & $PatchCmd[0] $PatchCmd[1..($PatchCmd.Length - 1)]
        if ($LASTEXITCODE) { throw "patch_g7221_scodec.py が失敗しました (exit $LASTEXITCODE)" }
        & $CC -O2 -w -o (Join-Path $OutDir 'g7221_sep_decode.exe') decode.c @Libs
        if ($LASTEXITCODE) { throw "g7221_sep_decode のビルドに失敗しました (exit $LASTEXITCODE)" }
    }
    finally { Pop-Location }

    Write-Host "[5/5] 完了: $OutDir"
    Get-ChildItem $OutDir
}
finally {
    Remove-Item -Recurse -Force $Work -ErrorAction SilentlyContinue
}
