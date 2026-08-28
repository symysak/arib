#Requires -Version 5.1
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$ProgressPreference = 'SilentlyContinue'

$RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$Src = Join-Path $RepoRoot 'third_party\T-REC-G.722.1-200505-I!!SOFT-ZST-E\Software\Fixed-200505-Rel.2.1'
$OutDir = Join-Path $RepoRoot 'build\g7221'
$Work = Join-Path ([IO.Path]::GetTempPath()) ("g7221_build_" + [Guid]::NewGuid().ToString('N'))

. (Join-Path $PSScriptRoot '..\lib\ensure_gcc.ps1')

try {
    Write-Host '[1/5] ITU-T G.722.1 参考実装を展開...'
    if (-not (Test-Path -LiteralPath $Src)) { throw "ERROR: G.722.1 ソースが見つかりません: $Src" }
    $CC = Resolve-CCompiler -ToolchainDir (Join-Path $RepoRoot 'build\toolchain')
    $B = Join-Path $Work 'build'
    Expand-ZipFast -Zip (Join-Path $Src 'common\stl-files.zip') -Destination $B
    foreach ($d in 'common', 'encode', 'decode') {
        Get-ChildItem -LiteralPath (Join-Path $Src $d) -File |
            Where-Object { $_.Extension -in '.c', '.h' } |
            Copy-Item -Destination $B
    }

    Write-Host '[2/5] パッチ適用（modern C 互換）...'
    & go -C $RepoRoot run ./scripts/std-t86/g7221patch normalize $B
    if ($LASTEXITCODE) { throw "normalize に失敗しました (exit $LASTEXITCODE)" }

    Write-Host '[3/5] ビルド（パッチ前の decode）...'
    $Libs = 'basop32.c', 'common.c', 'dct4_a.c', 'dct4_s.c', 'huff_tab.c', 'tables.c',
            'coef2sam.c', 'sam2coef.c', 'decoder.c', 'encoder.c', 'count.c'
    New-Item -ItemType Directory -Force $OutDir | Out-Null
    Write-Host "    compiler: $CC"
    Push-Location $B
    try {
        & $CC -O2 -w -o (Join-Path $OutDir 'g7221_decode.exe') decode.c @Libs
        if ($LASTEXITCODE) { throw "g7221_decode のビルドに失敗しました (exit $LASTEXITCODE)" }

        Write-Host '[4/5] S-Codec 適応分離パッチ（ARIB STD-T86 §5.6）→ g7221_sep_decode / g7221_encode...'
        & go -C $RepoRoot run ./scripts/std-t86/g7221patch scodec $B
        if ($LASTEXITCODE) { throw "g7221patch scodec に失敗しました (exit $LASTEXITCODE)" }
        & $CC -O2 -w -o (Join-Path $OutDir 'g7221_sep_decode.exe') decode.c @Libs
        if ($LASTEXITCODE) { throw "g7221_sep_decode のビルドに失敗しました (exit $LASTEXITCODE)" }
        & $CC -O2 -w -o (Join-Path $OutDir 'g7221_encode.exe') encode.c @Libs
        if ($LASTEXITCODE) { throw "g7221_encode のビルドに失敗しました (exit $LASTEXITCODE)" }
    }
    finally { Pop-Location }

    Write-Host "[5/5] 完了: $OutDir"
    Get-ChildItem $OutDir
}
finally {
    Remove-Item -Recurse -Force $Work -ErrorAction SilentlyContinue
}
